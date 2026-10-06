package tui

import (
	"fmt"
	"hash/fnv"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/missionctl-core/humanize"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/ui"
	"github.com/charmbracelet/x/ansi"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *Model) resetCompose(to, subject string) {
	m.toInput.SetValue(to)
	m.subjectInput.SetValue(subject)
	m.attachInput.SetValue("")
	m.bodyArea.SetValue("")
	m.composeFocus = focusTo
	m.toInput.Focus()
	m.subjectInput.Blur()
	m.attachInput.Blur()
	m.bodyArea.Blur()
}

func (m *Model) blurCompose(f int) {
	switch f {
	case focusTo:
		m.toInput.Blur()
	case focusSubject:
		m.subjectInput.Blur()
	case focusAttach:
		m.attachInput.Blur()
	case focusBody:
		m.bodyArea.Blur()
	}
}

func (m *Model) focusCompose(f int) {
	switch f {
	case focusTo:
		m.toInput.Focus()
	case focusSubject:
		m.subjectInput.Focus()
	case focusAttach:
		m.attachInput.Focus()
	case focusBody:
		m.bodyArea.Focus()
	}
}

// dropMessages removes sel from BOTH the visible (filtered) list and allMsgs,
// the full list search filters from. Editing only m.msgs left deleted mail in
// allMsgs: it came back when the search cleared, and with no active filter
// (msgs and allMsgs share one backing array) the in-place append corrupted
// allMsgs into duplicates.
func (m *Model) dropMessages(sel map[string]bool) {
	m.msgs = removeMessages(m.msgs, sel)
	m.allMsgs = removeMessages(m.allMsgs, sel)
}

// setRead sets the read flag of id in both the visible and the full list.
func (m *Model) setRead(id string, read bool) {
	for _, list := range [][]models.Message{m.msgs, m.allMsgs} {
		for i := range list {
			if list[i].ID == id {
				list[i].Read = read
			}
		}
	}
}

func (m *Model) setStatus(s string) {
	m.status = s
	m.statusTime = time.Now()
}

// detailBodyHeight calculates how many lines the viewport can use.
// Includes the detail view's own vertical padding so callers can pass the
// raw window height directly — this used to be the caller's job (only
// renderDetail did it, so the viewport.Model built at WindowSizeMsg time
// carried a Height 2 rows taller than what's actually displayed, and
// PgUp/PgDown — which scroll by vp.Height — overshot by that much on every
// press since that persisted Height, not renderDetail's per-render-only
// copy, is what viewport.Update() actually scrolls by).
func (m Model) detailBodyHeight() int {
	// subject(1) + from(1) + to(1) + date(1) + account(1) + divider(1)
	// + gap-before-footer(1) + footer-divider(1) + help(1) = 9
	h := m.height - detailPadV*2 - 9
	if h < 5 {
		h = 5
	}
	return h
}

// buildListLines pre-renders all message rows (with date group headers and body
// previews) and returns them as a flat string slice plus the visual index of cursor.
func (m Model) buildListLines(w int) ([]string, int) {
	lines, cursorLine, _ := m.buildListLinesWithMapping(w)
	return lines, cursorLine
}

// buildListLinesWithMapping is buildListLines plus a parallel lineToMsg
// slice (msg index for a main-row or preview line, -1 for a group-header
// line), so rowHitTest can map a clicked screen line back to a message
// without re-deriving this layout itself.
func (m Model) buildListLinesWithMapping(w int) ([]string, int, []int) {
	showAcct := m.activeTab == 0
	var lines []string
	var lineToMsg []int
	cursorLine := 0
	lastGroup := ""

	for i := range m.msgs {
		msg := &m.msgs[i]

		// date group header
		group := dateGroup(msg.Date)
		if group != lastGroup {
			lines = append(lines, renderGroupHeader(group, w))
			lineToMsg = append(lineToMsg, -1)
			lastGroup = group
		}

		if i == m.cursor {
			cursorLine = len(lines)
		}

		// main row
		state := rowNormal
		switch {
		case i == m.cursor:
			state = rowSelected
		case i == m.hoverRow:
			state = rowHover
		}
		rowW, prefix := w, ""
		if m.selecting {
			rowW = w - 4 // the checkbox column keeps the whole line within w
			prefix = styleMeta.Render("[ ] ")
			if m.selected[msg.ID] {
				prefix = styleSelected.Render("[x]") + " "
			}
		}
		lines = append(lines, prefix+formatListRow(msg, rowW, showAcct, state, m.searchQ))
		lineToMsg = append(lineToMsg, i)

		// body preview (only when body is available) — part of the same
		// selection/hover bar as its message row
		if preview := formatPreview(msg, rowW, showAcct); preview != "" {
			lines = append(lines, prefix2(prefix)+paintRow(rowW, state, preview))
			lineToMsg = append(lineToMsg, i)
		}
	}
	return lines, cursorLine, lineToMsg
}

