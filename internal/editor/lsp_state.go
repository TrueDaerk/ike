package editor

import (
	"image/color"
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// lsp_state.go holds the editor-side LSP UI state — diagnostics, the completion
// popup, and the hover popup — plus the Update handling and rendering helpers
// (Roadmap 0100). Results arrive as ilsp.* messages routed by the app; the editor
// caches them and the app composites the popups at the cursor.

// completionState is the live autocomplete popup. anchor is where the trigger
// fired (just after the "."); the prefix the user types after it filters the
// list. On accept the partial identifier before the cursor is replaced (see
// identifierStart) — not the anchor..cursor span, which is empty for a manual
// trigger anchored at the cursor.
type completionState struct {
	items  []ilsp.CompletionItem
	sel    int
	anchor buffer.Position
}

// hoverState is the live hover popup content (already parsed to display rows).
type hoverState struct {
	lines []hoverLine
}

// hoverLine is one display row of the hover popup: either pre-styled text or a
// thematic break, which HoverView draws as a rule sized to the popup width.
type hoverLine struct {
	text string
	rule bool
}

// setDiagnostics replaces the diagnostic set and rebuilds the per-line index.
func (m *Model) setDiagnostics(diags []ilsp.Diagnostic) {
	m.diags = diags
	m.diagByLine = make(map[int][]ilsp.Diagnostic, len(diags))
	for _, d := range diags {
		for ln := d.Range.Start.Line; ln <= d.Range.End.Line; ln++ {
			m.diagByLine[ln] = append(m.diagByLine[ln], d)
		}
	}
}

// worstSeverityOnLine returns the most severe diagnostic severity on a line
// (lower number = more severe) and whether any exists, for gutter colouring.
func (m Model) worstSeverityOnLine(line int) (int, bool) {
	ds := m.diagByLine[line]
	if len(ds) == 0 {
		return 0, false
	}
	worst := 5
	for _, d := range ds {
		if d.Severity != 0 && d.Severity < worst {
			worst = d.Severity
		}
	}
	if worst == 5 {
		worst = 1 // unspecified severity: treat as error
	}
	return worst, true
}

// diagSeverityAt returns the diagnostic severity covering a specific cell (for
// inline underlining) and whether one exists.
func (m Model) diagSeverityAt(line, col int) (int, bool) {
	worst, found := 5, false
	for _, d := range m.diagByLine[line] {
		if !diagCovers(d, line, col) {
			continue
		}
		found = true
		sev := d.Severity
		if sev == 0 {
			sev = 1
		}
		if sev < worst {
			worst = sev
		}
	}
	if !found {
		return 0, false
	}
	return worst, true
}

// diagCovers reports whether diagnostic d covers (line, col), accounting for
// multi-line ranges. A zero-width range still marks its start cell.
func diagCovers(d ilsp.Diagnostic, line, col int) bool {
	s, e := d.Range.Start, d.Range.End
	if line < s.Line || line > e.Line {
		return false
	}
	startCol := 0
	if line == s.Line {
		startCol = s.Col
	}
	endCol := col + 1 // whole line for middle rows
	if line == e.Line {
		endCol = e.Col
	}
	if endCol <= startCol {
		endCol = startCol + 1 // zero-width: mark one cell
	}
	return col >= startCol && col < endCol
}

// diagnosticJump moves the cursor to the next (forward) or previous diagnostic
// in document order, wrapping around the file (#369). Document order — not
// severity order — keeps repeated presses a monotone walk through the file;
// the severity is surfaced in the toast instead. Returns the toast command
// with the diagnostic's message, or a "no diagnostics" notice.
func (m *Model) diagnosticJump(forward bool) tea.Cmd {
	if len(m.diags) == 0 {
		return notice("no diagnostics in this file")
	}
	sorted := make([]ilsp.Diagnostic, len(m.diags))
	copy(sorted, m.diags)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Range.Start.Before(sorted[j].Range.Start)
	})
	// Strictly past the cursor in the walk direction, so a press while
	// standing on a diagnostic's start moves on; falling off either end
	// wraps to the other.
	pick, found := sorted[0], false
	if forward {
		for _, d := range sorted {
			if m.cursor.Before(d.Range.Start) {
				pick, found = d, true
				break
			}
		}
	} else {
		pick = sorted[len(sorted)-1]
		for i := len(sorted) - 1; i >= 0; i-- {
			if sorted[i].Range.Start.Before(m.cursor) {
				pick, found = sorted[i], true
				break
			}
		}
	}
	wrapped := ""
	if !found {
		wrapped = " (wrapped)"
	}
	m.SetCursor(pick.Range.Start.Line, pick.Range.Start.Col)
	msg := pick.Message
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return notice(severityLabel(pick.Severity) + ": " + msg + wrapped)
}

