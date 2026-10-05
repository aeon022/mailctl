package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/emptystate"
	"github.com/aeon022/missionctl-core/humanize"
	"github.com/aeon022/missionctl-core/keymap"
	"github.com/aeon022/missionctl-core/overlay"
	"github.com/aeon022/missionctl-core/palette"
	"github.com/aeon022/missionctl-core/statusbar"
)

// ── View ──────────────────────────────────────────────────────────────────────

func (m Model) View() tea.View {
	v := tea.NewView(m.viewContent())
	// v1's tea.WithAltScreen()/WithMouseAllMotion() Program options are
	// gone in v2 — AltScreen/MouseMode are now per-View fields, set on
	// every render instead of once at Program startup. Same pattern as
	// notectl's v2 migration.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	v.ReportFocus = true // FocusMsg → reload the list when the window regains focus
	return v
}

func (m Model) viewContent() string {
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
		return overlay.CenterDim(m.renderList(), m.renderHelpPopup(), m.width, m.height, 0)
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
		Row("o", "open in mail client").
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

	vp := viewport.New(viewport.WithWidth(popW-6), viewport.WithHeight(popH-5)) // border 1+1, padding(1,2) → 2 rows/4 cols; -1 row for footer
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
	if m.helpVP.TotalLineCount() > m.helpVP.Height() {
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

	preListLines := strings.Count(b.String(), "\n")
	if m.loading {
		b.WriteString(emptystate.Loading(w, listH, m.sp.View(), "Loading messages…") + "\n")
	} else if len(m.msgs) == 0 {
		b.WriteString(emptystate.Render(w, listH, "✉", "No messages", "press s to sync") + "\n")
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
	// Pin the status bar to the bottom of the screen instead of letting it
	// glue itself right under a short list — pad the list block out to its
	// full line budget, same pattern taskctl/notectl use.
	for written := strings.Count(b.String(), "\n") - preListLines; written < listH; written++ {
		b.WriteString("\n")
	}

	// ── status / help bar ──
	countStr := ""
	if len(m.msgs) > 0 {
		countStr = styleHelp.Render(fmt.Sprintf("%d/%d", m.cursor+1, len(m.msgs)))
	}
	var helpBar string
	if m.err != nil {
		helpBar = styleErr.Render("✗ " + m.err.Error())
	} else if m.status != "" {
		helpBar = styleOK.Render("✓ " + m.status)
	} else {
		helpBar = statusbar.Hints(w-lipgloss.Width(countStr)-2,
			[2]string{"enter", "open"}, [2]string{"n", "new"}, [2]string{"s", "sync"}, [2]string{"?", "help"},
			[2]string{"q", "quit"}, [2]string{"/", "search"}, [2]string{"u", "unread"}, [2]string{"d", "delete"},
			[2]string{"y", "copy"}, [2]string{"o", "mail"}, [2]string{"tab", "acct"})
	}
	b.WriteString(styleDivider.Render(strings.Repeat("─", w)) + "\n")
	// Line keeps the count flush right and truncates the left side (never the
	// count, never past w) when a long error/status message leaves no room.
	b.WriteString(statusbar.Line(w, helpBar, countStr))
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
	// Floor at a sane minimum, same defensive pattern as the WindowSizeMsg
	// handler's height clamp — a degenerate/transient terminal width (0, or
	// anything smaller than the padding+margin this subtracts) would
	// otherwise go negative here and panic downstream on strings.Repeat
	// with a negative count (renderDetail, formatDetail's wrap width, the
	// scrollbar). Found via TestProgramSmoke_DetailScrollHeavy driving a
	// real tea.Program through a startup race where the terminal's initial
	// size query can report 0x0 before the real size arrives.
	if w := m.width - detailPadH*2 - 1; w >= 10 {
		return w
	}
	return 10
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
	m.vp.SetWidth(w - 2) // leave 2 cols for scrollbar track
	m.vp.SetHeight(m.detailBodyHeight())
	b.WriteString(renderScrollbar(m.vp))

	// ── footer ──
	b.WriteString("\n\n" + styleDivider.Render(strings.Repeat("─", w)) + "\n")
	hints := [][2]string{{"esc", "back"}, {"r", "reply"}, {"q", "quit"}, {"a", "ai draft"}, {"u", "unread"},
		{"d", "delete"}, {"y", "copy"}, {"o", "mail"}, {"↑↓/jk", "scroll"}}
	if m.detail != nil && findUnsubscribeURL(m.detail.Body) != "" {
		hints = append(hints, [2]string{"U", "unsubscribe"})
	}
	b.WriteString(statusbar.Hints(w, hints...))
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
	h := vp.Height()
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
		if lw := lipgloss.Width(l); lw < vp.Width() {
			l += strings.Repeat(" ", vp.Width()-lw)
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
