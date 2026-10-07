package tui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/tuitest"
	"github.com/charmbracelet/x/ansi"
	runewidth "github.com/mattn/go-runewidth"
)

func TestFormatDetail_WrapsOnRuneBoundaryNotByte(t *testing.T) {
	// Regression test: formatDetail used to wrap long lines with l[:w], a
	// byte-length slice. Placing a 2-byte UTF-8 character (a German umlaut,
	// extremely common in real mail bodies) so the cut point lands inside
	// its encoding corrupts the line into invalid UTF-8, which throws off
	// that line's measured width and breaks the detail view's scrollbar
	// alignment for every line after it.
	w := 20
	body := strings.Repeat("a", w-1) + "ä" + strings.Repeat("b", 30)
	msg := &models.Message{Body: body}

	out := formatDetail(msg, w+2) // formatDetail uses width-2 as its wrap width
	for _, line := range strings.Split(out, "\n") {
		if !utf8.ValidString(line) {
			t.Errorf("formatDetail produced invalid UTF-8 in line %q", line)
		}
	}
}

func TestRenderScrollbarAlignsGlyphColumn(t *testing.T) {
	vp := viewport.New(viewport.WithWidth(20), viewport.WithHeight(5))
	// Content with very different line lengths, and more lines than the
	// viewport height so the scrollbar thumb/track actually renders.
	vp.SetContent("a\nbb\nccccccccccccccccc\nd\nee\nfff\ng")

	out := renderScrollbar(vp)
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		t.Fatal("expected rendered lines, got none")
	}

	glyphCol := -1
	for i, l := range lines {
		// The glyph is the last rune of each rendered line (track "│" or
		// thumb "┃", both single-width). Its rune column should be
		// identical across every line regardless of that line's own text
		// length — a mismatch means the glyph isn't forming a straight bar.
		// ansi.Strip, not a raw rune count: lipgloss v2 (unlike v1 under a
		// non-tty test run) always emits real ANSI codes, and colors of
		// different numeric width (e.g. "38;5;33" vs "38;5;244") change the
		// raw string's rune length without changing anything visible.
		col := len([]rune(ansi.Strip(l))) - 1
		if glyphCol == -1 {
			glyphCol = col
			continue
		}
		if col != glyphCol {
			t.Errorf("line %d: glyph at column %d, want %d (same as other lines) — scrollbar not vertically aligned: %q", i, col, glyphCol, l)
		}
	}
}

func TestRenderScrollbarLineAtExactWidthStillHasGapBeforeGlyph(t *testing.T) {
	// Regression test: a content line that reaches exactly vp.Width used to
	// get no separating space before the scrollbar glyph. The old
	// implementation joined content and glyphs via lipgloss.JoinHorizontal,
	// which pads short lines up to the widest line ACTUALLY present in the
	// block — a line that IS that widest line gets no padding at all,
	// gluing the glyph directly onto its last character (looked like
	// "...Unternehme┃" in a real long line that happened to hit the wrap
	// width exactly). renderScrollbar now pads against vp.Width itself
	// instead of the tallest line's incidental width.
	vp := viewport.New(viewport.WithWidth(10), viewport.WithHeight(3))
	vp.SetContent(strings.Repeat("x", 10) + "\nshort\n" + strings.Repeat("y", 10) + "\nmore\nlines\nhere\nto force scrolling")

	out := renderScrollbar(vp)
	for i, l := range strings.Split(out, "\n") {
		r := []rune(ansi.Strip(l))
		if len(r) < 2 {
			continue
		}
		if beforeGlyph := r[len(r)-2]; beforeGlyph != ' ' {
			t.Errorf("line %d: expected a space before the scrollbar glyph, got %q (full line: %q)", i, string(beforeGlyph), l)
		}
	}
}

