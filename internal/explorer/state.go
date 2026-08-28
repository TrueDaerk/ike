package explorer

import (
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// State captures the explorer's session-restorable state: the set of expanded
// directory paths (excluding the always-open root), the show-hidden toggle, and
// the path under the cursor.
type State struct {
	Expanded   []string
	ShowHidden bool
	Cursor     string
	// Scratches section state (#1963): whether it is folded to its divider
	// and the dragged body height (0 = keep the configured default).
	ScratchCollapsed bool
	ScratchHeight    int
}

// ShowingHidden reports whether dot-entries are currently rendered, whichever
// path set it last (config apply or the runtime `.` toggle). The app compares
// it around a live reconfigure to persist config-driven changes (#642).
func (m Model) ShowingHidden() bool { return m.showHidden }

// Snapshot returns the current restorable state.
func (m Model) Snapshot() State {
	var expanded []string
	var walk func(n *node)
	walk = func(n *node) {
		for _, c := range n.children {
			if c.isDir && c.expanded {
				expanded = append(expanded, c.path)
			}
			walk(c)
		}
	}
	walk(m.root)
	cursor := ""
	if n := m.currentConst(); n != nil {
		cursor = n.path
	}
	return State{
		Expanded:         expanded,
		ShowHidden:       m.showHidden,
		Cursor:           cursor,
		ScratchCollapsed: m.scrCollapsed,
		ScratchHeight:    m.scrHeight,
	}
}

// currentConst is a non-mutating variant of current for snapshotting.
func (m Model) currentConst() *node {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor]
}

// Restore re-applies a saved State. Only the root loads on the constructor
// thread (#2260) — one ReadDir for the rows the first frame shows. The saved
// expanded directories load through the async scan path instead: Restore
// records them in restorePending, Init issues the scans that are already
// reachable, and each landing scan expands the pending descendants it just
// uncovered (continueRestore). Directories that no longer exist are dropped as
// soon as their parent's scan proves them gone. The root being loaded still
// means Init must not issue a competing root re-scan.
func (m *Model) Restore(s State) {
	m.showHidden = s.ShowHidden
	m.scrCollapsed = s.ScratchCollapsed
	if s.ScratchHeight > 0 {
		m.scrHeight = s.ScratchHeight
	}
	m.loadSync(m.root)

	if len(s.Expanded) > 0 {
		m.restorePending = make(map[string]bool, len(s.Expanded))
		for _, p := range s.Expanded {
			m.restorePending[p] = true
			// Already-reachable nodes (the root's own children) show their
			// expanded arrow from the first frame; deeper ones appear as their
			// ancestors load. No I/O here — Init issues the scans.
			if n := nodeByPath(m.root, p); n != nil && n.isDir {
				n.expanded = true
			}
		}
	}

	m.rebuild()
	if s.Cursor != "" {
		placed := false
		for i, n := range m.rows {
			if n.path == s.Cursor {
				m.cursor = i
				placed = true
				break
			}
		}
		if !placed && len(m.restorePending) > 0 {
			// The saved cursor sits inside a directory still loading: park it
			// as a pending snap, resolved by the rebuild of whichever restore
			// scan makes the row visible. finishRestore clears a snap whose
			// row never appeared (the file is gone).
			m.restoreCursor = s.Cursor
			m.pendingSel = s.Cursor
			m.followSel = true
		}
	}
	m.followCursor()
}

// continueRestore advances the async session restore (#2260): every pending
// saved-expanded path whose node became reachable is expanded and scanned;
// paths whose loaded parent no longer lists them are dropped. Init calls it
// for the paths reachable right after Restore, applyScan after every landing
// scan. Nil when nothing new is scannable.
func (m *Model) continueRestore() tea.Cmd {
	if len(m.restorePending) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for p := range m.restorePending {
		n := nodeByPath(m.root, p)
		if n == nil {
			if parent := nodeByPath(m.root, filepath.Dir(p)); parent != nil && parent.loaded {
				delete(m.restorePending, p) // gone from disk since the save
			}
			continue
		}
		if !n.isDir {
			delete(m.restorePending, p)
			continue
		}
		n.expanded = true
		if n.loaded {
			delete(m.restorePending, p)
			continue
		}
		if !n.loading {
			n.loading = true
			cmds = append(cmds, scanCmd(n.path))
		}
	}
	return tea.Batch(cmds...)
}

// finishRestore runs after a landing scan's rebuild: once nothing is pending
// and the saved cursor's snap did not resolve, the row is gone for good — the
// slot is released so the stability snap (#1140) works again.
func (m *Model) finishRestore() {
	if len(m.restorePending) != 0 || m.restoreCursor == "" {
		return
	}
	if m.pendingSel == m.restoreCursor {
		m.pendingSel = ""
	}
	m.restoreCursor = ""
}

// loadSync reads a directory node's children on the update thread. Unlike the
// async scanCmd path it blocks, which is acceptable during startup restore. A
// read error leaves the node empty but marked loaded.
func (m *Model) loadSync(n *node) {
	if !n.isDir || n.loaded {
		return
	}
	n.loading = false
	n.loaded = true
	des, err := os.ReadDir(n.path)
	if err != nil {
		return
	}
	if fi, err := os.Stat(n.path); err == nil {
		n.modTime = fi.ModTime()
	}
	entries := make([]scanEntry, len(des))
	for i, de := range des {
		entries[i] = scanEntry{name: de.Name(), isDir: de.IsDir()}
	}
	m.setChildren(n, entries)
}
