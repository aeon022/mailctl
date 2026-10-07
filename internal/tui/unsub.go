package tui

import (
	"context"
	"github.com/aeon022/mailctl/internal/actlog"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/unsub"
	"github.com/aeon022/missionctl-core/statusbar"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/charmbracelet/x/ansi"
)

// Unsubscribe flow: "U" in the detail view looks up the message's own
// List-Unsubscribe headers (on demand — they aren't cached), then shows a
// confirm popup naming the sender and the method. Nothing is sent, POSTed or
// opened until the user confirms.

type unsubPrompt struct {
	msgID    string
	sender   string
	target   unsub.Target
	fromBody bool // no header found; this is a link guessed from the body text
}

type unsubTargetMsg struct {
	msgID    string
	sender   string
	target   unsub.Target
	ok       bool
	fromBody bool
}

type unsubDoneMsg struct {
	err error
	via string
}

// Hooks so tests never reach a mail client or the network.
var (
	fetchUnsubHeaders = mail.FetchUnsubscribeHeaders
	performUnsub      = func(t unsub.Target) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		return unsub.Do(ctx, t)
	}
)

func findUnsubCmd(msg models.Message) tea.Cmd {
	return func() tea.Msg {
		out := unsubTargetMsg{msgID: msg.ID, sender: msg.From}
		if lu, post, err := fetchUnsubHeaders(msg.Account, msg.Subject, msg.From); err == nil {
			if t, ok := unsub.Choose(unsub.Parse(lu, post)); ok {
				out.target, out.ok = t, true
				return out
			}
		}
		if link := findUnsubscribeURL(msg.Body); link != "" {
			out.target, out.ok, out.fromBody = unsub.Target{Kind: unsub.HTTPS, URL: link}, true, true
		}
		return out
	}
}

func doUnsubCmd(t unsub.Target, sender string) tea.Cmd {
	return func() tea.Msg {
		err := performUnsub(t)
		if err == nil {
			actlog.Unsubscribed(sender)
		}
		return unsubDoneMsg{err: err, via: string(t.Kind)}
	}
}

// handleUnsubKey runs while the confirm popup is open; every other key is
// swallowed so nothing can happen behind it.
func (m Model) handleUnsubKey(key string) (Model, tea.Cmd) {
	p := m.unsubPrompt
	switch key {
	case "enter", "y":
		m.unsubPrompt = nil
		if p.target.Kind == unsub.Mailto {
			// Compose flow: pre-filled, reviewed and sent by the user.
			m.replyTo = nil
			m.resetCompose(p.target.To, p.target.Subject)
			m.bodyArea.SetValue(p.target.Body)
			m.view = viewCompose
			m.setStatus("Review the unsubscribe email, then ctrl+s to send")
			return m, nil
		}
		m.setStatus("Unsubscribing…")
		return m, doUnsubCmd(p.target, p.sender)
	case "esc", "n", "q":
		m.unsubPrompt = nil
		m.setStatus("Unsubscribe cancelled")
	}
	return m, nil
}

func (m Model) renderUnsubPopup() string {
	p := m.unsubPrompt
	w := min(62, m.width-2)
	var b strings.Builder
	b.WriteString("Unsubscribe from\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(p.sender) + "\n\n")
	b.WriteString("Method: " + p.target.Describe() + "\n")
	if p.fromBody {
		b.WriteString("\nNo List-Unsubscribe header — this link was guessed from the message text. Check the page before you confirm anything there.\n")
	}
	b.WriteString("\n" + statusbar.Hint("enter", "confirm") + "   " + statusbar.Hint("esc", "cancel"))
	// wrap (never truncate: the user must see the whole target they confirm)
	rows := strings.Split(ansi.Wrap(b.String(), max(w-5, 8), ""), "\n")
	for i := range rows {
		rows[i] = " " + rows[i]
	}
	return ui.Panel(w, len(rows)+2, "Unsubscribe", strings.Join(rows, "\n"), true)
}
