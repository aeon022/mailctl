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
	"github.com/charmbracelet/bubbles/textarea"
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

const (
	focusTo      = 0
	focusSubject = 1
	focusBody    = 2
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	colorBlue   = lipgloss.AdaptiveColor{Light: "21", Dark: "39"}
	colorGreen  = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colorRed    = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "244", Dark: "240"}
	colorSubtle = lipgloss.AdaptiveColor{Light: "250", Dark: "237"}

	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.AdaptiveColor{Light: "21", Dark: "39"}).
			Padding(0, 2)
	styleTabInact = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 2)
	styleTabCount = lipgloss.NewStyle().Foreground(colorMuted)

	styleDivider  = lipgloss.NewStyle().Foreground(colorSubtle)
	styleUnread   = lipgloss.NewStyle().Bold(true)
	styleRead     = lipgloss.NewStyle().Foreground(colorMuted)
	styleSelected = lipgloss.NewStyle().
			Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"}).
			Bold(true)
	styleHeader   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "21", Dark: "39"})
	styleSubject  = lipgloss.NewStyle().Bold(true)
	styleMeta     = lipgloss.NewStyle().Foreground(colorMuted)
	styleLabel    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "21", Dark: "39"}).Width(9)
	styleHelp     = lipgloss.NewStyle().Foreground(colorMuted)
	styleErr      = lipgloss.NewStyle().Foreground(colorRed)
	styleOK       = lipgloss.NewStyle().Foreground(colorGreen)
	styleAcctBadge = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "33", Dark: "75"})
	styleSyncing  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "214", Dark: "220"})
)

// ── Messages ──────────────────────────────────────────────────────────────────

type msgsLoadedMsg struct {
	msgs     []models.Message
	accounts []string
}
type syncDoneMsg struct {
	count    int
	accounts []string
	err      error
}
type sentMsg struct{ err error }
type draftedMsg struct{ err error }
type errMsg struct{ err error }
type bodyLoadedMsg struct {
	body string
	err  error
}
type readMarkedMsg struct{}

// ── Model ─────────────────────────────────────────────────────────────────────

type Model struct {
	view   view
	width  int
	height int

	// list
	msgs       []models.Message
	cursor     int
	unreadOnly bool
	searchQ    string
	searching  bool
	searchInput textinput.Model

	// tabs
	accounts  []string // ["Alle", "iCloud", ...]
	activeTab int      // 0 = Alle

	// detail
	detail *models.Message
	vp     viewport.Model

	// compose
	toInput      textinput.Model
	subjectInput textinput.Model
	bodyArea     textarea.Model
	composeFocus int
	replyTo      *models.Message

	// status
	status     string
	statusTime time.Time
	err        error
	syncing    bool
}

