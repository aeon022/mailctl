package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func focus(m Model) (Model, tea.Cmd) {
	tm, cmd := m.Update(tea.FocusMsg{})
	return tm.(Model), cmd
}

func idleModel(t *testing.T) Model {
	m := newTestModel(t, sample()...)
	m.loading = false
	m.lastLoad = time.Now().Add(-time.Minute)
	return m
}

func TestFocusReloadsStaleListAndDebounces(t *testing.T) {
	m := idleModel(t)
	m, cmd := focus(m)
	if cmd == nil {
		t.Fatal("focus on a stale list must reload")
	}
	if m.loading {
		t.Error("focus reload must not set loading — the list would blank out")
	}
	if _, cmd = focus(m); cmd != nil {
		t.Error("a second focus right after must not reload again (debounce)")
	}
}

func TestFocusDoesNotReloadWhenBusyOrTyping(t *testing.T) {
	cases := map[string]func(*Model){
		"compose view":   func(m *Model) { m.view = viewCompose },
		"detail view":    func(m *Model) { m.view = viewDetail },
		"help view":      func(m *Model) { m.view = viewHelp },
		"searching":      func(m *Model) { m.searching = true },
		"palette":        func(m *Model) { m.inPalette = true },
		"batch select":   func(m *Model) { m.selecting = true },
		"template pick":  func(m *Model) { m.templatePicking = true },
		"delete confirm": func(m *Model) { m.confirmID = "a" },
		"batch confirm":  func(m *Model) { m.batchConfirmDelete = true },
		"syncing":        func(m *Model) { m.syncing = true },
		"loading":        func(m *Model) { m.loading = true },
		"ai drafting":    func(m *Model) { m.aiDrafting = true },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			m := idleModel(t)
			set(&m)
			before := m.lastLoad
			m2, cmd := focus(m)
			if cmd != nil {
				t.Error("focus must not reload in this state")
			}
			if !m2.lastLoad.Equal(before) {
				t.Error("lastLoad must be untouched when no reload happens")
			}
		})
	}
}

func TestLoadedMsgStampsLastLoad(t *testing.T) {
	m := idleModel(t)
	m.lastLoad = time.Time{}
	tm, _ := m.Update(msgsLoadedMsg{msgs: sample()})
	if time.Since(tm.(Model).lastLoad) > time.Second {
		t.Error("msgsLoadedMsg must stamp lastLoad")
	}
}

func TestViewReportsFocus(t *testing.T) {
	if !idleModel(t).View().ReportFocus {
		t.Error("View must set ReportFocus or FocusMsg never arrives")
	}
}

func TestCopyUsesOSC52AndPbcopy(t *testing.T) {
	// Running a tea.Batch cmd only yields the BatchMsg; the inner commands
	// (pbcopy!) are not executed here.
	msg := copyToClipboardCmd("hello")()
	b, ok := msg.(tea.BatchMsg)
	if !ok || len(b) != 2 {
		t.Fatalf("copy cmd = %T %v, want a 2-command batch (OSC 52 + pbcopy)", msg, msg)
	}
}
