package tui

import (
	"context"
	"fmt"
	"hash/fnv"
	"os/exec"
	"strings"
	"time"

	"github.com/aeon022/mailctl/internal/ai"
	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/aeon022/mailctl/internal/templates"
	"github.com/aeon022/missionctl-core/humanize"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/lastsync"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/theme"
	"github.com/aeon022/missionctl-core/uistate"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"
)

// ── Views ─────────────────────────────────────────────────────────────────────

type view int

const (
	viewList    view = iota
	viewDetail  view = iota
	viewCompose view = iota
	viewHelp    view = iota
)

const (
	focusTo      = 0
	focusSubject = 1
	focusAttach  = 2
	focusBody    = 3
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	// palette — shared across the suite via missionctl-core/theme.
	colorBlue   = theme.Blue
	colorGreen  = theme.Green
	colorRed    = theme.Red
	colorMuted  = theme.Muted
	colorSubtle = theme.Subtle
	colorAmber  = theme.Amber
	colorTabBg  = lipgloss.AdaptiveColor{Light: "252", Dark: "235"} // inactive tab bg

	// tab bar
	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(theme.OnAccent).
			Background(colorBlue).
			Padding(0, 3)
	styleTabInact = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "237", Dark: "252"}).
			Background(colorTabBg).
			Padding(0, 3)

	// list
	styleDivider  = lipgloss.NewStyle().Foreground(colorSubtle)
	styleUnread   = lipgloss.NewStyle().Bold(true)
	styleRead     = lipgloss.NewStyle().Foreground(colorMuted)
	styleSelected = lipgloss.NewStyle().
			Background(theme.SelectedBg).
			Foreground(theme.SelectedFg).
			Bold(true)
	styleAcctBadge = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "75"})

	// detail / compose
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	styleSubject = lipgloss.NewStyle().Bold(true)
	styleMeta    = lipgloss.NewStyle().Foreground(colorMuted)
	styleLabel   = lipgloss.NewStyle().Foreground(colorBlue).Width(9)

	// status
	styleHelp      = lipgloss.NewStyle().Foreground(colorMuted)
	styleErr       = lipgloss.NewStyle().Foreground(colorRed)
	styleOK        = lipgloss.NewStyle().Foreground(colorGreen)
	styleSyncing   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "214", Dark: "220"})
	styleToday     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "214", Dark: "220"}).Bold(true)
	styleDateWeek  = lipgloss.NewStyle().Foreground(colorMuted)
	styleDateMonth = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "247", Dark: "242"})
	styleDateOld   = lipgloss.NewStyle().Foreground(colorSubtle)
)

// senderPalette: 8 distinct colors, avoid red/green (used for status).
// Pad sender name BEFORE applying color so ANSI codes don't break width math.
var senderPalette = []lipgloss.AdaptiveColor{
	{Light: "25", Dark: "39"},   // blue
	{Light: "91", Dark: "135"},  // purple
	{Light: "30", Dark: "43"},   // teal
	{Light: "130", Dark: "173"}, // orange
	{Light: "23", Dark: "44"},   // dark cyan
	{Light: "125", Dark: "168"}, // magenta
	{Light: "58", Dark: "136"},  // gold
	{Light: "17", Dark: "69"},   // navy
}

func senderStyle(from string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(extractEmail(from)))
	return lipgloss.NewStyle().Foreground(senderPalette[int(h.Sum32())%len(senderPalette)])
}

// ── Messages ──────────────────────────────────────────────────────────────────

type msgsLoadedMsg struct {
	msgs         []models.Message
	accounts     []string
	unreadCounts map[string]int
}
type syncDoneMsg struct {
	count    int
	accounts []string
	err      error
}
type sentMsg struct{ err error }
type draftedMsg struct{ err error }
type aiDraftMsg struct{ body string }
type aiDraftErrMsg struct{ err error }
type errMsg struct{ err error }
type bodyLoadedMsg struct {
	body string
	err  error
}
type readMarkedMsg struct{}
type unreadMarkedMsg struct{}
type deletedMsg struct{ err error }
type openedMsg struct{}
type clipboardMsg struct{}

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	view   view
	width  int
	height int

	// list
	msgs         []models.Message // filtered (by searchQ) view of allMsgs
	allMsgs      []models.Message // everything loaded for the current unread/account scope
	cursor       int
	hoverRow     int // m.msgs index under the mouse cursor, -1 when none
	lastClickRow int // m.msgs index of the previous left-click, -1 when none — double-click opens the message detail, same window/pattern taskctl uses
	lastClickAt  time.Time
	unreadOnly   bool
	searchQ      string
	searching    bool
	searchInput  textinput.Model
	// ":" command palette
	inPalette     bool
	paletteInput  textinput.Model
	paletteCursor int

	// batch select mode ("v") — bulk mark-read / bulk-delete, same pattern
	// taskctl's own select mode uses.
	selecting          bool
	selected           map[string]bool // keyed by message ID
	batchConfirmDelete bool            // "d" once arms it, "d" again executes — mirrors the single-message m.confirmID press-twice pattern

	// tabs
	accounts     []string // ["Alle", "iCloud", ...]
	activeTab    int      // 0 = Alle
	unreadCounts map[string]int

	// pendingAccountRestore holds the persisted last-active account name
	// (see uistate) until m.accounts first loads, at which point it's
	// resolved to an index and cleared — same one-shot-until-data-arrives
	// pattern as timectl/taskctl/notectl's own cross-tool "jump to X" flows.
	pendingAccountRestore string

	// detail
	detail *models.Message
	vp     viewport.Model

	// compose
	toInput      textinput.Model
	subjectInput textinput.Model
	attachInput  textinput.Model
	bodyArea     textarea.Model
	composeFocus int
	replyTo      *models.Message

	// template picker (ctrl+t while composing)
	templatePicking bool
	templateNames   []string
	templateCursor  int

	// status
	status     string
	statusTime time.Time
	err        error
	syncing    bool
	lastSynced time.Time // zero = never synced this install
	sp         spinner.Model
	aiDrafting bool
	confirmID  string
	loading    bool

	// "?" transient help popup
	helpVP   viewport.Model
	helpPopW int
	helpPopH int
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
	{Name: "openapp", Desc: "Open in Mail.app", Key: "o"},
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

	pi := textinput.New()
	pi.Placeholder = "command…"
	pi.CharLimit = 40

	to := textinput.New()
	to.Placeholder = "to@example.com"
	to.CharLimit = 500
	to.Focus()

	sub := textinput.New()
	sub.Placeholder = "Subject"
	sub.CharLimit = 300

	att := textinput.New()
	att.Placeholder = "/path/to/file.pdf, /path/to/other.pdf"
	att.CharLimit = 2000

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