func TestRenderScrollbarAlignsGlyphColumn_VariationSelectorEmoji(t *testing.T) {
	// Regression test mirroring notectl's fix: an emoji + U+FE0F variation
	// selector (e.g. from a real email body) can be measured a different
	// width than it actually renders, throwing one line's scrollbar glyph
	// out of alignment with the rest. formatDetail strips the selector via
	// stripVariationSelectors before content ever reaches the viewport, so
	// exercise that same normalization here.
	vp := viewport.New(viewport.WithWidth(20), viewport.WithHeight(4))
	body := stripVariationSelectors("plain line one\n🏖️ Urlaub\nplain line three\nplain line four\nplain line five\nplain line six")
	vp.SetContent(body)

	out := renderScrollbar(vp)
	lines := strings.Split(out, "\n")

	glyphCol := -1
	for i, l := range lines {
		col := len([]rune(ansi.Strip(l))) - 1
		if glyphCol == -1 {
			glyphCol = col
			continue
		}
		if col != glyphCol {
			t.Errorf("line %d (%q): glyph at column %d, want %d", i, l, col, glyphCol)
		}
	}
}

func TestStripVariationSelectorsAgreeOnWidth(t *testing.T) {
	in := "🏖️ Urlaub"
	got := stripVariationSelectors(in)
	if strings.ContainsRune(got, '️') || strings.ContainsRune(got, '︎') {
		t.Errorf("stripVariationSelectors(%q) = %q, still contains a variation selector", in, got)
	}
	if lipgloss.Width(got) != runewidth.StringWidth(got) {
		t.Errorf("after stripping, lipgloss.Width(%q)=%d and runewidth.StringWidth=%d should agree, but don't", got, lipgloss.Width(got), runewidth.StringWidth(got))
	}
}

// TestStripVariationSelectorsNormalizesLineSeparators is a regression test
// for a real, live-reproduced bug: Apple Mail's HTML-to-plaintext
// conversion left U+2028 (LINE SEPARATOR) embedded mid-paragraph in a real
// Coinbase price-alert email, inside text formatDetail treats as a single
// '\n'-delimited line. go-runewidth measures U+2028 as width 0, but it can
// force an actual line break when a terminal renders it — desyncing the
// app's row count (and the scrollbar drawn against it) from what the
// terminal actually draws, for every row after the first occurrence. Only
// showed up in long, multi-paragraph emails, which is exactly where Apple
// Mail needs internal soft-wraps. Normalizing to '\n' up front removes the
// hidden control character from the stream entirely.
func TestStripVariationSelectorsNormalizesLineSeparators(t *testing.T) {
	in := "one\u2028two\u2029three"
	got := stripVariationSelectors(in)
	if strings.ContainsRune(got, '\u2028') || strings.ContainsRune(got, '\u2029') {
		t.Errorf("stripVariationSelectors(%q) = %q, still contains a line/paragraph separator", in, got)
	}
	if want := "one\ntwo\nthree"; got != want {
		t.Errorf("stripVariationSelectors(%q) = %q, want %q", in, got, want)
	}
}

func TestCommandPalette_TypeFilterAndExecute(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30

	mi, _ := m.Update(tea.KeyPressMsg{Text: ":", Code: ':'})
	m = mi.(Model)
	if !m.inPalette {
		t.Fatal("expected inPalette after ':'")
	}

	for _, r := range "un" {
		mi, _ = m.Update(tea.KeyPressMsg{Text: string(r), Code: r})
		m = mi.(Model)
	}
	matches := palette.Match(paletteCommands, m.paletteInput.Value())
	if len(matches) == 0 || matches[0].Name != "unread" {
		t.Fatalf("expected 'unread' to be the top match for query %q, got %v", m.paletteInput.Value(), matches)
	}

	before := m.unreadOnly
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(Model)
	if m.inPalette {
		t.Error("expected palette to close after executing a command")
	}
	if m.unreadOnly == before {
		t.Error("expected 'unread' command to replay 'u' and flip unreadOnly")
	}
}

