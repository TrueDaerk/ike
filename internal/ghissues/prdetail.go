package ghissues

// prdetail.go is the PR view's detail and write side (#2089): a full-area
// pull-request detail (markdown description, per-check CI status, linked
// issue) on top of #2090's PR list, and the two irreversible actions — merge
// with a comment, close with a comment — behind a capability gate (push, from
// #2083's probe) and a two-stage dialog: an optional comment first, then an
// explicit confirm naming PR, branches and merge method. The pane stays
// subprocess-free: the app injects a per-PR fetch factory
// (forge.PRDetailFactory) and a write factory (forge.PRActionFactory); a
// successful action refetches the whole listing — a merged PR whose
// "Closes #N" issue closed alongside must show forge truth on both tabs. A
// rejection surfaces the forge's own reason (merge conflict, branch
// protection) in the filter row, like a failed issue mutation. After a merge
// the pane offers — never runs — the change-workflow branch cleanup, emitting
// CleanupRequestMsg only on the user's confirmation.

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/ui"
)

// CleanupRequestMsg asks the app to run the post-merge branch cleanup
// (forge.CleanupBranchCmd) for the merged PR's head branch — emitted only
// after the user accepted the offer.
type CleanupRequestMsg struct {
	Branch string
}

// SetPRDetailFetch injects the per-PR fetch factory (forge.PRDetailFactory at
// the workspace root).
func (m *Model) SetPRDetailFetch(fn func(pr int) tea.Cmd) { m.prFetch = fn }

// SetPRAction injects the PR write factory (forge.PRActionFactory at the
// workspace root). Without it the merge/close actions are not offered at all.
func (m *Model) SetPRAction(fn func(forge.PRAction) tea.Cmd) { m.prAction = fn }

// PRDetailOpen reports whether the PR detail view is showing (tests).
func (m *Model) PRDetailOpen() bool { return m.prDetail }

// PRActionDialogOpen reports whether the merge/close dialog owns the keyboard
// (tests).
func (m *Model) PRActionDialogOpen() bool { return m.ov == ovPRAct }

// CleanupOfferOpen reports whether the post-merge cleanup offer is showing
// (tests).
func (m *Model) CleanupOfferOpen() bool { return m.ov == ovCleanup }

// openPRDetail flips into the detail view for the selected pull request and
// returns its fetch.
func (m *Model) openPRDetail() tea.Cmd {
	pr := m.SelectedPR()
	if pr == nil {
		return nil
	}
	m.prDetail = true
	m.prdTop = 0
	return m.fetchPRDetail(pr.Number)
}

// fetchPRDetail starts (or restarts) the full fetch for one PR.
func (m *Model) fetchPRDetail(number int) tea.Cmd {
	m.prdFor = number
	m.prd = nil
	m.prdErr = ""
	m.prdRev++
	if m.prFetch == nil {
		return nil
	}
	m.prdLoading = true
	return m.prFetch(number)
}

// SetPRDetailResult applies one finished PR fetch; an answer for a PR the
// pane no longer waits on is dropped.
func (m *Model) SetPRDetailResult(msg forge.PRDetailMsg) {
	if msg.Number != m.prdFor {
		return
	}
	m.prdLoading = false
	m.prdRev++
	if msg.Err != nil {
		m.prdErr = msg.Err.Error()
		return
	}
	m.prdErr = ""
	d := msg.Detail
	m.prd = &d
}

