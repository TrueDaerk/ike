package app

// nav.go integrates the editor navigation history (Roadmap 0220, #218): the
// root model records a history entry whenever the caret jumps through the
// open funnel (openPath / openPathAt — file switches, go-to-definition,
// references picks, find-in-path results), and nav.back / nav.forward
// traverse the recorded positions via the same funnel.

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/nav"
)

// NavBackMsg asks the root model to navigate to the previous history
// position. Dispatched by nav.back (cmd+left-bracket, menu, palette).
type NavBackMsg struct{}

// NavForwardMsg re-traverses after a NavBackMsg. Dispatched by nav.forward.
type NavForwardMsg struct{}

// currentNavPos captures the active editor's file+caret as a history
// position (zero when no editor holds a file). Editor cursors are 1-based;
// history positions are 0-based like editor.SetCursor.
func (m Model) currentNavPos() nav.Position {
	key := m.activeEditorKey()
	if key == "" {
		return nav.Position{}
	}
	ed := m.panes.Get(key).Editor()
	if ed == nil || !ed.HasFile() {
		return nav.Position{}
	}
	line, col := ed.Cursor()
	return nav.Position{Path: ed.Path(), Line: line - 1, Col: col - 1}
}

// recordNavFrom records cur as a departure point unless history navigation
// itself is driving the open (navSkip).
func (m Model) recordNavFrom(cur nav.Position) {
	if !m.navSkip {
		m.navHist.RecordJump(cur)
	}
}

// navigateHistory runs one back/forward step: it hands the current position
// to the history (keeping the opposite stack consistent), then navigates to
// the target through the standard open flow with recording suppressed.
func (m Model) navigateHistory(step func(nav.Position) (nav.Position, bool), emptyNote string) (tea.Model, tea.Cmd) {
	target, ok := step(m.currentNavPos())
	if !ok {
		m.host.Notify(host.Info, emptyNote)
		return m, nil
	}
	m.navSkip = true
	model, cmd := m.openPathAt(target.Path, target.Line, target.Col)
	if mm, isModel := model.(Model); isModel {
		mm.navSkip = false
		return mm, cmd
	}
	return model, cmd
}