// severityLabel names an LSP severity for the diagnostic-jump toast;
// unspecified severity is treated as an error, matching the gutter.
func severityLabel(sev int) string {
	switch sev {
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	}
	return "error"
}

// DiagnosticCounts returns the number of error- and warning-severity diagnostics,
// for the status line.
func (m Model) DiagnosticCounts() (errors, warnings int) {
	for _, d := range m.diags {
		switch d.Severity {
		case 1:
			errors++
		case 2:
			warnings++
		}
	}
	return errors, warnings
}

// --- completion popup ---

// openCompletion shows the popup if the request still matches the cursor: the
// trigger position must be the current line and at or before the cursor.
func (m *Model) openCompletion(msg ilsp.CompletionMsg) {
	if msg.Line != m.cursor.Line || msg.Col > m.cursor.Col {
		return // the cursor moved away before the result arrived
	}
	if len(msg.Items) == 0 {
		m.comp = nil
		return
	}
	m.comp = &completionState{items: msg.Items, anchor: buffer.Position{Line: msg.Line, Col: msg.Col}}
	if m.filteredCompletion() == nil {
		m.comp = nil
	}
}

// CompletionOpen reports whether the autocomplete popup is showing.
func (m Model) CompletionOpen() bool { return m.comp != nil && len(m.filteredCompletion()) > 0 }

// completionPrefix is the text typed since the trigger (anchor..cursor on the
// anchor line). A cursor off the anchor line or before the anchor yields "" with
// ok=false, which closes the popup.
func (m Model) completionPrefix() (string, bool) {
	if m.comp == nil || m.cursor.Line != m.comp.anchor.Line || m.cursor.Col < m.comp.anchor.Col {
		return "", false
	}
	runes := []rune(m.buf.Line(m.cursor.Line))
	if m.comp.anchor.Col > len(runes) || m.cursor.Col > len(runes) {
		return "", false
	}
	return string(runes[m.comp.anchor.Col:m.cursor.Col]), true
}

// filteredCompletion returns the items matching the current prefix.
func (m Model) filteredCompletion() []ilsp.CompletionItem {
	if m.comp == nil {
		return nil
	}
	prefix, ok := m.completionPrefix()
	if !ok {
		return nil
	}
	if prefix == "" {
		return m.comp.items
	}
	lower := strings.ToLower(prefix)
	var out []ilsp.CompletionItem
	for _, it := range m.comp.items {
		if strings.HasPrefix(strings.ToLower(it.Label), lower) {
			out = append(out, it)
		}
	}
	return out
}

// completionMove changes the selection by delta, wrapping around.
func (m *Model) completionMove(delta int) {
	n := len(m.filteredCompletion())
	if n == 0 {
		return
	}
	m.comp.sel = ((m.comp.sel+delta)%n + n) % n
}

// completionAccept inserts the selected item, replacing the typed prefix, and
// closes the popup.
func (m *Model) completionAccept() {
	items := m.filteredCompletion()
	if len(items) == 0 {
		m.comp = nil
		return
	}
	if m.comp.sel >= len(items) {
		m.comp.sel = 0
	}
	item := items[m.comp.sel]
	m.comp = nil
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	// Replace the partial identifier already typed before the cursor with the
	// item's insert text — at every caret when carets are active (#145). The
	// replacement starts at the identifier boundary (not the request anchor):
	// a manual ctrl+space trigger anchors at the cursor, so an anchor-only
	// range would be empty and splice the full insert text after the typed
	// prefix, duplicating it (e.g. "xyz.__" + "__dict__" → "xyz.____dict__", #330).
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		start := m.identifierStart(pos)
		start = m.extendPrefixMatch(start, pos, item.InsertText)
		return m.insert.rec.Apply(buffer.Edit{Range: buffer.Range{Start: start, End: pos}, Text: item.InsertText})
	})
	m.dirtyFromInsert()
}