// prDetailKey handles the full-area PR detail view: scroll, back to the list
// with the cursor untouched, PR walking, and the actions that stay meaningful
// with a PR on screen.
func (m *Model) prDetailKey(msg tea.KeyPressMsg) tea.Cmd {
	page := m.bodyHeight()
	switch msg.String() {
	case "esc", "q", "backspace":
		m.prDetail = false
	case "ctrl+j":
		return m.stepPR(1)
	case "ctrl+k":
		return m.stepPR(-1)
	case "j", "down":
		m.prdTop++
	case "k", "up":
		m.prdTop--
	case "pgdown", "ctrl+d", "space":
		m.prdTop += page
	case "pgup", "ctrl+u":
		m.prdTop -= page
	case "g", "home":
		m.prdTop = 0
	case "G", "end":
		m.prdTop = len(m.prdLines) - page
	case "m", "?":
		m.openActionMenu()
	case "tab", "ctrl+pgdown":
		m.switchTab(1)
	case "shift+tab", "ctrl+pgup":
		m.switchTab(-1)
	case "r":
		return tea.Batch(m.startRefresh(), m.fetchPRDetail(m.prdFor))
	case "o":
		return m.openInBrowser()
	case "y":
		// The PR detail selects and copies exactly like the issue detail
		// (#2374) — same lines, same gesture, same chord.
		return m.copySelection()
	default:
		if ui.CopyChord(msg.String()) {
			return m.copySelection()
		}
		return m.prActionKey(msg.String())
	}
	m.clampPRDetail()
	return nil
}

// stepPR walks to the next/previous pull request from inside the detail view,
// moving the list cursor with it, and returns the new PR's fetch.
func (m *Model) stepPR(delta int) tea.Cmd {
	if len(m.prRows) == 0 {
		return nil
	}
	m.prCursor = ui.StepIndex(m.prCursor, delta, len(m.prRows))
	m.clampScroll()
	m.prdTop = 0
	pr := m.SelectedPR()
	if pr == nil || pr.Number == m.prdFor {
		return nil
	}
	return m.fetchPRDetail(pr.Number)
}

// clampPRDetail bounds the PR detail scroll.
func (m *Model) clampPRDetail() {
	max := len(m.prdLines) - m.bodyHeight()
	if max < 0 {
		max = 0
	}
	if m.prdTop > max {
		m.prdTop = max
	}
	if m.prdTop < 0 {
		m.prdTop = 0
	}
}

// prActionKey routes the PR write keys, which the PR list and the PR detail
// share: 'M' merges with a comment, 'c' closes with a comment ('C' is an
// alias — on the issue views the shifted key is the close-with-comment
// variant, and the PR close dialog always carries its comment stage, #2114).
// Each checks the capability gate itself, so a key without permission
// explains rather than doing nothing.
func (m *Model) prActionKey(key string) tea.Cmd {
	if m.prAction == nil || m.tab != TabPRs {
		return nil
	}
	switch key {
	case "M":
		return m.openPRActionDialog(forge.PRMerge)
	case "c", "C":
		return m.openPRActionDialog(forge.PRClose)
	}
	return nil
}

// canPRAct reports whether the user may merge and close pull requests.
func (m *Model) canPRAct() bool { return m.prAction != nil && m.capsOK && m.caps.Push }

// prActReason explains a closed PR-action gate in one clause, "" when open.
func (m *Model) prActReason() string {
	switch {
	case m.prAction == nil:
		return "no forge backend"
	case !m.capsOK:
		return "checking permissions…"
	case !m.caps.Push:
		return "needs push permission"
	}
	return ""
}

// prActionTarget is the pull request an action applies to, nil (with the
// reason recorded) when there is none, it is not open, or the gate is closed.
func (m *Model) prActionTarget() *forge.PR {
	if reason := m.prActReason(); reason != "" {
		m.mutErr = "cannot change this pull request: " + reason
		return nil
	}
	pr := m.SelectedPR()
	if pr == nil {
		m.mutErr = "no pull request selected"
		return nil
	}
	if !strings.EqualFold(pr.State, "OPEN") && pr.State != "" {
		m.mutErr = "PR #" + strconv.Itoa(pr.Number) + " is already " + strings.ToLower(pr.State)
		return nil
	}
	m.mutErr = ""
	return pr
}

// openPRActionDialog opens the merge/close dialog on its comment stage. A
// merge opened from the list has no detail yet, so the fetch runs alongside —
// the confirm then names the repository's real merge method and the base
// branch instead of the fallbacks.
func (m *Model) openPRActionDialog(kind string) tea.Cmd {
	pr := m.prActionTarget()
	if pr == nil {
		return nil
	}
	m.ov = ovPRAct
	m.prActKind, m.prActStage, m.prActFor = kind, 0, pr.Number
	m.prActHead, m.prActBase = pr.HeadRef, m.prBaseRef(pr.Number)
	m.cmInput, m.cmCur = "", 0
	if kind == forge.PRMerge && (m.prd == nil || m.prd.Number != pr.Number) {
		return m.fetchPRDetail(pr.Number)
	}
	return nil
}