func New() Model {
	si := textinput.New()
	si.Placeholder = "search…"
	si.CharLimit = 200

	to := textinput.New()
	to.Placeholder = "to@example.com"
	to.CharLimit = 500
	to.Focus()

	sub := textinput.New()
	sub.Placeholder = "Subject"
	sub.CharLimit = 300

	body := textarea.New()
	body.Placeholder = "Write your message here…"
	body.ShowLineNumbers = false
	body.SetHeight(10)

	return Model{
		searchInput:  si,
		toInput:      to,
		subjectInput: sub,
		bodyArea:     body,
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
	return tea.Batch(loadMsgsCmd(false, "", ""), tea.WindowSize())
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
		m.vp = viewport.New(msg.Width, m.detailBodyHeight())
		m.bodyArea.SetWidth(msg.Width - 12)
		m.bodyArea.SetHeight(m.height - 12)

	case msgsLoadedMsg:
		m.msgs = msg.msgs
		if len(msg.accounts) > 0 {
			m.accounts = append([]string{"Alle"}, msg.accounts...)
		}
		if m.cursor >= len(m.msgs) {
			m.cursor = max(0, len(m.msgs)-1)
		}

	case syncDoneMsg:
		m.syncing = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			if len(msg.accounts) > 0 {
				m.accounts = append([]string{"Alle"}, msg.accounts...)
			}
			m.setStatus(fmt.Sprintf("Synced %d messages", msg.count))
			// reload with active account filter to preserve tab
			return m, loadMsgsCmd(m.unreadOnly, m.searchQ, m.activeAccount())
		}

	case bodyLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
		} else if m.detail != nil {
			m.detail.Body = msg.body
			m.vp.SetContent(formatDetail(m.detail, m.width))
		}

	case readMarkedMsg:
		// local state already updated optimistically

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
		if time.Since(m.statusTime) > 4*time.Second {
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
			m.cursor = 0
			return m, loadMsgsCmd(m.unreadOnly, m.searchQ, m.activeAccount())
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
	case "tab":
		if len(m.accounts) > 0 {
			m.activeTab = (m.activeTab + 1) % len(m.accounts)
			m.cursor = 0
			return m, loadMsgsCmd(m.unreadOnly, m.searchQ, m.activeAccount())
		}
	case "shift+tab":
		if len(m.accounts) > 0 {
			m.activeTab = (m.activeTab - 1 + len(m.accounts)) % len(m.accounts)
			m.cursor = 0
			return m, loadMsgsCmd(m.unreadOnly, m.searchQ, m.activeAccount())
		}
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
	case "n":
		m.replyTo = nil
		m.resetCompose("", "")
		m.view = viewCompose
	case "s":
		if !m.syncing {
			m.syncing = true
			m.setStatus("Syncing…")
			return m, syncCmd()
		}
	case "u":
		m.unreadOnly = !m.unreadOnly
		m.cursor = 0
		return m, loadMsgsCmd(m.unreadOnly, m.searchQ, m.activeAccount())
	case "/":
		m.searching = true
		m.searchInput.Focus()
		m.searchInput.SetValue("")
	case "esc":
		if m.searchQ != "" {
			m.searchQ = ""
			m.searchInput.SetValue("")
			m.cursor = 0
			return m, loadMsgsCmd(m.unreadOnly, "", m.activeAccount())
		}
	}
	return m, nil
}

func (m Model) updateDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		m.view = viewList
		m.detail = nil
		return m, nil
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
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m Model) updateCompose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		return m, sendCmd(m.toInput.Value(), m.subjectInput.Value(), m.bodyArea.Value())
	case "ctrl+d":
		return m, draftCmd(m.toInput.Value(), m.subjectInput.Value(), m.bodyArea.Value())
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
		return m.renderCompose()
	default:
		return m.renderList()
	}
}

