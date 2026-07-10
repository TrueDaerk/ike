package app

import (
	"os"

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
		return m.panes.Get(key)
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
	inst := m.panes.Get(key)
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
	m.activateTab(inst, idx)
	if ed := inst.Editor(); ed.HasFile() {
		m.explorer().SetActive(ed.Path())
	}
	saveLayout(m.tree, m.panes)
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
		saveLayout(m.tree, m.panes)
	}
}

// rememberClosedTab pushes a closing tab's document and caret onto the reopen
// ring. Scratch tabs have no path to restore and are skipped.
func (m *Model) rememberClosedTab(ed *editor.Model) {
	if ed == nil || !ed.HasFile() {
		return
	}
	line, col := ed.CursorPos()
	m.closedTabs = append(m.closedTabs, closedTab{path: ed.Path(), line: line, col: col})
	if len(m.closedTabs) > closedTabRing {
		m.closedTabs = m.closedTabs[len(m.closedTabs)-closedTabRing:]
	}
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
