package ghissues

// mutations.go is the pane's write side (#2088): the label and assignee
// pickers, the close/reopen actions with their optional comment, and the
// capability gate in front of all of them. The pane stays subprocess-free —
// the app injects a mutation factory (forge.MutateFactory) and a one-shot
// repository-metadata probe (forge.MetaFactory); every write leaves as a
// forge.Mutation and comes back as a forge.MutationMsg.
//
// A write is applied optimistically so the list never freezes on the network,
// and the pre-mutation issue is kept: a forge rejection (a token without the
// scope, a label the repository does not have) rolls the row back and shows
// the forge's own error. A success refetches instead of trusting the guess —
// the UI must show forge truth.

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/ui"
)

// SetMutate injects the write factory (forge.MutateFactory at the workspace
// root). Without it the mutation actions are not offered at all.
func (m *Model) SetMutate(fn func(forge.Mutation) tea.Cmd) { m.mutate = fn }

// SetMeta injects the repository-metadata probe (forge.MetaFactory at the
// workspace root): capabilities plus, when they allow mutating, the label set
// and the assignable users.
func (m *Model) SetMeta(fn func() tea.Cmd) { m.meta = fn }

// SetRepoMeta applies one finished metadata probe. A failure keeps whatever
// arrived (the capabilities travel even when a listing failed) and leaves the
// pickers on their listing-derived fallback.
func (m *Model) SetRepoMeta(msg forge.RepoMetaMsg) {
	if msg.Err == nil || msg.Caps != (forge.Capabilities{}) {
		m.caps, m.capsOK = msg.Caps, true
	}
	if len(msg.Labels) > 0 {
		m.repoLabels = msg.Labels
	}
	if len(msg.Users) > 0 {
		m.repoUsers = msg.Users
	}
	if msg.Err != nil {
		// A failed probe may be retried by 'r'; the gate stays closed until
		// one answers.
		m.metaRun = m.capsOK
	}
}

// Capabilities reports the probed permissions and whether one answered
// (tests, and the app's own gating).
func (m *Model) Capabilities() (forge.Capabilities, bool) { return m.caps, m.capsOK }

// startMeta returns the metadata probe the pane still owes, nil when it
// already ran (or no factory was injected).
func (m *Model) startMeta() tea.Cmd {
	if m.meta == nil || m.metaRun {
		return nil
	}
	m.metaRun = true
	return m.meta()
}

// canMutate reports whether the user may change labels, assignees and state.
func (m *Model) canMutate() bool { return m.mutate != nil && m.capsOK && m.caps.Triage }

// mutateReason explains a closed gate in one clause, "" when it is open.
func (m *Model) mutateReason() string {
	switch {
	case m.mutate == nil:
		return "no forge backend"
	case !m.capsOK:
		return "checking permissions…"
	case !m.caps.Triage:
		return "needs triage permission"
	}
	return ""
}

// MutationBusy reports whether a write is in flight (tests).
func (m *Model) MutationBusy() bool { return m.mutBusy > 0 }

// MutationError returns the last forge rejection, "" when none (tests).
func (m *Model) MutationError() string { return m.mutErr }

// LabelEditorOpen / AssigneeEditorOpen / CommentPromptOpen report which
// mutation modal owns the keyboard (tests).
func (m *Model) LabelEditorOpen() bool    { return m.ov == ovLabelEdit }
func (m *Model) AssigneeEditorOpen() bool { return m.ov == ovAssignEdit }
func (m *Model) CommentPromptOpen() bool  { return m.ov == ovComment }

// EditSelection returns the open picker's working set in row order (tests).
// It reads the full row set on purpose: a running type-ahead hides rows, it
// never unticks them.
func (m *Model) EditSelection() []string { return selected(m.editRows(), m.editSel) }

// EditVisible returns the rows the open picker currently shows — the whole
// row set, or what the type-ahead narrowed it to (tests).
func (m *Model) EditVisible() []string { return m.editViewRows() }

// SpeedSearchQuery returns the open picker's type-ahead text, "" when none is
// running (tests).
func (m *Model) SpeedSearchQuery() string { return m.ovSearch.Query() }