// listStartY returns the number of preamble lines above the message list —
// header, tab bar, divider, optional filter chips, optional search input —
// shared by renderList (to size the list) and rowHitTest (to locate it) so
// the two can't drift apart.
func (m Model) listStartY() int {
	y := 3 // header + tab bar + divider
	if m.unreadOnly || m.searchQ != "" {
		y++
	}
	if m.selecting {
		y++
	}
	if m.searching {
		y += 2
	}
	if m.inPalette {
		y += 8
	}
	return y
}

// tabHit is the column span [x, x+w) of one visible account tab.
type tabHit struct{ idx, x, w int }

// syncSuffix is the "syncing…"/"synced 2m ago" text at the right end of the
// tab bar, and tabBudget the columns left for the tabs themselves — shared by
// renderList and tabHitTest so what is drawn and what is clickable can't drift.
func (m Model) syncSuffix() string {
	if m.syncing {
		return "  " + m.sp.View() + styleSyncing.Render(" syncing…")
	}
	if !m.lastSynced.IsZero() {
		return "  " + styleMeta.Render("synced "+humanize.TimeAgo(m.lastSynced))
	}
	return ""
}

func (m Model) tabBudget() int { return min(m.width, 130) - 4 - lipgloss.Width(m.syncSuffix()) }

// tabBar draws the account tabs with ui.Tabs (active pill, others dimmed,
// unread count after the name) and works out where each visible tab sits, by
// finding its " label count " cell in the plain text — ui.Tabs only drops tabs
// from the ends, so the visible ones are contiguous around the active tab.
func (m Model) tabBar(w int) (string, []tabHit) {
	if len(m.accounts) == 0 {
		return "", nil
	}
	counts := make([]int, len(m.accounts))
	for i := range m.accounts {
		key := m.accounts[i]
		if i == 0 {
			key = ""
		}
		counts[i] = m.unreadCounts[key]
	}
	bar := ui.Tabs(w, m.accounts, m.activeTab, counts)
	plain := ansi.Strip(bar)
	seg := func(i int) string {
		if counts[i] > 0 {
			return fmt.Sprintf(" %s %d ", m.accounts[i], counts[i])
		}
		return " " + m.accounts[i] + " "
	}
	col := func(byteIdx int) int { return runewidth.StringWidth(plain[:byteIdx]) }

	at := strings.Index(plain, seg(m.activeTab))
	if at < 0 {
		return bar, nil
	}
	hits := []tabHit{{m.activeTab, col(at), runewidth.StringWidth(seg(m.activeTab))}}
	for end, j := at+len(seg(m.activeTab)), m.activeTab+1; j < len(m.accounts); j++ { // right neighbours
		sg := seg(j)
		if !strings.HasPrefix(plain[end:], " "+sg) {
			break
		}
		hits = append(hits, tabHit{j, col(end + 1), runewidth.StringWidth(sg)})
		end += 1 + len(sg)
	}
	for start, j := at, m.activeTab-1; j >= 0; j-- { // left neighbours
		sg := seg(j)
		if !strings.HasSuffix(plain[:start], sg+" ") {
			break
		}
		start -= 1 + len(sg)
		hits = append(hits, tabHit{j, col(start), runewidth.StringWidth(sg)})
	}
	return bar, hits
}

