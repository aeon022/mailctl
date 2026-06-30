package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/mailctl/internal/config"
	"github.com/aeon022/mailctl/internal/mail"
	"github.com/aeon022/mailctl/internal/models"
	"github.com/aeon022/mailctl/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Views ─────────────────────────────────────────────────────────────────────

type view int

const (
	viewList    view = iota
	viewDetail  view = iota
	viewCompose view = iota
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleUnread   = lipgloss.NewStyle().Bold(true)
	styleRead     = lipgloss.NewStyle().Faint(true)
	styleCursor   = lipgloss.NewStyle().Background(lipgloss.Color("237")).Width(0)
	styleSubject  = lipgloss.NewStyle().Bold(true)
	styleMeta     = lipgloss.NewStyle().Faint(true)
	styleLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Width(10)
	styleHelp     = lipgloss.NewStyle().Faint(true)
	styleErr      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleSelected = lipgloss.NewStyle().Background(lipgloss.Color("235"))
	styleSent     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// ── Messages ──────────────────────────────────────────────────────────────────

type msgsLoadedMsg struct{ msgs []models.Message }
type syncDoneMsg struct {
	msgs []models.Message
	err  error
}
type sentMsg struct{ err error }
type draftedMsg struct{ err error }
type errMsg struct{ err error }
type bodyLoadedMsg struct {
	body string
	err  error
}

// ── Form fields ───────────────────────────────────────────────────────────────

const (
	fTo      = 0
	fSubject = 1
	fBody    = 2
	fCount   = 3
)

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	view       view
	msgs       []models.Message
	cursor     int
	detail     *models.Message
	vp         viewport.Model
	width      int
	height     int
	unreadOnly bool
	searchQ    string
	searching  bool
	searchInput textinput.Model
	composeInputs [fCount]textinput.Model
	composeFocus  int
	replyTo    *models.Message
	status     string
	statusTime time.Time
	err        error
	syncing    bool
}

func New() Model {
	si := textinput.New()
	si.Placeholder = "search…"
	si.CharLimit = 100

	var ci [fCount]textinput.Model
	placeholders := [fCount]string{"to@example.com", "Subject", "Body (ctrl+s send, ctrl+d draft, esc cancel)"}
	for i := range ci {
		ci[i] = textinput.New()
		ci[i].Placeholder = placeholders[i]
		ci[i].CharLimit = 500
	}
	ci[fTo].Focus()

	return Model{
		searchInput:   si,
		composeInputs: ci,
	}
}

func Run() error {
	m := New()
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// ── Init ──────────────────────────────────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadMsgsCmd(false, ""), tea.WindowSize())
}

// ── Update ────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.vp = viewport.New(msg.Width, msg.Height-4)

	case msgsLoadedMsg:
		m.msgs = msg.msgs
		if m.cursor >= len(m.msgs) {
			m.cursor = max(0, len(m.msgs)-1)
		}

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.msgs = msg.msgs
			m.cursor = 0
			m.setStatus(fmt.Sprintf("Synced %d messages", len(msg.msgs)))
		}

	case bodyLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else if m.detail != nil {
			m.detail.Body = msg.body
			m.vp.SetContent(formatDetail(m.detail))
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

	case errMsg:
		m.err = msg.err

	case tea.KeyMsg:
		m.err = nil
		// clear old status
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
		}
	}

	// forward to viewport in detail view
	if m.view == viewDetail {
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.searching {
		switch msg.String() {
		case "enter":
			m.searchQ = m.searchInput.Value()
			m.searching = false
			return m, loadMsgsCmd(m.unreadOnly, m.searchQ)
		case "esc":
			m.searching = false
			m.searchInput.SetValue("")
			m.searchQ = ""
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.msgs)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(0, len(m.msgs)-1)
	case "enter":
		if len(m.msgs) > 0 {
			msg := m.msgs[m.cursor]
			m.detail = &msg
			m.vp.SetContent("Loading…")
			m.vp.GotoTop()
			m.view = viewDetail
			return m, loadBodyCmd(&msg)
		}
	case "n":
		m.replyTo = nil
		m.resetCompose("")
		m.view = viewCompose
	case "s":
		if !m.syncing {
			m.syncing = true
			m.setStatus("Syncing…")
			return m, syncCmd()
		}
	case "u":
		m.unreadOnly = !m.unreadOnly
		return m, loadMsgsCmd(m.unreadOnly, m.searchQ)
	case "/":
		m.searching = true
		m.searchInput.Focus()
		m.searchInput.SetValue("")
	case "esc":
		m.searchQ = ""
		m.searchInput.SetValue("")
		return m, loadMsgsCmd(m.unreadOnly, "")
	}
	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.view = viewList
		m.detail = nil
	case "r":
		if m.detail != nil {
			m.replyTo = m.detail
			m.resetCompose(extractEmail(m.detail.From))
			m.view = viewCompose
		}
	}
	return m, nil
}

