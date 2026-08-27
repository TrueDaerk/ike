package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/palette"
	"ike/internal/pane"
)

// problems_quickfix.go is the app half of the Problems pane's quick-fix key
// (#2175): "a" (or alt+enter) on a row asks the LSP bridge for the code
// actions of that diagnostic, right where it is listed — no jump to the
// location and no intention popup at a caret in between.
//
// The route is deliberately the same seam alt+enter uses, just entered from
// outside an editor: the pane emits problems.QuickFixMsg, the app runs
// lsp.quickFixProblem, the bridge answers that command with the continuation
// (ilsp.QuickFixPromptMsg) and the app calls it with the marked row's range.
// The reply arrives as an ordinary CodeActionsMsg carrying QuickFix, so the
// offer renders through the very same actionsMode the intention popup fills,
// and applying routes through the bridge's WorkspaceEdit path — one undo unit
// in an open buffer, a direct rewrite for a file no editor holds. The pane
// itself refreshes on the publishDiagnostics that follows the edit, exactly
// as it does for any other change.

// problemsQuickFix runs the bridge command that installs the quick-fix
// continuation. It is deliberately dispatchCommand-based (via RunCommand):
// unlike the workspace-symbol priming (#679) this *is* a user invocation, so
// EventCommandExecuted firing is correct.
func (m Model) problemsQuickFix() tea.Cmd { return m.RunCommand("lsp.quickFixProblem") }

// requestProblemQuickFix calls the bridge continuation with the marked
// Problems row. It is also what invoking lsp.quickFixProblem from the palette
// resolves to, so the command means the same thing wherever it is run from.
func (m *Model) requestProblemQuickFix(apply func(ilsp.QuickFixRequest) tea.Cmd) tea.Cmd {
	if apply == nil {
		return nil
	}
	p := m.problemsPanel()
	if p == nil {
		m.host.Notify(host.Info, "quick fix: open the Problems pane first")
		return nil
	}
	path, d, ok := p.SelectedDiagnostic()
	if !ok {
		m.host.Notify(host.Info, "quick fix: no problem marked")
		return nil
	}
	return apply(ilsp.QuickFixRequest{Path: path, Range: d.Range})
}

// openProblemQuickFixes shows the offer for a Problems row: the server's
// actions alone — no caret exists, so no intention provider could apply — in
// the picker anchored under the marked row. An empty offer says so instead of
// opening an empty box, which is the whole "no fixes" verdict: a lint note, a
// task-matcher finding or a file no server tracks simply has nothing to offer.
// The fixes preview like the caret's intentions do (#2252), so the returned
// command is the palette's preview debounce for the row highlighted on open.
func (m *Model) openProblemQuickFixes(msg ilsp.CodeActionsMsg) tea.Cmd {
	m.actions.Set(msg)
	m.actions.SetPalette(m.pal())
	if m.actions.Len() == 0 {
		m.host.Notify(host.Info, "no quick fixes for this problem")
		return nil
	}
	m.palette.SetSize(m.width, m.height)
	cx := palette.Context{ContextID: m.focusContext(), Root: "."}
	if x, y, w, ok := m.problemsPopupAnchor(m.actions.Len()); ok {
		m.palette.OpenAnchoredWith(cx, actionsPrefix, "", x, y, w)
		return m.palette.SelectionKick()
	}
	m.palette.OpenLocked(cx, actionsPrefix)
	return m.palette.SelectionKick()
}

// problemsPopupAnchor places the quick-fix popup one row below the marked
// Problems row — caretPopupAnchor's math for a list pane, where the row is
// the cursor's offset in the visible window plus the pane's own header line.
// ok is false without a laid-out Problems pane to anchor on, which sends the
// caller to the centered palette.
func (m Model) problemsPopupAnchor(rows int) (x, y, w int, ok bool) {
	p := m.problemsPanel()
	r, found := m.lay.Panes[pane.ProblemsKey]
	if p == nil || !found {
		return 0, 0, 0, false
	}
	x = r.X + paneContentX
	// +1 for the pane's scope header, +1 to sit *below* the marked row.
	y = r.Y + m.contentYOff(pane.ProblemsKey) + p.CursorRow() + 2
	x, y, w = m.fitPopupAnchor(x, y, rows)
	return x, y, w, true
}
