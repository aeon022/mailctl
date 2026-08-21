package tui

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/mailctl/internal/models"
)

// TestProgramSmoke drives the real tea.Program end to end (the same wiring
// Run() uses, including the FPS cap and motion-throttle filter) — startup,
// a resize, a few keypresses, a mouse click and motion burst, then quit —
// checking only that it never panics and produces a non-empty final frame.
// This is diagnostic-only coverage for the v1→v2 migration itself (same
// pattern as notectl's own smoke test), not a replacement for a live human
// check of the actual rendered TUI.
func TestProgramSmoke(t *testing.T) {
	m := New()
	pr, pw := io.Pipe()
	defer pw.Close()
	var out safeBuf

	p := tea.NewProgram(m, tea.WithInput(pr), tea.WithOutput(&out),
		tea.WithFilter(motionThrottleFilter()), tea.WithFPS(30))

	done := make(chan struct{})
	go func() {
		_, _ = p.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	p.Send(tea.WindowSizeMsg{Width: 100, Height: 40})
	time.Sleep(20 * time.Millisecond)
	p.Send(tea.KeyPressMsg{Text: "j", Code: 'j'})
	p.Send(tea.KeyPressMsg{Text: "k", Code: 'k'})
	p.Send(tea.MouseClickMsg{Button: tea.MouseLeft, X: 5, Y: 5})
	for i := 0; i < 10; i++ {
		p.Send(tea.MouseMotionMsg{X: i, Y: 5})
	}
	time.Sleep(50 * time.Millisecond)
	p.Quit()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("program did not quit within 3s")
	}

	frame := out.String()
	if !strings.Contains(frame, "mailctl") {
		t.Errorf("final frame missing header text, got:\n%s", frame)
	}
}

// TestProgramSmoke_DetailScrollHeavy drives the real tea.Program straight
// into the detail view with a long message body open, then hammers it with
// PgDn/j keys and mouse wheel events — the specific scenario the detail
// view's scrollbar was live-observed to render jagged/broken in under
// bubbletea v1 (a render-corruption bug class, not a Unicode-width bug;
// confirmed live even on plain German text with no special characters).
// This is the regression test for that bug class, same reasoning as
// notectl's own v2 migration: only fully fixed by moving off v1's
// uncapped-fps diff renderer + WithMouseAllMotion combination. Checks only
// that a scroll-heavy sequence through the real Program never panics and
// keeps producing a non-empty final frame — not a substitute for a live
// human check of the actual rendered scrollbar.
func TestProgramSmoke_DetailScrollHeavy(t *testing.T) {
	m := New()
	m.width, m.height = 100, 40

	var body strings.Builder
	for i := 0; i < 500; i++ {
		body.WriteString(strings.Repeat("Lörem ipsüm dölor sit ämet, äöüß ", 4))
		body.WriteString("\n")
	}
	msg := models.Message{
		Subject: "Long message for scroll-heavy smoke test",
		From:    "sender@example.com",
		Body:    body.String(),
	}
	m.detail = &msg
	m.view = viewDetail
	m.vp = viewport.New(viewport.WithWidth(m.detailRawWidth()-2), viewport.WithHeight(m.detailBodyHeight()))
	m.vp.SetContent(formatDetail(m.detail, m.detailRawWidth()))

	pr, pw := io.Pipe()
	defer pw.Close()
	var out safeBuf

	p := tea.NewProgram(m, tea.WithInput(pr), tea.WithOutput(&out),
		tea.WithFilter(motionThrottleFilter()), tea.WithFPS(30))

	done := make(chan struct{})
	go func() {
		_, _ = p.Run()
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	p.Send(tea.WindowSizeMsg{Width: 100, Height: 40})
	time.Sleep(20 * time.Millisecond)

	for i := 0; i < 60; i++ {
		p.Send(tea.KeyPressMsg{Text: "j", Code: 'j'})
	}
	for i := 0; i < 10; i++ {
		p.Send(tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	for i := 0; i < 20; i++ {
		p.Send(tea.MouseWheelMsg{Button: tea.MouseWheelDown, X: 10, Y: 10})
	}
	for i := 0; i < 10; i++ {
		p.Send(tea.KeyPressMsg{Text: "k", Code: 'k'})
	}
	for i := 0; i < 20; i++ {
		p.Send(tea.MouseWheelMsg{Button: tea.MouseWheelUp, X: 10, Y: 10})
	}
	time.Sleep(50 * time.Millisecond)
	p.Quit()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("program did not quit within 3s")
	}

	frame := out.String()
	if frame == "" {
		t.Error("expected a non-empty final frame after a scroll-heavy detail-view sequence")
	}
	if !strings.Contains(frame, "Long message for scroll-heavy smoke test") {
		t.Errorf("final frame missing the detail view's subject line, got:\n%s", frame)
	}
}

type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
