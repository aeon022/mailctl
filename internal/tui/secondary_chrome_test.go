package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/mailctl/internal/unsub"
	"github.com/charmbracelet/x/ansi"
)

// Every secondary view shares the chrome: "mailctl · <View>" header, divider,
// exactly m.height lines, one-line footer with an esc hint, nothing too wide.
func TestSecondaryViewsShareChrome(t *testing.T) {
	views := map[string]func(m Model) string{
		"detail": func(m Model) string {
			d := sample()[0]
			m.detail, m.view = &d, viewDetail
			return m.renderDetail()
		},
		"compose": func(m Model) string { m.resetCompose("", ""); m.view = viewCompose; return m.renderCompose() },
		"reply": func(m Model) string {
			d := sample()[0]
			m.replyTo = &d
			m.resetCompose("a@b.test", "Re: Invoice March")
			m.view = viewCompose
			return m.renderCompose()
		},
	}
	for name, render := range views {
		for _, w := range []int{40, 60, 80, 100, 140, 170} {
			for _, h := range []int{20, 40} {
				m := newTestModel(t, sample()...)
				m.width, m.height = w, h
				m.bodyArea.SetWidth(w - 12)
				m.bodyArea.SetHeight(m.composeBodyHeight())
				lines := strings.Split(ansi.Strip(render(m)), "\n")
				if len(lines) != h {
					t.Errorf("%s %dx%d: %d lines, want %d", name, w, h, len(lines), h)
				}
				for _, l := range lines {
					if lipgloss.Width(l) > w {
						t.Errorf("%s %dx%d: line %d wide: %q", name, w, h, lipgloss.Width(l), l)
					}
				}
				if !strings.Contains(lines[0], "mailctl") {
					t.Errorf("%s %dx%d: header missing: %q", name, w, h, lines[0])
				}
				if last := lines[len(lines)-1]; !strings.Contains(last, "esc") {
					t.Errorf("%s %dx%d: footer lacks esc hint: %q", name, w, h, last)
				}
			}
		}
	}
}

func TestPopupsFitAndHavePanelBorder(t *testing.T) {
	for _, w := range []int{40, 60, 100, 170} {
		m := newTestModel(t, sample()...)
		m.width, m.height = w, 30
		m.templateNames = []string{"thanks", "followup"}
		m.unsubPrompt = &unsubPrompt{sender: "Billing <bill@corp.test>", fromBody: true,
			target: unsub.Target{Kind: unsub.HTTPS, URL: "https://corp.test/unsubscribe?token=" + strings.Repeat("x", 80)}}
		for name, out := range map[string]string{"template": m.renderTemplatePicker(), "help": m.openHelp().renderHelpPopup(), "unsub": m.renderUnsubPopup()} {
			lines := strings.Split(ansi.Strip(out), "\n")
			for _, l := range lines {
				if lipgloss.Width(l) > w {
					t.Errorf("%s at %d: line %d wide", name, w, lipgloss.Width(l))
				}
			}
			if len(lines) > 30 || !strings.ContainsAny(lines[0], "╭┌┏") {
				t.Errorf("%s at %d: not a bordered panel (%d lines): %q", name, w, len(lines), lines[0])
			}
		}
		if got := ansi.Strip(m.renderUnsubPopup()); !strings.Contains(got, "Billing") || !strings.Contains(got, "corp.test") || !strings.Contains(got, "esc cancel") {
			t.Errorf("unsub popup at %d lost sender/target/hints:\n%s", w, got)
		}
	}
}
