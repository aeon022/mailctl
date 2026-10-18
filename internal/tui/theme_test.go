package tui

import (
	"strings"
	"testing"

	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/tuitest"
)

func TestThemeKeyOpensPickerWithLivePreview(t *testing.T) {
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newTestModel(t)
	before := styleTabActive.GetBackground()
	mm, _ := tuitest.Send(m, tuitest.Key("T"))
	if !strings.Contains(tuitest.Text(mm), "Colour theme") {
		t.Fatalf("T opens the theme popup:\n%s", tuitest.Text(mm))
	}
	mm, _ = tuitest.Send(mm, tuitest.Key("j"))     // previews the next theme live
	mm, _ = tuitest.Send(mm, tuitest.Key("enter")) // keeps it

	if theme.Current() == orig {
		t.Fatalf("T must switch to the next theme, still %q", orig)
	}
	if styleTabActive.GetBackground() == before {
		t.Error("cached styles must be rebuilt when the theme changes")
	}
	if got := mm.(Model).status; got != "Theme: "+theme.Current() {
		t.Errorf("a short confirmation should name the theme, got %q", got)
	}
}
