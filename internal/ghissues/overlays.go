package ghissues

// overlays.go holds the modals the pane composites over its body: the action
// menu that makes every key of the current view discoverable (#2090) and the
// text-edit picker that chooses which of the issue's texts a markdown buffer
// opens (#2087, textedit.go). The unified filter overlay (#2104) lives in
// filterov.go, the mutation pickers and the comment prompt (#2088) in
// mutations.go. All own the keyboard while open, navigate with the shared
// list semantics, and are dismissed with esc.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
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
			acts = append(acts, action{key: "p", hint: "more", label: "Load more activity (next page)", run: (*Model).loadMoreTimeline})
		}
		acts = append(acts, m.copyAction()...)
		acts = append(acts, m.editAction()...)
		acts = append(acts, m.mutationActions()...)
		acts = append(acts, m.commentAction()...)
		return append(acts,
			action{key: "s", hint: "start work", label: "Start work (create the branch)", run: (*Model).startWork},
			action{key: "o", hint: "browser", label: "Open in browser", run: (*Model).openInBrowser},
			action{key: "tab", hint: "view", label: "Switch view (Issues / PRs)", run: func(m *Model) tea.Cmd { m.switchTab(1); return nil }},
			action{key: "r", hint: "refresh", label: "Refresh the issue and its activity", run: (*Model).refreshDetail},
		)
	}
	if m.prDetail && m.tab == TabPRs {
		acts := []action{
			act("esc", "back", "Back to the list", func(m *Model) tea.Cmd { m.prDetail = false; return nil }),
			act("ctrl+j", "next PR", "Next pull request", func(m *Model) tea.Cmd { return m.stepPR(1) }),
			act("ctrl+k", "prev PR", "Previous pull request", func(m *Model) tea.Cmd { return m.stepPR(-1) }),
			act("j/k", "scroll", "Scroll the body", nil),
		}
		acts = append(acts, m.copyAction()...)
		acts = append(acts, m.prActionActions()...)
		return append(acts,
			act("o", "browser", "Open in browser", (*Model).openInBrowser),
			act("tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(1); return nil }),
			act("r", "refresh", "Refresh the pull request", func(m *Model) tea.Cmd {
				return tea.Batch(m.startRefresh(), m.fetchPRDetail(m.prdFor))
			}),
		)
	}
	if m.tab == TabPRs {
		acts := []action{
			act("enter", "detail", "Open the pull request detail", (*Model).openPRDetail),
			act("o", "browser", "Open in browser", (*Model).openInBrowser),
		}
		acts = append(acts, m.prActionActions()...)
		acts = append(acts,
			act("f", "filter", "Filter (match / state / sort)", func(m *Model) tea.Cmd { m.openFilterOverlay(fovMatch); return nil }),
			act("t", "state", "State filter (open / closed / all)", (*Model).cycleState),
			act("a", "sort", "Sort order ("+m.sort.String()+")", func(m *Model) tea.Cmd { m.cycleSort(); return nil }))
		acts = append(acts, m.savedFilterActions()...)
		return append(acts,
			act("esc", "clear filters", "Clear the filters", (*Model).clearFilters),
			act("tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(-1); return nil }),
			act("r", "refresh", "Refresh the listing", (*Model).startRefresh),
		)
	}
	acts := []action{
		act("enter", "detail", "Open the issue detail", func(m *Model) tea.Cmd { m.openDetail(); return nil }),
		act("s", "start work", "Start work (create the branch)", (*Model).startWork),
		act("o", "browser", "Open in browser", (*Model).openInBrowser),
	}
	acts = append(acts, m.editAction()...)
	acts = append(acts, m.mutationActions()...)
	acts = append(acts,
		act("f", "filter", "Filter (match / state / labels)", func(m *Model) tea.Cmd { m.openFilterOverlay(fovMatch); return nil }),
		act("l", "labels", "Filter by label (the filter's label section)", func(m *Model) tea.Cmd { m.openLabelSection(); return nil }),
		act("t", "state", "State filter (open / closed / all)", (*Model).cycleState),
		act("a", "sort", "Sort order ("+m.sort.String()+")", func(m *Model) tea.Cmd { m.cycleSort(); return nil }),
		// Grouping lost its direct key in the #2114 consolidation ('g' means
		// "jump to the top" in every mode now); it stays reachable here and in
		// the filter overlay. An empty key marks a menu-only action the footer
		// skips.
		act("", "", "Group by label (also in the filter overlay)", func(m *Model) tea.Cmd { m.toggleGroup(); return nil }))
	acts = append(acts, m.savedFilterActions()...)
	return append(acts,
		act("esc", "clear filter", "Clear a filter (peels one at a time)", (*Model).clearFilters),
		act("tab", "view", "Switch view (Issues / PRs)", func(m *Model) tea.Cmd { m.switchTab(1); return nil }),
		act("r", "refresh", "Refresh the listing", (*Model).startRefresh),
	)
}

// mutationActions are the state-write actions of the issue views (#2088):
// close/reopen, plain and with a comment. (The label and assignee pickers sit
// behind the unified 'e' edit picker since #2114.) They are only listed once a
// forge backend is bound, and they carry the capability gate — without triage
// permission each is disabled and names why, so the user learns what is
// missing instead of finding the keys dead.
func (m *Model) mutationActions() []action {
	if m.mutate == nil {
		return nil
	}
	verb := m.stateVerb()
	acts := []action{
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

// prActionActions are the write actions of the PR views (#2089): merge with
// a comment and close with a comment, both behind the confirm dialog. Only
// listed once a forge backend is bound; without push permission each is
// disabled and names why, mirroring the issue mutations.
func (m *Model) prActionActions() []action {
	if m.prAction == nil {
		return nil
	}
	acts := []action{
		act("M", "merge", "Merge the pull request (with an optional comment)",
			func(m *Model) tea.Cmd { return m.openPRActionDialog(forge.PRMerge) }),
		act("c", "close", "Close the pull request (with an optional comment)",
			func(m *Model) tea.Cmd { return m.openPRActionDialog(forge.PRClose) }),
	}
	if reason := m.prActReason(); reason != "" {
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

// viewActions is what the action menu renders and navigates: actions()
// narrowed by the type-ahead (#2111), matched against key and label together
// so both "browser" and "o" find "Open in browser".
func (m *Model) viewActions() []action {
	return ui.Narrow(&m.ovSearch, m.actions(), func(a action) string { return a.key + " " + a.label })
}

// openActionMenu opens the action list of the current view.
func (m *Model) openActionMenu() {
	m.ov, m.ovCursor, m.ovTop = ovActions, 0, 0
	m.ovSearch.Reset()
}

// closeOverlay drops the modal without touching what it changed.
func (m *Model) closeOverlay() { m.ov = ovNone }

// overlayItems is how many rows the open modal lists — the comment prompt's
// two rows are not navigable, they only size its box.
func (m *Model) overlayItems() int {
	switch m.ov {
	case ovFilter:
		return m.fovFixedRows() + m.fovLabelRows()
	case ovActions:
		return len(m.viewActions())
	case ovLabelEdit, ovAssignEdit:
		return len(m.editViewRows())
	case ovComment:
		return 2 // the input line and its hint
	case ovPRAct, ovCleanup:
		return 3 // fixed dialog rows, not navigable — they only size the box
	case ovEdit:
		return len(m.editEntries())
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
	ui.ClampWindow(&m.ovCursor, &m.ovTop, n, m.overlayHeight())
}

// overlayKey routes one key to the open modal.
func (m *Model) overlayKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	// The comment prompt is a text field: every printable key belongs to it,
	// so it never sees the list navigation.
	if m.ov == ovComment {
		return m.commentPromptKey(msg)
	}
	// The PR dialogs are text/confirm surfaces too (#2089): every key is
	// theirs, never the list navigation.
	if m.ov == ovPRAct {
		return m.prActionDialogKey(msg)
	}
	if m.ov == ovCleanup {
		return m.cleanupOfferKey(msg)
	}
	// The filter overlay's match row is a text input: it routes its own keys
	// (#2104), so the shared list navigation never eats a printable.
	if m.ov == ovFilter {
		return m.filterOvKey(msg)
	}
	// The speed-searchable pickers (#2111) own every printable key, so they
	// only get the navigation aliases that are not letters — j/k/g/G would
	// eat the type-ahead's first rune.
	nav := ui.NavFull
	switch m.ov {
	case ovActions, ovLabelEdit, ovAssignEdit:
		nav = ui.NavDefault
	}
	if ui.ListNav(key, &m.ovCursor, m.overlayItems(), m.overlayHeight(), nav) {
		m.clampOverlay()
		return nil
	}
	switch m.ov {
	case ovActions:
		return m.actionMenuKey(msg)
	case ovLabelEdit, ovAssignEdit:
		return m.editorKey(msg)
	case ovEdit:
		return m.editPickerKey(key)
	}
	return nil
}

// actionMenuKey handles the action list: enter runs the selected action (and
// closes first, so an action that opens another modal wins), esc peels the
// type-ahead (#2111) before it closes. Printable keys narrow the list, which
// is why 'q'/'m'/'?' no longer double as close aliases — esc is the one exit.
func (m *Model) actionMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter":
		acts := m.viewActions()
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
	case "esc":
		if m.ovSearch.EscClears() {
			m.clampOverlay()
			return nil
		}
		m.closeOverlay()
		return nil
	}
	if handled, changed := m.ovSearch.Key(msg); handled && changed {
		m.ovCursor, m.ovTop = 0, 0
		m.clampOverlay()
	}
	return nil
}
