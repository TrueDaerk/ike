package editor

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/docpath"
	"ike/internal/editor/register"
	"ike/internal/highlight"
)

// docpath.go is the editor side of the JSON/YAML path breadcrumb (#1660): the
// caret's structural path, shown in the status line and copyable in three
// flavours (dotted, jq, yq). The derivation lives in internal/docpath; here it
// is only cached and rendered.
//
// The scan reads the buffer from its first line up to the caret, so it is
// cached per document version *and* caret position — one scan per edit or
// motion, not one per frame — and the whole feature is off in large-file mode
// (InsightOff), like every other whole-buffer analysis.

// docPathMaxCells bounds the status-line label. A deep manifest path is far
// wider than its slot, and the *tail* is the interesting part ("which key am I
// on"), so the label truncates from the left; the copy commands always take
// the full path.
const docPathMaxCells = 44

// docPathState caches the caret's path. A pointer field on Model, like
// svTable, so the value copies each Update returns share it.
type docPathState struct {
	valid     bool
	version   int
	path      string
	line, col int
	steps     []docpath.Step
}

// DocPathKind selects the flavour a copy command writes.
type DocPathKind int

const (
	// DocPathDotted is the reading form, `spec.containers[2].name`.
	DocPathDotted DocPathKind = iota
	// DocPathJQ is the jq expression, `.spec.containers[2].name`.
	DocPathJQ
	// DocPathYQ is the yq (v4) expression — jq's syntax with yq's spelling
	// for keys that need quotes.
	DocPathYQ
)

// docPathLang returns the buffer's language id when it has a path scanner and
// the buffer is small enough to scan, "" otherwise.
func (m Model) docPathLang() string {
	if m.InsightOff() {
		return ""
	}
	id := highlight.Lang(m.langPath())
	if !docpath.IsLang(id) {
		return ""
	}
	return id
}

// docPathSteps returns the caret's path, recomputing it when the document
// version or the caret moved.
func (m Model) docPathSteps() []docpath.Step {
	id := m.docPathLang()
	if id == "" {
		return nil
	}
	st := m.docPathCache
	if st == nil {
		return docpath.At(id, m.buf, m.cursor.Line, m.cursor.Col)
	}
	if st.valid && st.version == m.docVersion && st.path == m.path &&
		st.line == m.cursor.Line && st.col == m.cursor.Col {
		return st.steps
	}
	st.valid, st.version, st.path = true, m.docVersion, m.path
	st.line, st.col = m.cursor.Line, m.cursor.Col
	st.steps = docpath.At(id, m.buf, m.cursor.Line, m.cursor.Col)
	return st.steps
}

// DocPathLabel is the status-line label for the caret's path (#1660):
// `…containers[2].env[0].name`, truncated from the left so the tail always
// shows. Empty for every buffer without a path scanner, and at the document
// root — which hides the segment.
func (m Model) DocPathLabel() string {
	steps := m.docPathSteps()
	if len(steps) == 0 {
		return ""
	}
	return truncLeftCells(docpath.Dotted(steps), docPathMaxCells)
}

// DocPath renders the caret's full path in one flavour for the copy commands.
// ok is false when there is nothing to copy.
func (m Model) DocPath(kind DocPathKind) (string, bool) {
	steps := m.docPathSteps()
	if len(steps) == 0 {
		return "", false
	}
	switch kind {
	case DocPathJQ:
		return docpath.JQ(steps), true
	case DocPathYQ:
		return docpath.YQ(steps), true
	default:
		return docpath.Dotted(steps), true
	}
}

// copyDocPath puts the caret's path on the system clipboard with a toast, the
// #1173 copy-path pattern applied to a position inside the document instead of
// to the file.
func (m *Model) copyDocPath(kind DocPathKind) tea.Cmd {
	text, ok := m.DocPath(kind)
	if !ok {
		if m.docPathLang() == "" {
			return notice("no json/yaml path in this file")
		}
		return notice("no json/yaml path at the caret")
	}
	m.regs.Yank('+', register.Entry{Text: text})
	if cmd := m.takeClipboardSignal(); cmd != nil {
		return cmd
	}
	return notice("copied " + text)
}

// truncLeftCells clips s to max rune cells from the left, marking the cut with
// a leading ellipsis.
func truncLeftCells(s string, max int) string {
	r := []rune(s)
	if len(r) <= max || max < 2 {
		return s
	}
	return "…" + string(r[len(r)-max+1:])
}
