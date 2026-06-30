package tui

import (
	"context"
	"fmt"
	"hash/fnv"
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
	// palette
	colorBlue    = lipgloss.AdaptiveColor{Light: "25",  Dark: "33"}
	colorGreen   = lipgloss.AdaptiveColor{Light: "28",  Dark: "42"}
	colorRed     = lipgloss.AdaptiveColor{Light: "160", Dark: "203"}
	colorMuted   = lipgloss.AdaptiveColor{Light: "243", Dark: "246"} // readable on both bg
	colorSubtle  = lipgloss.AdaptiveColor{Light: "250", Dark: "239"}
	colorTabBg   = lipgloss.AdaptiveColor{Light: "252", Dark: "235"} // inactive tab bg

	// tab bar
	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(colorBlue).
			Padding(0, 3)
	styleTabInact = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "237", Dark: "252"}).
			Background(colorTabBg).
			Padding(0, 3)

	// list
	styleDivider   = lipgloss.NewStyle().Foreground(colorSubtle)
	styleUnread    = lipgloss.NewStyle().Bold(true)
	styleRead      = lipgloss.NewStyle().Foreground(colorMuted)
	styleSelected  = lipgloss.NewStyle().
				Background(lipgloss.AdaptiveColor{Light: "189", Dark: "17"}).
				Foreground(lipgloss.AdaptiveColor{Light: "16",  Dark: "255"}).
				Bold(true)
	styleAcctBadge = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "75"})

	// detail / compose
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(colorBlue)
	styleSubject = lipgloss.NewStyle().Bold(true)
	styleMeta    = lipgloss.NewStyle().Foreground(colorMuted)
	styleLabel   = lipgloss.NewStyle().Foreground(colorBlue).Width(9)

	// status
	styleHelp    = lipgloss.NewStyle().Foreground(colorMuted)
	styleErr     = lipgloss.NewStyle().Foreground(colorRed)
	styleOK      = lipgloss.NewStyle().Foreground(colorGreen)
	styleSyncing = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "214", Dark: "220"})
	styleToday   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "214", Dark: "220"}).Bold(true)
)

// senderPalette: 8 distinct colors, avoid red/green (used for status).
// Pad sender name BEFORE applying color so ANSI codes don't break width math.
var senderPalette = []lipgloss.AdaptiveColor{
	{Light: "25",  Dark: "39"},  // blue
	{Light: "91",  Dark: "135"}, // purple
	{Light: "30",  Dark: "43"},  // teal
	{Light: "130", Dark: "173"}, // orange
	{Light: "23",  Dark: "44"},  // dark cyan
	{Light: "125", Dark: "168"}, // magenta
	{Light: "58",  Dark: "136"}, // gold
	{Light: "17",  Dark: "69"},  // navy
}

