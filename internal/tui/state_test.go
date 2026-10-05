package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/mailctl/internal/models"
)

// press feeds one key (text or a named special) through Update. Commands are
// returned but never executed — they would talk to Apple Mail / the network.
func press(t *testing.T, m Model, keys ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
		case "ctrl+t":
			msg = tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		default:
			msg = tea.KeyPressMsg{Text: k, Code: []rune(k)[0]}
		}
		var tm tea.Model
		tm, cmd = m.Update(msg)
		m = tm.(Model)
	}
	return m, cmd
}

// newTestModel returns a list-view Model with msgs loaded, in an isolated HOME
// (the model persists small UI state under it).
func newTestModel(t *testing.T, msgs ...models.Message) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := New()
	m.width, m.height = 100, 30
	m.allMsgs = msgs
	m.msgs = filterMsgs(msgs, "")
	return m
}

func sample() []models.Message {
	now := time.Now()
	return []models.Message{
		{ID: "a", Subject: "Invoice March", From: "Billing <bill@corp.test>", Date: now, Body: "pay now"},
		{ID: "b", Subject: "Lunch?", From: "Anna <anna@x.test>", Date: now, Body: "hi"},
		{ID: "c", Subject: "Invoice April", From: "Billing <bill@corp.test>", Date: now, Body: "again"},
	}
}

func ids(msgs []models.Message) string {
	var s []string
	for _, m := range msgs {
		s = append(s, m.ID)
	}
	return strings.Join(s, ",")
}

func TestListCursorNavigation(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "k")
	if m.cursor != 0 {
		t.Errorf("k at top moved cursor to %d", m.cursor)
	}
	m, _ = press(t, m, "j", "j", "j", "j")
	if m.cursor != 2 {
		t.Errorf("j must clamp at last row, got %d", m.cursor)
	}
	m, _ = press(t, m, "g")
	if m.cursor != 0 {
		t.Errorf("g → %d", m.cursor)
	}
	m, _ = press(t, m, "G")
	if m.cursor != 2 {
		t.Errorf("G → %d", m.cursor)
	}
}

func TestSearchFiltersLiveAndEscRestores(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "/", "i", "n", "v")
	if !m.searching || ids(m.msgs) != "a,c" {
		t.Fatalf("live filter: searching=%v msgs=%s", m.searching, ids(m.msgs))
	}
	m, _ = press(t, m, "enter")
	if m.searching || ids(m.msgs) != "a,c" || m.cursor != 0 {
		t.Errorf("enter must close the box but keep the filter: searching=%v msgs=%s", m.searching, ids(m.msgs))
	}
	m, _ = press(t, m, "esc")
	if m.searchQ != "" || ids(m.msgs) != "a,b,c" {
		t.Errorf("esc must clear the filter: q=%q msgs=%s", m.searchQ, ids(m.msgs))
	}
	// esc while typing also resets
	m, _ = press(t, m, "/", "x", "esc")
	if m.searching || m.searchQ != "" || ids(m.msgs) != "a,b,c" {
		t.Errorf("esc inside search: searching=%v q=%q msgs=%s", m.searching, m.searchQ, ids(m.msgs))
	}
}

func TestDeleteNeedsConfirmAndKeepsFullListInSync(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "j") // on b

	m, cmd := press(t, m, "d")
	if cmd != nil || m.confirmID != "b" || ids(m.msgs) != "a,b,c" {
		t.Fatalf("first d must only ask: cmd=%v confirm=%q msgs=%s", cmd != nil, m.confirmID, ids(m.msgs))
	}
	// moving to another message invalidates the pending confirm
	m, _ = press(t, m, "j", "d")
	if m.confirmID != "c" || ids(m.msgs) != "a,b,c" {
		t.Fatalf("d on a different row must re-ask: confirm=%q msgs=%s", m.confirmID, ids(m.msgs))
	}
	m, _ = press(t, m, "esc")
	if m.confirmID != "" {
		t.Error("esc must cancel the confirm")
	}

	m, _ = press(t, m, "k", "d")
	m, cmd = press(t, m, "d") // second d on b deletes (cmd not executed)
	if cmd == nil || ids(m.msgs) != "a,c" {
		t.Fatalf("delete: cmd=%v msgs=%s", cmd != nil, ids(m.msgs))
	}
	// the full list the search filters from must not resurrect or duplicate it
	if got := ids(m.allMsgs); got != "a,c" {
		t.Errorf("allMsgs = %s after delete, want a,c", got)
	}
	m, _ = press(t, m, "/", "l", "u", "n") // "lun" only matched the deleted "Lunch?"
	if len(m.msgs) != 0 {
		t.Errorf("deleted message came back through search: %s", ids(m.msgs))
	}
}

func TestDeleteWhileFilteredDoesNotReturnAfterClearingSearch(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "/", "i", "n", "v", "enter") // a, c
	m, _ = press(t, m, "d", "d")                    // delete a
	if ids(m.msgs) != "c" {
		t.Fatalf("msgs = %s", ids(m.msgs))
	}
	m, _ = press(t, m, "esc")
	if got := ids(m.msgs); got != "b,c" {
		t.Errorf("after clearing the search the list is %s, want b,c (a was deleted)", got)
	}
}

