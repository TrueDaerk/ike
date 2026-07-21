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
	"ike/internal/fuzzy"
	"ike/internal/highlight"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
	"ike/internal/lsp/snippet"
	"ike/internal/vcs"
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
	// resolved caches completionItem/resolve results by item ID (#847): lazy
	// documentation for the doc rows and late additionalTextEdits for accept.
	resolved map[int]resolvedCompletion
	// incomplete marks a partial server reply (#849): typing re-queries the
	// server instead of only narrowing the client-side filter.
	incomplete bool
}

// resolvedCompletion is one cached completionItem/resolve result (#847).
type resolvedCompletion struct {
	doc   string
	edits []ilsp.FormatEdit
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
	// Anchor at the start of the identifier under the request position, not
	// the position itself: an identifier-rune auto-trigger (#527) fires after
	// the first typed letter, and the partial word before the cursor must
	// count into the prefix filter or further typing filters against the
	// wrong span. Sigil-carrying items ("$he" → "$hello", #427) widen the
	// anchor past the sigil the same way the accept path does, so the sigil
	// counts into the prefix instead of failing the filter. For "."-style
	// triggers nothing precedes the anchor, so this is the old behavior.
	pos := buffer.Position{Line: msg.Line, Col: msg.Col}
	anchor := m.extendAnchorMatch(m.identifierStart(pos), pos, msg.Items)
	// Base order is the server's ranking: sortText, label when absent (#845).
	// The fuzzy filter sorts stably by match score, so this order breaks ties.
	items := make([]ilsp.CompletionItem, len(msg.Items))
	copy(items, msg.Items)
	sort.SliceStable(items, func(i, j int) bool {
		return completionSortKey(items[i]) < completionSortKey(items[j])
	})
	m.comp = &completionState{items: items, anchor: anchor, resolved: map[int]resolvedCompletion{}, incomplete: msg.IsIncomplete}
	if m.filteredCompletion() == nil {
		m.comp = nil
		return
	}
	m.requestCompletionResolve()
}

// requestCompletionResolve asks the bridge to resolve the selected item when
// it still lacks documentation (#847).
func (m *Model) requestCompletionResolve() {
	items := m.filteredCompletion()
	if m.comp == nil || len(items) == 0 {
		return
	}
	sel := m.comp.sel
	if sel >= len(items) {
		sel = 0
	}
	it := items[sel]
	if it.Doc != "" {
		return
	}
	if _, ok := m.comp.resolved[it.ID]; ok {
		return
	}
	m.emitCompletionSelect(it.ID)
}

// applyCompletionResolve caches a resolve reply for the open popup (#847); a
// reply for a stale popup (different path handled by the caller, popup already
// closed here) is dropped.
func (m *Model) applyCompletionResolve(msg ilsp.CompletionResolveMsg) {
	if m.comp == nil {
		return
	}
	m.comp.resolved[msg.ID] = resolvedCompletion{doc: msg.Doc, edits: msg.AdditionalEdits}
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

// filteredCompletion returns the items matching the current prefix, best match
// first. Matching is fuzzy-subsequence with CamelCase/snake_case boundary
// bonuses (internal/fuzzy, #845) against the server's filterText (label when
// absent), so "gCN" finds "getClassName" and a scattered match still passes.
// The sort is stable over the sortText base order, so equal scores keep the
// server's ranking.
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
	type scored struct {
		item  ilsp.CompletionItem
		score int
	}
	var matched []scored
	for _, it := range m.comp.items {
		r, ok := fuzzy.Match(prefix, completionFilterText(it))
		if !ok {
			continue
		}
		matched = append(matched, scored{item: it, score: r.Score})
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].score > matched[j].score })
	out := make([]ilsp.CompletionItem, len(matched))
	for i, s := range matched {
		out[i] = s.item
	}
	return out
}

// completionFilterText is the text an item is matched against: the server's
// filterText when present, else the label (LSP spec default).
func completionFilterText(it ilsp.CompletionItem) string {
	if it.FilterText != "" {
		return it.FilterText
	}
	return it.Label
}

// completionSortKey is the server-ranking key: sortText when present, else the
// label (LSP spec default).
func completionSortKey(it ilsp.CompletionItem) string {
	if it.SortText != "" {
		return it.SortText
	}
	return it.Label
}

// completionMove changes the selection by delta, wrapping around.
func (m *Model) completionMove(delta int) {
	n := len(m.filteredCompletion())
	if n == 0 {
		return
	}
	m.comp.sel = ((m.comp.sel+delta)%n + n) % n
	m.requestCompletionResolve()
}

