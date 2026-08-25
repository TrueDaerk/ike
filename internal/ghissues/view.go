package ghissues

// view.go renders the pane (#2090): the tab bar, the filter row, the active
// full-area view — the issue list with its age/author columns and optional
// label groups, the PR list, or the glamour-rendered issue detail with its
// position header — the footer of the current view's actions, and the two
// modals composited over the body.

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
	"ike/internal/overlay"
	"ike/internal/theme"
	"ike/internal/ui"
)

// Column widths of the list's right-hand metadata. Both are dropped whole
// when the pane is too narrow to keep the title readable.
const (
	authorColW = 10
	ageColW    = 4
	branchColW = 22
)

// View renders the tab bar, the filter row, the body (list, PR list or
// detail) with any modal composited on it, and the footer.
func (m *Model) View() string {
	pal := m.theme()
	var b strings.Builder
	b.WriteString(m.tabBar(pal))
	b.WriteString("\n")
	if m.filterRowShown() {
		b.WriteString(m.filterRow(pal))
		b.WriteString("\n")
	}
	height := m.bodyHeight()
	var body string
	switch {
	case m.detail && m.tab == TabIssues:
		b.WriteString(m.detailHeader(pal))
		b.WriteString("\n")
		body = m.renderDetail(pal, height)
	case m.prDetail && m.tab == TabPRs:
		b.WriteString(m.prDetailHeader(pal))
		b.WriteString("\n")
		body = m.renderPRDetail(pal, height)
	case m.tab == TabPRs:
		body = m.renderPRRows(pal, height)
	default:
		body = m.renderRows(pal, height)
	}
	if m.ov != ovNone {
		body = overlay.Center(strings.TrimRight(body, "\n"), m.overlayBox(pal), m.width, height)
		body = padLines(body, height)
	}
	b.WriteString(body)
	b.WriteString(m.footer(pal))
	return b.String()
}

// padLines makes sure a body block occupies exactly height rows, so the
// footer never rides up after compositing.
func padLines(body string, height int) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	var b strings.Builder
	for k := 0; k < height; k++ {
		if k < len(lines) {
			b.WriteString(lines[k])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// tabLabels are the tab bar's two entries with their filtered counts.
func (m *Model) tabLabels() []string {
	issues, prs := "Issues", "PRs"
	if m.loaded {
		issues += " " + strconv.Itoa(len(m.visible))
		prs += " " + strconv.Itoa(len(m.prVisible))
	}
	return []string{issues, prs}
}

// tabBar renders the pane's first row: the two view labels, the active one
// accented, separated like the editor's tab bar, with the fetch state noted
// right-aligned.
func (m *Model) tabBar(pal *theme.Palette) string {
	active := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	idle := lipgloss.NewStyle().Foreground(pal.Foreground).Faint(true)
	frame := lipgloss.NewStyle().Foreground(pal.Border)
	var b strings.Builder
	for i, label := range m.tabLabels() {
		if i > 0 {
			b.WriteString(frame.Render("│"))
		}
		style := idle
		if Tab(i) == m.tab {
			style = active
		}
		b.WriteString(style.Render(" " + label + " "))
	}
	line := b.String()
	if note := m.stateNote(); note != "" {
		noted := lipgloss.NewStyle().Foreground(pal.Warning).Render(note + " ")
		if pad := m.width - lipgloss.Width(line) - lipgloss.Width(noted); pad > 0 {
			line += strings.Repeat(" ", pad) + noted
		}
	}
	return line
}

// tabBarSpans returns the [start, end) column range of each tab label, so a
// click on the bar resolves to the view it drew.
func (m *Model) tabBarSpans() [][2]int {
	spans := make([][2]int, 0, 2)
	x := 0
	for i, label := range m.tabLabels() {
		if i > 0 {
			x++ // the │ separator
		}
		w := lipgloss.Width(label) + 2 // the label's padding spaces
		spans = append(spans, [2]int{x, x + w})
		x += w
	}
	return spans
}

// stateNote is the short right-aligned fetch state on the tab bar.
func (m *Model) stateNote() string {
	switch {
	case m.cached:
		// The listing is the persisted snapshot (#2108): usable, but stale
		// until the fetch (or the next background poll) replaces it.
		return "cached · updating…"
	case m.loading:
		return "fetching…"
	case m.setup != "":
		return "unavailable"
	case m.errMsg != "":
		return "fetch failed"
	}
	return ""
}

// filterRow renders the permanent status row under the tab bar (#2104): the
// active-filter chips — each clearable by a click, the geometry mirrored by
// chipSpans —, a mutation's in-flight or error state beside them rather than
// instead of them (#2088), and a faint hint while nothing narrows.
func (m *Model) filterRow(pal *theme.Palette) string {
	type seg struct {
		text  string
		style lipgloss.Style
	}
	warn := lipgloss.NewStyle().Foreground(pal.Warning)
	var segs []seg
	for _, c := range m.filterChips() {
		segs = append(segs, seg{chipText(c), warn})
	}
	switch {
	case m.mutErr != "":
		segs = append(segs, seg{"⚠ " + m.mutErr, lipgloss.NewStyle().Foreground(pal.Error)})
	case m.mutBusy > 0:
		segs = append(segs, seg{"applying the change…", lipgloss.NewStyle().Faint(true)})
	}
	if len(segs) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" (f filters the list)")
	}
	out, used := "", 0
	for _, s := range segs {
		text := " " + s.text
		if m.width > 0 && used+len([]rune(text)) > m.width {
			text = truncate(text, m.width-used)
		}
		out += s.style.Render(text)
		used += len([]rune(text))
		if m.width > 0 && used >= m.width {
			break
		}
	}
	return out
}

