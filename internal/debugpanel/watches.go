package debugpanel

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/dap"
)

// watches.go is the watch-expressions section (#1914): a synthetic "Watches"
// root leading the variables tree. The app owns the expression list and
// re-evaluates it on every stop (DAP evaluate); the panel renders the pushed
// results and turns add/edit/remove intents into messages, reusing the inline
// line editor (#627) for the expression text. A structured result (non-zero
// variablesReference) expands like any variable.

// AddWatchMsg asks the app to append a watch expression.
type AddWatchMsg struct{ Expr string }

// EditWatchMsg asks the app to replace watch Index's expression.
type EditWatchMsg struct {
	Index int
	Expr  string
}

// RemoveWatchMsg asks the app to delete watch Index.
type RemoveWatchMsg struct{ Index int }

// WatchResult is one evaluated watch row. A zero Value with empty Err renders
// as a pending expression (no stop to evaluate against yet); Err renders in
// place of the value.
type WatchResult struct {
	Expr  string
	Value string
	Type  string
	Ref   int
	Err   string
}

// SetEvaluateSupported records the session's evaluate capability (#2174).
// With it off, the watches section keeps listing the expressions — the list
// is per project, not per adapter — but says so in its header instead of
// showing values that will never arrive.
func (m *Model) SetEvaluateSupported(v bool) { m.noEval = !v }

// EvaluateSupported reports the recorded evaluate capability (#2174).
func (m Model) EvaluateSupported() bool { return !m.noEval }

// watchesUnsupportedNote is the header suffix of a watches section whose
// adapter cannot evaluate (#2174). Short on purpose — the variables column is
// narrow and would truncate a sentence; the full wording rides the
// notification the app posts once per session.
const watchesUnsupportedNote = "evaluate unsupported"

// SetWatches replaces the watches section with the pushed results; an empty
// list removes it. Any open inline editor is cancelled — the rows it anchored
// to are being replaced (#640).
func (m *Model) SetWatches(results []WatchResult) {
	m.cancelEdit()
	if len(results) == 0 {
		m.watchRoot = nil
		m.clampVarSel()
		return
	}
	root := &varNode{
		v:        dap.Variable{Name: "Watches"},
		expanded: true,
		loaded:   true,
	}
	if m.noEval {
		root.v.Value = watchesUnsupportedNote
	}
	for i, r := range results {
		v := dap.Variable{Name: r.Expr, Value: r.Value, Type: r.Type, VariablesReference: r.Ref}
		if r.Err != "" {
			v.Value = "⚠ " + r.Err
			v.VariablesReference = 0
		}
		root.children = append(root.children, &varNode{
			v: v, depth: 1, isWatch: true, watchIdx: i,
		})
	}
	m.watchRoot = root
	m.clampVarSel()
}

// clampVarSel keeps the variables selection inside the (possibly shrunk) row
// count after a watches replace.
func (m *Model) clampVarSel() {
	rows := len(m.flat())
	m.varSel = clamp(m.varSel, 0, max(0, rows-1))
	m.varTop = scrollToShow(m.varTop, m.varSel, m.bodyHeight(), rows)
}

// startWatchAdd opens the inline editor on a fresh placeholder row under the
// watches section (key "a" in the variables column). Allowed while running —
// the expression evaluates on the next stop.
func (m *Model) startWatchAdd() {
	m.cancelEdit()
	if m.watchRoot == nil {
		m.watchRoot = &varNode{v: dap.Variable{Name: "Watches"}, expanded: true, loaded: true}
	}
	m.watchRoot.expanded = true
	placeholder := &varNode{depth: 1, isWatch: true, watchIdx: -1}
	m.watchRoot.children = append(m.watchRoot.children, placeholder)
	for i, n := range m.flat() {
		if n == placeholder {
			m.varSel = i
			break
		}
	}
	m.col = colVars
	m.varTop = scrollToShow(m.varTop, m.varSel, m.bodyHeight(), len(m.flat()))
	m.editing = true
	m.editWatch = true
	m.editWatchIdx = -1
	m.edit.Clear()
}

// startWatchEdit opens the inline editor on watch row n's expression.
func (m *Model) startWatchEdit(n *varNode) {
	m.editing = true
	m.editWatch = true
	m.editWatchIdx = n.watchIdx
	m.edit.Set(n.v.Name)
}

// commitWatch turns the closed editor's text into the watch mutation message:
// a new expression appends, an edited one replaces, and an emptied one
// removes its row (add with empty text is simply dropped).
func (m *Model) commitWatch(idx int, expr string) tea.Cmd {
	if expr == "" {
		if idx < 0 {
			return nil
		}
		return func() tea.Msg { return RemoveWatchMsg{Index: idx} }
	}
	if idx < 0 {
		return func() tea.Msg { return AddWatchMsg{Expr: expr} }
	}
	return func() tea.Msg { return EditWatchMsg{Index: idx, Expr: expr} }
}

// dropWatchPlaceholder removes the pending add-watch row (editor cancelled or
// committed); a section materialized only for the add disappears with it.
func (m *Model) dropWatchPlaceholder() {
	if m.watchRoot == nil {
		return
	}
	kids := m.watchRoot.children[:0]
	for _, n := range m.watchRoot.children {
		if n.isWatch && n.watchIdx < 0 {
			continue
		}
		kids = append(kids, n)
	}
	m.watchRoot.children = kids
	if len(kids) == 0 {
		m.watchRoot = nil
	}
	m.clampVarSel()
}