func Run() error {
	m := New()
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	_, err := p.Run()
	return err
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadMsgsCmd(m.unreadOnly, m.pendingAccountRestore), tea.WindowSize(), m.sp.Tick, loadLastSyncedCmd())
}

type lastSyncedLoadedMsg struct{ t time.Time }

func loadLastSyncedCmd() tea.Cmd {
	return func() tea.Msg {
		t, _ := lastsync.Load(config.LastSyncedPath())
		return lastSyncedLoadedMsg{t: t}
	}
}

func (m Model) activeAccount() string {
	if m.activeTab == 0 || m.activeTab >= len(m.accounts) {
		return ""
	}
	return m.accounts[m.activeTab]
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Match the width/height renderDetail actually displays with (see
		// detailRawWidth/detailBodyHeight) — this is what PgUp/PgDown
		// scroll by via vp.Update(), so it has to agree with what's really
		// on screen or scrolling overshoots what the viewport shows per page.
		m.vp = viewport.New(m.detailRawWidth()-2, m.detailBodyHeight())
		m.bodyArea.SetWidth(msg.Width - 12)
		m.bodyArea.SetHeight(m.height - 12)

	case msgsLoadedMsg:
		m.loading = false
		m.allMsgs = msg.msgs
		m.msgs = filterMsgs(m.allMsgs, m.searchQ)
		if len(msg.accounts) > 0 {
			m.accounts = append([]string{"Alle"}, msg.accounts...)
		}
		if m.pendingAccountRestore != "" {
			for i, a := range m.accounts {
				if a == m.pendingAccountRestore {
					m.activeTab = i
					break
				}
			}
			m.pendingAccountRestore = ""
		}
		if m.cursor >= len(m.msgs) {
			m.cursor = max(0, len(m.msgs)-1)
		}
		if msg.unreadCounts != nil {
			m.unreadCounts = msg.unreadCounts
		}

	case lastSyncedLoadedMsg:
		m.lastSynced = msg.t

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			if len(msg.accounts) > 0 {
				m.accounts = append([]string{"Alle"}, msg.accounts...)
			}
			m.setStatus(fmt.Sprintf("Synced %d messages", msg.count))
			m.lastSynced = time.Now()
			_ = lastsync.Save(config.LastSyncedPath(), m.lastSynced)
			// reload with active account filter to preserve tab
			return m, loadMsgsCmd(m.unreadOnly, m.activeAccount())
		}

	case bodyLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else if m.detail != nil {
			m.detail.Body = msg.body
			m.vp.SetContent(formatDetail(m.detail, m.detailRawWidth()))
		}

	case readMarkedMsg:
		// local state already updated optimistically

	case unreadMarkedMsg:
		// local state already updated optimistically

	case deletedMsg:
		if msg.err != nil {
			m.err = msg.err
		}

	case sentMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.setStatus("Sent!")
			m.view = viewList
		}

	case draftedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.setStatus("Saved to Drafts")
			m.view = viewList
		}

	case aiDraftMsg:
		m.aiDrafting = false
		if m.detail != nil {
			replySubject := m.detail.Subject
			if !strings.HasPrefix(replySubject, "Re: ") {
				replySubject = "Re: " + replySubject
			}
			m.replyTo = m.detail
			m.resetCompose(extractEmail(m.detail.From), replySubject)
			m.bodyArea.SetValue(msg.body)
			m.setStatus("AI drafted a reply — review and ctrl+s to send")
			m.view = viewCompose
		}
		return m, nil

	case aiDraftErrMsg:
		m.aiDrafting = false
		m.setStatus("AI error: " + msg.err.Error())
		return m, nil

	case errMsg:
		m.err = msg.err

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.view == viewDetail {
				m.vp.LineUp(3)
			} else if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseButtonWheelDown:
			if m.view == viewDetail {
				m.vp.LineDown(3)
			} else if m.cursor < len(m.msgs)-1 {
				m.cursor++
			}
		case tea.MouseButtonLeft:
			if msg.Action != tea.MouseActionPress || m.view != viewList {
				return m, nil
			}
			if i := m.tabHitTest(msg.X, msg.Y); i >= 0 {
				if i != m.activeTab {
					m.activeTab = i
					m.cursor = 0
					return m, loadMsgsCmd(m.unreadOnly, m.activeAccount())
				}
				return m, nil
			}
			if i := m.rowHitTest(msg.Y); i >= 0 {
				now := time.Now()
				if i == m.lastClickRow && now.Sub(m.lastClickAt) < doubleClickWindow {
					m.cursor = i
					m.lastClickRow = -1 // consumed, so a third click starts fresh
					msgv := m.msgs[i]
					m.detail = &msgv
					if !msgv.Read {
						m.msgs[i].Read = true
						m.detail.Read = true
					}
					m.vp.SetContent("Loading body…")
					m.vp.GotoTop()
					m.view = viewDetail
					return m, tea.Batch(loadBodyCmd(&msgv), markReadCmd(msgv.ID))
				}
				m.cursor = i
				m.lastClickRow = i
				m.lastClickAt = now
			}
		case tea.MouseButtonNone:
			if msg.Action == tea.MouseActionMotion && m.view == viewList {
				m.hoverRow = m.rowHitTest(msg.Y)
			}
		}
		return m, nil

	case clipboardMsg:
		// no-op; status already set

	case spinner.TickMsg:
		if m.syncing || m.aiDrafting || m.loading {
			var cmd tea.Cmd
			m.sp, cmd = m.sp.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		m.err = nil
		if time.Since(m.statusTime) > 3*time.Second {
			m.status = ""
		}
		switch m.view {
		case viewList:
			return m.updateList(msg)
		case viewDetail:
			return m.updateDetail(msg)
		case viewCompose:
			return m.updateCompose(msg)
		case viewHelp:
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "q", "esc", "?":
				m.view = viewList
				return m, nil
			}
			var cmd tea.Cmd
			m.helpVP, cmd = m.helpVP.Update(msg)
			return m, cmd
		}
	}

	if m.view == viewDetail {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.inPalette {
		closePalette := func(mm Model) Model {
			mm.inPalette = false
			mm.paletteInput.Blur()
			mm.paletteInput.SetValue("")
			mm.paletteCursor = 0
			return mm
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return closePalette(m), nil
		case "up", "ctrl+p":
			if m.paletteCursor > 0 {
				m.paletteCursor--
			}
			return m, nil
		case "down", "ctrl+n":
			matches := palette.Match(paletteCommands, m.paletteInput.Value())
			if m.paletteCursor < len(matches)-1 {
				m.paletteCursor++
			}
			return m, nil
		case "enter":
			matches := palette.Match(paletteCommands, m.paletteInput.Value())
			if len(matches) == 0 {
				return closePalette(m), nil
			}
			if m.paletteCursor >= len(matches) {
				m.paletteCursor = len(matches) - 1
			}
			chosen := matches[m.paletteCursor]
			m = closePalette(m)
			replay := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(chosen.Key)}
			if chosen.Key == "enter" {
				replay = tea.KeyMsg{Type: tea.KeyEnter}
			}
			return m.updateList(replay)
		}
		var cmd tea.Cmd
		m.paletteInput, cmd = m.paletteInput.Update(msg)
		m.paletteCursor = 0
		return m, cmd
	}

	if m.searching {
		switch msg.String() {
		case "enter":
			// Filtering already happened live as the user typed (below) —
			// enter just closes the input box, no DB round-trip needed.
			m.searching = false
			m.cursor = 0
		case "esc":
			m.searching = false
			m.searchInput.SetValue("")
			m.searchQ = ""
			m.cursor = 0
			m.msgs = filterMsgs(m.allMsgs, "")
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.searchQ = m.searchInput.Value()
			m.cursor = 0
			m.msgs = filterMsgs(m.allMsgs, m.searchQ)
			return m, cmd
		}
		return m, nil
	}

	if m.selecting {
		switch msg.String() {
		case "esc":
			m.selecting = false
			m.selected = nil
			m.batchConfirmDelete = false
		case "up", "k":
			m.batchConfirmDelete = false
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			m.batchConfirmDelete = false
			if m.cursor < len(m.msgs)-1 {
				m.cursor++
			}
		case " ":
			m.batchConfirmDelete = false
			if len(m.msgs) > 0 {
				id := m.msgs[m.cursor].ID
				if m.selected[id] {
					delete(m.selected, id)
				} else {
					m.selected[id] = true
				}
			}
		case "A":
			m.batchConfirmDelete = false
			for _, msg := range m.msgs {
				m.selected[msg.ID] = true
			}
		case "r":
			if len(m.selected) == 0 {
				break
			}
			ids := selectedIDs(m.selected)
			for i := range m.msgs {
				if m.selected[m.msgs[i].ID] {
					m.msgs[i].Read = true
				}
			}
			m.selecting = false
			m.selected = nil
			m.setStatus(fmt.Sprintf("Marked %d read", len(ids)))
			return m, batchMarkReadCmd(ids)
		case "d":
			if len(m.selected) == 0 {
				break
			}
			if !m.batchConfirmDelete {
				m.batchConfirmDelete = true
				m.setStatus(fmt.Sprintf("Press d again to delete %d message(s)  esc to cancel", len(m.selected)))
				return m, nil
			}
			ids := selectedIDs(m.selected)
			m.msgs = removeMessages(m.msgs, m.selected)
			if m.cursor >= len(m.msgs) {
				m.cursor = max(0, len(m.msgs)-1)
			}
			m.selecting = false
			m.selected = nil
			m.batchConfirmDelete = false
			m.setStatus(fmt.Sprintf("Deleted %d message(s)", len(ids)))
			return m, batchDeleteCmd(ids)
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		if len(m.accounts) > 0 {
			m.activeTab = (m.activeTab + 1) % len(m.accounts)
			m.cursor = 0
			m.saveUIState()
			return m, loadMsgsCmd(m.unreadOnly, m.activeAccount())
		}
	case "shift+tab":
		if len(m.accounts) > 0 {
			m.activeTab = (m.activeTab - 1 + len(m.accounts)) % len(m.accounts)
			m.cursor = 0
			m.saveUIState()
			return m, loadMsgsCmd(m.unreadOnly, m.activeAccount())
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// jump to the nth visible (on-screen) message, date-group headers
		// not counted — mirrors rowHitTest's own scroll-window math
		// (buildListLinesWithMapping + the same start-line calc) so a
		// digit lands on the same message a click at that position would.
		n := int(msg.String()[0] - '0')
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
		count := 0
		for _, msgIdx := range lineToMsg[start:] {
			if msgIdx < 0 {
				continue
			}
			count++
			if count == n {
				m.cursor = msgIdx
				break
			}
		}
	case "j", "down":
		if m.cursor < len(m.msgs)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "pgdown", "ctrl+f":
		page := max(1, m.height/3)
		m.cursor = min(len(m.msgs)-1, m.cursor+page)
	case "pgup", "ctrl+b":
		page := max(1, m.height/3)
		m.cursor = max(0, m.cursor-page)
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(0, len(m.msgs)-1)
	case "enter":
		if len(m.msgs) > 0 {
			msg := m.msgs[m.cursor]
			m.detail = &msg
			// optimistic mark-read
			if !msg.Read {
				m.msgs[m.cursor].Read = true
				m.detail.Read = true
			}
			m.vp.SetContent("Loading body…")
			m.vp.GotoTop()
			m.view = viewDetail
			return m, tea.Batch(loadBodyCmd(&msg), markReadCmd(msg.ID))
		}
	case "o":
		if len(m.msgs) > 0 {
			return m, openInMailCmd(m.msgs[m.cursor].ID)
		}
	case "d":
		if len(m.msgs) > 0 {
			id := m.msgs[m.cursor].ID
			if m.confirmID == id {
				m.confirmID = ""
				m.msgs = append(m.msgs[:m.cursor], m.msgs[m.cursor+1:]...)
				if m.cursor >= len(m.msgs) {
					m.cursor = max(0, len(m.msgs)-1)
				}
				m.setStatus("Deleted")
				return m, deleteCmd(id)
			}
			m.confirmID = id
			m.setStatus("Press d again to confirm delete  esc to cancel")
			return m, nil
		}
	case "y":
		if len(m.msgs) > 0 {
			msg := &m.msgs[m.cursor]
			m.setStatus("Copied to clipboard")
			return m, copyToClipboardCmd(msg.Subject + " — " + msg.From)
		}
	case "v":
		if len(m.msgs) > 0 {
			m.selecting = true
			m.selected = map[string]bool{m.msgs[m.cursor].ID: true}
		}
	case "n":
		m.replyTo = nil
		m.resetCompose("", "")
		m.view = viewCompose
	case "s":
		if !m.syncing {
			m.syncing = true
			m.setStatus("Syncing…")
			return m, tea.Batch(syncCmd(), m.sp.Tick)
		}
	case "u":
		m.unreadOnly = !m.unreadOnly
		m.cursor = 0
		m.saveUIState()
		return m, loadMsgsCmd(m.unreadOnly, m.activeAccount())
	case ":":
		m.inPalette = true
		m.paletteCursor = 0
		m.paletteInput.SetValue("")
		return m, m.paletteInput.Focus()
	case "/":
		m.searching = true
		m.searchInput.Focus()
		m.searchInput.SetValue("")
	case "?":
		m = m.openHelp()
	case "esc":
		if m.confirmID != "" {
			m.confirmID = ""
			m.status = ""
			return m, nil
		}
		if m.searchQ != "" {
			m.searchQ = ""
			m.searchInput.SetValue("")
			m.cursor = 0
			m.msgs = filterMsgs(m.allMsgs, "")
		}
	}
	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		if m.confirmID != "" {
			m.confirmID = ""
			m.status = ""
			return m, nil
		}
		m.view = viewList
		m.detail = nil
		return m, nil
	case "o":
		if m.detail != nil {
			return m, openInMailCmd(m.detail.ID)
		}
	case "u":
		if m.detail != nil {
			m.detail.Read = false
			// reflect in list
			for i := range m.msgs {
				if m.msgs[i].ID == m.detail.ID {
					m.msgs[i].Read = false
					break
				}
			}
			return m, markUnreadCmd(m.detail.ID)
		}
	case "U":
		if m.detail != nil {
			if link := findUnsubscribeURL(m.detail.Body); link != "" {
				m.setStatus("Opening unsubscribe link…")
				return m, openURLCmd(link)
			}
			m.setStatus("No unsubscribe link found in this email")
		}
	case "y":
		if m.detail != nil {
			m.setStatus("Copied to clipboard")
			return m, copyToClipboardCmd(m.detail.Subject + " — " + m.detail.From)
		}
	case "d":
		if m.detail != nil {
			id := m.detail.ID
			if m.confirmID == id {
				m.confirmID = ""
				for i := range m.msgs {
					if m.msgs[i].ID == id {
						m.msgs = append(m.msgs[:i], m.msgs[i+1:]...)
						if m.cursor >= len(m.msgs) {
							m.cursor = max(0, len(m.msgs)-1)
						}
						break
					}
				}
				m.detail = nil
				m.view = viewList
				m.setStatus("Deleted")
				return m, deleteCmd(id)
			}
			m.confirmID = id
			m.setStatus("Press d again to confirm delete  esc to cancel")
			return m, nil
		}
	case "r":
		if m.detail != nil {
			m.replyTo = m.detail
			replySubject := m.detail.Subject
			if !strings.HasPrefix(replySubject, "Re: ") {
				replySubject = "Re: " + replySubject
			}
			quote := buildQuote(m.detail)
			m.resetCompose(extractEmail(m.detail.From), replySubject)
			m.bodyArea.SetValue(quote)
			m.view = viewCompose
			return m, nil
		}
	case "a":
		if m.detail != nil && !m.aiDrafting {
			if !config.IsPro() {
				m.setStatus("AI draft reply is a missionctl Bundle feature — see missionctl.sh/#pricing")
				return m, nil
			}
			m.aiDrafting = true
			m.setStatus("Drafting a reply…")
			detail := m.detail
			return m, tea.Batch(m.sp.Tick, func() tea.Msg {
				body, err := ai.Draft(detail.Subject, detail.Body)
				if err != nil {
					return aiDraftErrMsg{err}
				}
				return aiDraftMsg{body}
			})
		}
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m Model) updateCompose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.templatePicking {
		switch msg.String() {
		case "esc", "ctrl+t":
			m.templatePicking = false
		case "j", "down":
			if m.templateCursor < len(m.templateNames)-1 {
				m.templateCursor++
			}
		case "k", "up":
			if m.templateCursor > 0 {
				m.templateCursor--
			}
		case "enter":
			m.templatePicking = false
			if m.templateCursor < len(m.templateNames) {
				if d, err := templates.Load(m.templateNames[m.templateCursor]); err == nil {
					m.subjectInput.SetValue(d.Subject)
					m.bodyArea.SetValue(d.Body)
				}
			}
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+s":
		return m, sendCmd(m.toInput.Value(), m.subjectInput.Value(), m.bodyArea.Value(), parseAttachments(m.attachInput.Value()))
	case "ctrl+d":
		return m, draftCmd(m.toInput.Value(), m.subjectInput.Value(), m.bodyArea.Value(), parseAttachments(m.attachInput.Value()))
	case "ctrl+t":
		names, _ := templates.List()
		m.templatePicking = true
		m.templateNames = names
		m.templateCursor = 0
		return m, nil
	case "esc":
		m.view = viewList
		return m, nil
	case "tab":
		if m.composeFocus < focusBody {
			m.blurCompose(m.composeFocus)
			m.composeFocus++
			m.focusCompose(m.composeFocus)
		}
		return m, nil
	case "shift+tab":
		if m.composeFocus > focusTo {
			m.blurCompose(m.composeFocus)
			m.composeFocus--
			m.focusCompose(m.composeFocus)
		}
		return m, nil
	}

	var cmd tea.Cmd
	switch m.composeFocus {
	case focusTo:
		m.toInput, cmd = m.toInput.Update(msg)
	case focusSubject:
		m.subjectInput, cmd = m.subjectInput.Update(msg)
	case focusAttach:
		m.attachInput, cmd = m.attachInput.Update(msg)
	case focusBody:
		m.bodyArea, cmd = m.bodyArea.Update(msg)
	}
	return m, cmd
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	switch m.view {
	case viewDetail:
		return m.renderDetail()
	case viewCompose:
		if m.templatePicking {
			return overlay.Center(m.renderCompose(), m.renderTemplatePicker(), m.width, m.height, 0)
		}
		return m.renderCompose()
	case viewHelp:
		// "?" is only reachable from the main list, so the list is always
		// the correct background to keep visible behind the popup. No
		// enclosing border on the list view, so inset 0 is safe.
		return overlay.Center(m.renderList(), m.renderHelpPopup(), m.width, m.height, 0)
	default:
		return m.renderList()
	}
}

func (m Model) helpContent() string {
	return keymap.New("mailctl", "email from the terminal").
		Section("Navigation").
		Row("j / k", "move down / up").
		Row("g / G", "jump to top / bottom").
		Row("pgdn/up", "page down / up").
		Row("tab", "next account").
		Row("s-tab", "previous account").
		Section("Messages").
		Row("enter", "open message").
		Row("n", "new message").
		Row("o", "open in Mail.app").
		Row("d", "delete (asks to confirm)").
		Row("y", "copy subject + sender to clipboard").
		Section("Other").
		Row("u", "toggle unread-only filter").
		Row("s", "sync").
		Row("/", "search (esc clears)").
		Row(":", "command palette — type an action by name").
		Row("?", "toggle this help").
		Row("q", "quit").
		String()
}

// openHelp sizes and populates the transient help popup (see
// renderHelpPopup/overlay.Center) from the ACTUAL rendered background
// height, not the terminal size.
func (m Model) openHelp() Model {
	bgLines := strings.Split(m.renderList(), "\n")

	safeH := max(6, len(bgLines))
	popH := min(safeH, 22)
	popW := min(70, m.width)
	if popW < 40 {
		popW = 40
	}

	vp := viewport.New(popW-6, popH-5) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for footer
	vp.SetContent(m.helpContent())

	m.helpVP = vp
	m.helpPopW = popW
	m.helpPopH = popH
	m.view = viewHelp
	return m
}

// renderHelpPopup renders the help viewport in a bordered box, meant to be
// composited over the list view via overlay.Center rather than replacing
// the whole screen — the list stays visible around it.
func (m Model) renderHelpPopup() string {
	footer := "esc / ?  close"
	if m.helpVP.TotalLineCount() > m.helpVP.Height {
		footer = fmt.Sprintf("j/k scroll (%d%%)  ·  %s", int(m.helpVP.ScrollPercent()*100), footer)
	}
	body := m.helpVP.View() + "\n" + styleMeta.Render(footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2).
		Width(m.helpPopW).
		Render(body)
}

func (m Model) renderList() string {
	w := min(m.width, 130)
	var b strings.Builder

	// ── app header ──
	appName := styleHeader.Render("mailctl")
	dateStr := styleMeta.Render(time.Now().Format("Mon, 02 Jan 2006"))
	pad := w - lipgloss.Width(appName) - lipgloss.Width(dateStr)
	if pad < 1 {
		pad = 1
	}
	b.WriteString(appName + strings.Repeat(" ", pad) + dateStr + "\n")

	// ── account tab bar ──
	if len(m.accounts) > 0 {
		// Reserve the syncing indicator's own room BEFORE handing tabWindow
		// its budget — otherwise it competes with tab entries for the same
		// width and MaxWidth's truncation (a last-resort safety net, not
		// meant to be relied on) can cut it off right when you'd want to
		// see it confirm a sync actually started.
		syncSuffix := ""
		if m.syncing {
			syncSuffix = "  " + m.sp.View() + styleSyncing.Render(" syncing…")
		} else if !m.lastSynced.IsZero() {
			syncSuffix = "  " + styleMeta.Render("synced "+humanize.TimeAgo(m.lastSynced))
		}
		entries, hasLeft, hasRight := m.tabWindow(w - 4 - lipgloss.Width(syncSuffix))
		var sb strings.Builder
		if hasLeft {
			sb.WriteString(styleMeta.Render("‹ "))
		}
		for i, e := range entries {
			if i > 0 {
				sb.WriteString("  ")
			}
			sb.WriteString(e.text)
		}
		if hasRight {
			sb.WriteString(styleMeta.Render(" ›"))
		}
		bar := sb.String() + syncSuffix
		b.WriteString(lipgloss.NewStyle().MaxWidth(w).Render(bar) + "\n")
	} else if m.syncing {
		b.WriteString(m.sp.View() + styleSyncing.Render(" syncing…") + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", w)) + "\n")

	// ── filter chips ──
	if m.unreadOnly || m.searchQ != "" {
		var chips []string
		if m.unreadOnly {
			chips = append(chips, styleTabInact.Render("unread"))
		}
		if m.searchQ != "" {
			chips = append(chips, styleTabInact.Render("/"+m.searchQ))
		}
		b.WriteString(strings.Join(chips, "  ") + "\n")
	}

	// ── batch select mode badge ──
	if m.selecting {
		badge := styleSelected.Render(fmt.Sprintf("select: %d", len(m.selected)))
		hint := styleMeta.Render("  space toggle  A all  r mark read  d delete  esc cancel")
		b.WriteString(badge + hint + "\n")
	}

	// ── search input ──
	if m.searching {
		b.WriteString("  " + m.searchInput.View() + "\n\n")
	}

	// ── command palette ──
	if m.inPalette {
		b.WriteString("  " + m.paletteInput.View() + "\n")
		matches := palette.Match(paletteCommands, m.paletteInput.Value())
		if len(matches) > 6 {
			matches = matches[:6]
		}
		if len(matches) == 0 {
			b.WriteString("    " + styleHelp.Render("no matching command") + "\n")
		}
		for i, c := range matches {
			row := fmt.Sprintf("%-9s %s", c.Name, c.Desc)
			if i == m.paletteCursor {
				b.WriteString("    " + styleSelected.Render("▶ "+row) + "\n")
			} else {
				b.WriteString("      " + styleHelp.Render(row) + "\n")
			}
		}
		b.WriteString("\n")
	}

	// ── message list ──
	listH := m.height - m.listStartY() - 2 // statusbar
	if listH < 1 {
		listH = 1
	}

	if m.loading {
		b.WriteString("\n  " + m.sp.View() + styleHelp.Render(" Loading messages…") + "\n")
	} else if len(m.msgs) == 0 {
		b.WriteString("\n" + styleHelp.Render("  No messages — press s to sync") + "\n")
	} else {
		lines, cursorLine := m.buildListLines(w)
		start := 0
		if cursorLine >= listH {
			start = cursorLine - listH + 1
		}
		end := min(len(lines), start+listH)
		for _, l := range lines[start:end] {
			b.WriteString(l + "\n")
		}
	}

	// ── status / help bar ──
	countStr := ""
	if len(m.msgs) > 0 {
		countStr = styleHelp.Render(fmt.Sprintf(" %d/%d", m.cursor+1, len(m.msgs)))
	}
	var helpBar string
	if m.err != nil {
		helpBar = styleErr.Render("✗ " + m.err.Error())
	} else if m.status != "" {
		helpBar = styleOK.Render("✓ " + m.status)
	} else {
		helpBar = styleHelp.Render("enter:open  n:new  s:sync  u:unread  d:delete  y:copy  o:mail  /:search  tab:acct  ?:help  q:quit")
	}
	rightPad := w - lipgloss.Width(helpBar) - lipgloss.Width(countStr)
	if rightPad < 0 {
		rightPad = 0
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", w)) + "\n")
	b.WriteString(helpBar + strings.Repeat(" ", rightPad) + countStr)
	return b.String()
}

// detailPadV/detailPadH inset the opened-mail view from the terminal edges
// — it previously rendered flush against row/column 0. Shrinking the
// model's effective width/height first (rather than padding the finished
// string) means every width calc below — dividers, the viewport, the
// scrollbar — already accounts for the smaller canvas.
const detailPadV, detailPadH = 1, 2

// doubleClickWindow opens the message detail on a second click within this
// window, same pattern and duration taskctl uses for its own double-click.
const doubleClickWindow = 400 * time.Millisecond

// detailRawWidth is the width renderDetail actually lays out with, after
// the outer padding and the 1-column terminal-edge safety margin (see
// renderDetail) — but BEFORE the further -2 for the scrollbar track that
// both renderDetail (via w-2) and formatDetail (via width-2) apply
// themselves. bodyLoadedMsg must wrap the fetched body to this same value,
// not the raw terminal width — wrapping wider than the viewport actually
// displays cut lines at the wrong point once rendered, visually corrupting
// the scrollbar's glyph column exactly where a wrapped line happened to be
// too wide for the real display area.
func (m Model) detailRawWidth() int {
	return m.width - detailPadH*2 - 1
}

func (m Model) renderDetail() string {
	if m.detail == nil {
		return ""
	}
	m.width = m.detailRawWidth()
	w := min(m.width, 130)
	var b strings.Builder

	// ── header ──
	b.WriteString(styleSubject.Render(m.detail.Subject) + "\n")
	b.WriteString(styleLabel.Render("From:") + " " + m.detail.From + "\n")
	if len(m.detail.To) > 0 {
		b.WriteString(styleLabel.Render("To:") + " " + strings.Join(m.detail.To, ", ") + "\n")
	}
	b.WriteString(styleLabel.Render("Date:") + " " + m.detail.Date.Format("Mon, 02 Jan 2006  15:04") + "\n")
	if m.detail.Account != "" {
		b.WriteString(styleLabel.Render("Account:") + " " + styleMeta.Render(m.detail.Account) + "\n")
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", w)) + "\n")

	// ── body viewport with scrollbar ──
	m.vp.Width = w - 2 // leave 2 cols for scrollbar track
	m.vp.Height = m.detailBodyHeight()
	b.WriteString(renderScrollbar(m.vp))

	// ── footer ──
	b.WriteString("\n" + styleDivider.Render(strings.Repeat("─", w)) + "\n")
	helpLine := "esc:back  r:reply  a:ai draft  u:unread  d:delete  y:copy  o:mail  ↑↓/jk:scroll  q:quit"
	if m.detail != nil && findUnsubscribeURL(m.detail.Body) != "" {
		helpLine += "  U:unsubscribe"
	}
	b.WriteString(styleHelp.Render(helpLine))
	if m.aiDrafting {
		b.WriteString("\n  " + m.sp.View() + styleSyncing.Render(" Drafting a reply…"))
	} else if m.err != nil {
		b.WriteString("\n  " + styleErr.Render("✗ "+m.err.Error()))
	} else if m.status != "" {
		b.WriteString("\n  " + styleOK.Render("✓ "+m.status))
	}
	return lipgloss.NewStyle().Padding(detailPadV, detailPadH).Render(b.String())
}

// renderScrollbar renders viewport content with a sidebar scrollbar track.
func renderScrollbar(vp viewport.Model) string {
	content := vp.View()
	lines := strings.Split(content, "\n")
	h := vp.Height
	if h <= 0 {
		h = len(lines)
	}
	total := vp.TotalLineCount()

	// no scrollbar needed if content fits
	if total <= h {
		return content
	}

	// compute thumb size and position
	thumbH := max(1, h*h/total)
	thumbTop := int(vp.ScrollPercent() * float64(h-thumbH))

	track := styleDivider.Render("│")
	thumb := lipgloss.NewStyle().Foreground(colorBlue).Render("┃")

	// Pad each line out to vp.Width explicitly rather than relying on
	// lipgloss.JoinHorizontal, which pads to the widest line ACTUALLY
	// present in the content rather than the viewport's declared width.
	// A line that happens to reach exactly that actual-widest width gets
	// its glyph glued on with no gap — the padding lipgloss would have
	// added assumes there's slack to add, and there isn't when this line
	// IS the widest one. Padding against the width we set ourselves
	// removes that ambiguity entirely.
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if lw := lipgloss.Width(l); lw < vp.Width {
			l += strings.Repeat(" ", vp.Width-lw)
		}
		b.WriteString(l + " ")
		if i >= thumbTop && i < thumbTop+thumbH {
			b.WriteString(thumb)
		} else {
			b.WriteString(track)
		}
	}

	return b.String()
}

func (m Model) renderCompose() string {
	title := "New Message"
	if m.replyTo != nil {
		title = "Reply"
	}
	w := min(m.width, 130)
	var b strings.Builder
	b.WriteString(styleHeader.Render("mailctl") + "  " + styleMeta.Render(title) + "\n")
	b.WriteString(styleDivider.Render(strings.Repeat("─", w)) + "\n\n")

	focused := func(i int) string {
		if m.composeFocus == i {
			return styleTabActive.Render("›")
		}
		return "  "
	}

	b.WriteString(focused(focusTo) + " " + styleLabel.Render("To:") + "      " + m.toInput.View() + "\n")
	b.WriteString(focused(focusSubject) + " " + styleLabel.Render("Subject:") + "  " + m.subjectInput.View() + "\n")
	b.WriteString(focused(focusAttach) + " " + styleLabel.Render("Attach:") + "   " + m.attachInput.View() + "\n\n")
	b.WriteString(focused(focusBody) + " " + styleLabel.Render("Body:") + "\n")
	b.WriteString(m.bodyArea.View() + "\n\n")

	if m.err != nil {
		b.WriteString(styleErr.Render("✗ "+m.err.Error()) + "\n")
	} else {
		b.WriteString(styleHelp.Render("tab:next  ctrl+s:send  ctrl+d:draft  ctrl+t:template  esc:cancel  attach:comma-sep paths"))
	}
	return b.String()
}

// renderTemplatePicker overlays a simple j/k list of saved templates on top
// of the compose view — same overlay.Center + rounded-border pattern
// renderHelpPopup uses.
func (m Model) renderTemplatePicker() string {
	var b strings.Builder
	b.WriteString(styleHeader.Render("Insert template") + "\n\n")
	if len(m.templateNames) == 0 {
		b.WriteString(styleMeta.Render("No templates yet — `mailctl template new <name>`") + "\n")
	}
	for i, n := range m.templateNames {
		if i == m.templateCursor {
			b.WriteString(styleTabActive.Render("› "+n) + "\n")
		} else {
			b.WriteString("  " + n + "\n")
		}
	}
	b.WriteString("\n" + styleMeta.Render("j/k move  enter insert  esc cancel"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2).
		Width(min(50, m.width-4)).
		Render(b.String())
}

// ── Commands ──────────────────────────────────────────────────────────────────

// loadMsgsCmd fetches messages for account/unreadOnly, unfiltered by search
// text — search is applied client-side (filterMsgs) over the result, live
// as the user types, rather than round-tripping to SQLite on every
// keystroke or requiring enter to submit. Store.Filter.Query / the SQL LIKE
// path still exists and is still used by `mailctl search` and the MCP
// search tool, just not from here anymore.
func loadMsgsCmd(unreadOnly bool, account string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return errMsg{err}
		}
		defer s.Close()
		ctx := context.Background()
		msgs, err := s.ListMessages(ctx, store.Filter{
			Account:    account,
			UnreadOnly: unreadOnly,
			Limit:      500,
		})
		if err != nil {
			return errMsg{err}
		}
		accounts, _ := s.ListAccounts(ctx)
		counts, _ := s.UnreadCounts(ctx)
		return msgsLoadedMsg{msgs: msgs, accounts: accounts, unreadCounts: counts}
	}
}

// filterMsgs fuzzy-matches q against each message's subject OR from
// (github.com/sahilm/fuzzy), keeping a message if either matches. Does NOT
// re-rank by match quality — messages are naturally date-ordered (grouped
// by day in the list), and re-sorting by fuzzy score would scramble that.
func filterMsgs(msgs []models.Message, q string) []models.Message {
	q = strings.TrimSpace(q)
	if q == "" {
		return msgs
	}
	subjects := make([]string, len(msgs))
	froms := make([]string, len(msgs))
	for i, m := range msgs {
		subjects[i] = m.Subject
		froms[i] = m.From
	}
	matched := make(map[int]bool, len(msgs))
	for _, mt := range fuzzy.Find(q, subjects) {
		matched[mt.Index] = true
	}
	for _, mt := range fuzzy.Find(q, froms) {
		matched[mt.Index] = true
	}
	out := make([]models.Message, 0, len(matched))
	for i, m := range msgs {
		if matched[i] {
			out = append(out, m)
		}
	}
	return out
}

func syncCmd() tea.Cmd {
	return func() tea.Msg {
		msgs, err := mail.FetchInbox(150, false)
		if err != nil {
			return syncDoneMsg{err: err}
		}
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return syncDoneMsg{err: err}
		}
		defer s.Close()
		ctx := context.Background()
		_ = s.DeleteBySource(ctx, "apple")
		for i := range msgs {
			_ = s.UpsertMessage(ctx, &msgs[i])
		}
		accounts, _ := s.ListAccounts(ctx)
		return syncDoneMsg{count: len(msgs), accounts: accounts}
	}
}

func loadBodyCmd(msg *models.Message) tea.Cmd {
	account, subject, from := msg.Account, msg.Subject, msg.From
	return func() tea.Msg {
		body, err := mail.FetchMessageBody(account, subject, from)
		return bodyLoadedMsg{body: body, err: err}
	}
}

func markReadCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return readMarkedMsg{}
		}
		defer s.Close()
		_ = s.MarkRead(context.Background(), id)
		return readMarkedMsg{}
	}
}