// tabHitTest returns the account-tab index at column x on the tab bar row
// (row 1: header is row 0), or -1 if the click didn't land on a tab.
func (m Model) tabHitTest(x, y int) int {
	if y != 1 || len(m.accounts) == 0 {
		return -1
	}
	_, hits := m.tabBar(m.tabBudget())
	for _, h := range hits {
		if x >= h.x && x < h.x+h.w {
			return h.idx
		}
	}
	return -1
}

// rowHitTest returns the message index at screen row y, or -1 if the click
// landed on a group header, preview-only gap, or outside the list. Mirrors
// buildListLinesWithMapping's line layout and renderList's scroll window
// (start := cursorLine - listH + 1 once the cursor scrolls past view).
func (m Model) rowHitTest(y int) int {
	idx := y - m.listStartY()
	if idx < 0 || len(m.msgs) == 0 {
		return -1
	}
	w := min(m.width, 130)
	_, cursorLine, lineToMsg := m.buildListLinesWithMapping(w)
	listH := m.height - m.listStartY() - 2
	if listH < 1 {
		listH = 1
	}
	start := 0
	if cursorLine >= listH {
		start = cursorLine - listH + 1
	}
	lineIdx := start + idx
	if lineIdx >= len(lineToMsg) {
		return -1
	}
	return lineToMsg[lineIdx]
}

func dateGroup(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Today"
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	case t.After(now.AddDate(0, 0, -7)):
		return t.Format("Monday")
	case t.After(now.AddDate(0, 0, -14)):
		return "Last week"
	case t.Year() == now.Year():
		return t.Format("January")
	default:
		return t.Format("January 2006")
	}
}

func renderGroupHeader(group string, width int) string {
	label := " " + group + " "
	dashes := strings.Repeat("─", max(0, width-2-len(label)))
	return styleDivider.Render("──" + label + dashes)
}

// rowState is how a list row is drawn: plain, under the mouse, or selected.
type rowState int

const (
	rowNormal rowState = iota
	rowHover
	rowSelected
)

var styleSelectedText = lipgloss.NewStyle().Bold(true).Foreground(theme.SelectedFgV2)

// paintRow puts a finished (possibly multi-colored) row line on one
// continuous background: ui.Row's accent bar + selection background for the
// selected row, the hover background for a hovered one, a plain 2-column
// gutter otherwise. Inner colors survive — the old per-segment approach left
// holes in the selection wherever a column had its own color.
func paintRow(width int, state rowState, content string) string {
	switch state {
	case rowSelected:
		return ui.Row(width, true, content)
	case rowHover:
		return paintBackground("  "+ansi.Truncate(content, max(width-2, 0), "…"), width, theme.HoverV2)
	}
	return ui.Row(width, false, content)
}

// paintBackground pads text to width and paints st's background behind it,
// re-applying it after every reset inside text.
func paintBackground(text string, width int, st lipgloss.Style) string {
	probe := st.Render("\x00")
	i := strings.Index(probe, "\x00")
	if i < 0 {
		return text
	}
	pre, post := probe[:i], probe[i+1:]
	text += strings.Repeat(" ", max(width-lipgloss.Width(text), 0))
	text = strings.ReplaceAll(text, "\x1b[0m", "\x1b[0m"+pre)
	text = strings.ReplaceAll(text, "\x1b[m", "\x1b[m"+pre)
	return pre + text + post
}

// prefix2 pads the checkbox prefix of a preview line to the same width.
func prefix2(prefix string) string { return strings.Repeat(" ", lipgloss.Width(prefix)) }

// weekdayGroup reports whether a date-group header is a bare weekday name
// ("Monday") — dateGroup's label for 2–6 days ago.
func weekdayGroup(group string) bool {
	switch group {
	case "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday":
		return true
	}
	return false
}

// rowDate is the date text of a list row: only what the group header above it
// doesn't already say. Under Today/Yesterday/a weekday that is just the time;
// "Last week" adds the weekday, older groups the day and month.
func rowDate(t time.Time, group string) string {
	switch {
	case group == "Today", group == "Yesterday", weekdayGroup(group):
		return t.Format("15:04")
	case group == "Last week":
		return t.Format("Mon 15:04")
	}
	return t.Format("Jan 02 15:04")
}

