package explorer

// marks.go implements the explorer's sticky multi-select (#2166): a set of
// marked entries, toggled one row at a time with space, that survives every
// cursor motion and every rebuild. It sits beside — not instead of — the
// contiguous shift+j/k range (#1044): the range is a transient "extend from
// here" gesture that any plain motion collapses, while marks are deliberate
// and persist until esc clears them or a bulk operation consumes them.
//
// Marks are keyed by absolute path, not by row index, so a rescan, a sort
// change or a hidden-files toggle cannot silently re-point them at the wrong
// entries. A marked entry inside a collapsed directory keeps its mark; the
// bulk prompts list every target by name, so nothing acts invisibly.

import (
	"path/filepath"
	"strconv"
	"strings"
)

// markGlyph is the per-row multi-select marker. It only occupies a column
// while at least one entry is marked, so the unmarked tree renders exactly as
// before.
const markGlyph = "✓"

// toggleMark flips the mark on the cursor row. The root is never markable (no
// operation may target it) and the Scratches section (#1963) has no
// multi-select at all — permanent scratch deletes stay one-at-a-time
// deliberate. The map is copied on write: Model is passed around by value, so
// mutating a shared map in place would leak marks into stale copies.
func (m *Model) toggleMark() {
	if m.inScratch() {
		return
	}
	n := m.current()
	if n == nil || n == m.root {
		return
	}
	next := make(map[string]bool, len(m.marks)+1)
	for p := range m.marks {
		next[p] = true
	}
	if next[n.path] {
		delete(next, n.path)
	} else {
		next[n.path] = true
	}
	if len(next) == 0 {
		next = nil
	}
	m.marks = next
}

// clearMarks drops the whole multi-select. Esc calls it, and so does every
// bulk operation once it has run — the selection never outlives the action it
// was built for.
func (m *Model) clearMarks() { m.marks = nil }

// marked reports whether an absolute path carries a mark.
func (m Model) marked(path string) bool { return m.marks[path] }

// markCount is the number of marked entries; zero means the explorer behaves
// exactly as it did before the multi-select existed.
func (m Model) markCount() int { return len(m.marks) }

// MarkedPaths returns the marked entries in tree order, for app commands that
// want to act on the explorer's multi-select (file.move, #175/#2166). It is
// empty when nothing is marked.
func (m Model) MarkedPaths() []string {
	ts := m.markTargets()
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.path)
	}
	return out
}

// markTargets resolves the marked entries a bulk operation acts on, in tree
// order: the whole tree is walked (not just the visible rows), so a mark under
// a collapsed directory still counts, and an entry nested under another marked
// directory is dropped — operating on the ancestor already carries its subtree,
// so the nested target's own rename/copy would fail or duplicate work. A
// depth-first walk emits ancestors before descendants, which is what makes the
// prefix filter below sufficient.
func (m Model) markTargets() []delTarget {
	if len(m.marks) == 0 {
		return nil
	}
	var ts []delTarget
	var walk func(n *node)
	walk = func(n *node) {
		if n != m.root && m.marks[n.path] && !nestedIn(ts, n.path) {
			ts = append(ts, delTarget{path: n.path, isDir: n.isDir})
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(m.root)
	return ts
}

// nestedIn reports whether path lives under one of the already-collected
// directory targets.
func nestedIn(ts []delTarget, path string) bool {
	for _, t := range ts {
		if t.isDir && strings.HasPrefix(path, t.path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// pruneMarks drops marks whose entry vanished from the tree (deleted outside
// IKE, filtered away by a config change). Called from rebuild, and only when
// something is actually marked, so the common path never walks the tree twice.
func (m *Model) pruneMarks() {
	if len(m.marks) == 0 {
		return
	}
	live := make(map[string]bool, len(m.marks))
	var walk func(n *node)
	walk = func(n *node) {
		if m.marks[n.path] {
			live[n.path] = true
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(m.root)
	if len(live) == 0 {
		live = nil
	}
	m.marks = live
}

// opTargets resolves what a bulk-capable operation (delete, move, copy) acts
// on, in precedence order: the sticky marks (#2166) first, then an active
// contiguous range (#1044), then the plain cursor entry. bulk is true for the
// first two — it selects the multi-entry prompt wording and the batched undo
// step; false keeps the single-entry behaviour byte-for-byte unchanged.
func (m Model) opTargets() (targets []delTarget, bulk bool) {
	if m.inScratch() {
		return nil, false
	}
	if ts := m.markTargets(); len(ts) > 0 {
		return ts, true
	}
	if lo, hi, ok := m.selRange(); ok && hi > lo {
		return m.selTargets(), true
	}
	n := m.current()
	if n == nil || n == m.root {
		return nil, false
	}
	return []delTarget{{path: n.path, isDir: n.isDir}}, false
}

// entryNoun names a target count the way the prompts read it: a dir that
// swallowed its own marked children boils down to one "entry".
func entryNoun(count int) string {
	if count == 1 {
		return "entry"
	}
	return "entries"
}

// maxPromptLines caps the target list a bulk prompt spells out; a longer
// selection reports the remainder as a count so the box cannot outgrow the
// pane.
const maxPromptLines = 8

// targetLines renders the target list a bulk prompt shows, each entry
// project-relative so the list stays readable in a narrow pane.
func (m Model) targetLines(ts []delTarget) []string {
	var out []string
	for i, t := range ts {
		if i == maxPromptLines && len(ts) > maxPromptLines+1 {
			out = append(out, "… and "+strconv.Itoa(len(ts)-maxPromptLines)+" more")
			break
		}
		name := t.path
		if rel, err := filepath.Rel(m.root.path, t.path); err == nil {
			name = rel
		}
		if t.isDir {
			name += string(filepath.Separator)
		}
		out = append(out, "• "+name)
	}
	return out
}
