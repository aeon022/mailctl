package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
)

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
