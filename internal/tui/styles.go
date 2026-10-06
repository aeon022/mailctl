package tui

import (
	"hash/fnv"
	"image/color"
	"os"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/uistate"
)

// ── Styles ────────────────────────────────────────────────────────────────────

// isDarkBG is resolved once at package load — lipgloss v2 dropped
// AdaptiveColor (a static Light/Dark struct lipgloss itself resolved
// internally) in favor of a LightDarkFunc meant to be re-resolved against a
// live tea.BackgroundColorMsg on every render. This app builds all its
// styles once, at package load, same as before v2 — adaptiveColor below is
// the one-shot equivalent of what AdaptiveColor did implicitly. Same
// pattern as notectl's v2 migration.
var isDarkBG = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)

// adaptiveColor picks light or dark once, at package load — see isDarkBG.
func adaptiveColor(light, dark string) color.Color {
	if isDarkBG {
		return lipgloss.Color(dark)
	}
	return lipgloss.Color(light)
}

var (
	// palette — shared across the suite via missionctl-core/theme.
	colorBlue   = theme.BlueV2
	colorGreen  = theme.GreenV2
	colorRed    = theme.RedV2
	colorMuted  = theme.MutedV2
	colorSubtle = theme.SubtleV2
	colorAmber  = theme.AmberV2
	colorTabBg  = adaptiveColor("252", "235") // inactive tab bg

	// tab bar
	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.OnAccentV2).
			Background(colorBlue).
			Padding(0, 3)
	styleTabInact = lipgloss.NewStyle().
			Foreground(adaptiveColor("237", "252")).
			Background(colorTabBg).
			Padding(0, 3)

	// list
	styleDivider  = lipgloss.NewStyle().Foreground(colorSubtle)
	styleUnread   = lipgloss.NewStyle().Bold(true)
	styleRead     = lipgloss.NewStyle().Foreground(colorMuted)
	styleSelected = lipgloss.NewStyle().
			Background(theme.SelectedBgV2).
			Foreground(theme.SelectedFgV2).
			Bold(true)

	// detail / compose
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	styleSubject = lipgloss.NewStyle().Bold(true)
	styleMeta    = lipgloss.NewStyle().Foreground(colorMuted)
	styleLabel   = lipgloss.NewStyle().Foreground(colorBlue).Width(9)

	// status
	styleHelp      = lipgloss.NewStyle().Foreground(colorMuted)
	styleErr       = lipgloss.NewStyle().Foreground(colorRed)
	styleOK        = lipgloss.NewStyle().Foreground(colorGreen)
	styleSyncing   = lipgloss.NewStyle().Foreground(adaptiveColor("214", "220"))
	styleToday     = lipgloss.NewStyle().Foreground(adaptiveColor("214", "220")).Bold(true)
	styleDateWeek  = lipgloss.NewStyle().Foreground(colorMuted)
	styleDateMonth = lipgloss.NewStyle().Foreground(adaptiveColor("247", "242"))
	styleDateOld   = lipgloss.NewStyle().Foreground(colorSubtle)
)

// senderPalette: 8 distinct colors, avoid red/green (used for status).
// Pad sender name BEFORE applying color so ANSI codes don't break width math.
var senderPalette = []color.Color{
	adaptiveColor("25", "39"),   // blue
	adaptiveColor("91", "135"),  // purple
	adaptiveColor("30", "43"),   // teal
	adaptiveColor("130", "173"), // orange
	adaptiveColor("23", "44"),   // dark cyan
	adaptiveColor("125", "168"), // magenta
	adaptiveColor("58", "136"),  // gold
	adaptiveColor("17", "69"),   // navy
}

func senderStyle(from string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(extractEmail(from)))
	return lipgloss.NewStyle().Foreground(senderPalette[int(h.Sum32())%len(senderPalette)])
}