func (m Model) updateCompose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		return m, sendCmd(m.composeInputs)
	case "ctrl+d":
		return m, draftCmd(m.composeInputs)
	case "esc":
		m.view = viewList
		return m, nil
	case "tab", "enter":
		if m.composeFocus < fCount-1 {
			m.composeInputs[m.composeFocus].Blur()
			m.composeFocus++
			m.composeInputs[m.composeFocus].Focus()
		}
	case "shift+tab":
		if m.composeFocus > 0 {
			m.composeInputs[m.composeFocus].Blur()
			m.composeFocus--
			m.composeInputs[m.composeFocus].Focus()
		}
	default:
		var cmd tea.Cmd
		m.composeInputs[m.composeFocus], cmd = m.composeInputs[m.composeFocus].Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	switch m.view {
	case viewDetail:
		return m.renderDetail()
	case viewCompose:
		return m.renderCompose()
	default:
		return m.renderList()
	}
}

func (m Model) renderList() string {
	var b strings.Builder

	// header
	title := "mailctl"
	filters := ""
	if m.unreadOnly {
		filters += " [unread]"
	}
	if m.searchQ != "" {
		filters += fmt.Sprintf(" [/%s]", m.searchQ)
	}
	if m.syncing {
		filters += " ⟳"
	}
	b.WriteString(styleHeader.Render(title+filters) + "\n\n")

	if m.searching {
		b.WriteString("Search: " + m.searchInput.View() + "\n\n")
	}

	if len(m.msgs) == 0 {
		b.WriteString(styleRead.Render("No messages — press s to sync") + "\n")
	} else {
		listH := m.height - 5
		if m.searching {
			listH -= 2
		}
		start := 0
		if m.cursor >= listH {
			start = m.cursor - listH + 1
		}
		end := min(len(m.msgs), start+listH)
		for i := start; i < end; i++ {
			msg := m.msgs[i]
			line := formatListRow(&msg, m.width)
			if i == m.cursor {
				line = styleSelected.Width(m.width).Render(line)
			} else if !msg.Read {
				line = styleUnread.Render(line)
			} else {
				line = styleRead.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(styleErr.Render("✗ "+m.err.Error()) + "\n")
	} else if m.status != "" {
		b.WriteString(styleSent.Render("✓ "+m.status) + "\n")
	} else {
		b.WriteString(styleHelp.Render("enter:open  n:new  r:reply  s:sync  u:unread  /:search  q:quit"))
	}
	return b.String()
}

func (m Model) renderDetail() string {
	if m.detail == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(styleSubject.Render(m.detail.Subject) + "\n")
	b.WriteString(styleLabel.Render("From:") + " " + m.detail.From + "\n")
	if len(m.detail.To) > 0 {
		b.WriteString(styleLabel.Render("To:") + " " + strings.Join(m.detail.To, ", ") + "\n")
	}
	b.WriteString(styleLabel.Render("Date:") + " " + m.detail.Date.Format("Mon, 02 Jan 2006 15:04") + "\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n")
	b.WriteString(m.vp.View())
	b.WriteString("\n" + styleHelp.Render("esc:back  r:reply  ↑↓/jk:scroll  q:quit"))
	return b.String()
}

func (m Model) renderCompose() string {
	title := "New Message"
	if m.replyTo != nil {
		title = "Reply: " + m.replyTo.Subject
	}
	var b strings.Builder
	b.WriteString(styleHeader.Render(title) + "\n\n")

	labels := [fCount]string{"To", "Subject", "Body"}
	for i, inp := range m.composeInputs {
		b.WriteString(styleLabel.Render(labels[i]+":") + " " + inp.View() + "\n")
	}

	b.WriteString("\n")
	if m.err != nil {
		b.WriteString(styleErr.Render("✗ "+m.err.Error()) + "\n")
	} else {
		b.WriteString(styleHelp.Render("tab:next  ctrl+s:send  ctrl+d:draft  esc:cancel"))
	}
	return b.String()
}

// ── Commands ──────────────────────────────────────────────────────────────────

func loadMsgsCmd(unreadOnly bool, query string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return errMsg{err}
		}
		defer s.Close()
		msgs, err := s.ListMessages(context.Background(), store.Filter{
			UnreadOnly: unreadOnly,
			Query:      query,
			Limit:      200,
		})
		if err != nil {
			return errMsg{err}
		}
		return msgsLoadedMsg{msgs}
	}
}