// identifierStart returns the position of the start of the identifier run
// ending at pos: it walks back over identifier characters (letters, digits, and
// underscore) on the same line. This is the span a completion replaces — the
// partial word already typed — independent of where the request was anchored.
func (m Model) identifierStart(pos buffer.Position) buffer.Position {
	runes := []rune(m.buf.Line(pos.Line))
	col := pos.Col
	if col > len(runes) {
		col = len(runes)
	}
	for col > 0 && isIdentRune(runes[col-1]) {
		col--
	}
	return buffer.Position{Line: pos.Line, Col: col}
}

// extendPrefixMatch widens the replacement span leftwards beyond the
// identifier boundary while the widened typed text is still a prefix of the
// insert text. This covers sigil-carrying completions — PHP's "$he" completed
// to "$hello" must replace the "$" too, or the insert doubles it ("$$hello",
// #427) — without hard-coding per-language identifier characters.
func (m Model) extendPrefixMatch(start, cursor buffer.Position, insertText string) buffer.Position {
	runes := []rune(m.buf.Line(start.Line))
	end := cursor.Col
	if end > len(runes) {
		end = len(runes)
	}
	col := start.Col
	for col > 0 && strings.HasPrefix(insertText, string(runes[col-1:end])) {
		col--
	}
	return buffer.Position{Line: start.Line, Col: col}
}

// isIdentRune reports whether r can appear in a completion identifier.
func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// completionCancel hides the popup without inserting.
func (m *Model) completionCancel() { m.comp = nil }

