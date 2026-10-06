package tui

import (
	"context"
	"github.com/aeon022/mailctl/internal/actlog"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/sahilm/fuzzy"
)

// ── Commands ──────────────────────────────────────────────────────────────────

// loadMsgsCmd fetches messages for account/unreadOnly, unfiltered by search
// text — search is applied client-side (filterMsgs) over the result, live
// as the user types, rather than round-tripping to SQLite on every
// keystroke or requiring enter to submit. Store.Filter.Query / the SQL LIKE
// path still exists and is still used by `mailctl search` and the MCP
// search tool, just not from here anymore.
func loadMsgsCmd(unreadOnly bool, account string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return errMsg{err}
		}
		defer s.Close()
		ctx := context.Background()
		msgs, err := s.ListMessages(ctx, store.Filter{
			Account:    account,
			UnreadOnly: unreadOnly,
			Limit:      500,
		})
		if err != nil {
			return errMsg{err}
		}
		accounts, _ := s.ListAccounts(ctx)
		counts, _ := s.UnreadCounts(ctx)
		return msgsLoadedMsg{msgs: msgs, accounts: accounts, unreadCounts: counts}
	}
}

// filterMsgs fuzzy-matches q against each message's subject OR from
// (github.com/sahilm/fuzzy), keeping a message if either matches. Does NOT
// re-rank by match quality — messages are naturally date-ordered (grouped
// by day in the list), and re-sorting by fuzzy score would scramble that.
func filterMsgs(msgs []models.Message, q string) []models.Message {
	q = strings.TrimSpace(q)
	if q == "" {
		return msgs
	}
	subjects := make([]string, len(msgs))
	froms := make([]string, len(msgs))
	for i, m := range msgs {
		subjects[i] = m.Subject
		froms[i] = m.From
	}
	matched := make(map[int]bool, len(msgs))
	for _, mt := range fuzzy.Find(q, subjects) {
		matched[mt.Index] = true
	}
	for _, mt := range fuzzy.Find(q, froms) {
		matched[mt.Index] = true
	}
	out := make([]models.Message, 0, len(matched))
	for i, m := range msgs {
		if matched[i] {
			out = append(out, m)
		}
	}
	return out
}

func syncCmd() tea.Cmd {
	return func() tea.Msg {
		msgs, accounts, err := mail.Sync(150)
		if err != nil {
			return syncDoneMsg{err: err}
		}
		return syncDoneMsg{count: len(msgs), accounts: accounts}
	}
}

func loadBodyCmd(msg *models.Message) tea.Cmd {
	account, subject, from := msg.Account, msg.Subject, msg.From
	return func() tea.Msg {
		body, err := mail.FetchMessageBody(account, subject, from)
		return bodyLoadedMsg{body: body, err: err}
	}
}

func markReadCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return readMarkedMsg{}
		}
		defer s.Close()
		_ = s.MarkRead(context.Background(), id)
		return readMarkedMsg{}
	}
}

// sendMail is a variable so tests never reach a mail client or SMTP server.
var sendMail = mail.Send

func sendCmd(to, subject, body string, attachments []string) tea.Cmd {
	return func() tea.Msg {
		d := &models.Draft{To: []string{to}, Subject: subject, Body: body, Attachments: attachments}
		if err := sendMail(d); err != nil {
			return sentMsg{err}
		}
		actlog.Sent(subject)
		return sentMsg{}
	}
}

func draftCmd(to, subject, body string, attachments []string) tea.Cmd {
	return func() tea.Msg {
		d := &models.Draft{To: []string{to}, Subject: subject, Body: body, Attachments: attachments}
		if err := mail.SaveDraft(d); err != nil {
			return draftedMsg{err}
		}
		return draftedMsg{}
	}
}

func parseAttachments(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func openInMailCmd(messageID string) tea.Cmd {
	return func() tea.Msg {
		_ = mail.OpenInMail(messageID)
		return openedMsg{}
	}
}

func markUnreadCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return unreadMarkedMsg{}
		}
		defer s.Close()
		_ = s.MarkUnread(context.Background(), id)
		_ = mail.MarkUnreadInMail(id)
		return unreadMarkedMsg{}
	}
}

func deleteCmd(id string) tea.Cmd {
	return func() tea.Msg {
		// Real Apple Mail delete first, local cache row only removed once
		// that actually succeeds — used to delete the cache row
		// unconditionally and discard DeleteInMail's error, which on a
		// failure (stale id, message already gone) left the real message
		// alive in Mail while the cache said it was gone. Same bug class
		// fixed in calctl's DeleteEvent on 2026-08-01.
		if err := mail.DeleteInMail(id); err != nil {
			return deletedMsg{err}
		}
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return deletedMsg{err}
		}
		defer s.Close()
		if err := s.DeleteMessage(context.Background(), id); err != nil {
			return deletedMsg{err}
		}
		return deletedMsg{}
	}
}

// selectedIDs flattens a selection set into a slice.
func selectedIDs(sel map[string]bool) []string {
	ids := make([]string, 0, len(sel))
	for id := range sel {
		ids = append(ids, id)
	}
	return ids
}

// removeMessages returns msgs with every message whose ID is in sel dropped
// — used to optimistically update the list right after a batch delete,
// mirroring the single-delete "d" key's own optimistic removal.
func removeMessages(msgs []models.Message, sel map[string]bool) []models.Message {
	out := msgs[:0:0]
	for _, msg := range msgs {
		if !sel[msg.ID] {
			out = append(out, msg)
		}
	}
	return out
}

// batchMarkReadCmd is the batch-mode ("v" + "r") version of markReadCmd.
func batchMarkReadCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return readMarkedMsg{}
		}
		defer s.Close()
		ctx := context.Background()
		for _, id := range ids {
			_ = s.MarkRead(ctx, id)
		}
		return readMarkedMsg{}
	}
}

// batchDeleteCmd is the batch-mode ("v" + "d") version of deleteCmd.
func batchDeleteCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return deletedMsg{err}
		}
		defer s.Close()
		ctx := context.Background()
		var lastErr error
		for _, id := range ids {
			// Same real-delete-before-cache-delete ordering as deleteCmd —
			// skip the cache row entirely if Apple Mail's delete fails,
			// instead of discarding that error and dropping the row anyway.
			if err := mail.DeleteInMail(id); err != nil {
				lastErr = err
				continue
			}
			if err := s.DeleteMessage(ctx, id); err != nil {
				lastErr = err
			}
		}
		return deletedMsg{lastErr}
	}
}
