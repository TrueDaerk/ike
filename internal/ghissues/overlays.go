package ghissues

// overlays.go holds the modals the pane composites over its body: the label
// multi-picker that replaced the old 'l' cycling (#2090), the action menu that
// makes every key of the current view discoverable (#2090), and the
// text-edit picker that chooses which of the issue's texts a markdown buffer
// opens (#2087, textedit.go). The mutation pickers and the comment prompt
// (#2088) live in mutations.go. All own the keyboard while open, navigate with
// the shared list semantics, and are dismissed with esc.

import (
	"strings"

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
	// disabled marks an action the current permissions do not allow (#2088):
	// the footer drops it, the action menu still lists it — greyed, with the
	// reason spelled out — so an unavailable action is explained, not silently
	// missing.
	disabled bool
	reason   string
}

// act builds one enabled action; the mutation actions (#2088) are the only
// ones that fill the disabled/reason fields.
func act(key, hint, label string, run func(*Model) tea.Cmd) action {
	return action{key: key, hint: hint, label: label, run: run}
}

// actions lists what the current view and mode can do, in footer order. The
// action menu renders it verbatim, so a key can never be in one and missing
// from the other.
func (m *Model) actions() []action {
	if m.detail && m.tab == TabIssues {
		acts := []action{
			act("esc", "back", "Back to the list", func(m *Model) tea.Cmd { m.detail = false; return nil }),
			act("ctrl+j", "next issue", "Next issue", (*Model).nextIssue),
			act("ctrl+k", "prev issue", "Previous issue", (*Model).prevIssue),
			act("j/k", "scroll", "Scroll the body", nil),
		}
		if m.tlMore {
			acts = append(acts, action{key: "L", hint: "more", label: "Load more activity", run: (*Model).loadMoreTimeline})
		}
		acts = append(acts, m.mutationActions()...)
		acts = append(acts, m.textEditActions()...)
		return append(acts,
			action{key: "s", hint: "start work", label: "Start work (create the branch)", run: (*Model).startWork},
			action{key: "o", hint: "browser", label: "Open in browser", run: (*Model).openInBrowser},
			action{key: "tab", hint: "view", label: "Switch view (Issues / PRs)", run: func(m *Model) tea.Cmd { m.switchTab(1); return nil }},
			action{key: "r", hint: "refresh", label: "Refresh the issue and its activity", run: (*Model).refreshDetail},
		)
	}
	if m.tab == TabPRs {
		return []action{
			act("enter", "open PR", "Open the pull request in the browser", (*Model).openInBrowser),
			act("f", "filter", "Filter pull requests", func(m *Model) tea.Cmd { m.openFilter(); return nil }),
			act("t", "state", "State filter (open / closed / all)", (*Model).cycleState),
			act("a", "sort", "Sort order ("+m.sort.String()+")", func(m *Model) tea.Cmd { m.cycleSort(); return nil }),
			act("esc", "clear filters", "Clear the filters", (*Model).clearFilters),
			act("tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(-1); return nil }),
			act("r", "refresh", "Refresh the listing", (*Model).startRefresh),
		}
	}
	acts := []action{
		act("enter", "detail", "Open the issue detail", func(m *Model) tea.Cmd { m.openDetail(); return nil }),
		act("s", "start work", "Start work (create the branch)", (*Model).startWork),
		act("o", "browser", "Open in browser", (*Model).openInBrowser),
	}
	acts = append(acts, m.mutationActions()...)
	return append(acts,
		act("f", "filter", "Filter issues", func(m *Model) tea.Cmd { m.openFilter(); return nil }),
		act("l", "labels", "Label picker", func(m *Model) tea.Cmd { m.openLabelPicker(); return nil }),
		act("t", "state", "State filter (open / closed / all)", (*Model).cycleState),
		act("a", "sort", "Sort order ("+m.sort.String()+")", func(m *Model) tea.Cmd { m.cycleSort(); return nil }),
		act("g", "group", "Group by label", func(m *Model) tea.Cmd { m.toggleGroup(); return nil }),
		act("esc", "clear filters", "Clear the filters", (*Model).clearFilters),
		act("tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(1); return nil }),
		act("r", "refresh", "Refresh the listing", (*Model).startRefresh),
	)
}

// mutationActions are the write actions of the issue views (#2088): the label
// and assignee pickers and the two state changes. They are only listed once a
// forge backend is bound, and they carry the capability gate — without triage
// permission each is disabled and names why, so the user learns what is
// missing instead of finding the keys dead.
func (m *Model) mutationActions() []action {
	if m.mutate == nil {
		return nil
	}
	verb := m.stateVerb()
	acts := []action{
		act("e", "labels±", "Edit the issue's labels", (*Model).openLabelEditor),
		act("u", "assignees", "Edit the issue's assignees", (*Model).openAssigneeEditor),
		act("c", verb, capitalize(verb)+" the issue", (*Model).toggleIssueState),
		act("C", verb+"+comment", capitalize(verb)+" the issue with a comment", (*Model).openCommentPrompt),
	}
	if reason := m.mutateReason(); reason != "" {
		for i := range acts {
			acts[i].disabled, acts[i].reason = true, reason
			acts[i].label += " — " + reason
		}
	}
	return acts
}

// capitalize upper-cases a verb's first letter for the menu's sentences.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
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

// overlayItems is how many rows the open modal lists — the comment prompt's
// two rows are not navigable, they only size its box.
func (m *Model) overlayItems() int {
	switch m.ov {
	case ovLabels:
		return len(m.labels)
	case ovActions:
		return len(m.actions())
	case ovLabelEdit, ovAssignEdit:
		return len(m.editRows())
	case ovComment:
		return 2 // the input line and its hint
	case ovTextEdit:
		return len(m.textTargets())
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
	// The comment prompt is a text field: every printable key belongs to it,
	// so it never sees the list navigation.
	if m.ov == ovComment {
		return m.commentPromptKey(msg)
	}
	if ui.ListNav(key, &m.ovCursor, m.overlayItems(), m.overlayHeight(), ui.NavFull) {
		m.clampOverlay()
		return nil
	}
	switch m.ov {
	case ovLabels:
		return m.labelPickerKey(key)
	case ovActions:
		return m.actionMenuKey(key)
	case ovLabelEdit, ovAssignEdit:
		return m.editorKey(key)
	case ovTextEdit:
		return m.textPickerKey(key)
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
