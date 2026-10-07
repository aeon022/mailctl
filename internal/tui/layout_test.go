package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/charmbracelet/x/ansi"
)

// layoutMsgs spreads n messages over several day groups; every other one is read.
func layoutMsgs(n int) []models.Message {
	now := time.Now()
	var out []models.Message
	for i := 0; i < n; i++ {
		out = append(out, models.Message{
			ID: fmt.Sprintf("m%02d", i), Subject: fmt.Sprintf("Subject %02d", i),
			From: fmt.Sprintf("Sender %02d <s%02d@x.test>", i, i), To: []string{"me@x.test"},
			Account: []string{"Brücke", "Gmail"}[i%2], Date: now.AddDate(0, 0, -i/4).Add(-time.Duration(i) * time.Minute),
			Read: i%2 == 1,
		})
	}
	return out
}

// layoutModel is a sized model with n messages and (optionally) account tabs.
func layoutModel(t *testing.T, w, h, n int, accounts bool) Model {
	t.Helper()
	m := newTestModel(t, layoutMsgs(n)...)
	if accounts {
		m.accounts = []string{"Alle", "Brücke", "Gmail"}
		m.unreadCounts = map[string]int{"": 3, "Gmail": 2}
	}
	mi, _ := tuitest.Send(m, tuitest.Resize(w, h))
	m = mi.(Model)
	m.loading = false
	return m
}

func TestListLayoutFitsEveryWidthWithConstantHeight(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 140, 170} {
		for _, h := range []int{24, 30} {
			for name, mut := range map[string]func(*Model){
				"list":       func(*Model) {},
				"unreadOnly": func(m *Model) { m.unreadOnly = true; m.searchQ = "sub" },
				"selecting":  func(m *Model) { m.selecting = true },
				"loading":    func(m *Model) { m.loading = true },
				"empty":      func(m *Model) { m.msgs, m.allMsgs = nil, nil },
				"syncing":    func(m *Model) { m.syncing = true },
			} {
				for _, acc := range []bool{true, false} {
					m := layoutModel(t, w, h, 40, acc)
					mut(&m)
					out := m.renderList()
					lines := strings.Split(out, "\n")
					for i, l := range lines {
						if lw := lipgloss.Width(l); lw > w {
							t.Errorf("%dx%d %s acc=%v: line %d is %d wide: %q", w, h, name, acc, i, lw, ansi.Strip(l))
						}
					}
					if len(lines) != m.height {
						t.Errorf("%dx%d %s acc=%v: %d lines, want the constant height %d", w, h, name, acc, len(lines), m.height)
					}
				}
			}
		}
	}
}

func TestHeightTiersSpaciousAndCompact(t *testing.T) {
	compact := layoutModel(t, 100, 29, 10, true)  // m.height 28
	spacious := layoutModel(t, 100, 30, 10, true) // m.height 29
	if compact.spacious() || !spacious.spacious() {
		t.Fatalf("tier threshold: compact=%v spacious=%v", compact.spacious(), spacious.spacious())
	}
	cl := strings.Split(ansi.Strip(compact.renderList()), "\n")
	sl := strings.Split(ansi.Strip(spacious.renderList()), "\n")
	if strings.Contains(strings.Join(cl[:4], "\n"), "Accounts") || !strings.Contains(cl[1], "Alle") || !strings.HasPrefix(cl[2], "──") {
		t.Errorf("compact: header, unlabeled tabs, divider:\n%s", strings.Join(cl[:4], "\n"))
	}
	if !strings.HasPrefix(sl[1], "──") || strings.TrimSpace(sl[2]) != "" || !strings.Contains(sl[3], "Accounts") || strings.TrimSpace(sl[4]) != "" {
		t.Errorf("spacious: header, divider, blank, 'Accounts' tabs, blank:\n%s", strings.Join(sl[:6], "\n"))
	}
	if compact.chrome().rows != 3 || spacious.chrome().rows != 5 || spacious.chrome().tabsY != 3 || compact.chrome().tabsY != 1 {
		t.Errorf("chrome: compact %+v spacious %+v", compact.chrome(), spacious.chrome())
	}
}

func TestHeaderShowsScopeUnreadAndDate(t *testing.T) {
	m := layoutModel(t, 100, 30, 10, true)
	head := ansi.Strip(strings.SplitN(m.renderList(), "\n", 2)[0])
	if !strings.Contains(head, "mailctl") || !strings.Contains(head, "all accounts · 3 unread") || !strings.Contains(head, time.Now().Format("Mon 02 Jan")) {
		t.Errorf("header = %q", head)
	}
	m.activeTab = 2
	if head := ansi.Strip(strings.SplitN(m.renderList(), "\n", 2)[0]); !strings.Contains(head, "Gmail · 2 unread") {
		t.Errorf("header for the Gmail tab = %q", head)
	}
}

