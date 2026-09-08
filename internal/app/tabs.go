package app

import (
	"os"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
)

// tabs.go holds the app-level tab operations behind the editor.tab.* commands
// (Roadmap 0190, #158): cycling, selecting, reordering and reopening tabs of
// the active editor pane. The tab list itself lives on pane.Instance (#156).

// closedTab is one entry of the reopen ring: enough to restore a closed tab's
// document and caret.
type closedTab struct {
	path      string
	line, col int
}

// closedTabRing bounds how many closed tabs the reopen ring remembers.
const closedTabRing = 10

// tabPane returns the editor pane tab commands act on — the focused editor,
// else the most recent one — or nil when no editor exists.
func (m *Model) tabPane() *pane.Instance {
	if key := m.activeEditorKey(); key != "" {
		return m.activeWS().Panes.Get(key)
	}
	return nil
}

// stepTab cycles the active tab by delta, wrapping around the tab list.
func (m *Model) stepTab(delta int) {
	if inst := m.tabPane(); inst != nil {
		m.cycleTabs(inst, delta)
	}
}

// cycleTabs advances inst's active tab by delta with wrap-around; shared by
// the next/prev commands and the wheel over the tab bar (#159).
func (m *Model) cycleTabs(inst *pane.Instance, delta int) {
	n := inst.TabCount()
	if n < 2 {
		return
	}
	m.switchTab(inst, ((inst.ActiveTab()+delta)%n+n)%n)
}

// closeBarTab closes tab idx of pane key after a middle-click on its bar
// segment (#159), with the same guard as editor.closeTab: the crash-backup
// snapshot survives only while the document is open elsewhere, and the pane
// itself closes when its last tab goes.
func (m *Model) closeBarTab(key string, idx int) {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		return
	}
	if inst.TabCount() > 1 {
		m.closeTab(inst, idx)
		return
	}
	m.setFocus(key)
	m.closeFocused()
}

// tabCloseScope names one of the batch tab-close commands (#1128, #2538).
// They differ only in which tabs they pick; pinned tabs (#1172), the guard and
// the pane bookkeeping are shared.
type tabCloseScope int

const (
	closeScopeOthers     tabCloseScope = iota // every tab but the active one
	closeScopeLeft                            // every tab before the active one
	closeScopeRight                           // every tab after the active one
	closeScopeUnmodified                      // every tab without unsaved changes
	closeScopeAll                             // every tab; the pane goes with them
)

// closeTabScope runs one of the batch tab closes on the active editor pane.
// Pinned tabs (#1172) are never picked — pinning is the way to hold a tab
// through a Close Others / Close All — and tabs whose close would drop unsaved
// changes route the whole batch through the guard prompt first (#259), which
// answers for all of them at once instead of once per file.
func (m *Model) closeTabScope(scope tabCloseScope) {
	inst := m.tabPane()
	if inst == nil {
		return
	}
	victims, pinned := tabCloseVictims(inst, scope)
	if pinned > 0 {
		noun := " pinned tabs kept"
		if pinned == 1 {
			noun = " pinned tab kept"
		}
		m.host.Notify(host.Info, strconv.Itoa(pinned)+noun)
	}
	if len(victims) == 0 {
		return
	}
	// Nothing would be left behind: the pane closes with its last tab, the
	// way cmd+w on a single-tab pane already behaves (#156).
	whole := len(victims) == inst.TabCount()
	if dirty := m.dirtyInTabs(inst, victims); len(dirty) > 0 {
		m.openBatchClosePrompt(inst.Key(), victims, dirty, whole)
		return
	}
	m.closeTabSet(inst, victims, whole)
}

// tabCloseVictims picks the tabs scope selects, in ascending order, and counts
// the pinned ones it skipped so the caller can report them.
func tabCloseVictims(inst *pane.Instance, scope tabCloseScope) (victims []int, pinned int) {
	active := inst.ActiveTab()
	for i := 0; i < inst.TabCount(); i++ {
		var want bool
		switch scope {
		case closeScopeOthers:
			want = i != active
		case closeScopeLeft:
			want = i < active
		case closeScopeRight:
			want = i > active
		case closeScopeUnmodified:
			// A tab restored but never activated (#2177) has no editor yet,
			// so it cannot hold unsaved changes.
			ed := inst.TabEditor(i)
			want = ed == nil || !ed.Dirty()
		case closeScopeAll:
			want = true
		}
		if !want {
			continue
		}
		if inst.TabPinned(i) {
			pinned++
			continue
		}
		victims = append(victims, i)
	}
	return victims, pinned
}