// rowDateW is the (per-group, so columns stay aligned within a group) width
// of rowDate's output.
func rowDateW(group string) int {
	switch {
	case group == "Today", group == "Yesterday", weekdayGroup(group):
		return 5
	case group == "Last week":
		return 9
	}
	return 12
}

const (
	fromW = 20
	acctW = 10 // "● " + 8-column short name
)

// rowIndent is the number of columns before the subject: gutter(2) + dot(1)
// + space + date + 2 + from + 2 (+ account column).
func rowIndent(msg *models.Message, showAcct bool) int {
	n := 2 + 1 + 1 + rowDateW(dateGroup(msg.Date)) + 2 + fromW + 2
	if showAcct && msg.Account != "" {
		n += acctW + 2
	}
	return n
}

// acctStyle gives each account its own stable color (same palette as senders).
func acctStyle(name string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return lipgloss.NewStyle().Foreground(senderPalette[int(h.Sum32())%len(senderPalette)])
}

// formatPreview is the body-preview line under a message: the first
// non-quoted body line, indented to the subject column (minus the 2-column
// gutter paintRow adds). "" when there is no body or no room.
func formatPreview(msg *models.Message, width int, showAcct bool) string {
	preview := ""
	for _, line := range strings.Split(msg.Body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, ">") && line != "--" {
			preview = line
			break
		}
	}
	if preview == "" {
		return ""
	}
	indent := rowIndent(msg, showAcct) - 2
	avail := width - 2 - indent
	if avail < 10 {
		return ""
	}
	return styleMeta.Render(strings.Repeat(" ", indent) + truncRunes(preview, avail))
}

// formatListRow builds a message list row exactly width columns wide:
// gutter, read-dot, date, sender, account, subject. The segments carry their
// own colors; the selected/hover background is laid over the finished line by
// paintRow rather than applied per segment — a per-segment background left
// holes between the independently colored date/sender/account columns, and an
// outer wrap would be clobbered by their inner resets.
func formatListRow(msg *models.Message, width int, showAcct bool, state rowState, query string) string {
	base, dot := styleRead, "○"
	if !msg.Read {
		base, dot = styleUnread, "●"
	}
	if state == rowSelected {
		base = styleSelectedText
	}
	group := dateGroup(msg.Date)
	dateStyled := coloredDate(padRunes(rowDate(msg.Date, group), rowDateW(group)), msg.Date)

	from := msg.From
	if idx := strings.Index(from, "<"); idx > 0 {
		from = strings.TrimSpace(from[:idx])
	}
	fromStyled := senderStyle(msg.From).Render(padRunes(truncRunes(from, fromW), fromW))

	acct := ""
	if showAcct && msg.Account != "" {
		name := padRunes(runeLimit(acctShort(msg.Account), acctW-2), acctW-2)
		acct = acctStyle(msg.Account).Render("● ") + styleMeta.Render(name) + "  "
	}

	subjectW := max(width-rowIndent(msg, showAcct), 10)
	subject := highlightMatches(truncRunes(msg.Subject, subjectW), fuzzyMatchIndexes(query, msg.Subject), base)

	content := base.Render(dot) + " " + dateStyled + "  " + fromStyled + "  " + acct + subject
	return paintRow(width, state, content)
}

// stripVariationSelectors removes characters whose display width is
// ambiguous or undefined across terminals/fonts, rather than trying to
// measure and wrap around them correctly: U+FE0E/FE0F (emoji variation
// selectors — a real width mismatch already caused a scrollbar-alignment
// bug, see the regression test) and U+FFFC (the object-replacement
// character HTML mail leaves behind for an inline image/attachment once
// stripped to plain text — it has no meaningful width or content once
// there's no image to show, and different terminals render it at
// different widths, which can misalign a terminal's own cursor tracking
// on lines further down the same frame).
//
// It also normalizes U+2028/U+2029 (line/paragraph separator) to '\n'.
// Apple Mail's HTML-to-plaintext conversion uses these for soft line
// breaks inside a paragraph — live-reproduced in a real Coinbase price
// alert full of them. formatDetail only splits on '\n', so a line
// carrying an embedded U+2028 was one row to the app's row-accounting
// (go-runewidth measures it as width 0) but could force an actual line
// break when the terminal renders it, desyncing the scrollbar/viewport
// from everything drawn after the first occurrence — exactly why it only
// showed up in long, multi-paragraph emails. Converting them to '\n' up
// front makes every row the app counts match a row the terminal draws.
func stripVariationSelectors(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\uFE0E', '\uFE0F', '\uFFFC':
			return -1
		case '\u2028', '\u2029':
			return '\n'
		}
		return r
	}, s)
}