// filterOvRows renders the filter overlay's rows (#2104): the match input,
// the state radio, the sort cycle, the grouping toggle and — on the issue
// view — one row per label with its chip and count. The row under the cursor
// is accented, exactly like every other overlay.
func (m *Model) filterOvRows(pal *theme.Palette) []string {
	sel := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	plain := lipgloss.NewStyle().Foreground(pal.Foreground)
	style := func(i int) lipgloss.Style {
		if i == m.ovCursor {
			return sel
		}
		return plain
	}
	var rows []string
	if m.ovCursor == fovMatch {
		rows = append(rows, sel.Render("match: ")+ui.CursorView(m.fInput, m.fCur))
	} else if m.fInput == "" {
		rows = append(rows, plain.Render("match: ")+lipgloss.NewStyle().Faint(true).Render("(type on this row)"))
	} else {
		rows = append(rows, plain.Render("match: "+m.fInput))
	}
	rows = append(rows, style(fovState).Render("state: "+stateRadio(m.state)))
	rows = append(rows, style(fovSort).Render("sort:  ‹ "+m.sort.String()+" ›"))
	if m.tab == TabPRs {
		return rows
	}
	mark := "[ ]"
	if m.group {
		mark = "[x]"
	}
	rows = append(rows, style(fovGroup).Render(mark+" group by label"))
	rows = append(rows, style(fovMode).Render("labels: "+labelModeRadio(m.labelAll)))
	labels := m.filterViewLabels()
	if len(labels) == 0 && m.ovSearch.Active() {
		// The placeholder fovLabelRows() reserves (#2111): the query is still
		// editable, so the section says why it is empty instead of vanishing.
		return append(rows, lipgloss.NewStyle().Faint(true).
			Render("(no label matches "+m.ovSearch.Query()+")"))
	}
	for i, l := range labels {
		mark := "[ ] "
		if m.labelSel[l.Name] {
			mark = "[x] "
		}
		st := style(m.fovFixedRows() + i)
		rows = append(rows, st.Render(mark)+chip(l)+st.Render("  "+strconv.Itoa(m.labelCount(l.Name))))
	}
	return rows
}

// labelModeWord is the footer's word for the active label semantics.
func (m *Model) labelModeWord() string {
	if m.labelAll {
		return "all"
	}
	return "any"
}

// labelModeRadio renders the label section's any-of/all-of switch (#2112)
// with the active semantics marked, so the filter never narrows silently.
func labelModeRadio(all bool) string {
	if all {
		return "○ any of  ● all of"
	}
	return "● any of  ○ all of"
}

// stateRadio renders the state row's three options with the active one
// marked, so cycling is never blind.
func stateRadio(s StateFilter) string {
	names := []string{"open", "closed", "all"}
	parts := make([]string, 0, len(names))
	for i, n := range names {
		mark := "○ "
		if StateFilter(i) == s {
			mark = "● "
		}
		parts = append(parts, mark+n)
	}
	return strings.Join(parts, "  ")
}