func sendCmd(to, subject, body string, attachments []string) tea.Cmd {
	return func() tea.Msg {
		d := &models.Draft{To: []string{to}, Subject: subject, Body: body, Attachments: attachments}
		if err := mail.Send(d); err != nil {
			return sentMsg{err}
		}
		return sentMsg{}
	}
}

func draftCmd(to, subject, body string, attachments []string) tea.Cmd {
	return func() tea.Msg {
		d := &models.Draft{To: []string{to}, Subject: subject, Body: body, Attachments: attachments}
		if err := mail.SaveDraft(d); err != nil {
			return draftedMsg{err}
		}
		return draftedMsg{}
	}
}

func parseAttachments(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func openInMailCmd(messageID string) tea.Cmd {
	return func() tea.Msg {
		_ = mail.OpenInMail(messageID)
		return openedMsg{}
	}
}

func markUnreadCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return unreadMarkedMsg{}
		}
		defer s.Close()
		_ = s.MarkUnread(context.Background(), id)
		_ = mail.MarkUnreadInMail(id)
		return unreadMarkedMsg{}
	}
}

func deleteCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return deletedMsg{err}
		}
		defer s.Close()
		if err := s.DeleteMessage(context.Background(), id); err != nil {
			return deletedMsg{err}
		}
		_ = mail.DeleteInMail(id)
		return deletedMsg{}
	}
}