func syncCmd() tea.Cmd {
	return func() tea.Msg {
		msgs, err := mail.FetchInbox(100, false)
		if err != nil {
			return syncDoneMsg{err: err}
		}
		s, err := store.New(config.DBPath())
		if err != nil {
			return syncDoneMsg{err: err}
		}
		defer s.Close()
		ctx := context.Background()
		_ = s.DeleteBySource(ctx, "apple")
		for i := range msgs {
			_ = s.UpsertMessage(ctx, &msgs[i])
		}
		loaded, _ := s.ListMessages(ctx, store.Filter{Limit: 200})
		return syncDoneMsg{msgs: loaded}
	}
}

func sendCmd(inputs [fCount]textinput.Model) tea.Cmd {
	to := inputs[fTo].Value()
	subject := inputs[fSubject].Value()
	body := inputs[fBody].Value()
	return func() tea.Msg {
		d := &models.Draft{
			To:      []string{to},
			Subject: subject,
			Body:    body,
		}
		if err := mail.Send(d); err != nil {
			return sentMsg{err}
		}
		return sentMsg{}
	}
}

func loadBodyCmd(msg *models.Message) tea.Cmd {
	subject := msg.Subject
	from := msg.From
	return func() tea.Msg {
		body, err := mail.FetchMessageBody(subject, from)
		return bodyLoadedMsg{body: body, err: err}
	}
}

func draftCmd(inputs [fCount]textinput.Model) tea.Cmd {
	to := inputs[fTo].Value()
	subject := inputs[fSubject].Value()
	body := inputs[fBody].Value()
	return func() tea.Msg {
		d := &models.Draft{
			To:      []string{to},
			Subject: subject,
			Body:    body,
		}
		if err := mail.SaveDraft(d); err != nil {
			return draftedMsg{err}
		}
		return draftedMsg{}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *Model) resetCompose(to string) {
	placeholders := [fCount]string{"to@example.com", "Subject", "Body (ctrl+s send, ctrl+d draft, esc cancel)"}
	for i := range m.composeInputs {
		m.composeInputs[i].SetValue("")
		m.composeInputs[i].Placeholder = placeholders[i]
		m.composeInputs[i].Blur()
	}
	m.composeInputs[fTo].SetValue(to)
	if m.replyTo != nil && !strings.HasPrefix(m.replyTo.Subject, "Re: ") {
		m.composeInputs[fSubject].SetValue("Re: " + m.replyTo.Subject)
	}
	m.composeFocus = 0
	m.composeInputs[0].Focus()
}

func (m *Model) setStatus(s string) {
	m.status = s
	m.statusTime = time.Now()
}

func formatListRow(msg *models.Message, width int) string {
	unread := "○"
	if !msg.Read {
		unread = "●"
	}
	date := msg.Date.Format("Jan 02 15:04")
	from := msg.From
	if idx := strings.Index(from, "<"); idx > 0 {
		from = strings.TrimSpace(from[:idx])
	}
	if len(from) > 22 {
		from = from[:20] + "…"
	}
	subject := msg.Subject
	subjectWidth := width - 35
	if subjectWidth < 10 {
		subjectWidth = 10
	}
	if len(subject) > subjectWidth {
		subject = subject[:subjectWidth-1] + "…"
	}
	return fmt.Sprintf("%s  %s  %-22s  %s", unread, date, from, subject)
}

func formatDetail(msg *models.Message) string {
	return strings.TrimSpace(msg.Body)
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
