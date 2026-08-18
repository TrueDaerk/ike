package ghissues

// view.go renders the pane: the list with label chips and PR markers, the
// filter line, and the glamour-rendered detail view (the markdown pipeline
// the preview pane uses, #62).

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	"charm.land/glamour/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"

	"ike/internal/forge"
	"ike/internal/theme"
	"ike/internal/ui"
)

// View renders the title header, the body (list or detail), and the footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.headerLine(pal))
	b.WriteString("\n")
	if m.detail {
		b.WriteString(m.renderDetail(pal, m.bodyHeight()))
	} else {
		b.WriteString(m.renderRows(pal, m.bodyHeight()))
	}
	b.WriteString(m.footer(pal))
	return b.String()
}

// headerLine renders the title accented, the counts and active filters faint.
func (m *Model) headerLine(pal *theme.Palette) string {
	head := lipgloss.NewStyle().Foreground(pal.Accent).Bold(m.focused).Render(" " + m.Title())
	var notes []string
	if lf := m.LabelFilter(); lf != "" {
		notes = append(notes, "label: "+lf)
	}
	if m.fInput != "" && !m.fEditing {
		notes = append(notes, "filter: "+m.fInput)
	}
	if len(notes) > 0 {
		head += lipgloss.NewStyle().Foreground(pal.Warning).Render("   " + strings.Join(notes, " · "))
	}
	return head
}