// selected keeps the rows the picker has ticked, in row order.
func selected(rows []string, set map[string]bool) []string {
	var out []string
	for _, name := range rows {
		if set[name] {
			out = append(out, name)
		}
	}
	return out
}

// pickerLabels are the label picker's rows: the repository's whole label set
// when the probe delivered it, otherwise the distinct labels the listing
// carries — a picker that can still remove what an issue has beats no picker.
func (m *Model) pickerLabels() []forge.Label {
	if len(m.repoLabels) > 0 {
		return m.repoLabels
	}
	seen := map[string]bool{}
	var out []forge.Label
	for i := range m.issues {
		for _, l := range m.issues[i].Labels {
			if !seen[l.Name] {
				seen[l.Name] = true
				out = append(out, l)
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// pickerUsers are the assignee picker's rows, with the same fallback: the
// probed collaborators, otherwise every login the listing shows as an
// assignee.
func (m *Model) pickerUsers() []string {
	if len(m.repoUsers) > 0 {
		return m.repoUsers
	}
	seen := map[string]bool{}
	var out []string
	for i := range m.issues {
		for _, a := range m.issues[i].Assignees {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

// editRows is the open picker's row labels.
func (m *Model) editRows() []string {
	switch m.ov {
	case ovLabelEdit:
		labels := m.pickerLabels()
		out := make([]string, 0, len(labels))
		for _, l := range labels {
			out = append(out, l.Name)
		}
		return out
	case ovAssignEdit:
		return m.pickerUsers()
	}
	return nil
}

// editViewRows is what the open picker renders and navigates: editRows()
// narrowed by the type-ahead (#2111). Cursor, row count and the space toggle
// all index into this set; the enter that writes the change reads editRows()
// instead, so a query never silently unticks what it merely hid.
func (m *Model) editViewRows() []string { return ui.NarrowStrings(&m.ovSearch, m.editRows()) }

// openLabelEditor opens the label picker for the selected issue, preselecting
// the labels it carries.
func (m *Model) openLabelEditor() tea.Cmd {
	is := m.mutationTarget()
	if is == nil {
		return nil
	}
	if len(m.pickerLabels()) == 0 {
		m.mutErr = "no labels to pick from"
		return nil
	}
	m.editSel = map[string]bool{}
	for _, l := range is.Labels {
		m.editSel[l.Name] = true
	}
	m.openEditor(ovLabelEdit, is.Number)
	return nil
}

// openAssigneeEditor opens the assignee picker for the selected issue.
func (m *Model) openAssigneeEditor() tea.Cmd {
	is := m.mutationTarget()
	if is == nil {
		return nil
	}
	if len(m.pickerUsers()) == 0 {
		m.mutErr = "no assignable users known — check the forge permissions"
		return nil
	}
	m.editSel = map[string]bool{}
	for _, a := range is.Assignees {
		m.editSel[a] = true
	}
	m.openEditor(ovAssignEdit, is.Number)
	return nil
}

// openEditor puts one picker on screen with its cursor on the first selected
// row, so an issue's own labels read first.
func (m *Model) openEditor(kind overlayKind, number int) {
	m.ov, m.editFor, m.ovCursor, m.ovTop = kind, number, 0, 0
	m.ovSearch.Reset()
	for i, name := range m.editRows() {
		if m.editSel[name] {
			m.ovCursor = i
			break
		}
	}
	m.clampOverlay()
}

// mutationTarget is the issue a mutation action applies to, nil (with the
// reason recorded) when there is none or the gate is closed.
func (m *Model) mutationTarget() *forge.Issue {
	if reason := m.mutateReason(); reason != "" {
		m.mutErr = "cannot change this issue: " + reason
		return nil
	}
	is := m.Selected()
	if is == nil {
		m.mutErr = "no issue selected"
		return nil
	}
	m.mutErr = ""
	return is
}

// editorKey handles both mutation pickers: space toggles the row under the
// cursor, backspace clears the whole set, enter applies the diff, esc drops
// it. Printable keys are the type-ahead (#2111) — they narrow the rows rather
// than acting — so esc peels the query first and 'q' only closes while no
// query is running.
func (m *Model) editorKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if m.ovSearch.EscClears() {
			m.clampOverlay()
			return nil
		}
		m.closeOverlay()
		return nil
	case "space", " ":
		view := m.editViewRows()
		if m.ovCursor >= 0 && m.ovCursor < len(view) {
			name := view[m.ovCursor]
			if m.editSel[name] {
				delete(m.editSel, name)
			} else {
				m.editSel[name] = true
			}
		}
		return nil
	case "enter":
		// The rows travel into the apply step: closing the modal first would
		// take editRows() — which reads the open modal's kind — with it. The
		// full set travels, not the narrowed one: a type-ahead hides rows, it
		// must not drop them from the write.
		rows, kind := m.editRows(), m.ov
		m.closeOverlay()
		if kind == ovLabelEdit {
			return m.applyLabelDiff(rows)
		}
		return m.applyAssignees(rows)
	}
	// Backspace deletes the last query rune while a type-ahead runs and only
	// falls through to "clear the selection" once the query is empty.
	if handled, changed := m.ovSearch.Key(msg); handled {
		if changed {
			m.ovCursor, m.ovTop = 0, 0
			m.clampOverlay()
		}
		return nil
	}
	switch msg.String() {
	case "backspace", "delete":
		m.editSel = map[string]bool{}
	}
	return nil
}

// applyLabelDiff turns the picker's working set into the add/remove diff
// against the issue's current labels and sends it off, applying it
// optimistically first.
func (m *Model) applyLabelDiff(rows []string) tea.Cmd {
	idx := m.issueIndex(m.editFor)
	if idx < 0 {
		return nil
	}
	is := &m.issues[idx]
	current := map[string]bool{}
	for _, l := range is.Labels {
		current[l.Name] = true
	}
	var add, remove []string
	for _, name := range rows {
		switch {
		case m.editSel[name] && !current[name]:
			add = append(add, name)
		case !m.editSel[name] && current[name]:
			remove = append(remove, name)
		}
	}
	// Labels the picker cannot show (the fallback row set is narrower than
	// the issue's own labels) are never removed behind the user's back.
	if len(add) == 0 && len(remove) == 0 {
		return nil
	}
	m.snapshot(idx)
	is.Labels = m.labelsAfter(is.Labels, add, remove)
	m.rebuildLabels()
	m.applyFilter()
	return m.send(forge.Mutation{
		Issue: is.Number, Kind: forge.MutateLabels, AddLabels: add, RemoveLabels: remove,
	})
}

// labelsAfter is the optimistic label set: the kept ones in their old order,
// then the added ones with the picker's colors.
func (m *Model) labelsAfter(current []forge.Label, add, remove []string) []forge.Label {
	drop := map[string]bool{}
	for _, name := range remove {
		drop[name] = true
	}
	out := make([]forge.Label, 0, len(current)+len(add))
	for _, l := range current {
		if !drop[l.Name] {
			out = append(out, l)
		}
	}
	colors := map[string]string{}
	for _, l := range m.pickerLabels() {
		colors[l.Name] = l.Color
	}
	for _, name := range add {
		out = append(out, forge.Label{Name: name, Color: colors[name]})
	}
	return out
}

// applyAssignees replaces the issue's assignee set with the picker's.
func (m *Model) applyAssignees(rows []string) tea.Cmd {
	idx := m.issueIndex(m.editFor)
	if idx < 0 {
		return nil
	}
	is := &m.issues[idx]
	next := selected(rows, m.editSel)
	if equalStrings(next, is.Assignees) {
		return nil
	}
	m.snapshot(idx)
	is.Assignees = next
	m.applyFilter()
	return m.send(forge.Mutation{
		Issue: is.Number, Kind: forge.MutateAssignees, Assignees: next, SetAssignees: true,
	})
}

// equalStrings compares two assignee sets order-insensitively.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// toggleIssueState closes an open issue or reopens a closed one ('c').
func (m *Model) toggleIssueState() tea.Cmd { return m.stateMutation("") }

// stateMutation is the shared close/reopen path; comment, when non-empty, is
// posted before the state change.
func (m *Model) stateMutation(comment string) tea.Cmd {
	is := m.mutationTarget()
	if is == nil {
		return nil
	}
	idx := m.issueIndex(is.Number)
	if idx < 0 {
		return nil
	}
	state := "closed"
	if strings.EqualFold(m.issues[idx].State, "CLOSED") {
		state = "open"
	}
	m.snapshot(idx)
	m.issues[idx].State = strings.ToUpper(state)
	m.applyFilter()
	return m.send(forge.Mutation{
		Issue: m.issues[idx].Number, Kind: forge.MutateState, State: state, Comment: comment,
	})
}

// openCommentPrompt opens the one-line prompt of "close (or reopen) with a
// comment" ('C').
func (m *Model) openCommentPrompt() tea.Cmd {
	is := m.mutationTarget()
	if is == nil {
		return nil
	}
	m.ov, m.editFor = ovComment, is.Number
	m.cmInput.Clear()
	return nil
}

// commentPromptKey feeds one key to the open prompt: enter posts the comment
// and flips the state, esc drops both.
func (m *Model) commentPromptKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.closeOverlay()
		m.cmInput.Clear()
	case "enter":
		body := strings.TrimSpace(m.cmInput.Text)
		m.closeOverlay()
		m.cmInput.Clear()
		if body == "" {
			m.mutErr = "empty comment — nothing was changed"
			return nil
		}
		return m.stateMutation(body)
	default:
		m.cmInput.Key(msg)
	}
	return nil
}

// stateVerb names what the close/reopen actions would do to the selected
// issue, so footer and menu read right in both directions.
func (m *Model) stateVerb() string {
	if is := m.Selected(); is != nil && strings.EqualFold(is.State, "CLOSED") {
		return "reopen"
	}
	return "close"
}

// send dispatches one mutation, counting it as in flight.
func (m *Model) send(mut forge.Mutation) tea.Cmd {
	if m.mutate == nil {
		return nil
	}
	m.mutBusy++
	m.mutErr = ""
	return m.mutate(mut)
}

// snapshot remembers one issue's pre-mutation state for the rollback; an
// issue already snapshotted keeps its oldest copy, so a chain of optimistic
// writes still unwinds to forge truth.
func (m *Model) snapshot(idx int) {
	if m.rollback == nil {
		m.rollback = map[int]forge.Issue{}
	}
	number := m.issues[idx].Number
	if _, ok := m.rollback[number]; ok {
		return
	}
	copyIssue := m.issues[idx]
	copyIssue.Labels = append([]forge.Label(nil), m.issues[idx].Labels...)
	copyIssue.Assignees = append([]string(nil), m.issues[idx].Assignees...)
	m.rollback[number] = copyIssue
}

// issueIndex finds an issue by number, -1 when the listing no longer has it.
func (m *Model) issueIndex(number int) int {
	for i := range m.issues {
		if m.issues[i].Number == number {
			return i
		}
	}
	return -1
}

// SetMutationResult applies one finished mutation. A failure rolls the
// optimistic row back and shows the forge's error; a success drops the
// snapshot and refetches the listing (and the open issue's timeline), because
// the pane must show forge truth rather than its own guess.
func (m *Model) SetMutationResult(msg forge.MutationMsg) tea.Cmd {
	if m.mutBusy > 0 {
		m.mutBusy--
	}
	if msg.Err != nil {
		m.undo(msg.Issue)
		m.mutErr = mutationErrorText(msg)
		m.applyFilter()
		return nil
	}
	delete(m.rollback, msg.Issue)
	m.mutErr = ""
	cmds := []tea.Cmd{m.startRefresh()}
	if m.detail && m.tlFor == msg.Issue {
		cmds = append(cmds, m.refetchTimeline())
	}
	return tea.Batch(cmds...)
}

// mutationErrorText words one rejection with what the user tried to change.
func mutationErrorText(msg forge.MutationMsg) string {
	what := "change"
	switch msg.Kind {
	case forge.MutateLabels:
		what = "label change"
	case forge.MutateAssignees:
		what = "assignee change"
	case forge.MutateState:
		what = "state change"
	}
	return what + " failed: " + msg.Err.Error()
}

// undo restores one issue's snapshot after a failed mutation.
func (m *Model) undo(number int) {
	saved, ok := m.rollback[number]
	if !ok {
		return
	}
	delete(m.rollback, number)
	if idx := m.issueIndex(number); idx >= 0 {
		m.issues[idx] = saved
	}
	m.rebuildLabels()
}
