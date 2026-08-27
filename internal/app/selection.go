package app

import (
	"ike/internal/pane"
)

// activeSelectionText returns the text of whatever selection is live right
// now, or "" when nothing is selected (#2165). It is the shared seam for
// selection-scoped actions that must work outside the editor — Find in Path
// prefills its query with it.
//
// The audit of selection sources (every pane that can hand out selected text):
//   - editor panes — the vim visual selection (editor.SelectionText);
//   - terminal panes, terminal tabs inside an editor pane, and the debug
//     area's console — the mouse stream selection (terminal.SelectionText),
//     all reachable through Instance.ActiveTerminal;
//   - diff panes — the mouse selection of one side column, plus the diff's
//     editable right side, which is a real editor with visual mode;
//   - merge panes — the mouse selection of the focused side column;
//   - the HTTP response viewer — the mouse selection of the composed view.
//
// Every other pane (explorer, VCS, problems, structure, usages, breakpoints,
// markdown/image/archive/data viewers) has a *row* cursor, not a text
// selection, and so contributes nothing here.
//
// The focused pane wins; when it has no selection of its own the last-focused
// editor is consulted, so invoking the command from a tool pane still picks up
// the visual selection the user left behind in the code.
func (m Model) activeSelectionText() string {
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil {
		if sel := instSelectionText(inst); sel != "" {
			return sel
		}
	}
	if ed := m.activeEditor(); ed != nil {
		if sel, ok := ed.SelectionText(); ok {
			return sel
		}
	}
	return ""
}

// instSelectionText extracts the selection of one pane instance, descending
// into a nested content tab (#1778) first — a viewer hosted as an editor tab
// selects like the equivalent dedicated pane.
func instSelectionText(inst *pane.Instance) string {
	if c := inst.ActiveContent(); c != nil {
		if sel := instSelectionText(c); sel != "" {
			return sel
		}
	}
	if t := inst.ActiveTerminal(); t != nil {
		return t.SelectionText()
	}
	switch inst.Kind() {
	case pane.KindEditor:
		if sel, ok := inst.Editor().SelectionText(); ok {
			return sel
		}
	case pane.KindDiff:
		if ed := inst.DiffEditor(); ed != nil {
			if sel, ok := ed.SelectionText(); ok {
				return sel
			}
		}
		return inst.Diff().SelectionText()
	case pane.KindMerge:
		return inst.Merge().SelectionText()
	case pane.KindHTTP:
		return inst.HTTP().SelectionText()
	}
	return ""
}
