package explorer

// ctxmenu.go holds the explorer's side of the keyboard context menu and the
// archive actions (#2805). The menu itself and the actions are the app's; the
// tree only answers where the cursor row sits on screen, which entries an
// action targets, and — once an extraction or compression produced a new
// entry — rescans and selects it.

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// SelectPathMsg rescans the directory holding Path and puts the cursor on the
// entry once the scan shows it — the refresh-and-select that follows an
// operation the app ran on disk (an extraction or a compression, #2805).
type SelectPathMsg struct{ Path string }

func (SelectPathMsg) explorerMsg() {}

// selectPath is SelectPathMsg's handler: the same snap-then-rescan a create
// performs.
func (m *Model) selectPath(path string) tea.Cmd {
	if path == "" {
		return nil
	}
	m.exitScratch()
	m.clearSel()
	m.snapCursorTo(path)
	return m.refreshDir(filepath.Dir(path))
}

// ContextRow locates the cursor row for the keyboard context menu (#2805) in
// content-local cells, scrolling it into view first: the tree's cursor row,
// or the Scratches entry under the cursor. The selection is left exactly as it
// is — a multi-select keeps its range and marks, so the menu's bulk actions
// act on it as they do after a right-click inside the selection. ok is false
// when there is no row to anchor at.
func (m *Model) ContextRow() (x, y int, ok bool) {
	if m.inScratch() {
		e, has := m.scratchSelected()
		if !has {
			return 0, 0, false
		}
		m.followScratchCursor()
		row, visible := m.scratchAnchorRow(e.Path)
		return 1, row, visible
	}
	if len(m.rows) == 0 {
		return 0, 0, false
	}
	m.current() // clamps a stale cursor
	m.followCursor()
	y = m.cursor - m.offset
	if _, textH, _, _, _ := m.viewport(); y < 0 || y >= textH {
		return 0, 0, false
	}
	return 1, y, true
}

// OpTargetPaths returns the entries a file operation acts on — the marked
// entries, else the shift range, else the cursor entry — and whether that is
// a multi-select. It is empty on the Scratches section and on the root.
func (m Model) OpTargetPaths() (paths []string, bulk bool) {
	ts, bulk := m.opTargets()
	for _, t := range ts {
		paths = append(paths, t.path)
	}
	return paths, bulk
}
