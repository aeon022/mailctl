package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/missionctl-core/tuitest"
)

// TestRenderLandingMockups writes an ANSI screenshot of the real TUI (made-up demo data) for the landing
// page: MAILCTL_MOCKUP_DIR=/dir go test ./internal/tui -run TestRenderLandingMockups
func TestRenderLandingMockups(t *testing.T) {
	dir := os.Getenv("MAILCTL_MOCKUP_DIR")
	if dir == "" {
		t.Skip("set MAILCTL_MOCKUP_DIR to render landing mockups")
	}
	t.Setenv("HOME", t.TempDir())
	now := time.Now()
	ago := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }
	msg := func(id, from, subj, body string, h int, read bool) models.Message {
		return models.Message{ID: id, Subject: subj, From: from, Date: ago(h), Read: read, Mailbox: "INBOX", Account: "iCloud", Body: body, Source: "apple"}
	}
	msgs := []models.Message{
		msg("1", "Mara Klein <mara@nordlicht.test>", "Re: Redesign — feedback on the hero", "Love the new hero — could we make the headline a bit shorter?", 1, false),
		msg("2", "Billing <billing@hosting.test>", "Invoice 2026-10 for missionctl.sh", "Your hosting invoice for October is attached. Amount due on the 25th.", 3, false),
		msg("3", "Alice Berger <alice@studio.test>", "Design review tomorrow 11:00", "Quick reminder: design review tomorrow, bring the latest mockups.", 5, false),
		msg("4", "GitHub <noreply@github.test>", "[missionctl] CI passed on main", "All checks have passed for commit 3a9f2c1 on main.", 7, true),
		msg("5", "Jonas <jonas@team.test>", "Lunch on Thursday?", "Thursday works for me — noon at the usual place?", 20, true),
		msg("6", "Café Central <hello@cafe.test>", "Your reservation is confirmed", "We look forward to seeing you on Saturday at 19:30.", 26, true),
		msg("7", "Newsletter <news@terminal.test>", "Terminal tools worth trying this month", "Five small terminal tools that make a big difference.", 40, true),
	}
	m := New()
	mi, _ := tuitest.Send(m, tea.WindowSizeMsg{Width: 118, Height: 20}, msgsLoadedMsg{msgs: msgs, accounts: []string{"iCloud"}, unreadCounts: map[string]int{"iCloud": 3}})
	out := mi.(Model).View().Content
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mailctl.ansi"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