// CompletionView renders the popup box (selected row highlighted). The app
// composites it at the anchor cell.
func (m Model) CompletionView() string {
	items := m.filteredCompletion()
	if len(items) == 0 {
		return ""
	}
	const maxRows = 8
	sel := m.comp.sel
	if sel >= len(items) {
		sel = 0
	}
	// Window the list around the selection.
	start := 0
	if sel >= maxRows {
		start = sel - maxRows + 1
	}
	endIdx := start + maxRows
	if endIdx > len(items) {
		endIdx = len(items)
	}

	width := lipgloss.Width(completionHint) // the hint row must stay readable (#308)
	for _, it := range items[start:endIdx] {
		if l := lipgloss.Width(completionLabel(it)); l > width {
			width = l
		}
	}
	if width > 40 {
		width = 40
	}

	normal := lipgloss.NewStyle().Background(m.theme().Panel).Foreground(m.theme().Foreground)
	selected := lipgloss.NewStyle().Background(m.theme().Primary).Foreground(m.theme().SelectionText)
	var rows []string
	for i := start; i < endIdx; i++ {
		label := completionLabel(items[i])
		st := normal
		if i == sel {
			st = selected
		}
		rows = append(rows, st.Width(width).Render(truncate(label, width)))
	}
	// The accept affordance stays visible (#308): the signature popup looks
	// similar but is informational, so the actionable list says its keys.
	hint := lipgloss.NewStyle().Background(m.theme().Panel).Foreground(m.theme().Border)
	rows = append(rows, hint.Width(width).Render(truncate(completionHint, width)))
	// The same rounded frame as signature/hover (#316); the rows carry their
	// own backgrounds, so the frame adds no padding.
	return m.popupFrame().Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

// CompletionAnchor returns the buffer-relative cell (col, line) the popup anchors
// to (the trigger point).
func (m Model) CompletionAnchor() (col, line int) {
	if m.comp == nil {
		return 0, 0
	}
	return m.comp.anchor.Col, m.comp.anchor.Line
}

// completionLabel renders one item's display text (label + optional detail).
func completionLabel(it ilsp.CompletionItem) string {
	if it.Detail != "" {
		return it.Label + " " + it.Detail
	}
	return it.Label
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if w <= 1 || len(r) <= 1 {
		return s
	}
	return string(r[:w-1]) + "…"
}

// completionHint is the completion popup's keys row (#308).
const completionHint = "↹/⏎ accept · esc close"

// --- signature popup ---

// signatureState is the showing call-signature popup: the active signature's
// label with the active parameter's rune highlight range, an optional first
// doc line, and how many other overloads exist.
type signatureState struct {
	label      string
	start, end int
	doc        string
	more       int
}

// applySignature installs or clears the popup from a SignatureHelpMsg: an
// empty label means the cursor left the call context.
func (m *Model) applySignature(msg ilsp.SignatureHelpMsg) {
	if msg.Label == "" {
		m.signature = nil
		return
	}
	// A reply that lands after insert mode ended is stale (#315): the popup
	// only lives while the call is being typed.
	if m.mode != Insert && m.mode != Replace {
		return
	}
	m.signature = &signatureState{label: msg.Label, start: msg.ParamStart, end: msg.ParamEnd, doc: msg.Doc, more: msg.More}
}

// SignatureOpen reports whether the signature popup is showing.
func (m Model) SignatureOpen() bool { return m.signature != nil }

// SignatureView renders the popup: the label with the active parameter
// emphasised, the doc line dimmed below, an overload counter when applicable.
// Long signatures wrap at the popup width cap instead of widening (#306).
func (m Model) SignatureView() string {
	s := m.signature
	if s == nil {
		return ""
	}
	box := m.popupFrame().Padding(0, 1)
	param := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true).Underline(true)
	dim := lipgloss.NewStyle().Foreground(m.theme().Border)

	runes := []rune(s.label)
	start, end := s.start, s.end
	if start < 0 || end > len(runes) || end < start {
		start, end = 0, 0
	}
	// The leading glyph marks this as the informational signature popup —
	// distinct from the actionable completion list (#308).
	line := dim.Render("ƒ ") + string(runes[:start]) + param.Render(string(runes[start:end])) + string(runes[end:])
	if s.more > 0 {
		line += dim.Render("  (+" + strconv.Itoa(s.more) + " overloads)")
	}
	rows := []string{line}
	if s.doc != "" {
		rows = append(rows, dim.Render(truncateTo(s.doc, 80)))
	}
	return m.clampPopup(box, strings.Join(rows, "\n"))
}

// popupFrame is the shared overlay frame for the LSP popups (#316): a rounded
// themed border like the floating shell, so the box reads as an overlay and
// not as buffer content, in light and dark themes alike.
func (m Model) popupFrame() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme().BorderFocus).
		BorderBackground(m.theme().Panel).
		Background(m.theme().Panel).
		Foreground(m.theme().Foreground)
}

// SetPopupMaxWidth sets the popup content-width cap. The app derives it from
// the terminal width now that popups may overflow their pane (#316); unset it
// falls back to the pane's text area (#306).
func (m *Model) SetPopupMaxWidth(w int) { m.popupMaxW = w }

// popupMaxWidth caps a popup's content width: wide enough for real
// signatures, never wider than the app-provided canvas (#316) — or, when the
// app never told us, the editor's own text area (#306).
func (m Model) popupMaxWidth() int {
	w := m.popupMaxW
	if w <= 0 {
		w = m.view.TextWidth(m.buf.LineCount()) - 2
	}
	if w > 80 {
		w = 80
	}
	if w < 20 {
		w = 20
	}
	return w
}

// popupMaxRows caps a popup's height so a wrapped monster signature cannot
// cover the whole screen (#306, #316).
const popupMaxRows = 10

// clampPopup wraps content at the popup width cap, truncates past
// popupMaxRows with an ellipsis row, and renders it inside box (which carries
// the popup frame, so the clamp happens on the content before the border is
// drawn).
func (m Model) clampPopup(box lipgloss.Style, content string) string {
	maxW := m.popupMaxWidth()
	if lipgloss.Width(content) > maxW {
		content = lipgloss.NewStyle().Width(maxW).Render(content)
	}
	lines := strings.Split(content, "\n")
	if len(lines) > popupMaxRows {
		lines = append(lines[:popupMaxRows], "…")
	}
	return box.Render(strings.Join(lines, "\n"))
}

