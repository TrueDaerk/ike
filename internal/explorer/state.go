package explorer

import (
	"os"
	"sort"
	"strings"
)

// State captures the explorer's session-restorable state: the set of expanded
// directory paths (excluding the always-open root), the show-hidden toggle, and
// the path under the cursor.
type State struct {
	Expanded   []string
	ShowHidden bool
	Cursor     string
}

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
	return State{Expanded: expanded, ShowHidden: m.showHidden, Cursor: cursor}
}

// currentConst is a non-mutating variant of current for snapshotting.
func (m Model) currentConst() *node {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor]
}

// Restore re-applies a saved State: it sets the show-hidden flag, synchronously
// re-expands the saved directories (shallowest first so ancestors load before
// their children), rebuilds the visible rows, and parks the cursor on the saved
// path when it is visible. Directories that no longer exist are skipped. Restore
// loads the root synchronously, so Init must not issue a competing async scan.
func (m *Model) Restore(s State) {
	m.showHidden = s.ShowHidden
	m.loadSync(m.root)

	// Shallower paths first: a child can only be reached once its parent's
	// children have been loaded.
	paths := append([]string(nil), s.Expanded...)
	sort.SliceStable(paths, func(i, j int) bool {
		return strings.Count(paths[i], string(os.PathSeparator)) <
			strings.Count(paths[j], string(os.PathSeparator))
	})
	for _, p := range paths {
		n := nodeByPath(m.root, p)
		if n == nil || !n.isDir {
			continue
		}
		n.expanded = true
		m.loadSync(n)
	}

	m.rebuild()
	if s.Cursor != "" {
		for i, n := range m.rows {
			if n.path == s.Cursor {
				m.cursor = i
				break
			}
		}
	}
	m.clampScroll()
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
	entries := make([]scanEntry, len(des))
	for i, de := range des {
		entries[i] = scanEntry{name: de.Name(), isDir: de.IsDir()}
	}
	m.setChildren(n, entries)
}