func TestCommandPalette_EscCloses(t *testing.T) {
	m := New()
	m.width, m.height = 100, 30
	mi, _ := m.Update(tea.KeyPressMsg{Text: ":", Code: ':'})
	m = mi.(Model)

	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(Model)
	if m.inPalette {
		t.Error("expected esc to close the palette")
	}
}

func TestHelpOverlay_OpenScrollClose(t *testing.T) {
	m := Model{width: 100, height: 30}

	mi, _ := m.Update(tea.KeyPressMsg{Text: "?", Code: '?'})
	m = mi.(Model)
	if m.view != viewHelp {
		t.Fatalf("expected viewHelp after '?', got %v", m.view)
	}
	if m.helpVP.TotalLineCount() == 0 {
		t.Fatal("expected help content to be populated")
	}

	before := m.helpVP.ScrollPercent()
	for i := 0; i < 5; i++ {
		mi, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
		m = mi.(Model)
	}
	if m.helpVP.ScrollPercent() <= before {
		t.Errorf("expected scroll to advance after pressing j, stayed at %v", before)
	}

	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = mi.(Model)
	if m.view != viewList {
		t.Errorf("expected esc to close help back to viewList, got %v", m.view)
	}
}

func TestHelpOverlay_FitsWithinBackgroundHeight(t *testing.T) {
	m := Model{width: 100, height: 30}
	m = m.openHelp()
	bgLines := len(strings.Split(m.renderList(), "\n"))
	if m.helpPopH > bgLines {
		t.Errorf("popup height %d exceeds background height %d", m.helpPopH, bgLines)
	}
}

func TestFilterMsgs_FuzzyMatchesSubjectOrFrom(t *testing.T) {
	msgs := []models.Message{
		{Subject: "budgetctl release", From: "team@example.com"},
		{Subject: "unrelated", From: "budgetctl-bot@example.com"},
		{Subject: "also unrelated", From: "nobody@example.com"},
	}
	got := filterMsgs(msgs, "bgt")
	if len(got) != 2 {
		t.Errorf("expected fuzzy 'bgt' to match subject OR from, got %d: %+v", len(got), got)
	}
}

func TestFilterMsgs_PreservesOriginalOrder(t *testing.T) {
	msgs := []models.Message{
		{Subject: "Zebra budgetctl", Date: mustParseMailDate(t, "2026-07-03")},
		{Subject: "Abudgetctl", Date: mustParseMailDate(t, "2026-07-02")},
		{Subject: "budgetctl", Date: mustParseMailDate(t, "2026-07-01")},
	}
	got := filterMsgs(msgs, "budgetctl")
	if len(got) != 3 || got[0].Subject != "Zebra budgetctl" || got[1].Subject != "Abudgetctl" || got[2].Subject != "budgetctl" {
		t.Errorf("expected original date-descending order preserved (no re-ranking by match quality), got %+v", got)
	}
}

func TestFilterMsgs_EmptyQueryReturnsAllUnfiltered(t *testing.T) {
	msgs := []models.Message{{Subject: "a"}, {Subject: "b"}}
	got := filterMsgs(msgs, "")
	if len(got) != 2 {
		t.Errorf("expected empty query to return all messages, got %d", len(got))
	}
}

func mustParseMailDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestFormatListRow_StyleSurvivesPastTheDateAndFromColumns(t *testing.T) {
	// Regression test: the date and from columns carry their OWN colors and
	// each ends with a full SGR reset, which clobbered an outer style for
	// everything after them (the subject lost its bold/muted styling).
	// Segments are styled individually; verify the row style's escape code
	// reappears AFTER the from column.
	msg := models.Message{Subject: "hello", From: "Alice <a@example.com>", Date: time.Now(), Read: false}
	row := formatListRow(&msg, 60, false, rowNormal, "")

	openCode := strings.SplitN(styleUnread.Render("x"), "x", 2)[0]
	fromIdx := strings.Index(row, "Alice")
	if fromIdx == -1 {
		t.Fatal("expected to find the sender name in the row")
	}
	if !strings.Contains(row[fromIdx:], openCode) {
		t.Error("expected rowStyle's escape code to reappear after the from column — styling was likely clobbered by an inner reset")
	}
}