// selectedIDs flattens a selection set into a slice.
func selectedIDs(sel map[string]bool) []string {
	ids := make([]string, 0, len(sel))
	for id := range sel {
		ids = append(ids, id)
	}
	return ids
}

// removeMessages returns msgs with every message whose ID is in sel dropped
// — used to optimistically update the list right after a batch delete,
// mirroring the single-delete "d" key's own optimistic removal.
func removeMessages(msgs []models.Message, sel map[string]bool) []models.Message {
	out := msgs[:0:0]
	for _, msg := range msgs {
		if !sel[msg.ID] {
			out = append(out, msg)
		}
	}
	return out
}

// batchMarkReadCmd is the batch-mode ("v" + "r") version of markReadCmd.
func batchMarkReadCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return readMarkedMsg{}
		}
		defer s.Close()
		ctx := context.Background()
		for _, id := range ids {
			_ = s.MarkRead(ctx, id)
		}
		return readMarkedMsg{}
	}
}

// batchDeleteCmd is the batch-mode ("v" + "d") version of deleteCmd.
func batchDeleteCmd(ids []string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath(), config.Shared())
		if err != nil {
			return deletedMsg{err}
		}
		defer s.Close()
		ctx := context.Background()
		var lastErr error
		for _, id := range ids {
			if err := s.DeleteMessage(ctx, id); err != nil {
				lastErr = err
				continue
			}
			_ = mail.DeleteInMail(id)
		}
		return deletedMsg{lastErr}
	}
}

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
	// + footer-divider(1) + help(1) = 8
	h := m.height - detailPadV*2 - 8
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
			rowStyle = theme.Hover
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
				preview = theme.Hover.Width(w).Render(preview)
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