// SignatureAnchor returns the buffer-relative cell the popup anchors to.
func (m Model) SignatureAnchor() (col, line int) { return m.cursor.Col, m.cursor.Line }

// dismissSignature clears the popup (esc / leaving insert); the server-driven
// clear path is an empty SignatureHelpMsg.
func (m *Model) dismissSignature() { m.signature = nil }

// truncateTo caps s at max runes with an ellipsis.
func truncateTo(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// --- hover popup ---

// newHover parses hover markdown into display rows (#379): fenced code blocks
// lose their ``` markers and are syntax-highlighted via the language registry
// (or fall back to an accent tint, so the signature still reads as code), and a
// thematic break ("---") becomes a rule row. Returns nil for empty content.
func (m Model) newHover(contents string) *hoverState {
	src := strings.Split(contents, "\n")
	var out []hoverLine
	for i := 0; i < len(src); i++ {
		trimmed := strings.TrimSpace(src[i])
		if strings.HasPrefix(trimmed, "```") {
			tag := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			var code []string
			for i++; i < len(src) && !strings.HasPrefix(strings.TrimSpace(src[i]), "```"); i++ {
				code = append(code, src[i])
			}
			out = append(out, m.styledHoverCode(tag, code)...)
			continue
		}
		if isThematicBreak(trimmed) {
			out = append(out, hoverLine{rule: true})
			continue
		}
		out = append(out, hoverLine{text: src[i]})
	}
	if len(out) == 0 {
		return nil
	}
	return &hoverState{lines: out}
}

// isThematicBreak reports whether a trimmed markdown line is a horizontal rule:
// three or more of the same marker (-, _, *) and nothing else.
func isThematicBreak(s string) bool {
	if len(s) < 3 {
		return false
	}
	marker := rune(s[0])
	if marker != '-' && marker != '_' && marker != '*' {
		return false
	}
	for _, r := range s {
		if r != marker {
			return false
		}
	}
	return true
}

// styledHoverCode renders a fenced code block's lines: syntax-highlighted when
// the fence tag resolves to a grammar, tinted with the accent colour otherwise,
// so the signature block stays visually distinct from the doc prose (#379).
func (m Model) styledHoverCode(tag string, code []string) []hoverLine {
	ix := highlight.NewIndex(highlight.HighlightFenced(tag, code))
	fallback := lipgloss.NewStyle().Foreground(m.theme().Accent)
	out := make([]hoverLine, 0, len(code))
	for ln, line := range code {
		if ix.Empty() {
			out = append(out, hoverLine{text: fallback.Render(line)})
			continue
		}
		out = append(out, hoverLine{text: m.styledCodeLine(ix, ln, line)})
	}
	return out
}

// styledCodeLine applies capture styles from ix to one line, grouping adjacent
// runes with the same capture into a single styled segment.
func (m Model) styledCodeLine(ix highlight.Index, ln int, line string) string {
	runes := []rune(line)
	var b strings.Builder
	for col := 0; col < len(runes); {
		capture := ix.CaptureAt(ln, col)
		end := col + 1
		for end < len(runes) && ix.CaptureAt(ln, end) == capture {
			end++
		}
		seg := string(runes[col:end])
		if st, ok := m.hlTheme.Style(capture); ok {
			seg = st.Render(seg)
		}
		b.WriteString(seg)
		col = end
	}
	return b.String()
}

// HoverOpen reports whether the hover popup is showing.
func (m Model) HoverOpen() bool { return m.hover != nil && len(m.hover.lines) > 0 }

// HoverView renders the hover content box, wrapped and capped like the
// signature popup (#306). Rule rows are drawn as a horizontal line sized to the
// widest content row (#379).
func (m Model) HoverView() string {
	if m.hover == nil {
		return ""
	}
	box := m.popupFrame().Padding(0, 1)
	const maxLines = 12
	lines := m.hover.lines
	if len(lines) > maxLines {
		lines = append(append([]hoverLine{}, lines[:maxLines]...), hoverLine{text: "…"})
	}
	width := 1
	for _, l := range lines {
		if w := lipgloss.Width(l.text); !l.rule && w > width {
			width = w
		}
	}
	if maxW := m.popupMaxWidth(); width > maxW {
		width = maxW
	}
	dim := lipgloss.NewStyle().Foreground(m.theme().Border)
	rows := make([]string, len(lines))
	for i, l := range lines {
		if l.rule {
			rows[i] = dim.Render(strings.Repeat("─", width))
		} else {
			rows[i] = l.text
		}
	}
	return m.clampPopup(box, strings.Join(rows, "\n"))
}

// HoverAnchor returns the buffer-relative cell the hover popup anchors to.
func (m Model) HoverAnchor() (col, line int) { return m.cursor.Col, m.cursor.Line }

// dismissHover clears any hover popup (called on the next key).
func (m *Model) dismissHover() { m.hover = nil }

// --- document highlight (#172) ---

// applyDocumentHighlights installs the occurrence marks for the symbol under
// the cursor. A reply anchored at a position the cursor has left is stale —
// it clears the marks instead of installing them (the move that outdated it
// already scheduled a fresh request).
func (m *Model) applyDocumentHighlights(msg ilsp.DocumentHighlightsMsg) {
	if msg.Line != m.cursor.Line || msg.Col != m.cursor.Col {
		m.occurrences = nil
		return
	}
	m.occurrences = msg.Highlights
}

// occurrenceAt returns the document-highlight kind covering a cell (for the
// subtle occurrence background) and whether one exists. Ranges are
// end-exclusive; a zero-width range marks nothing.
func (m Model) occurrenceAt(line, col int) (int, bool) {
	for _, h := range m.occurrences {
		s, e := h.Range.Start, h.Range.End
		if line < s.Line || line > e.Line {
			continue
		}
		startCol := 0
		if line == s.Line {
			startCol = s.Col
		}
		endCol := col + 1 // whole line for middle rows of a multi-line range
		if line == e.Line {
			endCol = e.Col
		}
		if col >= startCol && col < endCol {
			return h.Kind, true
		}
	}
	return 0, false
}

// occurrenceColor maps a document-highlight kind to its theme slot: write
// accesses get the warm slot, reads and plain text occurrences the cool one.
func (m Model) occurrenceColor(kind int) color.Color {
	if kind == protocol.HighlightWrite {
		return m.theme().OccurrenceWrite
	}
	return m.theme().OccurrenceRead
}

// --- inlay hints (#171) ---

// setInlayHints replaces the inlay-hint set and rebuilds the per-line index
// renderLine reads. Hints arrive sorted by position from the manager, so the
// per-line slices stay in column order.
func (m *Model) setInlayHints(hints []ilsp.InlayHint) {
	m.inlayHints = hints
	if len(hints) == 0 {
		m.hintsByLine = nil
		return
	}
	m.hintsByLine = make(map[int][]ilsp.InlayHint)
	for _, h := range hints {
		m.hintsByLine[h.Line] = append(m.hintsByLine[h.Line], h)
	}
}

// lineInlayHints returns the hints to render on a line, in column order; nil
// while the lsp.inlay_hints toggle is off (cached hints survive a toggle
// round-trip, they just stop rendering).
func (m Model) lineInlayHints(line int) []ilsp.InlayHint {
	if !m.showInlayHints {
		return nil
	}
	return m.hintsByLine[line]
}

// hintText is the display text of one inlay hint: the label with the
// server-requested padding spaces around it.
func hintText(h ilsp.InlayHint) string {
	text := h.Label
	if h.PadLeft {
		text = " " + text
	}
	if h.PadRight {
		text += " "
	}
	return text
}

// diagColor maps a diagnostic severity to the theme's diagnostic slots:
// error, warning, info, hint.
func (m Model) diagColor(severity int) color.Color {
	switch severity {
	case 1:
		return m.theme().Error
	case 2:
		return m.theme().Warning
	case 3:
		return m.theme().Info
	default:
		return m.theme().Hint
	}
}