func TestFormatListRow_SelectedIsOneContinuousBar(t *testing.T) {
	// The selected row used to get its background per segment, leaving holes
	// between the date, sender, account and subject columns (each has its own
	// color). Now the whole line sits on ONE background: full width, accent
	// bar first, and after every inner reset the background is painted again.
	msg := models.Message{Subject: "hi", From: "Alice <a@example.com>", Account: "Brücke", Date: time.Now(), Read: false}
	row := formatListRow(&msg, 70, true, rowSelected, "")
	if lipgloss.Width(row) != 70 {
		t.Errorf("expected the rendered row to be exactly 70 columns wide, got %d", lipgloss.Width(row))
	}
	if !strings.HasPrefix(ansi.Strip(row), "▌") {
		t.Errorf("selected row must start with the accent bar, got %q", ansi.Strip(row))
	}
	bgOpen := strings.SplitN(lipgloss.NewStyle().Background(theme.SelectedBgV2).Render("x"), "x", 2)[0]
	after := strings.Index(row, "▌") + len("▌")
	body := row[after:]
	resets := 0
	for i := 0; ; {
		j := strings.Index(body[i:], "\x1b[m")
		if j < 0 {
			break
		}
		i += j + len("\x1b[m")
		resets++
		if i < len(body) && !strings.HasPrefix(body[i:], bgOpen) {
			t.Fatalf("hole in the selection: after a reset the background is not re-applied: %q", body[i:min(i+30, len(body))])
		}
	}
	if resets < 3 {
		t.Errorf("expected several inner resets (date/sender/account columns), got %d — test no longer exercises the hole case", resets)
	}
}

func TestFormatListRow_HoverIsContinuousToo(t *testing.T) {
	// unread: the sender carries its own color, so there is an inner reset to paint over
	msg := models.Message{Subject: "hi", From: "Alice <a@example.com>", Date: time.Now(), Read: false}
	row := formatListRow(&msg, 60, false, rowHover, "")
	if lipgloss.Width(row) != 60 || strings.Contains(ansi.Strip(row), "▌") {
		t.Errorf("hover: width %d, plain %q (no accent bar expected)", lipgloss.Width(row), ansi.Strip(row))
	}
	hoverOpen := strings.SplitN(theme.HoverV2.Render("x"), "x", 2)[0]
	fromIdx := strings.Index(row, "Alice")
	if !strings.Contains(row[fromIdx:], hoverOpen) {
		t.Error("hover background must continue past the sender column")
	}
}

func TestRowDate_OnlyWhatTheGroupHeaderDoesNotSay(t *testing.T) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 10, 41, 0, 0, time.Local)
	if got := rowDate(today, "Today"); got != "10:41" {
		t.Errorf("Today group shows only the time, got %q", got)
	}
	if got := rowDate(today, "Yesterday"); got != "10:41" {
		t.Errorf("Yesterday group shows only the time, got %q", got)
	}
	if got := rowDate(today, "Monday"); got != "10:41" {
		t.Errorf("a weekday group shows only the time, got %q", got)
	}
	if got := rowDate(today, "Last week"); got != today.Format("Mon")+" 10:41" {
		t.Errorf("Last week adds the weekday, got %q", got)
	}
	if got := rowDate(today, "March"); got != today.Format("Jan 02")+" 10:41" {
		t.Errorf("older groups show day+month, got %q", got)
	}
	// the word "Today" must no longer be repeated inside a Today row
	msg := models.Message{Subject: "s", From: "a@example.com", Date: today, Read: true}
	if row := ansi.Strip(formatListRow(&msg, 80, false, rowNormal, "")); strings.Contains(row, "Today") || !strings.Contains(row, "10:41") {
		t.Errorf("row under the Today header: %q", row)
	}
	for _, g := range []string{"Today", "Yesterday", "Monday", "Last week", "March"} {
		if rowDateW(g) < len(rowDate(today, g)) {
			t.Errorf("column width for %q (%d) is narrower than its text %q", g, rowDateW(g), rowDate(today, g))
		}
	}
}

