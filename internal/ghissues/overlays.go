package ghissues

// overlays.go holds the two modals the pane composites over its body (#2090):
// the label multi-picker that replaced the old 'l' cycling, and the action
// menu that makes every key of the current view discoverable. Both own the
// keyboard while open, navigate with the shared list semantics, and are
// dismissed with esc.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// action is one entry of the action menu: the key that triggers it outside
// the menu, what it does, and the mutation enter runs from inside.
type action struct {
	key   string
	hint  string // compact footer wording
	label string // full sentence for the action menu
	run   func(*Model) tea.Cmd
}

// actions lists what the current view and mode can do, in footer order. The
// action menu renders it verbatim, so a key can never be in one and missing
// from the other.
func (m *Model) actions() []action {
	if m.detail && m.tab == TabIssues {
		return []action{
			{"esc", "back", "Back to the list", func(m *Model) tea.Cmd { m.detail = false; return nil }},
			{"ctrl+j", "next issue", "Next issue", func(m *Model) tea.Cmd { m.stepIssue(1); return nil }},
			{"ctrl+k", "prev issue", "Previous issue", func(m *Model) tea.Cmd { m.stepIssue(-1); return nil }},
			{"j/k", "scroll", "Scroll the body", nil},
			{"s", "start work", "Start work (create the branch)", (*Model).startWork},
			{"o", "browser", "Open in browser", (*Model).openInBrowser},
			{"tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(1); return nil }},
			{"r", "refresh", "Refresh the listing", (*Model).startRefresh},
		}
	}
	if m.tab == TabPRs {
		return []action{
			{"enter", "open PR", "Open the pull request in the browser", (*Model).openInBrowser},
			{"f", "filter", "Filter pull requests", func(m *Model) tea.Cmd { m.openFilter(); return nil }},
			{"t", "state", "State filter (open / closed / all)", (*Model).cycleState},
			{"a", "sort", "Sort order (" + m.sort.String() + ")", func(m *Model) tea.Cmd { m.cycleSort(); return nil }},
			{"esc", "clear filters", "Clear the filters", (*Model).clearFilters},
			{"tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(-1); return nil }},
			{"r", "refresh", "Refresh the listing", (*Model).startRefresh},
		}
	}
	return []action{
		{"enter", "detail", "Open the issue detail", func(m *Model) tea.Cmd { m.openDetail(); return nil }},
		{"s", "start work", "Start work (create the branch)", (*Model).startWork},
		{"o", "browser", "Open in browser", (*Model).openInBrowser},
		{"f", "filter", "Filter issues", func(m *Model) tea.Cmd { m.openFilter(); return nil }},
		{"l", "labels", "Label picker", func(m *Model) tea.Cmd { m.openLabelPicker(); return nil }},
		{"t", "state", "State filter (open / closed / all)", (*Model).cycleState},
		{"a", "sort", "Sort order (" + m.sort.String() + ")", func(m *Model) tea.Cmd { m.cycleSort(); return nil }},
		{"g", "group", "Group by label", func(m *Model) tea.Cmd { m.toggleGroup(); return nil }},
		{"esc", "clear filters", "Clear the filters", (*Model).clearFilters},
		{"tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(1); return nil }},
		{"r", "refresh", "Refresh the listing", (*Model).startRefresh},
	}
}

// Actions lists the current view's key/label pairs (tests, and the proof that
// footer and menu are one table).
func (m *Model) Actions() [][2]string {
	acts := m.actions()
	out := make([][2]string, 0, len(acts))
	for _, a := range acts {
		out = append(out, [2]string{a.key, a.label})
	}
	return out
}

// openLabelPicker opens the multi-select label overlay, remembering the
// current selection so esc can put it back.
func (m *Model) openLabelPicker() {
	if len(m.labels) == 0 {
		return
	}
	if m.labelSel == nil {
		m.labelSel = map[string]bool{}
	}
	m.ovSaved = map[string]bool{}
	for k, v := range m.labelSel {
		m.ovSaved[k] = v
	}
	m.ov, m.ovCursor, m.ovTop = ovLabels, 0, 0
	for i, name := range m.labels {
		if m.labelSel[name] {
			m.ovCursor = i
			break
		}
	}
	m.clampOverlay()
}

// openActionMenu opens the action list of the current view.
func (m *Model) openActionMenu() {
	m.ov, m.ovCursor, m.ovTop = ovActions, 0, 0
}

// closeOverlay drops the modal without touching what it changed.
func (m *Model) closeOverlay() { m.ov, m.ovSaved = ovNone, nil }

// overlayItems is how many rows the open modal lists.
func (m *Model) overlayItems() int {
	switch m.ov {
	case ovLabels:
		return len(m.labels)
	case ovActions:
		return len(m.actions())
	}
	return 0
}

// overlayHeight is the row budget the modal's list gets: the body minus the
// box's two border rows and its heading, so the whole box always fits the
// canvas it is composited onto.
func (m *Model) overlayHeight() int {
	h := m.bodyHeight() - 3
	if h < 1 {
		h = 1
	}
	if n := m.overlayItems(); n < h {
		h = n
	}
	if h < 1 {
		h = 1
	}
	return h
}

// clampOverlay keeps the modal's cursor valid and scrolled into view.
func (m *Model) clampOverlay() {
	n := m.overlayItems()
	if m.ovCursor > n-1 {
		m.ovCursor = n - 1
	}
	if m.ovCursor < 0 {
		m.ovCursor = 0
	}
	if m.ovTop > m.ovCursor {
		m.ovTop = m.ovCursor
	}
	if h := m.overlayHeight(); m.ovCursor >= m.ovTop+h {
		m.ovTop = m.ovCursor - h + 1
	}
	if m.ovTop < 0 {
		m.ovTop = 0
	}
}

// overlayKey routes one key to the open modal.
func (m *Model) overlayKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if ui.ListNav(key, &m.ovCursor, m.overlayItems(), m.overlayHeight(), ui.NavFull) {
		m.clampOverlay()
		return nil
	}
	switch m.ov {
	case ovLabels:
		return m.labelPickerKey(key)
	case ovActions:
		return m.actionMenuKey(key)
	}
	return nil
}

// labelPickerKey handles the multi-select overlay: space toggles the label
// under the cursor and re-narrows live, backspace clears the whole selection,
// enter keeps it, esc restores what the picker opened with.
func (m *Model) labelPickerKey(key string) tea.Cmd {
	switch key {
	case "space", " ", "x":
		if m.ovCursor >= 0 && m.ovCursor < len(m.labels) {
			name := m.labels[m.ovCursor]
			if m.labelSel[name] {
				delete(m.labelSel, name)
			} else {
				m.labelSel[name] = true
			}
			m.resetCursors()
			m.applyFilter()
		}
	case "backspace", "delete":
		m.labelSel = map[string]bool{}
		m.resetCursors()
		m.applyFilter()
	case "enter":
		m.closeOverlay()
	case "esc", "q", "l":
		m.labelSel = m.ovSaved
		if m.labelSel == nil {
			m.labelSel = map[string]bool{}
		}
		m.closeOverlay()
		m.resetCursors()
		m.applyFilter()
	}
	return nil
}

// actionMenuKey handles the action list: enter runs the selected action (and
// closes first, so an action that opens another modal wins), anything
// dismissive closes.
func (m *Model) actionMenuKey(key string) tea.Cmd {
	switch key {
	case "enter":
		acts := m.actions()
		if m.ovCursor < 0 || m.ovCursor >= len(acts) {
			m.closeOverlay()
			return nil
		}
		run := acts[m.ovCursor].run
		m.closeOverlay()
		if run == nil {
			return nil
		}
		return run(m)
	case "esc", "q", "m", "?":
		m.closeOverlay()
	}
	return nil
}
