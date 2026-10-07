package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAccountTabHitsAreExactlyWhatWasDrawn(t *testing.T) {
	m := newTestModel(t)
	m.accounts = []string{"Alle", "Abteilung83", "FH Burgenland", "Gerwin || Die Brücke", "Gmail"}
	m.unreadCounts = map[string]int{"": 1, "Gmail": 1}
	for _, active := range []int{0, 2, 4} {
		m.activeTab = active
		for _, w := range []int{200, 70, 45} {
			bar, hits := m.tabBar(w)
			if len(hits) == 0 {
				t.Fatalf("active %d width %d: no hits", active, w)
			}
			cells := []rune(ansi.Strip(bar))
			seenActive := false
			for _, h := range hits {
				if h.x < 0 || h.x+h.w > len(cells) {
					t.Fatalf("hit %+v outside %d drawn cells", h, len(cells))
				}
				got := strings.TrimSpace(string(cells[h.x : h.x+h.w]))
				if !strings.HasPrefix(got, m.accounts[h.idx]) {
					t.Errorf("active %d width %d: tab %d span shows %q, want %q", active, w, h.idx, got, m.accounts[h.idx])
				}
				seenActive = seenActive || h.idx == active
			}
			if !seenActive {
				t.Errorf("active tab %d must always be visible and clickable at width %d", active, w)
			}
		}
	}
}