// stripVariationSelectors removes U+FE0E/U+FE0F. Real email bodies
// occasionally contain emoji+variation-selector sequences (e.g. "🏖️"),
// and terminal width libraries disagree on how wide those render — the
// same disagreement that misaligned notectl's scrollbar. Stripping the
// selector removes the disagreement at its source instead of trying to
// reconcile width functions.
// stripVariationSelectors removes characters whose display width is
// ambiguous or undefined across terminals/fonts, rather than trying to
// measure and wrap around them correctly: U+FE0E/FE0F (emoji variation
// selectors \u2014 a real width mismatch already caused a scrollbar-alignment
// bug, see the regression test) and U+FFFC (the object-replacement
// character HTML mail leaves behind for an inline image/attachment once
// stripped to plain text \u2014 it has no meaningful width or content once
// there's no image to show, and different terminals render it at
// different widths, which can misalign a terminal's own cursor tracking
// on lines further down the same frame).
func stripVariationSelectors(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\uFE0E' || r == '\uFE0F' || r == '\uFFFC' {
			return -1
		}
		return r
	}, s)
}

func formatDetail(msg *models.Message, width int) string {
	body := stripVariationSelectors(strings.TrimSpace(msg.Body))
	if body == "" {
		return styleMeta.Render("(no body)")
	}
	w := min(width-2, 128) // leave room for scrollbar track
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