func formatDetail(msg *models.Message, width int) string {
	body := stripVariationSelectors(strings.TrimSpace(msg.Body))
	if body == "" {
		return styleMeta.Render("(no body)")
	}
	// Wrap width matches vp.Width exactly (both derive from detailRawWidth()-2),
	// so a paragraph line that reaches the wrap boundary fills the viewport with
	// zero slack before the scrollbar glyph. Some individual glyphs render a
	// hair wider on-screen than Go's width libraries count them (font
	// rendering, not a measurable logical-width bug — confirmed live: the
	// ambiguous-width character theory checked out mathematically and still
	// didn't fix a real, reproduced break). A real blank-column margin absorbs
	// that regardless of which glyph or why, instead of chasing it per-character.
	w := min(width-5, 128) // leave room for scrollbar track + margin

	var lines []string
	for _, l := range strings.Split(body, "\n") {
		lines = append(lines, wrapByWidth(l, w)...)
	}
	return strings.Join(lines, "\n")
}

// wrapByWidth breaks s into lines of at most w display columns, cutting on
// rune boundaries. formatDetail used to slice by byte length (l[:w]), which
// corrupts a line the moment a multi-byte UTF-8 character (any German
// umlaut, an emoji) straddles the cut point — the resulting invalid UTF-8
// throws off that line's measured width and visually breaks the
// scrollbar's glyph column for the rest of the message.
func wrapByWidth(s string, w int) []string {
	if w <= 0 || runewidth.StringWidth(s) <= w {
		return []string{s}
	}
	var lines []string
	runes := []rune(s)
	for len(runes) > 0 {
		cut := len(runes)
		width := 0
		for i, r := range runes {
			rw := runewidth.RuneWidth(r)
			if width+rw > w {
				cut = i
				break
			}
			width += rw
		}
		if cut == 0 {
			cut = 1 // a single rune wider than w: take it anyway, don't loop forever
		}
		lines = append(lines, string(runes[:cut]))
		runes = runes[cut:]
	}
	return lines
}

func buildQuote(msg *models.Message) string {
	if msg == nil {
		return ""
	}
	header := fmt.Sprintf("\n\n— On %s, %s wrote:\n",
		msg.Date.Format("Mon, 02 Jan 2006 15:04"), msg.From)
	if msg.Body == "" {
		return header
	}
	var quoted []string
	for _, l := range strings.Split(strings.TrimSpace(msg.Body), "\n") {
		quoted = append(quoted, "> "+l)
	}
	return header + strings.Join(quoted, "\n")
}

// smartDate returns a compact context-aware date string.
func smartDate(t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return "Today   " + t.Format("15:04")
	case t.After(now.AddDate(0, 0, -6)):
		return t.Format("Mon     15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 02  15:04")
	default:
		return t.Format("Jan 02   2006")
	}
}

func coloredDate(s string, t time.Time) string {
	now := time.Now()
	switch {
	case sameDay(t, now):
		return styleToday.Render(s)
	case t.After(now.AddDate(0, 0, -7)):
		return styleDateWeek.Render(s)
	case t.After(now.AddDate(0, 0, -30)):
		return styleDateMonth.Render(s)
	default:
		return styleDateOld.Render(s)
	}
}

// copyToClipboardCmd copies via OSC 52 (works over SSH/tmux) and pbcopy
// (for terminals that ignore OSC 52, e.g. Terminal.app).
func copyToClipboardCmd(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return clipboardMsg{}
	})
}

const focusReloadAfter = 5 * time.Second

