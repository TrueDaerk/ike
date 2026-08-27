package editor

import (
	"ike/internal/concealexplain"
	ilsp "ike/internal/lsp"
)

// intentions.go exports the caret probes the intention-action context needs
// (#2020): each is a cheap read over state the editor already holds, so the
// app can snapshot the caret facts on every alt+enter without re-deriving
// anything.

// LangID is the buffer's registered language id ("" for an unsaved buffer or
// an unknown extension) — the langID the conceal explainer and the doc-path
// scanner already resolve.
func (m Model) LangID() string { return m.langID() }

// DiagnosticOnCaretLine reports an LSP diagnostic on the caret line that
// lsp.ignoreDiagnostic could actually write a rule for (#1259): a diagnostic
// carrying neither source, code nor message matches on nothing, and the
// command answers that instead of ignoring anything (#2026).
func (m Model) DiagnosticOnCaretLine() bool {
	for _, d := range m.diagByLine[m.cursor.Line] {
		if ilsp.IgnoreRuleFor(d) != "" {
			return true
		}
	}
	return false
}

// HunkAtCursor reports a VCS gutter mark (added/changed/deleted, #464) on
// the caret line — the vcs.revertHunk gate.
func (m Model) HunkAtCursor() bool {
	_, ok := m.gitMarks[m.cursor.Line]
	return ok
}

// CanToggleValueAtCaret reports whether editor.toggleValue would change
// something: the first token at or after the caret has a known counterpart
// (#1658).
func (m *Model) CanToggleValueAtCaret() bool {
	if m.cursor.Line >= m.buf.LineCount() {
		return false
	}
	_, _, _, ok := toggleTokenAt([]rune(m.buf.Line(m.cursor.Line)), m.cursor.Col, m.togglePairs())
	return ok
}

// ToggleValuePreview computes what editor.toggleValue would make of the caret
// line (#2252) without touching the buffer: the line as it is and the line
// with the token flipped, built from a copy of its runes. ok is false when
// nothing on the line toggles — the same verdict CanToggleValueAtCaret gives,
// so an offered entry always previews.
func (m *Model) ToggleValuePreview() (before, after string, line int, ok bool) {
	line = m.cursor.Line
	if line >= m.buf.LineCount() {
		return "", "", 0, false
	}
	before = m.buf.Line(line)
	runes := []rune(before)
	start, end, repl, ok := toggleTokenAt(runes, m.cursor.Col, m.togglePairs())
	if !ok {
		return "", "", 0, false
	}
	return before, string(runes[:start]) + repl + string(runes[end:]), line, true
}

// ConcealExplainAtCaret reports whether a conceal stand-in the explainer
// speaks for sits under the caret (#1998), and names the concealfilter family
// gating it. It is the read-only twin of explainConceal, narrowed to what the
// intention popup may offer (#2026): explainConceal also explains a plain
// value ("why is this *not* masked", #1930), but as a popup entry that read
// fired on every identifier and answered that nothing conceals it. The
// stand-in is what makes "Explain Concealed Value" true, and a family that is
// toggled off in this view still has one — which is exactly the case worth
// asking about.
func (m *Model) ConcealExplainAtCaret() (family string, ok bool) {
	line := m.cursor.Line
	if line >= m.buf.LineCount() {
		return "", false
	}
	capture, r, hit := m.concealAtCaret()
	if !hit {
		return "", false
	}
	ex, ok := concealexplain.Explain(concealexplain.Request{
		Line: m.buf.Line(line), Col: m.cursor.Col, Lang: m.langID(),
		Start: r.start, End: r.end, Capture: capture, Display: r.repl,
	})
	if !ok {
		return "", false
	}
	return ex.Family, true
}