// closeTabSet closes the given tab indexes of inst, highest first so the lower
// ones stay valid. closeTab never empties a pane (#156), so when the batch
// covers every tab the last one rides out on the pane close instead; a pane
// that cannot close (the workspace's last leaf) keeps that one tab.
func (m *Model) closeTabSet(inst *pane.Instance, idxs []int, whole bool) {
	for n := len(idxs) - 1; n >= 0; n-- {
		if inst.TabCount() <= 1 {
			break
		}
		m.closeTab(inst, idxs[n])
	}
	if whole && inst.TabCount() == 1 {
		m.setFocus(inst.Key())
		m.closeFocused()
	}
}

// closeOtherTabs closes every tab of the active editor pane except the active
// one (#1128, "Close Others").
func (m *Model) closeOtherTabs() { m.closeTabScope(closeScopeOthers) }

// togglePinTab flips the active tab's pin (#1172) and persists it with the
// layout, so pins survive restarts like the tab list itself.
func (m *Model) togglePinTab() {
	inst := m.tabPane()
	if inst == nil {
		return
	}
	inst.ToggleTabPin(inst.ActiveTab())
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// selectTab activates the tab at idx; out-of-range indexes are a no-op.
func (m *Model) selectTab(idx int) {
	inst := m.tabPane()
	if inst == nil || idx < 0 || idx >= inst.TabCount() {
		return
	}
	m.switchTab(inst, idx)
}

// switchTab activates tab idx (autosaving the document being left, #174) and
// carries the pane's bookkeeping: the explorer accent follows the new active
// document and the persisted layout records it.
func (m *Model) switchTab(inst *pane.Instance, idx int) {
	if idx == inst.ActiveTab() {
		return
	}
	// Switching tabs leaves one file for another — a navigation jump like any
	// other (#816). Only the open funnel used to record (explorer, palette,
	// go-to-definition), so the most ordinary way to change file, the tab bar
	// and its next/prev chords, left no history at all and Back reported "no
	// earlier position". Read from inst rather than the focused editor: a tab
	// click can land on an unfocused pane.
	from := navPosOfPane(inst)
	m.usage.Layout("tab.switch", nil)
	m.activateTab(inst, idx)
	if to := navPosOfPane(inst); from.Path != "" && from.Path != to.Path {
		m.recordNavFrom(from)
	}
	if ed := inst.Editor(); ed != nil && ed.HasFile() {
		m.explorer().SetActive(ed.Path())
	}
	// The Problems pane's current-file scope tracks the tab switch (#1024).
	m.syncProblemsActive()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// moveTab reorders the active tab by delta positions; moves past either end
// are a no-op.
func (m *Model) moveTab(delta int) {
	inst := m.tabPane()
	if inst == nil || delta == 0 {
		return
	}
	from := inst.ActiveTab()
	if inst.MoveTab(from, from+delta) {
		m.usage.Layout("tab.move", nil)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
}

// rememberClosedTab pushes a closing tab's document and caret onto the reopen
// ring. Scratch tabs have no path to restore and are skipped.
func (m *Model) rememberClosedTab(ed *editor.Model) {
	if ed == nil || !ed.HasFile() {
		return
	}
	if ed.ReadOnly() {
		// A read-only preview's path names no file on disk (an archive entry,
		// #1762): the ring would only ever fail to reopen it.
		return
	}
	line, col := ed.CursorPos()
	m.closedTabs = appendClosedTab(m.closedTabs, closedTab{path: ed.Path(), line: line, col: col})
}

// appendClosedTab pushes one entry onto the reopen ring, trimming it to
// closedTabRing. Deferred tabs (#2177) close through it too — they have a
// path and a caret without ever having loaded a document.
func appendClosedTab(ring []closedTab, entry closedTab) []closedTab {
	ring = append(ring, entry)
	if len(ring) > closedTabRing {
		ring = ring[len(ring)-closedTabRing:]
	}
	return ring
}

// reopenClosedTab pops the reopen ring and opens the entry in the active pane,
// restoring the caret. Entries whose file vanished since (deleted externally)
// are skipped; an empty ring reports instead of failing silently.
func (m Model) reopenClosedTab() (tea.Model, tea.Cmd) {
	for len(m.closedTabs) > 0 {
		last := m.closedTabs[len(m.closedTabs)-1]
		m.closedTabs = m.closedTabs[:len(m.closedTabs)-1]
		if _, err := os.Stat(last.path); err != nil {
			continue
		}
		return m.openPathAt(last.path, last.line, last.col)
	}
	m.host.Notify(host.Info, "no closed tabs to reopen")
	return m, nil
}
