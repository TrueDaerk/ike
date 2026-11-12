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
// operation may target it). In the Scratches section (#1963) the mark lands
// in the section's own set (#3138, scrMarks): marks never mix sections, so a
// bulk operation consumes only the marks of the section it was started in.
// The safety net for the permanent scratch delete is the confirmation, which
// lists every target by name. The map is copied on write: Model is passed
// around by value, so mutating a shared map in place would leak marks into
// stale copies.
func (m *Model) toggleMark() {
	if m.inScratch() {
		if e, ok := m.scratchSelected(); ok {
			m.scrMarks = toggled(m.scrMarks, e.Path)
		}
		return
	}
	n := m.current()
	if n == nil || n == m.root {
		return
	}
	m.marks = toggled(m.marks, n.path)
}

// toggled returns a copy of set with path flipped; an emptied set is nil.
func toggled(set map[string]bool, path string) map[string]bool {
	next := make(map[string]bool, len(set)+1)
	for p := range set {
		next[p] = true
	}
	if next[path] {
		delete(next, path)
	} else {
		next[path] = true
	}
	if len(next) == 0 {
		return nil
	}
	return next
}

// clearMarks drops the whole multi-select. Esc calls it, and so does every
// bulk operation once it has run — the selection never outlives the action it
// was built for.
func (m *Model) clearMarks() { m.marks = nil }

// clearScratchMarks drops the Scratches section's marks (#3138). Kept apart
// from clearMarks so a tree bulk operation never consumes them.
func (m *Model) clearScratchMarks() { m.scrMarks = nil }

// scratchMarked reports whether a scratch path carries a section mark.
func (m Model) scratchMarked(path string) bool { return m.scrMarks[path] }

// extendScratchSel is extendSel inside the Scratches section (#3138): the
// first shifted motion anchors at the section cursor, and the range stays
// within the section — it never crosses the divider into the tree.
func (m *Model) extendScratchSel(delta int) {
	if len(m.scrEntries) == 0 {
		return
	}
	if m.scrSelAnchor < 0 {
		m.scrSelAnchor = m.scrCursor
	}
	m.scrCursor = clamp(m.scrCursor+delta, 0, len(m.scrEntries)-1)
	m.followScratchCursor()
}

// scratchSelRange is selRange for the section: the inclusive entry bounds of
// the section range, ok false when none is active.
func (m Model) scratchSelRange() (lo, hi int, ok bool) {
	if m.scrSelAnchor < 0 || !m.inScratch() || len(m.scrEntries) == 0 {
		return 0, 0, false
	}
	lo, hi = m.scrSelAnchor, m.scrCursor
	if lo > hi {
		lo, hi = hi, lo
	}
	last := len(m.scrEntries) - 1
	return clamp(lo, 0, last), clamp(hi, 0, last), true
}

// inScratchSelRange reports whether section entry i is part of the range.
func (m Model) inScratchSelRange(i int) bool {
	lo, hi, ok := m.scratchSelRange()
	return ok && i >= lo && i <= hi
}

// scratchOpTargets resolves what a delete started in the Scratches section
// acts on (#3138), with opTargets' precedence: the section marks, then the
// section range, then the cursor scratch. Targets come in section order. bulk
// is true for the first two; tree marks and ranges are never consulted.
func (m Model) scratchOpTargets() (paths []string, bulk bool) {
	if len(m.scrMarks) > 0 {
		for _, e := range m.scrEntries {
			if m.scrMarks[e.Path] {
				paths = append(paths, e.Path)
			}
		}
		if len(paths) > 0 {
			return paths, true
		}
	}
	if lo, hi, ok := m.scratchSelRange(); ok && hi > lo {
		for i := lo; i <= hi; i++ {
			paths = append(paths, m.scrEntries[i].Path)
		}
		return paths, true
	}
	if e, ok := m.scratchSelected(); ok {
		return []string{e.Path}, false
	}
	return nil, false
}

// pruneScratchMarks drops section marks whose scratch vanished from the store
// and re-bounds the section range anchor; RefreshScratches calls it.
func (m *Model) pruneScratchMarks() {
	if m.scrSelAnchor >= len(m.scrEntries) {
		m.scrSelAnchor = len(m.scrEntries) - 1
	}
	if len(m.scrMarks) == 0 {
		return
	}
	var live map[string]bool
	for _, e := range m.scrEntries {
		if m.scrMarks[e.Path] {
			if live == nil {
				live = map[string]bool{}
			}
			live[e.Path] = true
		}
	}
	m.scrMarks = live
}

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
// step; false keeps the single-entry behaviour byte-for-byte unchanged. In the
// Scratches section it is empty: the section's own delete resolves its
// targets through scratchOpTargets (#3138), and copy/cut/move stay tree-only.
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