// browsingIdle reports whether the user is just looking at the message list:
// no compose/detail/help view, search, palette, batch selection, template
// picker, pending delete confirmation, or work in flight.
func (m Model) browsingIdle() bool {
	return m.view == viewList && !m.searching && !m.inPalette && !m.selecting &&
		!m.templatePicking && m.confirmID == "" && !m.batchConfirmDelete &&
		!m.syncing && !m.loading && !m.aiDrafting
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// acctShort returns a short label for an account name.
func acctShort(name string) string {
	words := strings.Fields(name)
	var short string
	switch {
	case len(words) == 0:
		short = name
	case len(words) == 1:
		short = name
	default:
		// "Gerwin @ Brücke" → "Brücke", "FH Burgenland" → "Burgenland"
		short = words[len(words)-1]
	}
	return runeLimit(short, 8)
}

// runeLimit truncates s to at most n visible characters (rune-aware).
// truncRunes truncates s to at most n runes, appending "…" if it had to cut
// (the ellipsis itself counts toward n). Rune-safe, unlike raw byte slicing.
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

// padRunes right-pads s with spaces to n runes. Assumes s already fits
// within n runes (callers truncate first); no-ops otherwise.
func padRunes(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(r))
}

// fuzzyMatchIndexes returns the rune indexes within s that q fuzzy-matched,
// or nil if q is empty or doesn't match at all.
func fuzzyMatchIndexes(q, s string) []int {
	if q == "" {
		return nil
	}
	matches := fuzzy.Find(q, []string{s})
	if len(matches) == 0 {
		return nil
	}
	return matches[0].MatchedIndexes
}

// highlightMatches renders s with the rune positions in idxs (from
// fuzzyMatchIndexes) styled via a warm, underlined variant of base, and
// every other character via base itself — fzf-style match highlighting.
//
// Renders one character at a time rather than nesting a highlighted span
// inside a single outer Render() call: lipgloss's Render() ends every
// string with a full SGR reset, so an inner Render() call's reset would
// wipe out the outer style for everything after the first highlighted
// character. Per-character rendering keeps every segment self-contained.
//
// idxs are indexes into s BEFORE any truncation — callers must resolve
// indexes against the same, untruncated string used to compute them.
func highlightMatches(s string, idxs []int, base lipgloss.Style) string {
	if len(idxs) == 0 {
		return base.Render(s)
	}
	hi := base.Foreground(colorAmber).Underline(true)
	matchSet := make(map[int]bool, len(idxs))
	for _, i := range idxs {
		matchSet[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if matchSet[i] {
			b.WriteString(hi.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}

func runeLimit(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// extractEmail pulls "addr@host" from "Name <addr@host>" or returns as-is.
func extractEmail(s string) string {
	if start := strings.Index(s, "<"); start >= 0 {
		if end := strings.Index(s, ">"); end > start {
			return s[start+1 : end]
		}
	}
	return strings.TrimSpace(s)
}

// findUnsubscribeURL scans an email body for a URL near an "unsubscribe" keyword.
func findUnsubscribeURL(body string) string {
	lower := strings.ToLower(body)
	idx := strings.Index(lower, "unsubscribe")
	if idx < 0 {
		return ""
	}
	// search for https:// within ±500 chars of "unsubscribe"
	start := max(0, idx-300)
	end := idx + 500
	if end > len(body) {
		end = len(body)
	}
	window := body[start:end]
	// find https:// link
	hi := strings.Index(window, "https://")
	if hi < 0 {
		hi = strings.Index(window, "http://")
	}
	if hi < 0 {
		return ""
	}
	url := window[hi:]
	// cut at first whitespace, angle bracket, quote, or newline
	for i, r := range url {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' ||
			r == '<' || r == '>' || r == '"' || r == '\'' || r == ')' {
			url = url[:i]
			break
		}
	}
	if strings.Contains(strings.ToLower(url), "unsubscribe") || strings.Contains(strings.ToLower(window[:hi]), "unsubscribe") {
		return url
	}
	return ""
}

// openURLCmd opens a URL in the default browser (macOS).
func openURLCmd(url string) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("open", url).Start()
		return nil
	}
}