// renderRows draws the filtered issue list scrolled around the cursor.
func (m *Model) renderRows(pal *theme.Palette, height int) string {
	if len(m.rows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" "+m.emptyText()) + strings.Repeat("\n", height)
	}
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.top + k
		if i < len(m.rows) {
			if h := m.rows[i].header; h != "" {
				b.WriteString(m.renderGroupHeader(pal, h))
			} else {
				b.WriteString(m.renderRow(pal, i))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderGroupHeader draws one "group by label" divider row.
func (m *Model) renderGroupHeader(pal *theme.Palette, text string) string {
	return lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(m.clip(" ▸ " + text))
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
		return "(no " + m.state.String() + " issues)"
	case m.loaded:
		return "(no issues match the filter — esc clears it)"
	default:
		return "(press r to fetch the issue list)"
	}
}

// emptyPRText is emptyText for the PR view.
func (m *Model) emptyPRText() string {
	switch {
	case m.setup != "":
		return m.setup
	case m.loading:
		return "(fetching pull requests…)"
	case m.errMsg != "":
		return "(fetch failed: " + m.errMsg + " — r retries)"
	case m.loaded && len(m.prs) == 0:
		return "(no pull requests)"
	case m.loaded:
		return "(no pull requests match the filter — esc clears it)"
	default:
		return "(press r to fetch the listing)"
	}
}

// rowStyles resolves the base and number styles of one row by selection.
func (m *Model) rowStyles(pal *theme.Palette, selected bool) (lipgloss.Style, lipgloss.Style) {
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
	return base, num
}

// composeRow lays a row out as number + title + right-aligned metadata,
// shrinking the metadata in tiers before it truncates the title.
func (m *Model) composeRow(base, num lipgloss.Style, numText string, title string, meta func(tier int) string) string {
	metaText := ""
	for tier := 0; tier < 4; tier++ {
		metaText = meta(tier)
		if m.width-lipgloss.Width(metaText)-lipgloss.Width(numText) >= 16 || tier == 3 {
			break
		}
	}
	budget := m.width - lipgloss.Width(metaText) - lipgloss.Width(numText)
	if budget < 8 {
		metaText = ""
		budget = m.width - lipgloss.Width(numText)
	}
	if r := []rune(title); budget > 1 && len(r) > budget {
		title = string(r[:budget-1]) + "…"
	}
	line := num.Render(numText) + base.Render(title)
	if metaText != "" {
		if pad := m.width - lipgloss.Width(line) - lipgloss.Width(metaText); pad > 0 {
			line += base.Render(strings.Repeat(" ", pad))
		}
		line += metaText
	}
	return line
}

// renderRow draws one issue row: number accented, title, then the metadata
// columns — label chips in the forge's colors, assignee, author and age.
func (m *Model) renderRow(pal *theme.Palette, i int) string {
	is := &m.issues[m.rows[i].idx]
	base, num := m.rowStyles(pal, i == m.cursor)
	numText := " " + m.stateGlyph(pal, is) + "#" + strconv.Itoa(is.Number) + " "
	return m.composeRow(base, num, numText, is.Title, func(tier int) string {
		return m.rowMeta(pal, is, tier)
	})
}

// stateGlyph marks a closed issue while the state filter shows more than the
// open ones; with the default filter every row is open and the glyph is noise.
func (m *Model) stateGlyph(pal *theme.Palette, is *forge.Issue) string {
	if m.state == FilterOpen || is.State == "" {
		return ""
	}
	if is.State == "CLOSED" {
		return "✔ "
	}
	return "● "
}

// rowMeta renders the right-aligned metadata at one shrink tier: 0 is
// everything, 1 drops the label chips, 2 keeps only the age, 3 nothing.
func (m *Model) rowMeta(pal *theme.Palette, is *forge.Issue, tier int) string {
	if tier >= 3 {
		return ""
	}
	var parts []string
	if tier == 0 {
		for _, l := range is.Labels {
			parts = append(parts, chip(l))
		}
		if len(is.Assignees) > 0 {
			parts = append(parts, lipgloss.NewStyle().Faint(true).Render("@"+strings.Join(is.Assignees, ",")))
		}
	}
	if tier <= 1 && is.Author != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(pal.Info).Render(padLeft(truncate(is.Author, authorColW), authorColW)))
	}
	if age := ui.ShortAge(is.CreatedAt, m.clock()); age != "" {
		parts = append(parts, lipgloss.NewStyle().Faint(true).Render(padLeft(age, ageColW)))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

// renderPRRows draws the pull-request list full width.
func (m *Model) renderPRRows(pal *theme.Palette, height int) string {
	if len(m.prRows) == 0 {
		return lipgloss.NewStyle().Faint(true).Render(" "+m.emptyPRText()) + strings.Repeat("\n", height)
	}
	m.clampScroll()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.prTop + k
		if i < len(m.prRows) {
			b.WriteString(m.renderPRRow(pal, i))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderPRRow draws one pull-request row: number with its state color, title,
// then head branch, CI rollup, review decision and age.
func (m *Model) renderPRRow(pal *theme.Palette, i int) string {
	pr := &m.prs[m.prRows[i].idx]
	base, num := m.rowStyles(pal, i == m.prCursor)
	glyph, col := prGlyph(pal, pr)
	numText := " #" + strconv.Itoa(pr.Number) + " "
	line := m.composeRow(base, num.Foreground(col), numText, pr.Title, func(tier int) string {
		return m.prRowMeta(pal, pr, glyph, col, tier)
	})
	return line
}

// prRowMeta renders the PR row's metadata at one shrink tier: 0 is head
// branch, checks, review and age; 1 drops the branch; 2 keeps checks and age.
func (m *Model) prRowMeta(pal *theme.Palette, pr *forge.PR, glyph string, col color.Color, tier int) string {
	if tier >= 3 {
		return ""
	}
	var parts []string
	if tier == 0 && pr.HeadRef != "" {
		parts = append(parts, lipgloss.NewStyle().Faint(true).Render(truncate(pr.HeadRef, branchColW)))
	}
	if tier <= 1 {
		if text, rcol := reviewLabel(pal, pr); text != "" {
			parts = append(parts, lipgloss.NewStyle().Foreground(rcol).Render(text))
		}
	}
	state := strings.ToLower(pr.State)
	if state == "" {
		state = "open"
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(col).Render(state+glyph))
	if age := ui.ShortAge(pr.UpdatedAt, m.clock()); age != "" {
		parts = append(parts, lipgloss.NewStyle().Faint(true).Render(padLeft(age, ageColW)))
	}
	return strings.Join(parts, " ") + " "
}

// reviewLabel renders a PR's review decision compactly, "" when the backend
// reports none (Gitea has no equivalent field).
func reviewLabel(pal *theme.Palette, pr *forge.PR) (string, color.Color) {
	switch pr.Review {
	case "APPROVED":
		return "approved", pal.Success
	case "CHANGES_REQUESTED":
		return "changes", pal.Error
	case "REVIEW_REQUIRED":
		return "review", pal.Warning
	}
	return "", pal.Foreground
}

// truncate cuts s to n cells with an ellipsis; a short string passes through.
func truncate(s string, n int) string {
	r := []rune(s)
	if n < 1 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// padLeft right-aligns s in a field of n cells.
func padLeft(s string, n int) string {
	if pad := n - len([]rune(s)); pad > 0 {
		return strings.Repeat(" ", pad) + s
	}
	return s
}

// prGlyph maps a PR to its one-glyph state: merged/closed first, then the CI
// rollup of an open PR.
func prGlyph(pal *theme.Palette, pr *forge.PR) (string, color.Color) {
	switch strings.ToUpper(pr.State) {
	case "MERGED":
		return " ⇌", pal.Info
	case "CLOSED":
		return " ×", pal.Error
	}
	switch pr.Checks {
	case forge.ChecksFailing:
		return " ✗", pal.Error
	case forge.ChecksPending:
		return " …", pal.Warning
	case forge.ChecksPassing:
		return " ✓", pal.Success
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

// prDetailHeader is the PR detail view's context line, mirroring the issue
// detail's: the pull request on screen and its place in the filtered listing.
func (m *Model) prDetailHeader(pal *theme.Palette) string {
	pr := m.SelectedPR()
	if pr == nil {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).
		Render(" #" + strconv.Itoa(pr.Number) + " ")
	pos := 0
	for n, idx := range m.prVisible {
		if idx == m.prRows[m.prCursor].idx {
			pos = n + 1
			break
		}
	}
	note := lipgloss.NewStyle().Faint(true).
		Render(fmt.Sprintf("PR %d/%d — ctrl+j/ctrl+k walk ", pos, len(m.prVisible)))
	line := title + lipgloss.NewStyle().Foreground(pal.Foreground).Render(pr.Title)
	if pad := m.width - lipgloss.Width(line) - lipgloss.Width(note); pad > 0 {
		return line + strings.Repeat(" ", pad) + note
	}
	return m.clip(title + pr.Title)
}

// renderPRDetail draws the selected pull request's detail, scrolled.
func (m *Model) renderPRDetail(pal *theme.Palette, height int) string {
	pr := m.SelectedPR()
	if pr == nil {
		m.prDetail = false
		return m.renderPRRows(pal, height)
	}
	m.ensurePRDetail(pal)
	m.clampPRDetail()
	var b strings.Builder
	for k := 0; k < height; k++ {
		i := m.prdTop + k
		if i < len(m.prdLines) {
			b.WriteString(m.prdLines[i])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ensurePRDetail (re)renders the PR detail lines when the PR, the width or
// the fetched data changed: a markdown block (meta line, description, linked
// issue), then the per-check list and any load/error state as styled rows.
func (m *Model) ensurePRDetail(pal *theme.Palette) {
	if m.prdRenderFor == m.prdFor && m.prdW == m.width && m.prdRenderRev == m.prdRev && m.prdLines != nil {
		return
	}
	m.prdRenderFor, m.prdW, m.prdRenderRev = m.prdFor, m.width, m.prdRev
	faint := lipgloss.NewStyle().Faint(true)
	if m.prd == nil {
		switch {
		case m.prdLoading:
			m.prdLines = []string{"", faint.Render(" (fetching the pull request…)")}
		case m.prdErr != "":
			m.prdLines = []string{"", lipgloss.NewStyle().Foreground(pal.Error).
				Render(m.clip(" (fetch failed: " + m.prdErr + " — r retries)"))}
		default:
			m.prdLines = []string{""}
		}
		return
	}
	d := m.prd
	head := ""
	if meta := m.prDetailMeta(d); meta != "" {
		head += meta + "\n\n"
	}
	if n := forge.LinkedIssue(d.Body); n > 0 {
		link := "**Closes #" + strconv.Itoa(n)
		if idx := m.issueIndex(n); idx >= 0 {
			link += " — " + m.issues[idx].Title
		}
		head += link + "**\n\n"
	}
	body := d.Body
	if strings.TrimSpace(body) == "" {
		body = "*(no description)*"
	}
	out, err := m.renderMarkdown(head + body)
	if err != nil {
		out = head + body
	}
	m.prdLines = strings.Split(strings.TrimRight(out, "\n"), "\n")
	m.prdLines = append(m.prdLines, m.prCheckLines(pal, d)...)
	if m.prdErr != "" {
		m.prdLines = append(m.prdLines, "", lipgloss.NewStyle().Foreground(pal.Error).
			Render(m.clip(" (refresh failed: "+m.prdErr+" — r retries)")))
	}
}

// prDetailMeta is the author/branches/state line above a PR's description.
func (m *Model) prDetailMeta(d *forge.PRDetail) string {
	var parts []string
	if d.Author != "" {
		parts = append(parts, "@"+d.Author)
	}
	if d.HeadRef != "" {
		branches := d.HeadRef
		if d.BaseRef != "" {
			branches += " → " + d.BaseRef
		}
		parts = append(parts, branches)
	}
	if d.State != "" {
		parts = append(parts, strings.ToLower(d.State))
	}
	if text, _ := reviewLabel(m.theme(), &d.PR); text != "" {
		parts = append(parts, "review: "+text)
	}
	if d.Mergeable != "" {
		parts = append(parts, d.Mergeable)
	}
	if age := ui.RelTime(d.UpdatedAt, m.clock()); age != "" {
		parts = append(parts, "updated "+age)
	}
	if len(parts) == 0 {
		return ""
	}
	return "*" + strings.Join(parts, " · ") + "*"
}

// prCheckLines renders the per-check CI list under a PR's description behind
// a divider, one glyph-and-name row per check.
func (m *Model) prCheckLines(pal *theme.Palette, d *forge.PRDetail) []string {
	faint := lipgloss.NewStyle().Faint(true)
	lines := []string{"", lipgloss.NewStyle().Foreground(pal.Border).Render(" ── checks ──")}
	if len(d.CheckRuns) == 0 {
		return append(lines, faint.Render(" (no checks reported)"))
	}
	for _, c := range d.CheckRuns {
		glyph, col := checkGlyph(pal, c.State)
		lines = append(lines, " "+lipgloss.NewStyle().Foreground(col).Render(m.clip(glyph+" "+c.Name)))
	}
	return lines
}

// checkGlyph maps one check state to its glyph and color.
func checkGlyph(pal *theme.Palette, s forge.CheckState) (string, color.Color) {
	switch s {
	case forge.ChecksFailing:
		return "✗", pal.Error
	case forge.ChecksPending:
		return "…", pal.Warning
	case forge.ChecksPassing:
		return "✓", pal.Success
	default:
		return "·", pal.Foreground
	}
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

// footer lists the current view's actions with their keys, always ending in
// the action menu that shows the same table in full.
func (m *Model) footer(pal *theme.Palette) string {
	if m.ov == ovFilter {
		if m.ovSearch.Active() {
			return lipgloss.NewStyle().Faint(true).Render(m.clip(" typing narrows the labels · space toggles · backspace deletes · enter keeps · esc clears the search"))
		}
		if m.ovCursor == fovMatch {
			return lipgloss.NewStyle().Faint(true).Render(m.clip(" type to match · ↓ more filters · enter keeps · esc reverts"))
		}
		if m.tab != TabPRs && m.ovCursor >= m.fovFixedRows() {
			return lipgloss.NewStyle().Faint(true).Render(m.clip(" space toggles · type to narrow · backspace clears the row · enter keeps · esc reverts"))
		}
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" space toggles · labels match " + m.labelModeWord() + " selected · backspace clears the row · enter keeps · esc reverts"))
	}
	if m.ov == ovLabelEdit || m.ov == ovAssignEdit {
		if m.ovSearch.Active() {
			return lipgloss.NewStyle().Faint(true).Render(m.clip(" typing narrows · space toggle · backspace deletes · enter writes the change · esc clears the search"))
		}
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" type to narrow · space toggle · backspace clear · enter writes the change · esc cancel"))
	}
	if m.ov == ovComment {
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" type the comment · enter posts and " + m.stateVerb() + "s · esc cancel"))
	}
	if m.ov == ovActions {
		if m.ovSearch.Active() {
			return lipgloss.NewStyle().Faint(true).Render(m.clip(" typing narrows · backspace deletes · enter run · esc clears the search"))
		}
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" type to narrow · enter run · esc close"))
	}
	if m.ov == ovPRAct {
		if m.prActStage == 0 {
			return lipgloss.NewStyle().Faint(true).Render(m.clip(" optional comment · enter continues · esc cancels"))
		}
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter confirms the " + m.prActKind + " · backspace edits the comment · esc cancels"))
	}
	if m.ov == ovCleanup {
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter cleans up · esc keeps the branch"))
	}
	if m.ov == ovEdit {
		return lipgloss.NewStyle().Faint(true).Render(m.clip(" enter edit · esc cancel"))
	}
	var parts []string
	for _, a := range m.actions() {
		// An action the permissions forbid stays in the menu (with its
		// reason) but is not advertised in the footer (#2088); an action
		// without a direct key (grouping since #2114) is menu-only.
		if a.disabled || a.key == "" {
			continue
		}
		parts = append(parts, a.key+" "+a.hint)
	}
	parts = append(parts, "m menu")
	// On a narrow pane whole segments are dropped from the tail — but never
	// "m menu", the discoverability lifeline (#2104): the menu still lists
	// what the footer had to drop.
	line := " " + strings.Join(parts, " · ")
	for m.width > 0 && len([]rune(line)) > m.width && len(parts) > 1 {
		parts = append(parts[:len(parts)-2], parts[len(parts)-1])
		line = " " + strings.Join(parts, " · ")
	}
	return lipgloss.NewStyle().Faint(true).Render(m.clip(line))
}

// detailHeader is the detail view's context line: the issue on screen and its
// place in the filtered listing, so opening one never loses the list.
func (m *Model) detailHeader(pal *theme.Palette) string {
	is := m.Selected()
	if is == nil {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).
		Render(" #" + strconv.Itoa(is.Number) + " ")
	pos, total := m.Position()
	note := lipgloss.NewStyle().Faint(true).
		Render(fmt.Sprintf("issue %d/%d — ctrl+j/ctrl+k walk ", pos, total))
	line := title + lipgloss.NewStyle().Foreground(pal.Foreground).Render(is.Title)
	if pad := m.width - lipgloss.Width(line) - lipgloss.Width(note); pad > 0 {
		return line + strings.Repeat(" ", pad) + note
	}
	return m.clip(lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).
		Render(" #"+strconv.Itoa(is.Number)+" ") + is.Title)
}

// renderDetail draws the selected issue's rendered body and timeline,
// scrolled.
func (m *Model) renderDetail(pal *theme.Palette, height int) string {
	is := m.Selected()
	if is == nil {
		m.detail = false
		return m.renderRows(pal, height)
	}
	m.ensureDetail(pal, is)
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

// ensureDetail (re)renders the detail lines when the issue, the width or the
// timeline changed. The scroll only resets on an issue change — a timeline
// page landing under the reader must not yank the view back to the top.
func (m *Model) ensureDetail(pal *theme.Palette, is *forge.Issue) {
	if m.detailFor == is.Number && m.detailW == m.width && m.detailRev == m.tlRev && m.detailLines != nil {
		return
	}
	if m.detailFor != is.Number {
		// Opening a different issue starts at the top. Re-rendering the one
		// already shown — a width change, a fresh timeline page, or a
		// background poll (#2085) that brought a fresh body — must keep the
		// offset the user scrolled to, or every poll would yank a long issue
		// back to line one.
		m.detailTop = 0
	}
	m.detailFor, m.detailW, m.detailRev = is.Number, m.width, m.tlRev
	// No title heading: the position header above the body already names the
	// issue, in the pane's own accent rather than glamour's.
	head := ""
	if meta := m.detailMeta(is); meta != "" {
		head += meta + "\n\n"
	}
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
	m.detailLines = append(m.detailLines, m.timelineLines(pal, is)...)
	// A shorter body may leave the kept offset past the end.
	m.clampDetail()
}

// detailMeta is the author/age/state line above an issue's body, "" when the
// backend reported none of it.
func (m *Model) detailMeta(is *forge.Issue) string {
	var parts []string
	if is.Author != "" {
		parts = append(parts, "@"+is.Author)
	}
	if age := ui.RelTime(is.CreatedAt, m.clock()); age != "" {
		parts = append(parts, "opened "+age)
	}
	if age := ui.RelTime(is.UpdatedAt, m.clock()); age != "" {
		parts = append(parts, "updated "+age)
	}
	if is.State != "" {
		parts = append(parts, strings.ToLower(is.State))
	}
	if len(parts) == 0 {
		return ""
	}
	return "*" + strings.Join(parts, " · ") + "*"
}

// renderMarkdown renders through a fresh width- and theme-bound glamour
// renderer, the preview pane's pattern (#62).
func (m *Model) renderMarkdown(src string) (string, error) {
	return m.renderMarkdownWrap(src, m.width-2)
}

// renderMarkdownWrap is renderMarkdown at an explicit wrap width — the
// timeline's comment blocks reserve columns for their gutter bar (#2106).
func (m *Model) renderMarkdownWrap(src string, wrap int) (string, error) {
	wrap = max(10, wrap)
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(m.styleConfig()),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		return "", err
	}
	out, err := r.Render(src)
	if err != nil {
		return "", err
	}
	// Glamour cannot hang-indent wrapped list items itself (#2105).
	return ui.HangingIndent(out, wrap), nil
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

// overlayBox renders the open modal as a bordered, self-sized box the View
// composites centered over the body.
func (m *Model) overlayBox(pal *theme.Palette) string {
	title, lines := m.overlayContent(pal)
	if len(lines) == 0 {
		return ""
	}
	inner := lipgloss.Width(title)
	for _, l := range lines {
		if w := lipgloss.Width(l); w > inner {
			inner = w
		}
	}
	if maxW := m.width - 4; maxW > 0 && inner > maxW {
		inner = maxW
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(pal.Accent).Bold(true).Render(fitCells(title, inner)))
	for _, l := range lines {
		b.WriteString("\n" + fitCells(l, inner))
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(pal.Border).
		Background(pal.Background).
		Padding(0, 1).
		Render(b.String())
}

// overlayContent is the open modal's heading and its visible rows.
func (m *Model) overlayContent(pal *theme.Palette) (string, []string) {
	sel := lipgloss.NewStyle().Foreground(pal.Accent).Bold(true)
	plain := lipgloss.NewStyle().Foreground(pal.Foreground)
	m.clampOverlay()
	h := m.overlayHeight()
	var lines []string
	switch m.ov {
	case ovFilter:
		rows := m.filterOvRows(pal)
		for k := 0; k < h; k++ {
			i := m.ovTop + k
			if i >= len(rows) {
				break
			}
			lines = append(lines, rows[i])
		}
		return "Filter — " + m.viewName() + m.searchSuffix(), lines
	case ovLabelEdit, ovAssignEdit:
		rows := m.editViewRows()
		if len(rows) == 0 {
			return m.editorTitle(), []string{lipgloss.NewStyle().Faint(true).
				Render("(nothing matches " + m.ovSearch.Query() + ")")}
		}
		chips := map[string]forge.Label{}
		for _, l := range m.pickerLabels() {
			chips[l.Name] = l
		}
		for k := 0; k < h; k++ {
			i := m.ovTop + k
			if i >= len(rows) {
				break
			}
			mark := "[ ] "
			if m.editSel[rows[i]] {
				mark = "[x] "
			}
			style := plain
			if i == m.ovCursor {
				style = sel
			}
			text := rows[i]
			if m.ov == ovAssignEdit {
				text = "@" + text
			}
			line := style.Render(mark + text)
			// The label rows carry the forge's own chip, so the picker looks
			// like the list it edits.
			if m.ov == ovLabelEdit {
				if l, ok := chips[rows[i]]; ok {
					line = style.Render(mark) + chip(l)
				}
			}
			lines = append(lines, line)
		}
		return m.editorTitle() + m.searchSuffix(), lines
	case ovComment:
		verb := m.stateVerb()
		lines = append(lines,
			plain.Render("comment: ")+ui.CursorView(m.cmInput, m.cmCur),
			lipgloss.NewStyle().Faint(true).Render("enter posts it and "+verb+"s · esc cancels"))
		return capitalize(verb) + " #" + strconv.Itoa(m.editFor) + " with a comment", lines
	case ovActions:
		acts := m.viewActions()
		if len(acts) == 0 {
			return "Actions — " + m.viewName() + m.searchSuffix(), []string{
				lipgloss.NewStyle().Faint(true).Render("(nothing matches " + m.ovSearch.Query() + ")")}
		}
		width := 0
		for _, a := range acts {
			if w := len([]rune(a.key)); w > width {
				width = w
			}
		}
		off := lipgloss.NewStyle().Foreground(pal.Foreground).Faint(true)
		for k := 0; k < h; k++ {
			i := m.ovTop + k
			if i >= len(acts) {
				break
			}
			key := acts[i].key + strings.Repeat(" ", width-len([]rune(acts[i].key)))
			style := plain
			if acts[i].disabled {
				style = off
			}
			if i == m.ovCursor {
				style = sel
			}
			lines = append(lines, style.Render(key+"  "+acts[i].label))
		}
		return "Actions — " + m.viewName() + m.searchSuffix(), lines
	case ovPRAct:
		verb := capitalize(m.prActKind)
		n := "#" + strconv.Itoa(m.prActFor)
		if m.prActStage == 0 {
			lines = append(lines,
				plain.Render("comment: ")+ui.CursorView(m.cmInput, m.cmCur),
				lipgloss.NewStyle().Faint(true).Render("optional — posted before the "+m.prActKind+" · enter continues"))
			return verb + " " + n + " with a comment", lines
		}
		what := m.prActKind + " " + n
		if m.prActHead != "" {
			branches := m.prActHead
			// The base branch may have arrived with the detail fetch since
			// the dialog opened; prefer the live value.
			base := m.prBaseRef(m.prActFor)
			if base == "" {
				base = m.prActBase
			}
			if base != "" {
				branches += " → " + base
			}
			what += ": " + branches
		}
		if m.prActKind == forge.PRMerge {
			what += " (method: " + m.prMergeMethod(m.prActFor) + ")"
		}
		lines = append(lines, plain.Render(what))
		comment := "no comment"
		if strings.TrimSpace(m.cmInput) != "" {
			comment = "comment: " + truncate(strings.TrimSpace(m.cmInput), 40)
		}
		lines = append(lines,
			lipgloss.NewStyle().Faint(true).Render(comment),
			lipgloss.NewStyle().Foreground(pal.Warning).Render("this cannot be undone — enter confirms · esc cancels"))
		return "Confirm " + m.prActKind, lines
	case ovCleanup:
		lines = append(lines,
			plain.Render("delete "+m.cleanupBranch+" locally and on origin,"),
			plain.Render("switch to the default branch and pull"),
			lipgloss.NewStyle().Faint(true).Render("enter runs it · esc keeps the branch"))
		return "Merged — clean up the branch?", lines
	case ovEdit:
		entries := m.editEntries()
		for k := 0; k < h; k++ {
			i := m.ovTop + k
			if i >= len(entries) {
				break
			}
			style := plain
			if i == m.ovCursor {
				style = sel
			}
			lines = append(lines, style.Render(entries[i].label))
		}
		return "Edit what?", lines
	}
	return "", nil
}

// searchSuffix appends the running type-ahead to a modal's heading (#2111),
// "" while none runs — the query has to be visible somewhere, and the heading
// is the one line every picker already has.
func (m *Model) searchSuffix() string {
	if h := m.ovSearch.Hint(); h != "" {
		return "  " + h
	}
	return ""
}

// editorTitle names the open mutation picker and the issue it edits.
func (m *Model) editorTitle() string {
	what := "Labels"
	if m.ov == ovAssignEdit {
		what = "Assignees"
	}
	return what + " of #" + strconv.Itoa(m.editFor)
}

// viewName names the view the action menu lists the actions of.
func (m *Model) viewName() string {
	switch {
	case m.detail && m.tab == TabIssues:
		return "issue detail"
	case m.prDetail && m.tab == TabPRs:
		return "pull request detail"
	case m.tab == TabPRs:
		return "pull requests"
	default:
		return "issues"
	}
}

// fitCells pads or truncates one modal row to exactly n cells so the border
// stays rectangular whatever the row contains.
func fitCells(s string, n int) string {
	w := lipgloss.Width(s)
	if w > n {
		return truncate(s, n)
	}
	return s + strings.Repeat(" ", n-w)
}