func TestOpenDetailMarksReadInBothLists(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "/", "l", "u", "n", "enter") // only b visible (filtered copy)
	m, cmd := press(t, m, "enter")
	if m.view != viewDetail || m.detail == nil || m.detail.ID != "b" || cmd == nil {
		t.Fatalf("enter must open detail: view=%v detail=%v", m.view, m.detail)
	}
	if !m.detail.Read || !m.msgs[0].Read {
		t.Error("opened message must be marked read optimistically")
	}
	m, _ = press(t, m, "esc") // back to list
	m, _ = press(t, m, "esc") // clear search → full list again
	for _, x := range m.msgs {
		if x.ID == "b" && !x.Read {
			t.Error("read flag was lost when the search filter cleared")
		}
	}
}

func TestDetailKeys(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "enter")
	if m.view != viewDetail {
		t.Fatal("setup: detail not open")
	}

	// u marks unread everywhere
	m, cmd := press(t, m, "u")
	if m.detail.Read || m.msgs[0].Read || cmd == nil {
		t.Errorf("u: detail.Read=%v msgs[0].Read=%v cmd=%v", m.detail.Read, m.msgs[0].Read, cmd != nil)
	}

	// U with a real link opens it; without one it only reports
	m.detail.Body = "bye\nUnsubscribe: https://mail.test/unsubscribe?id=1 \nthanks"
	if _, cmd = press(t, m, "U"); cmd == nil {
		t.Error("U with an unsubscribe link must return a command")
	}
	m.detail.Body = "no link here"
	m, cmd = press(t, m, "U")
	if cmd != nil || !strings.Contains(m.status, "No unsubscribe link") {
		t.Errorf("U without link: cmd=%v status=%q", cmd != nil, m.status)
	}

	// a (AI draft) is a Bundle feature
	m, cmd = press(t, m, "a")
	if cmd != nil || m.aiDrafting || !strings.Contains(m.status, "Bundle") {
		t.Errorf("a without license: cmd=%v drafting=%v status=%q", cmd != nil, m.aiDrafting, m.status)
	}

	// reply prefills compose: address only, one "Re: ", quoted body
	m, _ = press(t, m, "r")
	if m.view != viewCompose || m.toInput.Value() != "bill@corp.test" || m.subjectInput.Value() != "Re: Invoice March" {
		t.Errorf("reply: view=%v to=%q subject=%q", m.view, m.toInput.Value(), m.subjectInput.Value())
	}
	if !strings.Contains(m.bodyArea.Value(), "> no link here") {
		t.Errorf("reply body must quote the original: %q", m.bodyArea.Value())
	}
	m, _ = press(t, m, "esc")
	m.detail = &models.Message{ID: "z", Subject: "Re: Already", From: "x@y.test"}
	m.view = viewDetail
	m, _ = press(t, m, "r")
	if m.subjectInput.Value() != "Re: Already" {
		t.Errorf("must not double the Re: prefix, got %q", m.subjectInput.Value())
	}

	// detail delete: confirm, then removes from both lists and returns to list
	m = newTestModel(t, sample()...)
	m, _ = press(t, m, "enter", "d")
	if m.view != viewDetail || m.confirmID != "a" {
		t.Fatalf("d in detail must ask first: view=%v confirm=%q", m.view, m.confirmID)
	}
	m, cmd = press(t, m, "d")
	if m.view != viewList || cmd == nil || ids(m.msgs) != "b,c" || ids(m.allMsgs) != "b,c" {
		t.Errorf("detail delete: view=%v msgs=%s all=%s", m.view, ids(m.msgs), ids(m.allMsgs))
	}
}

func TestBatchSelectionFlows(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "v", "j", "space")
	if !m.selecting || !m.selected["a"] || !m.selected["b"] {
		t.Fatalf("selection = %v", m.selected)
	}

	// d asks first; navigating cancels the pending confirm
	m, cmd := press(t, m, "d")
	if cmd != nil || !m.batchConfirmDelete {
		t.Fatal("first d must only ask")
	}
	m, _ = press(t, m, "j")
	if m.batchConfirmDelete {
		t.Error("moving must cancel the pending batch delete")
	}

	// A selects all, r marks all read and leaves selection mode
	m, _ = press(t, m, "A", "r")
	if m.selecting || m.selected != nil {
		t.Error("r must leave selection mode")
	}
	for _, x := range append(append([]models.Message{}, m.msgs...), m.allMsgs...) {
		if !x.Read {
			t.Errorf("%s not marked read", x.ID)
		}
	}

	// confirmed batch delete removes from both lists
	m, _ = press(t, m, "g", "v", "j", "space", "d")
	m, cmd = press(t, m, "d")
	if cmd == nil || ids(m.msgs) != "c" || ids(m.allMsgs) != "c" {
		t.Errorf("batch delete: cmd=%v msgs=%s all=%s", cmd != nil, ids(m.msgs), ids(m.allMsgs))
	}

	// esc leaves selection without side effects
	m, _ = press(t, m, "v", "esc")
	if m.selecting || m.selected != nil {
		t.Error("esc must leave selection mode")
	}
}

