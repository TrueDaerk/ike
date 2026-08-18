package app

// scratch_section.go wires the explorer's Scratches section (#1963, replacing
// the #1932 tool pane). The section itself lives in internal/explorer; here
// the old scratch.panel command re-points at it, so the palette entry and any
// bound chord keep working: instead of opening a second pane the command
// focuses the explorer with the cursor on the section.

// ScratchSectionFocusMsg runs scratch.panel (#1963): focus the explorer's
// Scratches section.
type ScratchSectionFocusMsg struct{}

// focusScratchSection focuses the explorer pane — re-inserting a hidden tree,
// the focusExplorer primitive — and puts the unified cursor on the Scratches
// section's first entry, re-expanding a collapsed section.
func (m *Model) focusScratchSection() {
	m.focusExplorer()
	m.explorer().FocusScratches()
}
