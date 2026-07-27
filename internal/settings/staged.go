package settings

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// staged.go is the staged-apply model (0460, #1296). Schema edits no longer
// hit disk per keystroke: they collect in an ordered staging buffer, the
// header counts them, and applying opens a diff of `page · key · old → new`
// before anything is written. One batch means one config reload, so the app
// re-themes and rebuilds its keymaps once instead of once per changed key.
//
// Custom pages keep writing directly — they own flows (installing a plugin,
// creating a virtualenv) that are not "a value in a file" and cannot be
// staged meaningfully.

// change is one staged edit. old is the value as it stood before the panel
// staged anything for the key, so discarding restores it and the diff can
// always show where the value came from.
type change struct {
	page  int
	entry Entry
	scope config.Scope
	old   string
	// value is what apply writes; ignored when reset is set.
	value any
	// shown is value's Flat() rendering — what the settings column displays
	// and what the diff shows on the right of the arrow.
	shown string
	// reset removes the key from its layer instead of writing a value.
	reset bool
}

// flatten renders a staged value the way config.Flat renders the live one, so
// a staged row and a written row read identically.
func flatten(v any) string { return fmt.Sprint(v) }

// value reads an entry's effective value: the staged one when the panel holds
// an edit for it, otherwise the live config.
func (m *Model) value(key string) string {
	if c, ok := m.change(key); ok {
		return c.shown
	}
	return liveValue(key)
}

// liveValue reads an entry's value from the live config, ignoring staging.
func liveValue(key string) string {
	return config.Get().Flat()[key]
}

// change returns the staged edit for key, if any.
func (m *Model) change(key string) (change, bool) {
	for _, c := range m.changes {
		if c.entry.Key == key {
			return c, true
		}
	}
	return change{}, false
}

// Dirty reports whether the panel holds unapplied edits.
func (m *Model) Dirty() bool { return len(m.changes) > 0 }

// stage records an edit for e. Staging a value equal to the one the key
// started with drops the edit instead of recording a no-op, so editing back
// and forth leaves nothing to apply. Returns any command the edit needs right
// now — a live preview, not a write.
func (m *Model) stage(e Entry, v any) tea.Cmd {
	return m.record(change{entry: e, page: m.pageOf(e), scope: m.scopeFor(e), value: v, shown: flatten(v)})
}

// stageReset records a reset: apply removes the key from its layer. The shown
// value is the built-in default — the value a reset lands on unless a lower
// override layer still holds the key, which the panel cannot know without
// writing.
func (m *Model) stageReset(e Entry) tea.Cmd {
	return m.record(change{entry: e, page: m.pageOf(e), scope: m.scopeFor(e), reset: true, shown: config.Defaults()[e.Key]})
}

// record inserts or replaces c in the staging buffer, keeping the original
// pre-edit value and dropping edits that come back to it.
func (m *Model) record(c change) tea.Cmd {
	c.old = liveValue(c.entry.Key)
	for i, old := range m.changes {
		if old.entry.Key != c.entry.Key {
			continue
		}
		c.old = old.old
		if !c.reset && c.shown == c.old {
			m.changes = append(m.changes[:i], m.changes[i+1:]...)
			return m.preview(c.entry, c.old)
		}
		m.changes[i] = c
		return m.preview(c.entry, c.shown)
	}
	if !c.reset && c.shown == c.old {
		return nil
	}
	m.changes = append(m.changes, c)
	return m.preview(c.entry, c.shown)
}

// pageOf finds the page an entry belongs to, for the diff's first column.
func (m *Model) pageOf(e Entry) int {
	for i, p := range m.pages {
		for _, pe := range p.Entries {
			if pe.Key == e.Key {
				return i
			}
		}
	}
	return m.cat
}

// PreviewMsg asks the app to show a staged value without committing it
// (#1296): the theme has to be visible while it is being chosen, but a
// preview must never reach disk. The app re-applies the live configuration
// when the panel discards or closes.
type PreviewMsg struct {
	Key   string
	Value string
}

// previewKeys are the keys worth previewing: ones whose whole point is what
// they look like.
var previewKeys = map[string]bool{"theme.name": true}

// preview emits a preview for keys that have one; everything else waits for
// apply.
func (m *Model) preview(e Entry, shown string) tea.Cmd {
	if !previewKeys[e.Key] {
		return nil
	}
	return func() tea.Msg { return PreviewMsg{Key: e.Key, Value: shown} }
}

// changedOnPage counts staged edits belonging to a page, for the rail marker.
func (m *Model) changedOnPage(page int) int {
	n := 0
	for _, c := range m.changes {
		if c.page == page {
			n++
		}
	}
	return n
}

// applyChanges writes the whole staging buffer in one batch and clears it.
func (m *Model) applyChanges() tea.Cmd {
	if len(m.changes) == 0 {
		return nil
	}
	muts := make([]config.Mutation, 0, len(m.changes))
	for _, c := range m.changes {
		muts = append(muts, config.Mutation{Scope: c.scope, Key: c.entry.Key, Value: c.value, Remove: c.reset})
	}
	m.changes = nil
	m.notice = "✓ applied " + plural(len(muts), "change")
	m.writeErr = ""
	m.rebuildEditor()
	return config.ApplyAndReload(m.opts, muts)
}

// discardChanges drops every staged edit and undoes any live preview.
func (m *Model) discardChanges() tea.Cmd {
	if len(m.changes) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, c := range m.changes {
		if previewKeys[c.entry.Key] {
			old := c.old
			cmds = append(cmds, func() tea.Msg { return PreviewMsg{Key: c.entry.Key, Value: old} })
		}
	}
	m.changes = nil
	m.notice = "discarded"
	m.rebuildEditor()
	return tea.Batch(cmds...)
}

// undoChange drops one staged edit (the diff panel's "u"), reverting its
// preview.
func (m *Model) undoChange(i int) tea.Cmd {
	if i < 0 || i >= len(m.changes) {
		return nil
	}
	c := m.changes[i]
	m.changes = append(m.changes[:i], m.changes[i+1:]...)
	m.rebuildEditor()
	if !previewKeys[c.entry.Key] {
		return nil
	}
	return func() tea.Msg { return PreviewMsg{Key: c.entry.Key, Value: c.old} }
}

// moveChangesTo retargets the whole batch at one layer (the diff panel's "s"),
// so a set of edits meant for the project can be redirected before it lands.
func (m *Model) moveChangesTo(scope config.Scope) {
	for i := range m.changes {
		m.changes[i].scope = scope
	}
}

// rebuildEditor drops the detail column's editor so it re-reads the value it
// should now show (a discard must not leave a stale input on screen).
func (m *Model) rebuildEditor() { m.editor, m.editorKey = nil, "" }

// plural renders "1 change" / "3 changes".
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// scopeLabelOf names a write scope for the diff panel.
func scopeLabelOf(s config.Scope) string {
	if s == config.ProjectScope {
		return "project"
	}
	return "user"
}

// targetFiles lists the distinct layer files the staged batch would touch, in
// a stable order, for the diff panel's title.
func (m *Model) targetFiles() string {
	var seen []string
	for _, c := range m.changes {
		label := scopeLabelOf(c.scope)
		if !contains(seen, label) {
			seen = append(seen, label)
		}
	}
	return strings.Join(seen, " + ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