// renderRows draws the filtered list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	if len(m.visible) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" "+m.emptyText()) + strings.Repeat("\n", height)
	}
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.top + k
		if i < len(m.visible) {
			b.WriteString(m.renderRow(pal, i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// emptyText explains the empty pane per state.
func (m *Model) emptyText() string {
	switch {
	case m.setup != "":
		return m.setup
	case m.loading:
		return "(fetching issues…)"
	case m.errMsg != "":
		return "(fetch failed: " + m.errMsg + " — r retries)"
	case m.loaded && len(m.issues) == 0:
		return "(no open issues)"
	case m.loaded:
		return "(no issues match the filter)"
	default:
		return "(press r to fetch the issue list)"
	}
}

// renderRow draws one issue line: number accented, title, label chips in the
// forge's colors, assignee and the linked PR's state.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	is := &m.issues[m.visible[i]]
	selected := i == m.cursor
	base := lipgloss.NewStyle().Foreground(pal.Foreground)
	num := lipgloss.NewStyle().Foreground(pal.Accent)
	if selected {
		bg := pal.SelectionMuted
		if m.focused {
			bg = pal.Selection
		}
		base = base.Background(bg).Bold(m.focused)
		num = num.Background(bg)
	}

	meta := m.rowMeta(pal, is)
	numText := " #" + strconv.Itoa(is.Number) + " "
	budget := m.width - lipgloss.Width(meta) - lipgloss.Width(numText)
	if budget < 12 {
		meta = ""
		budget = m.width - lipgloss.Width(numText)
	}
	title := is.Title
	if r := []rune(title); budget > 1 && len(r) > budget {
		title = string(r[:budget-1]) + "…"
	}
	line := num.Render(numText) + base.Render(title)
	if meta != "" {
		pad := m.width - lipgloss.Width(line) - lipgloss.Width(meta)
		if pad > 0 {
			line += base.Render(strings.Repeat(" ", pad))
		}
		line += meta
	}
	return line
}

// rowMeta renders the right-aligned metadata: chips, assignee, PR state.
func (m *Model) rowMeta(pal *theme.Palette, is *forge.Issue) string {
	var parts []string
	for _, l := range is.Labels {
		parts = append(parts, chip(l))
	}
	if len(is.Assignees) > 0 {
		parts = append(parts, lipgloss.NewStyle().Faint(true).Render("@"+strings.Join(is.Assignees, ",")))
	}
	if pr := forge.PRForIssue(m.prs, is.Number); pr != nil {
		parts = append(parts, m.prMarker(pal, pr))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// prMarker renders the linked PR compactly: number plus state/CI glyph.
func (m *Model) prMarker(pal *theme.Palette, pr *forge.PR) string {
	glyph, col := prGlyph(pal, pr)
	return lipgloss.NewStyle().Foreground(col).Render("PR#" + strconv.Itoa(pr.Number) + glyph)
}

// prGlyph maps a PR to its one-glyph state: merged/closed first, then the CI
// rollup of an open PR.
func prGlyph(pal *theme.Palette, pr *forge.PR) (string, color.Color) {
	switch strings.ToUpper(pr.State) {
	case "MERGED":
		return "⇌", pal.Info
	case "CLOSED":
		return "×", pal.Error
	}
	switch pr.Checks {
	case forge.ChecksFailing:
		return "✗", pal.Error
	case forge.ChecksPending:
		return "…", pal.Warning
	case forge.ChecksPassing:
		return "✓", pal.Success
	default:
		return "", pal.Info
	}
}

// prLine is the detail view's full PR sentence, "" without a linked PR.
func (m *Model) prLine(pr *forge.PR) string {
	if pr == nil {
		return ""
	}
	s := fmt.Sprintf("PR #%d — %s", pr.Number, strings.ToLower(pr.State))
	switch pr.Checks {
	case forge.ChecksFailing:
		s += ", checks failing"
	case forge.ChecksPending:
		s += ", checks running"
	case forge.ChecksPassing:
		s += ", checks passing"
	}
	return s
}

// chip renders one label as a colored pill in the forge-assigned color, with
// black/white text picked for contrast; an unparsable color degrades to a
// plain bracketed name.
func chip(l forge.Label) string {
	bg, ok := parseHex(l.Color)
	if !ok {
		return "[" + l.Name + "]"
	}
	fg := lipgloss.Color("#000000")
	if luminance(bg) < 140 {
		fg = lipgloss.Color("#ffffff")
	}
	return lipgloss.NewStyle().Background(bg).Foreground(fg).Render(" " + l.Name + " ")
}

// parseHex reads a bare rrggbb (or #rrggbb) label color.
func parseHex(s string) (color.RGBA, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return color.RGBA{}, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return color.RGBA{}, false
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

// luminance is the perceived brightness (0..255) picking the chip text color.
func luminance(c color.RGBA) int {
	return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
}

// footer shows the key hints, or the open filter line in their place.
func (m *Model) footer(pal *theme.Palette) string {
	if m.fEditing {
		prefix := lipgloss.NewStyle().Faint(true).Render(" filter: ")
		return prefix + ui.CursorView(m.fInput, m.fCur)
	}
	hints := " enter detail · s start work · o browser · / filter · l label · r refresh"
	if m.detail {
		hints = " esc back · s start work · o browser · j/k scroll · r refresh"
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(hints))
}

// renderDetail draws the selected issue's rendered body, scrolled.
func (m *Model) renderDetail(pal *theme.Palette, height int) string {
	is := m.Selected()
	if is == nil {
		m.detail = false
		return m.renderRows(pal, height)
	}
	m.ensureDetail(is)
	m.clampDetail()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.detailTop + k
		if i < len(m.detailLines) {
			b.WriteString(m.detailLines[i])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ensureDetail (re)renders the detail lines when the issue or width changed.
func (m *Model) ensureDetail(is *forge.Issue) {
	if m.detailFor == is.Number && m.detailW == m.width && m.detailLines != nil {
		return
	}
	m.detailFor, m.detailW = is.Number, m.width
	m.detailTop = 0
	head := "# #" + strconv.Itoa(is.Number) + " " + is.Title + "\n\n"
	if pr := forge.PRForIssue(m.prs, is.Number); pr != nil {
		head += "**" + m.prLine(pr) + "**\n\n"
	}
	body := is.Body
	if strings.TrimSpace(body) == "" {
		body = "*(no description)*"
	}
	out, err := m.renderMarkdown(head + body)
	if err != nil {
		out = head + body
	}
	m.detailLines = strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// renderMarkdown renders through a fresh width- and theme-bound glamour
// renderer, the preview pane's pattern (#62).
func (m *Model) renderMarkdown(src string) (string, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(m.styleConfig()),
		glamour.WithWordWrap(max(10, m.width-2)),
	)
	if err != nil {
		return "", err
	}
	return r.Render(src)
}

// styleConfig picks the stock glamour style off the palette's dark flag and
// maps the heading and link colors onto the active palette.
func (m *Model) styleConfig() gansi.StyleConfig {
	pal := m.theme()
	cfg := styles.LightStyleConfig
	if pal.Dark {
		cfg = styles.DarkStyleConfig
	}
	accent := hexColor(pal.Accent)
	link := hexColor(pal.Info)
	cfg.Heading.Color = &accent
	cfg.Link.Color = &link
	cfg.LinkText.Color = &accent
	return cfg
}

// hexColor formats a palette color as the #rrggbb string glamour styles take.
func hexColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