// completionAccept inserts the selected item, replacing the typed prefix, and
// closes the popup. A snippet item (#846) is expanded first and, with tabstops
// present, starts the placeholder session (single caret only — multi-caret
// inserts the expanded text plain).
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
	// A resolve may have delivered late additionalTextEdits (#847) — merge
	// them in unless the item already carried its own.
	if r, ok := m.comp.resolved[item.ID]; ok && len(item.AdditionalEdits) == 0 {
		item.AdditionalEdits = r.edits
	}
	m.comp = nil
	insertText := item.InsertText
	var stops []int
	if item.IsSnippet {
		if text, offs, err := snippet.Expand(insertText); err == nil {
			insertText, stops = text, offs
		}
		// A malformed snippet falls back to the raw text.
	}
	if m.insert.rec == nil {
		m.insert.rec = m.newRecorder()
	}
	// Auto-import first (#848): additionalTextEdits land away from the cursor
	// (typically the import block), so applying them before the main insert
	// keeps the item's coordinates valid; the cursor and carets shift by the
	// line delta of edits above them. Same recorder, so esc undoes the accept
	// and its import as one step.
	m.applyCompletionExtraEdits(item.AdditionalEdits)
	// Replace the partial identifier already typed before the cursor with the
	// item's insert text — at every caret when carets are active (#145). The
	// replacement starts at the identifier boundary (not the request anchor):
	// a manual ctrl+space trigger anchors at the cursor, so an anchor-only
	// range would be empty and splice the full insert text after the typed
	// prefix, duplicating it (e.g. "xyz.__" + "__dict__" → "xyz.____dict__", #330).
	m.fanApply(func(pos, _ buffer.Position) buffer.Position {
		start := m.identifierStart(pos)
		start = m.extendPrefixMatch(start, pos, insertText)
		return m.insert.rec.Apply(buffer.Edit{Range: buffer.Range{Start: start, End: pos}, Text: insertText})
	})
	m.dirtyFromInsert()
	if len(stops) > 0 && !m.hasCarets() {
		m.startSnippetSession(insertText, stops)
	}
}

// applyCompletionExtraEdits applies an accepted item's additionalTextEdits
// (auto-import, #848) through the open insert recorder, bottom-up so earlier
// positions stay valid. The cursor and secondary carets shift by the line
// delta of every edit that ends strictly above them (the import-block shape);
// same-line edits before the cursor are left unadjusted.
func (m *Model) applyCompletionExtraEdits(edits []ilsp.FormatEdit) {
	if len(edits) == 0 {
		return
	}
	sorted := make([]ilsp.FormatEdit, len(edits))
	copy(sorted, edits)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].StartLine != sorted[j].StartLine {
			return sorted[i].StartLine > sorted[j].StartLine
		}
		return sorted[i].StartCol > sorted[j].StartCol
	})
	for _, e := range sorted {
		m.insert.rec.Apply(buffer.Edit{
			Range: buffer.Range{
				Start: buffer.Position{Line: e.StartLine, Col: e.StartCol},
				End:   buffer.Position{Line: e.EndLine, Col: e.EndCol},
			},
			Text: e.Text,
		})
		delta := strings.Count(e.Text, "\n") - (e.EndLine - e.StartLine)
		if delta == 0 {
			continue
		}
		if e.EndLine < m.cursor.Line {
			m.cursor.Line += delta
		}
		for i := range m.carets {
			if e.EndLine < m.carets[i].pos.Line {
				m.carets[i].pos.Line += delta
			}
		}
	}
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