func TestAccountTabsAndUnreadToggle(t *testing.T) {
	m := newTestModel(t, sample()...)
	m.accounts = []string{"All", "Work", "Private"}

	m, cmd := press(t, m, "tab")
	if m.activeTab != 1 || cmd == nil || m.activeAccount() != "Work" {
		t.Errorf("tab: tab=%d account=%q cmd=%v", m.activeTab, m.activeAccount(), cmd != nil)
	}
	m, _ = press(t, m, "tab", "tab")
	if m.activeTab != 0 || m.activeAccount() != "" {
		t.Errorf("tab must wrap to the all-accounts tab: tab=%d account=%q", m.activeTab, m.activeAccount())
	}
	m, _ = press(t, m, "shift+tab")
	if m.activeTab != 2 {
		t.Errorf("shift+tab must wrap backwards, got %d", m.activeTab)
	}
	m.cursor = 2
	m, cmd = press(t, m, "u")
	if !m.unreadOnly || m.cursor != 0 || cmd == nil {
		t.Errorf("u: unreadOnly=%v cursor=%d cmd=%v", m.unreadOnly, m.cursor, cmd != nil)
	}
}

func TestComposeFocusAndTyping(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "n")
	if m.view != viewCompose || m.composeFocus != focusTo || m.toInput.Value() != "" {
		t.Fatalf("n: view=%v focus=%d", m.view, m.composeFocus)
	}
	m, _ = press(t, m, "a", "@", "b")
	m, _ = press(t, m, "tab", "H", "i")
	m, _ = press(t, m, "tab", "tab", "x") // attach → body
	if m.toInput.Value() != "a@b" || m.subjectInput.Value() != "Hi" || m.bodyArea.Value() != "x" {
		t.Errorf("typed into wrong fields: to=%q subject=%q body=%q", m.toInput.Value(), m.subjectInput.Value(), m.bodyArea.Value())
	}
	if m.composeFocus != focusBody {
		t.Errorf("focus = %d, want body", m.composeFocus)
	}
	m, _ = press(t, m, "tab") // already last: stays
	if m.composeFocus != focusBody {
		t.Errorf("tab past body moved focus to %d", m.composeFocus)
	}
	m, _ = press(t, m, "shift+tab", "shift+tab", "shift+tab", "shift+tab")
	if m.composeFocus != focusTo {
		t.Errorf("shift+tab past the first field moved focus to %d", m.composeFocus)
	}

	// ctrl+s / ctrl+d hand off to the send/draft commands (not executed here)
	if _, cmd := press(t, m, "ctrl+t"); cmd != nil {
		t.Error("template picker must not return a command")
	}
	m, _ = press(t, m, "esc")
	if m.view != viewList {
		t.Errorf("esc must leave compose, view=%v", m.view)
	}
}

func TestTemplatePickerOpensAndCloses(t *testing.T) {
	m := newTestModel(t, sample()...)
	m, _ = press(t, m, "n", "ctrl+t")
	if !m.templatePicking {
		t.Fatal("ctrl+t must open the template picker")
	}
	m, _ = press(t, m, "j", "j", "k")
	if m.templateCursor < 0 || (len(m.templateNames) > 0 && m.templateCursor >= len(m.templateNames)) {
		t.Errorf("cursor out of range: %d / %d", m.templateCursor, len(m.templateNames))
	}
	m, _ = press(t, m, "esc")
	if m.templatePicking || m.view != viewCompose {
		t.Errorf("esc closes only the picker: picking=%v view=%v", m.templatePicking, m.view)
	}
}

func TestHelpers(t *testing.T) {
	if got := parseAttachments(" /a.pdf, ,/b.pdf ,"); len(got) != 2 || got[0] != "/a.pdf" || got[1] != "/b.pdf" {
		t.Errorf("parseAttachments = %q", got)
	}
	if got := parseAttachments(""); len(got) != 0 {
		t.Errorf("empty input → %q", got)
	}

	q := buildQuote(&models.Message{From: "A", Date: time.Date(2026, 3, 2, 9, 5, 0, 0, time.UTC), Body: "l1\nl2\n"})
	if !strings.Contains(q, "A wrote:") || !strings.Contains(q, "> l1\n> l2") || strings.HasSuffix(q, "> ") {
		t.Errorf("buildQuote = %q", q)
	}
	if buildQuote(nil) != "" {
		t.Error("nil message must quote to nothing")
	}

	for _, c := range []struct{ body, want string }{
		{"Click https://x.test/unsubscribe?u=1 to leave", "https://x.test/unsubscribe?u=1"},
		{"To unsubscribe visit <https://x.test/leave> now", "https://x.test/leave"}, // keyword before link
		{"Unsubscribe: (https://x.test/u)", "https://x.test/u"},
		{"no keyword at all https://x.test", ""},
		{"unsubscribe but no link", ""},
	} {
		got := findUnsubscribeURL(c.body)
		if got != c.want {
			t.Errorf("findUnsubscribeURL(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}