// ── command palette (":") ────────────────────────────────────────────────────
//
// Types out full words instead of memorizing single-key shortcuts. Reuses
// the exact same key handling every shortcut already goes through
// (updateList) by replaying the mapped keypress, so behavior is guaranteed
// identical to typing the key directly. Matching logic lives in
// missionctl-core/palette (shared across the suite); this list is
// mailctl-specific.
var paletteCommands = []palette.Command{
	{Name: "new", Desc: "New message", Key: "n"},
	{Name: "open", Desc: "Open message", Key: "enter"},
	{Name: "openapp", Desc: "Open in mail client", Key: "o"},
	{Name: "delete", Desc: "Delete (press again to confirm)", Key: "d"},
	{Name: "copy", Desc: "Copy subject + sender to clipboard", Key: "y"},
	{Name: "select", Desc: "Select mode (batch actions)", Key: "v"},
	{Name: "unread", Desc: "Toggle unread-only filter", Key: "u"},
	{Name: "sync", Desc: "Sync", Key: "s"},
	{Name: "search", Desc: "Search messages", Key: "/"},
	{Name: "help", Desc: "Show help", Key: "?"},
	{Name: "quit", Desc: "Quit mailctl", Key: "q"},
}

func New() Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = styleSyncing

	si := textinput.New()
	si.Placeholder = "search…"
	si.CharLimit = 200
	si.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	pi := textinput.New()
	pi.Placeholder = "command…"
	pi.CharLimit = 40
	pi.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	to := textinput.New()
	to.Placeholder = "to@example.com"
	to.CharLimit = 500
	to.Focus()
	to.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	sub := textinput.New()
	sub.Placeholder = "Subject"
	sub.CharLimit = 300
	sub.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	att := textinput.New()
	att.Placeholder = "/path/to/file.pdf, /path/to/other.pdf"
	att.CharLimit = 2000
	att.SetWidth(40) // v2: width 0 clips the placeholder to 1 char

	body := textarea.New()
	body.Placeholder = "Write your message here…"
	body.ShowLineNumbers = false
	body.SetHeight(10)

	var state persistedState
	uistate.Load(config.UIStatePath(), &state)

	return Model{
		sp:                    sp,
		searchInput:           si,
		paletteInput:          pi,
		toInput:               to,
		subjectInput:          sub,
		attachInput:           att,
		bodyArea:              body,
		loading:               true,
		hoverRow:              -1,
		lastClickRow:          -1,
		unreadOnly:            state.UnreadOnly,
		pendingAccountRestore: state.LastAccount,
	}
}

// persistedState is what New() restores from and the tab/filter handlers
// save to via saveUIState — see missionctl-core/uistate.
type persistedState struct {
	LastAccount string `json:"last_account"`
	UnreadOnly  bool   `json:"unread_only"`
}

func (m Model) saveUIState() {
	_ = uistate.Save(config.UIStatePath(), persistedState{
		LastAccount: m.activeAccount(),
		UnreadOnly:  m.unreadOnly,
	})
}

// motionThrottleFilter drops MouseMotionMsg messages that arrive less than
// 16ms after the last one that was let through, capping how often a
// mouse-motion event alone can trigger a full render (~60/s) — everything
// else (keys, clicks, resize, all the async load/sync messages) always
// passes through untouched. Bubble Tea calls model.View() and writes a
// render for every single message it receives regardless of whether Update
// actually changed anything, and mouse-motion mode reports every pixel of
// movement, not just cell-boundary crossings — on a fast trackpad that's
// dozens of renders a second from hovering alone. Same fix, same reasoning,
// as notectl's v2 migration (see its internal/tui/tui.go).
func motionThrottleFilter() func(tea.Model, tea.Msg) tea.Msg {
	var lastMotion time.Time
	return func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(tea.MouseMotionMsg); !ok {
			return msg
		}
		now := time.Now()
		if now.Sub(lastMotion) < 16*time.Millisecond {
			return nil
		}
		lastMotion = now
		return msg
	}
}

func Run() error {
	m := New()
	// WithFPS(30), not the 60 default: mouse-all-motion mode forces a full
	// render on every pixel of mouse motion, and 60fps of heavily-styled
	// frames can outrun what the terminal can keep up with — this is what
	// caused a severe duplicate-content rendering corruption bug in
	// notectl, fixed the same way there (WithFPS(30) + motionThrottleFilter
	// + the v1→v2 migration itself). v1's tea.WithAltScreen()/
	// WithMouseAllMotion() Program options are gone in v2 — AltScreen/
	// MouseMode are now per-View fields, set in View() below instead.
	p := tea.NewProgram(m, tea.WithFilter(motionThrottleFilter()), tea.WithFPS(30))
	_, err := p.Run()
	return err
}