// extendAnchorMatch widens the popup anchor leftwards beyond the identifier
// boundary while the widened prefix still case-insensitively prefixes some
// item's label or insert text — the filter-side twin of extendPrefixMatch, so
// a sigil-carrying prefix ("$he" against "$hello") keeps matching.
func (m Model) extendAnchorMatch(start, pos buffer.Position, items []ilsp.CompletionItem) buffer.Position {
	runes := []rune(m.buf.Line(start.Line))
	end := pos.Col
	if end > len(runes) {
		end = len(runes)
	}
	col := start.Col
	for col > 0 && col <= end {
		widened := strings.ToLower(string(runes[col-1 : end]))
		ok := false
		for _, it := range items {
			if strings.HasPrefix(strings.ToLower(it.Label), widened) ||
				strings.HasPrefix(strings.ToLower(it.InsertText), widened) {
				ok = true
				break
			}
		}
		if !ok {
			break
		}
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
	// The selected item's documentation — inline or resolved (#847) — shows
	// dimmed below the hint, capped at a few lines.
	if doc := m.selectedCompletionDoc(items[sel]); doc != "" {
		const maxDocRows = 4
		docLines := completionDocLines(doc, maxDocRows)
		for _, l := range docLines {
			rows = append(rows, hint.Width(width).Render(truncate(l, width)))
		}
	}
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

// selectedCompletionDoc is the doc text for the selected item: its inline
// documentation, else the cached resolve result (#847).
func (m Model) selectedCompletionDoc(it ilsp.CompletionItem) string {
	if it.Doc != "" {
		return it.Doc
	}
	if r, ok := m.comp.resolved[it.ID]; ok {
		return r.doc
	}
	return ""
}

// completionDocLines flattens doc markdown-ish text to at most max display
// lines: fence markers drop, blank runs collapse, overflow gains an ellipsis.
func completionDocLines(doc string, max int) []string {
	var out []string
	blank := true
	for _, l := range strings.Split(doc, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			continue
		}
		if t == "" {
			if !blank {
				blank = true
			}
			continue
		}
		blank = false
		if len(out) == max {
			out[max-1] = out[max-1] + " …"
			return out
		}
		out = append(out, t)
	}
	return out
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
	label       string
	start, end  int
	doc         string
	more        int
	params      []ilsp.SignatureParam
	activeParam int
}

// applySignature installs or clears the popup from a SignatureHelpMsg: an
// empty label means the cursor left the call context.
func (m *Model) applySignature(msg ilsp.SignatureHelpMsg) {
	if msg.Label == "" {
		m.signature = nil
		return
	}
	// A reply that lands after insert mode ended is stale (#315) — unless it
	// answers the explicit parameter-info command (#523), which opens the
	// popup in any mode, or updates a popup that is already showing (the
	// cursor-follow retrigger works in normal mode too).
	if m.mode != Insert && m.mode != Replace && !msg.Manual && m.signature == nil {
		return
	}
	m.signature = &signatureState{
		label:       msg.Label,
		start:       msg.ParamStart,
		end:         msg.ParamEnd,
		doc:         msg.Doc,
		more:        msg.More,
		params:      msg.Params,
		activeParam: msg.ActiveParam,
	}
}

// SignatureOpen reports whether the signature popup is showing.
func (m Model) SignatureOpen() bool { return m.signature != nil }

// SignatureView renders the popup: the label with the active parameter
// emphasised, a parameter list with the active one marked (#523), the doc
// line dimmed below, an overload counter when applicable. Long signatures
// wrap at the popup width cap instead of widening (#306).
func (m Model) SignatureView() string {
	s := m.signature
	if s == nil {
		return ""
	}
	box := m.popupFrame().Padding(0, 1)
	param := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true).Underline(true)
	active := lipgloss.NewStyle().Foreground(m.theme().Accent).Bold(true)
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
	sep := dim.Render(strings.Repeat("─", min(lipgloss.Width(line), m.popupMaxWidth())))
	if len(s.params) > 0 {
		rows = append(rows, sep)
		for i, p := range s.params {
			if i == s.activeParam {
				rows = append(rows, active.Render("▶ "+p.Label))
			} else {
				rows = append(rows, "  "+p.Label)
			}
		}
	}
	// The active parameter's doc wins the detail row; the signature doc
	// follows when it adds anything.
	docs := []string{}
	if s.activeParam >= 0 && s.activeParam < len(s.params) {
		if d := s.params[s.activeParam].Doc; d != "" {
			docs = append(docs, d)
		}
	}
	if s.doc != "" && (len(docs) == 0 || docs[0] != s.doc) {
		docs = append(docs, s.doc)
	}
	if len(docs) > 0 {
		rows = append(rows, sep)
		for _, d := range docs {
			rows = append(rows, dim.Render(truncateTo(d, 80)))
		}
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

// --- diagnostic details popup (#739) ---

// ShowDiagnostics opens the hover popup with every diagnostic covering the
// caret line (#739): per entry a severity header — colored like the gutter
// mark — with the server source and rule code ("pyright · reportXyz"), then
// the message. Multiple entries separate with a rule. It reports whether any
// diagnostic exists on the line; without one no popup opens.
func (m *Model) ShowDiagnostics() bool {
	ds := m.diagByLine[m.cursor.Line]
	if len(ds) == 0 {
		return false
	}
	var out []hoverLine
	for i, d := range ds {
		if i > 0 {
			out = append(out, hoverLine{rule: true})
		}
		head := "● " + severityLabel(d.Severity)
		if attr := diagAttribution(d); attr != "" {
			head += " — " + attr
		}
		style := lipgloss.NewStyle().Foreground(m.diagColor(d.Severity)).Bold(true)
		out = append(out, hoverLine{text: style.Render(head)})
		for _, line := range strings.Split(d.Message, "\n") {
			out = append(out, hoverLine{text: line})
		}
	}
	m.hover = &hoverState{lines: out}
	return true
}

// diagAttribution renders "source · code" for the popup header, whichever
// parts the server sent — the handle for judging (and reporting) a false
// positive.
func diagAttribution(d ilsp.Diagnostic) string {
	switch {
	case d.Source != "" && d.Code != "":
		return d.Source + " · " + d.Code
	case d.Source != "":
		return d.Source
	default:
		return d.Code
	}
}

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

// gitMarkColor maps a gutter diff marker (Roadmap 0320, #464) to the theme's
// vcs status slots: added green, changed blue, deleted the dim border tone.
func (m Model) gitMarkColor(mk vcs.LineMark) color.Color {
	switch mk {
	case vcs.LineAdded:
		return m.theme().VCSAdded
	case vcs.LineDeleted:
		return m.theme().VCSDeleted
	default:
		return m.theme().VCSModified
	}
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