// prBaseRef is the base branch the confirm dialog names, from the fetched
// detail when it is the same PR; "" otherwise (the base only travels on the
// detail).
func (m *Model) prBaseRef(number int) string {
	if m.prd != nil && m.prd.Number == number {
		return m.prd.BaseRef
	}
	return ""
}

// prMergeMethod is the merge method the confirm dialog names and the action
// sends: the fetched detail's when it is the same PR, "merge" otherwise.
func (m *Model) prMergeMethod(number int) string {
	if m.prd != nil && m.prd.Number == number && m.prd.MergeMethod != "" {
		return m.prd.MergeMethod
	}
	return "merge"
}

// prActionDialogKey feeds one key to the open merge/close dialog. Stage 0 is
// the optional comment (enter continues, esc cancels); stage 1 the explicit
// confirm (enter or y runs the action — it is irreversible, so nothing else
// does).
func (m *Model) prActionDialogKey(msg tea.KeyPressMsg) tea.Cmd {
	key := msg.String()
	if key == "esc" {
		m.closeOverlay()
		m.cmInput, m.cmCur = "", 0
		return nil
	}
	if m.prActStage == 0 {
		if key == "enter" {
			m.prActStage = 1
			return nil
		}
		if out, ncur, handled, _ := ui.EditKey(msg, m.cmInput, m.cmCur); handled {
			m.cmInput, m.cmCur = out, ncur
		}
		return nil
	}
	switch key {
	case "enter", "y":
		return m.runPRAction()
	case "backspace":
		m.prActStage = 0 // back to the comment
	}
	return nil
}

// runPRAction dispatches the confirmed action and closes the dialog. The
// merged branch is remembered so a successful merge can offer the cleanup.
func (m *Model) runPRAction() tea.Cmd {
	act := forge.PRAction{
		PR:      m.prActFor,
		Kind:    m.prActKind,
		Comment: strings.TrimSpace(m.cmInput),
	}
	if act.Kind == forge.PRMerge {
		act.Method = m.prMergeMethod(m.prActFor)
	}
	m.closeOverlay()
	m.cmInput, m.cmCur = "", 0
	if m.prAction == nil {
		return nil
	}
	m.mutBusy++
	m.mutErr = ""
	return m.prAction(act)
}

// SetPRActionResult applies one finished merge/close. A rejection surfaces
// the forge's own reason in the filter row; a success refetches the listing
// (issues too — the merged PR may have closed its "Closes #N" issue) and the
// open detail, and a merge additionally offers the branch cleanup.
func (m *Model) SetPRActionResult(msg forge.PRActionMsg) tea.Cmd {
	if m.mutBusy > 0 {
		m.mutBusy--
	}
	if msg.Err != nil {
		m.mutErr = msg.Kind + " of PR #" + strconv.Itoa(msg.PR) + " failed: " + msg.Err.Error()
		return nil
	}
	m.mutErr = ""
	cmds := []tea.Cmd{m.startRefresh()}
	if m.prdFor == msg.PR {
		cmds = append(cmds, m.fetchPRDetail(msg.PR))
	}
	if msg.Kind == forge.PRMerge && msg.PR == m.prActFor && m.prActHead != "" {
		m.cleanupBranch = m.prActHead
		m.ov, m.ovCursor, m.ovTop = ovCleanup, 0, 0
	}
	return tea.Batch(cmds...)
}

// cleanupOfferKey feeds one key to the post-merge cleanup offer: enter (or y)
// asks the app to run it, esc keeps the branch.
func (m *Model) cleanupOfferKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "enter", "y":
		branch := m.cleanupBranch
		m.cleanupBranch = ""
		m.closeOverlay()
		if branch == "" {
			return nil
		}
		return func() tea.Msg { return CleanupRequestMsg{Branch: branch} }
	case "esc", "q", "n":
		m.cleanupBranch = ""
		m.closeOverlay()
	}
	return nil
}
