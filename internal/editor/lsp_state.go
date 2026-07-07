package editor

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

// lsp_state.go holds the editor-side LSP UI state — diagnostics, the completion
// popup, and the hover popup — plus the Update handling and rendering helpers
// (Roadmap 0100). Results arrive as ilsp.* messages routed by the app; the editor
// caches them and the app composites the popups at the cursor.

// completionState is the live autocomplete popup. anchor is where the trigger
// fired (just after the "."); the prefix the user types after it filters the
// list and is replaced on accept.
type completionState struct {
	items  []ilsp.CompletionItem
	sel    int
	anchor buffer.Position
}

// hoverState is the live hover popup content (already flattened to lines).
type hoverState struct {
	lines []string
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
	anchor := m.comp.anchor
	cursor := m.cursor
	m.comp = nil
	// Replace [anchor, cursor) (the typed prefix) with the item's insert text in
	// the open insert session so it joins the same undo unit.
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	end := m.insert.rec.Apply(buffer.Edit{Range: buffer.Range{Start: anchor, End: cursor}, Text: item.InsertText})
	m.cursor = end
	m.desiredCol = end.Col
	m.dirtyFromInsert()
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

	width := 0
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
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
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

// --- hover popup ---

// HoverOpen reports whether the hover popup is showing.
func (m Model) HoverOpen() bool { return m.hover != nil && len(m.hover.lines) > 0 }

// HoverView renders the hover content box.
func (m Model) HoverView() string {
	if m.hover == nil {
		return ""
	}
	box := lipgloss.NewStyle().Background(m.theme().Panel).Foreground(m.theme().Foreground).Padding(0, 1)
	const maxLines = 12
	lines := m.hover.lines
	if len(lines) > maxLines {
		lines = append(append([]string{}, lines[:maxLines]...), "…")
	}
	return box.Render(strings.Join(lines, "\n"))
}

// HoverAnchor returns the buffer-relative cell the hover popup anchors to.
func (m Model) HoverAnchor() (col, line int) { return m.cursor.Col, m.cursor.Line }

// dismissHover clears any hover popup (called on the next key).
func (m *Model) dismissHover() { m.hover = nil }

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
