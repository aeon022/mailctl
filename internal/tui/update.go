package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/mailctl/internal/ai"
	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/templates"
	"github.com/aeon022/missionctl-core/lastsync"
	"github.com/aeon022/missionctl-core/palette"
)

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadMsgsCmd(m.unreadOnly, m.pendingAccountRestore), tea.RequestWindowSize, m.sp.Tick, loadLastSyncedCmd())
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
		// -1, not msg.Height: render paths here fill their height budget
		// exactly, and View() never ends in a trailing newline — that
		// combination (output with exactly as many lines as the terminal,
		// no trailing newline) is a long-standing bubbletea quirk
		// (charmbracelet/bubbletea#304) where the renderer can fail to
		// fully redraw the last line. One row of slack avoids ever hitting
		// that boundary, regardless of terminal size.
		m.height = msg.Height - 1
		if m.height < 1 {
			m.height = 1
		}
		// Match the width/height renderDetail actually displays with (see
		// detailRawWidth/detailBodyHeight) — this is what PgUp/PgDown
		// scroll by via vp.Update(), so it has to agree with what's really
		// on screen or scrolling overshoots what the viewport shows per page.
		m.vp = viewport.New(viewport.WithWidth(m.detailRawWidth()-2), viewport.WithHeight(m.detailBodyHeight()))
		m.bodyArea.SetWidth(msg.Width - 12)
		m.bodyArea.SetHeight(m.composeBodyHeight())

	case tea.FocusMsg:
		// Back in the window: refresh the list from the local DB (not a
		// sync) — but only while just browsing, never under input or a
		// pending confirmation, and not more often than every 5s.
		if m.browsingIdle() && time.Since(m.lastLoad) > focusReloadAfter {
			m.lastLoad = time.Now() // debounce focus flicker; no loading flag, so the list never blanks
			return m, loadMsgsCmd(m.unreadOnly, m.activeAccount())
		}
		return m, nil

	case msgsLoadedMsg:
		m.loading = false
		m.lastLoad = time.Now()
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

	case unsubTargetMsg:
		if m.detail == nil || m.detail.ID != msg.msgID {
			return m, nil // user left the message meanwhile
		}
		if !msg.ok {
			m.setStatus("No unsubscribe option found in this email")
			return m, nil
		}
		m.status = ""
		m.unsubPrompt = &unsubPrompt{msgID: msg.msgID, sender: msg.sender, target: msg.target, fromBody: msg.fromBody}

	case unsubDoneMsg:
		if msg.err != nil {
			m.setStatus("✗ Unsubscribe failed: " + msg.err.Error())
		} else if msg.via == "one-click" {
			m.setStatus("✓ Unsubscribe request accepted")
		} else {
			m.setStatus("Unsubscribe page opened in your browser")
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

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.view == viewDetail {
				m.vp.ScrollUp(3)
			} else if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.view == viewDetail {
				m.vp.ScrollDown(3)
			} else if m.cursor < len(m.msgs)-1 {
				m.cursor++
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft || m.view != viewList {
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
		if i := m.rowHitTest(msg.X, msg.Y); i >= 0 {
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
		return m, nil

	case tea.MouseMotionMsg:
		if m.view == viewList {
			m.hoverRow = m.rowHitTest(msg.X, msg.Y)
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

	case tea.KeyPressMsg:
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

func (m Model) updateList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
			replay := tea.KeyPressMsg{Text: chosen.Key, Code: []rune(chosen.Key)[0]}
			if chosen.Key == "enter" {
				replay = tea.KeyPressMsg{Code: tea.KeyEnter}
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
		case "space":
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
			for _, id := range ids {
				m.setRead(id, true)
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
			m.dropMessages(m.selected)
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
		g := m.geom()
		_, cursorLine, lineToMsg := m.buildListLinesWithMapping(g.w)
		start := scrollStart(cursorLine, g.rowsVisible)
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
				m.setRead(msg.ID, true)
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
				m.dropMessages(map[string]bool{id: true})
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

func (m Model) updateDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.unsubPrompt != nil {
		return m.handleUnsubKey(msg.String())
	}
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
			m.setRead(m.detail.ID, false) // reflect in the list
			return m, markUnreadCmd(m.detail.ID)
		}
	case "U":
		if m.detail != nil {
			m.setStatus("Looking for the unsubscribe option…")
			return m, findUnsubCmd(*m.detail)
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
				m.dropMessages(map[string]bool{id: true})
				if m.cursor >= len(m.msgs) {
					m.cursor = max(0, len(m.msgs)-1)
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

func (m Model) updateCompose(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
