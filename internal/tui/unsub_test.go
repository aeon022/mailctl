package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/unsub"
	"github.com/aeon022/missionctl-core/tuitest"
)

// unsubFixture opens a message in the detail view with the header lookup
// stubbed and the side effect recorded — nothing real can run.
func unsubFixture(t *testing.T, listUnsub, post, body string) (Model, *[]unsub.Target) {
	t.Helper()
	oldF, oldP := fetchUnsubHeaders, performUnsub
	fetchUnsubHeaders = func(_, _, _ string) (string, string, error) { return listUnsub, post, nil }
	var performed []unsub.Target
	performUnsub = func(tg unsub.Target) error { performed = append(performed, tg); return nil }
	t.Cleanup(func() { fetchUnsubHeaders, performUnsub = oldF, oldP })

	msg := models.Message{ID: "n1", Subject: "Weekly", From: "News <news@list.test>", Body: body, Account: "a@x.test"}
	m := newTestModel(t, msg)
	m.view = viewDetail
	m.detail = &msg
	return m, &performed
}

// run executes cmd (our own stubbed lookup) and feeds its message back in.
func run(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	tm, _ := m.Update(cmd())
	return tm.(Model)
}

func TestUnsubNeedsConfirmBeforeAnyAction(t *testing.T) {
	m, performed := unsubFixture(t, "<https://list.test/u/1>", "List-Unsubscribe=One-Click", "")
	m, cmd := press(t, m, "U")
	m = run(t, m, cmd)
	if m.unsubPrompt == nil {
		t.Fatal("U must open the confirm popup")
	}
	if len(*performed) != 0 {
		t.Fatalf("acted before confirmation: %+v", *performed)
	}
	out := tuitest.Text(m)
	for _, want := range []string{"Unsubscribe from", "News <news@list.test>", "one-click unsubscribe request to list.test", "enter confirm", "esc cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("popup missing %q:\n%s", want, out)
		}
	}
	// any unrelated key is swallowed: no delete/reply/quit behind the popup
	m, cmd = press(t, m, "d")
	if m.unsubPrompt == nil || m.confirmID != "" || cmd != nil {
		t.Errorf("popup must swallow other keys: prompt=%v confirmID=%q cmd=%v", m.unsubPrompt != nil, m.confirmID, cmd != nil)
	}
}

func TestUnsubEscCancels(t *testing.T) {
	m, performed := unsubFixture(t, "<https://list.test/u/1>", "List-Unsubscribe=One-Click", "")
	m, cmd := press(t, m, "U")
	m = run(t, m, cmd)
	m, cmd = press(t, m, "esc")
	if m.unsubPrompt != nil || cmd != nil || len(*performed) != 0 {
		t.Errorf("esc: prompt=%v cmd=%v performed=%d", m.unsubPrompt != nil, cmd != nil, len(*performed))
	}
	if m.view != viewDetail {
		t.Error("esc closes only the popup, the message stays open")
	}
}

func TestUnsubConfirmPerformsChosenMethod(t *testing.T) {
	m, performed := unsubFixture(t, "<mailto:u@list.test>, <https://list.test/u/1>", "List-Unsubscribe=One-Click", "")
	m, cmd := press(t, m, "U")
	m = run(t, m, cmd)
	m, cmd = press(t, m, "enter")
	if m.unsubPrompt != nil {
		t.Error("confirm closes the popup")
	}
	tm, _ := m.Update(cmd())
	m = tm.(Model)
	if len(*performed) != 1 || (*performed)[0].Kind != unsub.OneClick || (*performed)[0].URL != "https://list.test/u/1" {
		t.Fatalf("performed = %+v, want the one-click target (precedence over mailto)", *performed)
	}
	if !strings.Contains(m.status, "accepted") {
		t.Errorf("status = %q", m.status)
	}
}

func TestUnsubFailureIsReported(t *testing.T) {
	m, _ := unsubFixture(t, "<https://list.test/u/1>", "List-Unsubscribe=One-Click", "")
	performUnsub = func(unsub.Target) error { return errors.New("500 boom") }
	m, cmd := press(t, m, "U")
	m = run(t, m, cmd)
	m, cmd = press(t, m, "y")
	m = run(t, m, cmd)
	if !strings.Contains(m.status, "failed") || !strings.Contains(m.status, "500 boom") {
		t.Errorf("status = %q", m.status)
	}
}

func TestUnsubMailtoOpensPrefilledComposeNeverSends(t *testing.T) {
	m, performed := unsubFixture(t, "<mailto:unsub@list.test?subject=Please%20remove&body=bye>", "", "")
	m, cmd := press(t, m, "U")
	m = run(t, m, cmd)
	m, cmd = press(t, m, "enter")
	if m.view != viewCompose || cmd != nil || len(*performed) != 0 {
		t.Fatalf("mailto → compose, nothing sent: view=%v cmd=%v performed=%d", m.view, cmd != nil, len(*performed))
	}
	if m.toInput.Value() != "unsub@list.test" || m.subjectInput.Value() != "Please remove" || m.bodyArea.Value() != "bye" {
		t.Errorf("compose not pre-filled: to=%q subject=%q body=%q", m.toInput.Value(), m.subjectInput.Value(), m.bodyArea.Value())
	}
}

func TestUnsubFallsBackToBodyLinkWithWarning(t *testing.T) {
	m, _ := unsubFixture(t, "", "", "Click here to unsubscribe: https://list.test/leave?x=1 thanks")
	m, cmd := press(t, m, "U")
	m = run(t, m, cmd)
	if m.unsubPrompt == nil || !m.unsubPrompt.fromBody {
		t.Fatal("body link should still be offered")
	}
	if out := tuitest.Text(m); !strings.Contains(out, "guessed") {
		t.Errorf("a guessed link must be flagged in the popup:\n%s", out)
	}
}

func TestUnsubNothingFound(t *testing.T) {
	m, _ := unsubFixture(t, "", "", "no links here")
	m, cmd := press(t, m, "U")
	m = run(t, m, cmd)
	if m.unsubPrompt != nil || !strings.Contains(m.status, "No unsubscribe option") {
		t.Errorf("prompt=%v status=%q", m.unsubPrompt != nil, m.status)
	}
}

func TestUnsubResultIgnoredAfterLeavingTheMessage(t *testing.T) {
	m, _ := unsubFixture(t, "<https://list.test/u/1>", "", "")
	m, cmd := press(t, m, "U")
	m, _ = press(t, m, "esc") // back to the list before the lookup returns
	m = run(t, m, cmd)
	if m.unsubPrompt != nil {
		t.Error("a late lookup result must not pop up over another view")
	}
}
