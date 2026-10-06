package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/charmbracelet/x/ansi"
	runewidth "github.com/mattn/go-runewidth"
)

func tabModel(t *testing.T, width, active int) Model {
	t.Helper()
	m := newTestModel(t, sample()...)
	m.accounts = []string{"Alle", "Abteilung83", "FH Burgenland", "Gerwin || Die Brücke", "Gmail"}
	m.unreadCounts = map[string]int{"": 1, "Gmail": 2}
	m.activeTab = active
	m.width, m.height = width, 30
	return m
}

func TestTabBarUsesPillsWithCountsAndFitsTheBudget(t *testing.T) {
	m := tabModel(t, 110, 0)
	bar, hits := m.tabBar(m.tabBudget())
	plain := ansi.Strip(bar)
	if !strings.Contains(plain, " Alle 1 ") || !strings.Contains(plain, " Gmail 2 ") || strings.Contains(plain, "·") {
		t.Errorf("tabs show the unread count after the name: %q", plain)
	}
	if lipgloss.Width(bar) > m.tabBudget() {
		t.Errorf("bar %d wider than budget %d", lipgloss.Width(bar), m.tabBudget())
	}
	if len(hits) != len(m.accounts) {
		t.Errorf("all %d tabs fit at 110 columns, got %d hits", len(m.accounts), len(hits))
	}
}

func TestTabHitTestMatchesDrawnPositions(t *testing.T) {
	for _, tc := range []struct{ width, active int }{{110, 0}, {110, 3}, {70, 4}, {50, 2}} {
		m := tabModel(t, tc.width, tc.active)
		bar, hits := m.tabBar(m.tabBudget())
		plain := ansi.Strip(bar)
		found := false
		for _, h := range hits {
			// the text under the hit span is exactly this tab's cell
			cell := cellsAt(plain, h.x, h.w)
			label := strings.TrimSpace(cell)
			if !strings.HasPrefix(label, m.accounts[h.idx]) {
				t.Errorf("w=%d active=%d: hit span of tab %d reads %q, want it to start with %q", tc.width, tc.active, h.idx, label, m.accounts[h.idx])
			}
			// clicking the middle of the span resolves to that tab, on row 1 only
			if got := m.tabHitTest(h.x+h.w/2, 1); got != h.idx {
				t.Errorf("w=%d active=%d: click on tab %d resolved to %d", tc.width, tc.active, h.idx, got)
			}
			if got := m.tabHitTest(h.x+h.w/2, 2); got != -1 {
				t.Errorf("row 2 is not the tab bar, got %d", got)
			}
			found = found || h.idx == tc.active
		}
		if !found {
			t.Errorf("w=%d: the active tab %d must stay visible and clickable", tc.width, tc.active)
		}
	}
}

func TestClickingATabSwitchesAccount(t *testing.T) {
	m := tabModel(t, 110, 0)
	_, hits := m.tabBar(m.tabBudget())
	var target tabHit
	for _, h := range hits {
		if h.idx == 2 {
			target = h
		}
	}
	mi, cmd := tuitest.Send(m, tuitest.Click(target.x+1, 1))
	got := mi.(Model)
	if got.activeTab != 2 || got.activeAccount() != "FH Burgenland" || len(cmd) == 0 {
		t.Errorf("click on the third tab: tab=%d account=%q cmds=%d", got.activeTab, got.activeAccount(), len(cmd))
	}
}

func TestSyncedSuffixDoesNotDriftTheHitTest(t *testing.T) {
	// the sync text takes room from the tabs; draw and click must agree on it
	m := tabModel(t, 70, 4)
	m.syncing = true
	bar, hits := m.tabBar(m.tabBudget())
	for _, h := range hits {
		if h.x+h.w > lipgloss.Width(bar) {
			t.Errorf("hit span %+v extends past the drawn bar (%d)", h, lipgloss.Width(bar))
		}
	}
	if lipgloss.Width(bar)+lipgloss.Width(m.syncSuffix()) > min(m.width, 130) {
		t.Errorf("tabs + sync text overflow: %d + %d", lipgloss.Width(bar), lipgloss.Width(m.syncSuffix()))
	}
}

// cellsAt returns the w display cells of s starting at cell column x.
func cellsAt(s string, x, w int) string {
	col, out := 0, ""
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if col >= x && col+rw <= x+w {
			out += string(r)
		}
		col += rw
	}
	return out
}