func senderStyle(from string) lipgloss.Style {
	h := fnv.New32a()
	_, _ = h.Write([]byte(extractEmail(from)))
	return lipgloss.NewStyle().Foreground(senderPalette[int(h.Sum32())%len(senderPalette)])
}

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
		var parts []string
		for i, a := range m.accounts {
			if i == m.activeTab {
				parts = append(parts, styleTabActive.Render(a))
			} else {
				parts = append(parts, styleTabInact.Render(a))
			}
		}
		bar := strings.Join(parts, "  ")
		if m.syncing {
			bar += "  " + styleSyncing.Render("⟳ syncing…")
		}
		b.WriteString(bar + "\n")
	} else if m.syncing {
		b.WriteString(styleSyncing.Render("⟳ syncing…") + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", w)) + "\n")

	// ── filter chips ──
	// overhead: header(1) + tab(1) + divider(1) + statusbar(2) = 5
	overhead := 5
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
		showAcct := m.activeTab == 0
		for i := start; i < end; i++ {
			row := &m.msgs[i]
			line := formatListRow(row, w, showAcct)
			switch {
			case i == m.cursor:
				line = styleSelected.Width(w).Render(line)
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
	var helpBar string
	if m.err != nil {
		helpBar = styleErr.Render("✗ " + m.err.Error())
	} else if m.status != "" {
		helpBar = styleOK.Render("✓ " + m.status)
	} else {
		helpBar = styleHelp.Render("enter:open  n:new  s:sync  u:unread  /:search  tab:acct  q:quit")
	}
	rightPad := w - lipgloss.Width(helpBar) - lipgloss.Width(countStr)
	if rightPad < 0 {
		rightPad = 0
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", w)) + "\n")
	b.WriteString(helpBar + strings.Repeat(" ", rightPad) + countStr)
	return b.String()
}

func (m Model) renderDetail() string {
	if m.detail == nil {
		return ""
	}
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
	b.WriteString(styleHelp.Render("esc:back  r:reply  ↑↓/jk:scroll  q:quit"))
	return b.String()
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
		var sb strings.Builder
		for _, l := range lines {
			sb.WriteString(l + "\n")
		}
		return strings.TrimRight(sb.String(), "\n")
	}

	// compute thumb size and position
	thumbH := max(1, h*h/total)
	thumbTop := int(vp.ScrollPercent() * float64(h-thumbH))

	track := styleDivider.Render("│")
	thumb := styleMeta.Render("█")

	var sb strings.Builder
	for i, l := range lines {
		glyph := track
		if i >= thumbTop && i < thumbTop+thumbH {
			glyph = thumb
		}
		sb.WriteString(l + " " + glyph + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
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
	// subject(1) + from(1) + to(1) + date(1) + account(1) + divider(1)
	// + footer-divider(1) + help(1) = 8
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

	// ── date column (14 chars, pad BEFORE styling) ──
	dateRaw := smartDate(msg.Date)
	datePadded := fmt.Sprintf("%-14s", dateRaw)
	var dateStyled string
	if strings.HasPrefix(dateRaw, "Today") {
		dateStyled = styleToday.Render(datePadded)
	} else {
		dateStyled = styleMeta.Render(datePadded)
	}

	// ── from column (20 chars, pad BEFORE styling) ──
	from := msg.From
	if idx := strings.Index(from, "<"); idx > 0 {
		from = strings.TrimSpace(from[:idx])
	}
	if len(from) > 20 {
		from = from[:19] + "…"
	}
	fromPadded := fmt.Sprintf("%-20s", from)
	fromStyled := senderStyle(msg.From).Render(fromPadded)

	// ── account badge (only in Alle tab, always 12 chars wide: [xxxxxxxx]·· ) ──
	const badgeInner = 8 // fixed visual width of text inside brackets
	const badgeTotal = badgeInner + 2 + 2 // "[" + inner + "]" + "  "
	acctBadge := ""
	acctW := 0
	if showAcct && msg.Account != "" {
		inner := runeLimit(acctShort(msg.Account), badgeInner)
		inner = inner + strings.Repeat(" ", badgeInner-lipgloss.Width(inner)) // pad to 8
		acctBadge = styleAcctBadge.Render("["+inner+"]") + "  "
		acctW = badgeTotal
	}

	// ── subject: fill remaining width ──
	// dot(1) + 2 + date(14) + 2 + from(20) + 2 + acctW + subject
	fixed := 1 + 2 + 14 + 2 + 20 + 2 + acctW
	subjectW := width - fixed
	if subjectW < 10 {
		subjectW = 10
	}
	subject := msg.Subject
	if len(subject) > subjectW {
		subject = subject[:subjectW-1] + "…"
	}

	return dot + "  " + dateStyled + "  " + fromStyled + "  " + acctBadge + subject
}

func formatDetail(msg *models.Message, width int) string {
	body := strings.TrimSpace(msg.Body)
	if body == "" {
		return styleMeta.Render("(no body)")
	}
	w := min(width-2, 128) // leave room for scrollbar track
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		if len(l) > w {
			for len(l) > w {
				lines = append(lines, l[:w])
				l = l[w:]
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
		return "Today   " + t.Format("15:04")
	case t.After(now.AddDate(0, 0, -6)):
		return t.Format("Mon     15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 02  15:04")
	default:
		return t.Format("Jan 02   2006")
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
