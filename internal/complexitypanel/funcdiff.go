package complexitypanel

// funcdiff.go is the Changes tab's 'd' (#2956): the selected function's
// two versions — base revision against working tree — in a diff pane. The
// pane only names the function; the root model fetches both texts, cuts
// them to the function ranges and opens the diff.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/complexity"
)

// FunctionDiffMsg asks the root model to open the diff of one Changes row's
// function. Change carries both ranges: on an added row Before is the zero
// unit (no base side), on a removed row After is. Ordinal is the unit's
// index among its file's same-named units, so overloads and repeated
// closures each get their own pane.
type FunctionDiffMsg struct {
	Path    string
	Name    string
	Ordinal int
	Change  complexity.Change
}

// FunctionDiff reports the diff request for the selected row: a function
// row of the Changes tab (a site or candidate row stands for its parent);
// false on the other tabs, a file row or no selection.
func (m *Model) FunctionDiff() (FunctionDiffMsg, bool) {
	if m.tab != TabChanges {
		return FunctionDiffMsg{}, false
	}
	r, ok := m.Selected()
	if ok && r.child() && r.Parent >= 0 && r.Parent < len(m.rows) {
		r = m.rows[r.Parent]
	}
	if !ok || r.aggregate() || r.Change == 0 || r.Path == "" {
		return FunctionDiffMsg{}, false
	}
	msg := FunctionDiffMsg{Path: r.Path, Name: r.Fn.Name, Ordinal: m.changeOrdinal(r)}
	switch r.Change {
	case complexity.Changed:
		msg.Change = complexity.Change{Before: r.Before, After: r.Fn}
	case complexity.Added:
		msg.Change = complexity.Change{After: r.Fn}
	case complexity.Removed:
		msg.Change = complexity.Change{Before: r.Fn}
	}
	return msg, true
}

// functionDiff is the pane's 'd'.
func (m *Model) functionDiff() tea.Cmd {
	msg, ok := m.FunctionDiff()
	if !ok {
		return nil
	}
	return func() tea.Msg { return msg }
}

// changeOrdinal is a Changes row's index among its file's same-named units
// — the pairing key of complexity.DiffFunctions. A head-side unit (changed,
// added) reads it off the index's measurement of the file; a removed unit
// is unpaired, so it sits past every head unit of that name. A file the
// index does not hold (or holds stale, an unsaved buffer) falls back to
// source order among the diff's own units — unique within the file either
// way, which is all the pane key needs.
func (m *Model) changeOrdinal(r Row) int {
	head := m.view.Files[r.Path]
	fd := m.changesDiff().Files[r.Path]
	before := func(fns []complexity.Function) int {
		n := 0
		for _, f := range fns {
			if f.Name == r.Fn.Name && f.Line < r.Fn.Line {
				n++
			}
		}
		return n
	}
	if r.Change == complexity.Removed {
		n := 0
		for _, f := range head {
			if f.Name == r.Fn.Name {
				n++
			}
		}
		return n + before(fd.Removed)
	}
	ords := complexity.Ordinals(head)
	for i, f := range head {
		if f.Name == r.Fn.Name && f.Line == r.Fn.Line {
			return ords[i]
		}
	}
	after := make([]complexity.Function, 0, len(fd.Changed)+len(fd.Added))
	for _, c := range fd.Changed {
		after = append(after, c.After)
	}
	return before(append(after, fd.Added...))
}
