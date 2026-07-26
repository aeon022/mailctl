package tui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aeon022/mailctl/internal/models"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
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
	vp := viewport.New(20, 5)
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
		col := len([]rune(l)) - 1
		if glyphCol == -1 {
			glyphCol = col
			continue
		}
		if col != glyphCol {
			t.Errorf("line %d: glyph at column %d, want %d (same as other lines) — scrollbar not vertically aligned: %q", i, col, glyphCol, l)
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
	vp := viewport.New(20, 4)
	body := stripVariationSelectors("plain line one\n🏖️ Urlaub\nplain line three\nplain line four\nplain line five\nplain line six")
	vp.SetContent(body)

	out := renderScrollbar(vp)
	lines := strings.Split(out, "\n")

	glyphCol := -1
	for i, l := range lines {
		col := len([]rune(l)) - 1
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

func TestHelpOverlay_OpenScrollClose(t *testing.T) {
	m := Model{width: 100, height: 30}

	mi, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = mi.(Model)
	if m.view != viewHelp {
		t.Fatalf("expected viewHelp after '?', got %v", m.view)
	}
	if m.helpVP.TotalLineCount() == 0 {
		t.Fatal("expected help content to be populated")
	}

	before := m.helpVP.ScrollPercent()
	for i := 0; i < 5; i++ {
		mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = mi.(Model)
	}
	if m.helpVP.ScrollPercent() <= before {
		t.Errorf("expected scroll to advance after pressing j, stayed at %v", before)
	}

	mi, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
	// Regression test: formatListRow used to be wrapped in a single outer
	// styleRead/styleUnread/styleSelected.Render() call at the caller. The
	// date and from columns carry their OWN independent colors, and each
	// one's Render() ends with a full SGR reset — which clobbered the
	// outer style for everything after it (confirmed with a forced ANSI
	// profile: the subject text lost its bold/muted/selected styling
	// entirely). formatListRow now applies rowStyle per-segment instead;
	// verify rowStyle's own escape code reappears AFTER the from column.
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	msg := models.Message{Subject: "hello", From: "Alice <a@example.com>", Date: time.Now(), Read: false}
	row := formatListRow(&msg, 60, false, styleUnread, "")

	openCode := strings.SplitN(styleUnread.Render("x"), "x", 2)[0]
	fromIdx := strings.Index(row, "Alice")
	if fromIdx == -1 {
		t.Fatal("expected to find the sender name in the row")
	}
	if !strings.Contains(row[fromIdx:], openCode) {
		t.Error("expected rowStyle's escape code to reappear after the from column — styling was likely clobbered by an inner reset")
	}
}

func TestFormatListRow_SelectedBackgroundSpansFullWidth(t *testing.T) {
	// Regression test for the same bug: a selected row's background must
	// fill the entire row width, not just up to wherever the first inner
	// reset clobbered it.
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	msg := models.Message{Subject: "hi", From: "a@example.com", Date: time.Now(), Read: true}
	row := formatListRow(&msg, 60, false, styleSelected, "")
	if lipgloss.Width(row) != 60 {
		t.Errorf("expected the rendered row to be exactly 60 columns wide, got %d", lipgloss.Width(row))
	}

	openCode := strings.SplitN(styleSelected.Render("x"), "x", 2)[0]
	// Find the LAST styled segment and confirm only whitespace (the
	// trailing padding) follows it before the final reset — rather than an
	// arbitrary fixed-size tail slice, which risks cutting mid-escape-
	// sequence and losing the leading "\x1b[" that openCode starts with.
	lastOpen := strings.LastIndex(row, openCode)
	if lastOpen == -1 {
		t.Fatal("expected to find the selected style's escape code in the row at all")
	}
	after := strings.TrimSuffix(row[lastOpen+len(openCode):], "\x1b[0m")
	if after == "" {
		t.Error("expected trailing padding spaces after the last styled segment")
	}
	if strings.TrimSpace(after) != "" {
		t.Errorf("expected only whitespace (padding) after the last styled segment, got %q", after)
	}
}

func TestHighlightMatches_ColorsOnlyMatchedRunes(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

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
