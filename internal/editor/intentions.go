package editor

import (
	"ike/internal/concealexplain"
)

// intentions.go exports the caret probes the intention-action context needs
// (#2020): each is a cheap read over state the editor already holds, so the
// app can snapshot the caret facts on every alt+enter without re-deriving
// anything.

// LangID is the buffer's registered language id ("" for an unsaved buffer or
// an unknown extension) — the langID the conceal explainer and the doc-path
// scanner already resolve.
func (m Model) LangID() string { return m.langID() }

// DiagnosticOnCaretLine reports an LSP diagnostic on the caret line — the
// lsp.ignoreDiagnostic gate (#1259).
func (m Model) DiagnosticOnCaretLine() bool {
	return len(m.diagByLine[m.cursor.Line]) > 0
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

// ConcealExplainAtCaret reports whether the conceal explainer resolves a
// value at the caret (#1998) and, when the value renders through a conceal
// stand-in, the concealfilter family gating it ("" for a plain value). It is
// the read-only twin of explainConceal.
func (m *Model) ConcealExplainAtCaret() (family string, ok bool) {
	line := m.cursor.Line
	if line >= m.buf.LineCount() {
		return "", false
	}
	req := concealexplain.Request{Line: m.buf.Line(line), Col: m.cursor.Col, Lang: m.langID()}
	if capture, r, hit := m.concealAtCaret(); hit {
		req.Start, req.End, req.Capture, req.Display = r.start, r.end, capture, r.repl
	}
	ex, ok := concealexplain.Explain(req)
	if !ok {
		return "", false
	}
	return ex.Family, true
}
