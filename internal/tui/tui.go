package tui

import (
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	"github.com/aeon022/mailctl/internal/models"
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
	unsubPrompt        *unsubPrompt    // confirm popup for "U" in the detail view
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
	lastLoad   time.Time // when msgsLoadedMsg last arrived; FocusMsg reloads only if this is stale

	// "?" transient help popup
	helpVP   viewport.Model
	helpPopW int
	helpPopH int
}
