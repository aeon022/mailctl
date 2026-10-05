package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/missionctl-core/theme"
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
		var rowStyle lipgloss.Style
		switch {
		case i == m.cursor:
			rowStyle = styleSelected
		case i == m.hoverRow:
			rowStyle = theme.HoverV2
		case !msg.Read:
			rowStyle = styleUnread
		default:
			rowStyle = styleRead
		}
		row := formatListRow(msg, w, showAcct, rowStyle, m.searchQ)
		if m.selecting {
			checkbox := styleMeta.Render("[ ] ")
			if m.selected[msg.ID] {
				checkbox = styleSelected.Render("[x]") + " "
			}
			row = checkbox + row
		}
		lines = append(lines, row)
		lineToMsg = append(lineToMsg, i)

		// body preview (only when body is available)
		if preview := formatPreview(msg, w, showAcct); preview != "" {
			switch {
			case i == m.cursor:
				preview = styleSelected.Width(w).Render(preview)
			case i == m.hoverRow:
				preview = theme.HoverV2.Width(w).Render(preview)
			default:
				preview = styleMeta.Render(preview)
			}
			lines = append(lines, preview)
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

// tabEntry is one visible account tab: its index into m.accounts, its
// already-styled label, and that label's rendered width.
type tabEntry struct {
	idx  int
	text string
	w    int
}

// tabWindow picks which contiguous run of accounts fits in the tab bar
// for the given width, always including the active tab — anchoring on
// activeTab and growing left then right, rather than always starting
// from account 0, is what makes the bar "scroll" as you tab through
// accounts instead of just hard-truncating the tail and hiding whichever
// accounts don't fit from index 0. Shared by renderList (drawing) and
// tabHitTest (click mapping) so the two can't disagree about which tab is
// at which column — exactly the kind of drift that caused the detail
// view's width bug earlier this session.
func (m Model) tabWindow(w int) (entries []tabEntry, hasLeft, hasRight bool) {
	if len(m.accounts) == 0 {
		return nil, false, false
	}
	widths := make([]int, len(m.accounts))
	rendered := make([]string, len(m.accounts))
	for i, a := range m.accounts {
		acctKey := a
		if i == 0 {
			acctKey = ""
		}
		label := a
		if c := m.unreadCounts[acctKey]; c > 0 {
			label = fmt.Sprintf("%s ·%d", a, c)
		}
		if i == m.activeTab {
			rendered[i] = styleTabActive.Render(label)
		} else {
			rendered[i] = styleTabInact.Render(label)
		}
		widths[i] = lipgloss.Width(rendered[i])
	}

	const sep = 2
	start := m.activeTab
	width := widths[m.activeTab]
	for start > 0 {
		cand := width + sep + widths[start-1]
		if cand > w {
			break
		}
		width = cand
		start--
	}
	end := m.activeTab + 1
	for end < len(m.accounts) {
		cand := width + sep + widths[end]
		if cand > w {
			break
		}
		width = cand
		end++
	}

	for i := start; i < end; i++ {
		entries = append(entries, tabEntry{idx: i, text: rendered[i], w: widths[i]})
	}
	return entries, start > 0, end < len(m.accounts)
}

// tabHitTest returns the account-tab index at column x on the tab bar row
// (row 1: header is row 0), or -1 if the click didn't land on a tab.
func (m Model) tabHitTest(x, y int) int {
	if y != 1 || len(m.accounts) == 0 {
		return -1
	}
	w := min(m.width, 130) - 4
	entries, hasLeft, _ := m.tabWindow(w)
	col := 0
	if hasLeft {
		col += 2 // "‹ "
	}
	for i, e := range entries {
		if i > 0 {
			col += 2 // "  " join separator
		}
		if x >= col && x < col+e.w {
			return e.idx
		}
		col += e.w
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

func formatPreview(msg *models.Message, width int, showAcct bool) string {
	// find first non-empty, non-quoted body line
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
	// indent to align with subject column
	indent := 1 + 2 + 14 + 2 + 20 + 2 // dot + date + from
	if showAcct {
		indent += 12 // badge + spaces
	}
	avail := width - indent
	if avail < 10 {
		return ""
	}
	runes := []rune(preview)
	if len(runes) > avail {
		preview = string(runes[:avail-1]) + "…"
	}
	return strings.Repeat(" ", indent) + preview
}

// formatListRow builds a message list row. rowStyle carries the read/
// unread/selected treatment (background+foreground+bold as appropriate)
// and is applied directly to every plain segment (dot, spacing, subject) —
// NOT via an outer Render() wrapping the whole composed string. That used
// to be how this worked (buildListLines wrapped the return value in
// styleRead/styleUnread/styleSelected.Render()), and it was broken:
// dateStyled/fromStyled below carry their OWN independent colors, and
// lipgloss's Render() ends every string with a full SGR reset — the FIRST
// inner segment's reset silently clobbered the outer wrap's style for
// everything after it. Confirmed empirically with a forced ANSI profile:
// the subject text (and the selected row's background) lost its intended
// styling entirely past the "from" column. Fixed by applying rowStyle
// per-segment instead, which also makes it safe to highlight fuzzy matches
// here even on the selected row (no outer wrap left to clobber).
func formatListRow(msg *models.Message, width int, showAcct bool, rowStyle lipgloss.Style, query string) string {
	dot := "○"
	if !msg.Read {
		dot = "●"
	}

	// ── date column (14 chars, pad BEFORE styling) — independently
	// colored by recency, unaffected by read/unread/selected state ──
	dateRaw := smartDate(msg.Date)
	datePadded := fmt.Sprintf("%-14s", dateRaw)
	dateStyled := coloredDate(datePadded, msg.Date)

	// ── from column (20 chars, pad BEFORE styling) — independently
	// colored per sender, unaffected by read/unread/selected state ──
	from := msg.From
	if idx := strings.Index(from, "<"); idx > 0 {
		from = strings.TrimSpace(from[:idx])
	}
	from = truncRunes(from, 20)
	fromStyled := senderStyle(msg.From).Render(padRunes(from, 20))

	// ── account badge (only in Alle tab, always 12 chars wide: [xxxxxxxx]·· ) ──
	const badgeInner = 8                  // fixed visual width of text inside brackets
	const badgeTotal = badgeInner + 2 + 2 // "[" + inner + "]" + "  "
	acctBadge := ""
	acctW := 0
	if showAcct && msg.Account != "" {
		inner := padRunes(runeLimit(acctShort(msg.Account), badgeInner), badgeInner)
		acctBadge = styleAcctBadge.Render("["+inner+"]") + rowStyle.Render("  ")
		acctW = badgeTotal
	}

	// ── subject: fill remaining width, fuzzy-highlighted ──
	// dot(1) + 2 + date(14) + 2 + from(20) + 2 + acctW + subject
	fixed := 1 + 2 + 14 + 2 + 20 + 2 + acctW
	subjectW := width - fixed
	if subjectW < 10 {
		subjectW = 10
	}
	matchIdx := fuzzyMatchIndexes(query, msg.Subject)
	subject := highlightMatches(truncRunes(msg.Subject, subjectW), matchIdx, rowStyle)

	row := rowStyle.Render(dot) + rowStyle.Render("  ") + dateStyled + rowStyle.Render("  ") +
		fromStyled + rowStyle.Render("  ") + acctBadge + subject

	// Pad to full width with rowStyle so a selected row's background spans
	// the whole line, not just up to the last character of content.
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += rowStyle.Render(strings.Repeat(" ", pad))
	}
	return row
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

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return clipboardMsg{}
	}
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