func TestPanelsOnlyFrom120Columns(t *testing.T) {
	narrow := ansi.Strip(layoutModel(t, 119, 36, 10, true).renderList())
	if strings.Contains(narrow, "Inbox") || strings.Contains(narrow, "Message") || strings.ContainsAny(narrow, "╭╰") {
		t.Errorf("no panel frames below 120 columns:\n%s", narrow)
	}
	wide := ansi.Strip(layoutModel(t, 120, 36, 10, true).renderList())
	if !strings.Contains(wide, "╭─ Inbox") || !strings.Contains(wide, "╭─ Message") {
		t.Errorf("120 columns get the Inbox and Message panels:\n%s", wide)
	}
	t.Setenv("MISSIONCTL_BORDERS", "none")
	flat := ansi.Strip(layoutModel(t, 140, 36, 10, true).renderList())
	if strings.ContainsAny(flat, "╭╰│") || !strings.Contains(flat, " Inbox ") {
		t.Errorf("borders=none keeps the titles but drops the frames:\n%s", flat)
	}
}

func TestPreviewFollowsCursorAndNeverFetches(t *testing.T) {
	msgs := []models.Message{
		{ID: "a", Subject: "First", From: "Ann <ann@x.test>", To: []string{"me@x.test"}, Account: "Brücke", Date: time.Now(), Body: "hello body line\nsecond line"},
		{ID: "b", Subject: "Second", From: "Bob <bob@x.test>", Account: "Gmail", Date: time.Now().AddDate(0, 0, -1), Read: true},
	}
	t.Setenv("MISSIONCTL_BORDERS", "")
	m := newTestModel(t, msgs...)
	m.loading = false
	mi, _ := tuitest.Send(m, tuitest.Resize(150, 36))
	text := func(m Model) string { return ansi.Strip(m.renderList()) }

	first := text(mi.(Model))
	for _, want := range []string{"Ann <ann@x.test>", "me@x.test", "Brücke", "hello body line", " unread  First", "today"} {
		if !strings.Contains(first, want) {
			t.Errorf("preview of the first message is missing %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "Press enter to open") {
		t.Error("a loaded body is shown, not the placeholder")
	}

	mm, cmds := tuitest.Keys(mi, "j")
	if len(cmds) != 0 {
		t.Errorf("moving the cursor must not start any fetch, got %d cmds", len(cmds))
	}
	second := text(mm.(Model))
	for _, want := range []string{"Bob <bob@x.test>", "Gmail", "Press enter to open", "yesterday"} {
		if !strings.Contains(second, want) {
			t.Errorf("preview follows the cursor — missing %q:\n%s", want, second)
		}
	}
	if strings.Contains(second, "Ann <ann@x.test>") || strings.Contains(second, " unread  Second") {
		t.Errorf("the previous message's preview must be gone:\n%s", second)
	}
}

// subjectRows maps each message index to the screen row of its list line,
// reading only the left (list) part of the screen.
func subjectRows(m Model) map[int]int {
	g := m.geom()
	rows := map[int]int{}
	for y, l := range strings.Split(m.renderList(), "\n") {
		left := ansi.Strip(ansi.Truncate(l, max(g.leftW, 1)+boolInt(!g.wide)*m.width, ""))
		for i := range m.msgs {
			if strings.Contains(left, fmt.Sprintf("Subject %02d", i)) {
				rows[i] = y
			}
		}
	}
	return rows
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestClickMappingInBothTiersAndLayouts(t *testing.T) {
	for _, h := range []int{24, 36} { // compact / spacious
		for _, w := range []int{100, 140} { // narrow / wide
			for _, acc := range []bool{true, false} {
				for _, cursor := range []int{0, 39} { // top / scrolled to the end
					m := layoutModel(t, w, h, 40, acc)
					m.cursor = cursor
					g := m.geom()
					rows := subjectRows(m)
					if len(rows) == 0 {
						t.Fatalf("%dx%d: no list rows found on screen", w, h)
					}
					seen := map[int]bool{}
					for i, y := range rows {
						seen[y] = true
						if got := m.rowHitTest(5, y); got != i {
							t.Errorf("%dx%d acc=%v cursor=%d: click on row y=%d resolved to %d, want %d", w, h, acc, cursor, y, got, i)
						}
					}
					if _, ok := rows[cursor]; !ok {
						t.Errorf("%dx%d cursor=%d: the cursor row must be on screen", w, h, cursor)
					}
					// everything else in the list area (group headers, blank lines) is no row
					for y := g.rowY0; y < g.rowY0+g.rowsVisible; y++ {
						if !seen[y] {
							if got := m.rowHitTest(5, y); got != -1 {
								t.Errorf("%dx%d cursor=%d: non-row line y=%d resolved to %d", w, h, cursor, y, got)
							}
						}
					}
					// above the list, below it (footer) and — wide — in the preview panel
					for _, y := range []int{0, g.rowY0 - 1, g.rowY0 + g.rowsVisible, m.height - 1} {
						if got := m.rowHitTest(5, y); got != -1 {
							t.Errorf("%dx%d: y=%d outside the list resolved to %d", w, h, y, got)
						}
					}
					if g.wide {
						if got := m.rowHitTest(g.leftW+3, g.rowY0); got != -1 {
							t.Errorf("%dx%d: click in the Message panel resolved to %d", w, h, got)
						}
					}
					// tab clicks: every drawn tab resolves to itself on the tab row, nothing one row down
					if acc {
						_, hits := m.tabBar(m.tabBudget())
						for _, th := range hits {
							if got := m.tabHitTest(tabMargin+th.x+th.w/2, m.chrome().tabsY); got != th.idx {
								t.Errorf("%dx%d: tab %d click resolved to %d", w, h, th.idx, got)
							}
						}
					}
				}
			}
		}
	}
}

func TestGroupHeadersHaveABlankLineBeforeThem(t *testing.T) {
	m := layoutModel(t, 100, 30, 40, false)
	lines, _, toMsg := m.buildListLinesWithMapping(100)
	headers := 0
	for i, l := range lines {
		if toMsg[i] == -1 && strings.TrimSpace(ansi.Strip(l)) != "" { // a group header
			headers++
			if i > 0 && strings.TrimSpace(ansi.Strip(lines[i-1])) != "" {
				t.Errorf("group header %q at line %d has no blank line before it", ansi.Strip(l), i)
			}
		}
	}
	if headers < 3 {
		t.Fatalf("test data should span several groups, got %d headers", headers)
	}
	if strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		t.Error("the first group header needs no blank line above it")
	}
}

func TestRowColorsAreCalm(t *testing.T) {
	from := "Alice <a@example.com>"
	senderOpen := strings.SplitN(senderStyle(from).Render("x"), "x", 2)[0]
	unread := models.Message{Subject: "s", From: from, Account: "Gmail", Date: time.Now(), Read: false}
	read := unread
	read.Read = true
	if !strings.Contains(formatListRow(&unread, 80, true, rowNormal, ""), senderOpen) {
		t.Error("an unread message keeps its sender color")
	}
	if strings.Contains(formatListRow(&read, 80, true, rowNormal, ""), senderOpen) {
		t.Error("a read message's sender is plain — no accent color")
	}
	boldOpen := strings.SplitN(styleUnread.Render("x"), "x", 2)[0]
	if !strings.Contains(formatListRow(&unread, 80, true, rowNormal, ""), boldOpen) || strings.Contains(formatListRow(&read, 80, true, rowNormal, ""), boldOpen) {
		t.Error("only unread subjects are bold")
	}
	// the time is always dim (Muted)
	dimOpen := strings.SplitN(styleMeta.Render("x"), "x", 2)[0]
	for _, msg := range []models.Message{unread, read} {
		if !strings.Contains(formatListRow(&msg, 80, true, rowNormal, ""), dimOpen+ansi.Strip(rowDate(msg.Date, "Today"))) {
			t.Errorf("the time must be dim for read=%v", msg.Read)
		}
	}
}

func TestFooterCarriesSyncAgeAndCounter(t *testing.T) {
	m := layoutModel(t, 100, 30, 40, true)
	m.lastSynced = time.Now().Add(-time.Hour)
	last := func(m Model) string {
		l := strings.Split(m.renderList(), "\n")
		return l[len(l)-1]
	}
	if f := ansi.Strip(last(m)); !strings.Contains(f, "synced 1h ago") || !strings.HasSuffix(strings.TrimRight(f, " "), "1/40") {
		t.Errorf("footer = %q, want the sync age then the 1/40 counter flush right", f)
	}
	if strings.Contains(last(m), strings.SplitN(styleSyncing.Render("x"), "x", 2)[0]+"synced") {
		t.Error("a recent sync is not amber")
	}
	m.lastSynced = time.Now().Add(-48 * time.Hour)
	if f := last(m); !strings.Contains(f, strings.SplitN(styleSyncing.Render("x"), "x", 2)[0]+"synced 2d ago") {
		t.Errorf("a sync older than 24h is amber: %q", f)
	}
	for _, w := range []int{40, 60} {
		m = layoutModel(t, w, 30, 40, true)
		m.lastSynced = time.Now().Add(-time.Hour)
		if lw := lipgloss.Width(last(m)); lw > w {
			t.Errorf("footer %d wide at %d columns", lw, w)
		}
	}
}