func TestFormatListRow_AccountTagIsJustADot(t *testing.T) {
	msg := models.Message{Subject: "hi", From: "a@example.com", Account: "Gerwin || Die Brücke", Date: time.Now(), Read: false}
	row := ansi.Strip(formatListRow(&msg, 90, true, rowNormal, ""))
	if strings.Contains(row, "[") || strings.Contains(row, "]") {
		t.Errorf("no brackets around the account: %q", row)
	}
	// calmer rows: the account is just its colored dot — the name moved to the
	// Message preview panel
	if strings.Contains(row, "Brücke") || strings.Count(row, "●") < 2 {
		t.Errorf("expected the unread dot plus the account dot and no account name, got %q", row)
	}
	if lipgloss.Width(row) != 90 {
		t.Errorf("width %d", lipgloss.Width(row))
	}
	// distinct accounts get stable, usually distinct colors
	if acctStyle("A").Render("x") != acctStyle("A").Render("x") {
		t.Error("account color must be stable")
	}
}

func TestPreviewLineSharesTheSelectionBar(t *testing.T) {
	m := newTestModel(t, models.Message{ID: "1", Subject: "s", From: "a@x.com", Date: time.Now(), Read: true, Body: "hello preview text here"})
	mi, _ := tuitest.Send(m, tuitest.Resize(100, 20))
	lines, cursorLine := mi.(Model).buildListLines(100)
	if len(lines) < cursorLine+2 {
		t.Fatalf("expected a preview line under the message row, got %d lines", len(lines))
	}
	if !strings.HasPrefix(ansi.Strip(lines[cursorLine+1]), "▌") || !strings.Contains(ansi.Strip(lines[cursorLine+1]), "hello preview") {
		t.Errorf("selected message's preview line must carry the bar too: %q", ansi.Strip(lines[cursorLine+1]))
	}
	for i, l := range lines {
		if lipgloss.Width(l) > 100 {
			t.Errorf("line %d is %d wide", i, lipgloss.Width(l))
		}
	}
}

func TestHighlightMatches_ColorsOnlyMatchedRunes(t *testing.T) {
	idxs := fuzzyMatchIndexes("bgt", "budgetctl")
	if idxs == nil {
		t.Fatal("expected 'bgt' to fuzzy-match 'budgetctl'")
	}
	base := lipgloss.NewStyle()
	out := highlightMatches("budgetctl", idxs, base)
	if out == base.Render("budgetctl") {
		t.Error("expected highlightMatches to differ from a plain render for a real match")
	}
}

// Regression: in Bubble Tea v2 a space key press stringifies as "space", not
// " " — a leftover `case " "` silently never matched. Drives real v2 key
// presses through batch-select mode (v starts it, j moves, space toggles).
func TestSpaceKey_TogglesBatchSelection(t *testing.T) {
	m := New()
	m.msgs = []models.Message{{ID: "a", Subject: "one"}, {ID: "b", Subject: "two"}}
	m.cursor = 0

	press := func(k tea.KeyPressMsg) {
		tm, _ := m.Update(k)
		m = tm.(Model)
	}
	press(tea.KeyPressMsg{Text: "v", Code: 'v'})
	press(tea.KeyPressMsg{Text: "j", Code: 'j'})
	if !m.selecting || !m.selected["a"] || m.selected["b"] {
		t.Fatalf("setup: selecting=%v selected=%v", m.selecting, m.selected)
	}

	press(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if !m.selected["b"] {
		t.Fatalf("space did not select the cursor row: %v", m.selected)
	}
	press(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.selected["b"] {
		t.Errorf("second space did not deselect: %v", m.selected)
	}
}