func (m Model) renderList() string {
	var b strings.Builder

	// ── tab bar ──
	if len(m.accounts) > 0 {
		var parts []string
		for i, a := range m.accounts {
			label := a
			if i == m.activeTab {
				parts = append(parts, styleTabActive.Render(label))
			} else {
				parts = append(parts, styleTabInact.Render(label))
			}
		}
		bar := strings.Join(parts, "")
		if m.syncing {
			bar += "  " + styleSyncing.Render("⟳ syncing…")
		}
		b.WriteString(bar + "\n")
	} else {
		title := "mailctl"
		if m.syncing {
			title += "  " + styleSyncing.Render("⟳")
		}
		b.WriteString(styleHeader.Render(title) + "\n")
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", m.width)) + "\n")

	// ── filter chips ──
	overhead := 3 // tab bar + divider + status bar
	if m.unreadOnly || m.searchQ != "" {
		var chips []string
		if m.unreadOnly {
			chips = append(chips, styleTabInact.Render("unread"))
		}
		if m.searchQ != "" {
			chips = append(chips, styleTabInact.Render("/"+m.searchQ))
		}
		b.WriteString(strings.Join(chips, "  ") + "\n")
		overhead++
	}

	// ── search input ──
	if m.searching {
		b.WriteString("  " + m.searchInput.View() + "\n\n")
		overhead += 2
	}

	// ── message list ──
	listH := m.height - overhead
	if listH < 1 {
		listH = 1
	}

	if len(m.msgs) == 0 {
		b.WriteString("\n" + styleHelp.Render("  No messages — press s to sync") + "\n")
	} else {
		start := 0
		if m.cursor >= listH {
			start = m.cursor - listH + 1
		}
		end := min(len(m.msgs), start+listH)
		showAcct := m.activeTab == 0 // show account badge in Alle tab
		for i := start; i < end; i++ {
			row := &m.msgs[i]
			line := formatListRow(row, m.width, showAcct)
			switch {
			case i == m.cursor:
				line = styleSelected.Width(m.width).Render(line)
			case !row.Read:
				line = styleUnread.Render(line)
			default:
				line = styleRead.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}

	// ── status / help bar ──
	countStr := ""
	if len(m.msgs) > 0 {
		countStr = styleHelp.Render(fmt.Sprintf(" %d msgs", len(m.msgs)))
	}
	var bar string
	if m.err != nil {
		bar = styleErr.Render("✗ " + m.err.Error())
	} else if m.status != "" {
		bar = styleOK.Render("✓ " + m.status)
	} else {
		bar = styleHelp.Render("enter:open  n:new  s:sync  u:unread  /:search  tab:acct  q:quit")
	}
	// right-align count
	pad := m.width - lipgloss.Width(bar) - lipgloss.Width(countStr)
	if pad < 0 {
		pad = 0
	}
	b.WriteString("\n" + bar + strings.Repeat(" ", pad) + countStr)
	return b.String()
}

func (m Model) renderDetail() string {
	if m.detail == nil {
		return ""
	}
	var b strings.Builder

	// header block
	b.WriteString(styleSubject.Render(m.detail.Subject) + "\n")
	b.WriteString(styleLabel.Render("From:") + " " + m.detail.From + "\n")
	if len(m.detail.To) > 0 {
		b.WriteString(styleLabel.Render("To:") + " " + strings.Join(m.detail.To, ", ") + "\n")
	}
	b.WriteString(styleLabel.Render("Date:") + " " + m.detail.Date.Format("Mon, 02 Jan 2006  15:04") + "\n")
	if m.detail.Account != "" {
		b.WriteString(styleLabel.Render("Account:") + " " + styleMeta.Render(m.detail.Account) + "\n")
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", m.width)) + "\n")

	// body viewport (height recalculated from current window)
	m.vp.Height = m.detailBodyHeight()
	b.WriteString(m.vp.View())

	// scroll hint
	pct := ""
	if m.vp.TotalLineCount() > m.vp.Height {
		pct = fmt.Sprintf(" %d%%", int(m.vp.ScrollPercent()*100))
	}
	b.WriteString("\n" + styleHelp.Render("esc:back  r:reply  ↑↓/jk:scroll  q:quit") + styleMeta.Render(pct))
	return b.String()
}

func (m Model) renderCompose() string {
	title := "New Message"
	if m.replyTo != nil {
		title = "Reply"
	}
	var b strings.Builder
	b.WriteString(styleHeader.Render(title) + "\n")
	b.WriteString(styleDivider.Render(strings.Repeat("─", m.width)) + "\n\n")

	focused := func(i int) string {
		if m.composeFocus == i {
			return styleTabActive.Render("›")
		}
		return "  "
	}

	b.WriteString(focused(focusTo) + " " + styleLabel.Render("To:") + "      " + m.toInput.View() + "\n")
	b.WriteString(focused(focusSubject) + " " + styleLabel.Render("Subject:") + "  " + m.subjectInput.View() + "\n\n")
	b.WriteString(focused(focusBody) + " " + styleLabel.Render("Body:") + "\n")
	b.WriteString(m.bodyArea.View() + "\n\n")

	if m.err != nil {
		b.WriteString(styleErr.Render("✗ " + m.err.Error()) + "\n")
	} else {
		b.WriteString(styleHelp.Render("tab:next field  ctrl+s:send  ctrl+d:save draft  esc:cancel"))
	}
	return b.String()
}

// ── Commands ──────────────────────────────────────────────────────────────────

func loadMsgsCmd(unreadOnly bool, query, account string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return errMsg{err}
		}
		defer s.Close()
		ctx := context.Background()
		msgs, err := s.ListMessages(ctx, store.Filter{
			Account:    account,
			UnreadOnly: unreadOnly,
			Query:      query,
			Limit:      500,
		})
		if err != nil {
			return errMsg{err}
		}
		accounts, _ := s.ListAccounts(ctx)
		return msgsLoadedMsg{msgs: msgs, accounts: accounts}
	}
}

func syncCmd() tea.Cmd {
	return func() tea.Msg {
		msgs, err := mail.FetchInbox(150, false)
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
		accounts, _ := s.ListAccounts(ctx)
		return syncDoneMsg{count: len(msgs), accounts: accounts}
	}
}

func loadBodyCmd(msg *models.Message) tea.Cmd {
	subject, from := msg.Subject, msg.From
	return func() tea.Msg {
		body, err := mail.FetchMessageBody(subject, from)
		return bodyLoadedMsg{body: body, err: err}
	}
}

func markReadCmd(id string) tea.Cmd {
	return func() tea.Msg {
		s, err := store.New(config.DBPath())
		if err != nil {
			return readMarkedMsg{}
		}
		defer s.Close()
		_ = s.MarkRead(context.Background(), id)
		return readMarkedMsg{}
	}
}

func sendCmd(to, subject, body string) tea.Cmd {
	return func() tea.Msg {
		d := &models.Draft{To: []string{to}, Subject: subject, Body: body}
		if err := mail.Send(d); err != nil {
			return sentMsg{err}
		}
		return sentMsg{}
	}
}

func draftCmd(to, subject, body string) tea.Cmd {
	return func() tea.Msg {
		d := &models.Draft{To: []string{to}, Subject: subject, Body: body}
		if err := mail.SaveDraft(d); err != nil {
			return draftedMsg{err}
		}
		return draftedMsg{}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *Model) resetCompose(to, subject string) {
	m.toInput.SetValue(to)
	m.subjectInput.SetValue(subject)
	m.bodyArea.SetValue("")
	m.composeFocus = focusTo
	m.toInput.Focus()
	m.subjectInput.Blur()
	m.bodyArea.Blur()
}

func (m *Model) blurCompose(f int) {
	switch f {
	case focusTo:
		m.toInput.Blur()
	case focusSubject:
		m.subjectInput.Blur()
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
	case focusBody:
		m.bodyArea.Focus()
	}
}

func (m *Model) setStatus(s string) {
	m.status = s
	m.statusTime = time.Now()
}

// detailBodyHeight calculates how many lines the viewport can use.
func (m Model) detailBodyHeight() int {
	// subject + from + to + date + account + divider = up to 6 lines
	// status bar = 1 line
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

func formatListRow(msg *models.Message, width int, showAcct bool) string {
	dot := "○"
	if !msg.Read {
		dot = "●"
	}

	// smart date
	date := smartDate(msg.Date)

	// from: strip email, truncate
	from := msg.From
	if idx := strings.Index(from, "<"); idx > 0 {
		from = strings.TrimSpace(from[:idx])
	}
	if len(from) > 20 {
		from = from[:19] + "…"
	}

	// account badge (only in Alle tab)
	acctBadge := ""
	if showAcct && msg.Account != "" {
		short := acctShort(msg.Account)
		acctBadge = styleAcctBadge.Render("[" + short + "]")
	}
	acctW := lipgloss.Width(acctBadge)

	// subject truncation: fill remaining width
	// layout: "●  Jan 02 15:04  FromName……………  [Acct]  Subject…"
	fixed := 2 + 14 + 2 + 20 + 2 + acctW + 2 // dot+sp, date, sp, from, sp, badge, sp
	subjectW := width - fixed
	if subjectW < 10 {
		subjectW = 10
	}
	subject := msg.Subject
	if len(subject) > subjectW {
		subject = subject[:subjectW-1] + "…"
	}

	line := fmt.Sprintf("%s  %-14s  %-20s  ", dot, date, from)
	if showAcct {
		line += acctBadge + "  "
	}
	line += subject
	return line
}

func formatDetail(msg *models.Message, width int) string {
	body := strings.TrimSpace(msg.Body)
	if body == "" {
		return styleMeta.Render("(no body)")
	}
	// soft-wrap long lines
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		if len(l) > width {
			for len(l) > width {
				lines = append(lines, l[:width])
				l = l[width:]
			}
		}
		lines = append(lines, l)
	}
	return strings.Join(lines, "\n")
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
		return t.Format("        15:04") // today: just time, right-aligned
	case t.After(now.AddDate(0, 0, -6)):
		return t.Format("Mon     15:04") // this week: weekday + time
	case t.Year() == now.Year():
		return t.Format("Jan 02  15:04") // this year: month day + time
	default:
		return t.Format("Jan 02   2006") // older: month day year
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
	if len(words) == 0 {
		return name
	}
	if len(words) == 1 {
		if len(name) > 8 {
			return name[:8]
		}
		return name
	}
	// "Gerwin @ Brücke" → "Brücke"
	return words[len(words)-1]
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
