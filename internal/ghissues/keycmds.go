package ghissues

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// keycmds.go holds the host-side entries of the pane's bound chords (#2400).
// Telemetry recorded cmd+c and ctrl+up / ctrl+down pressed in the issues
// window and logged unbound: the pane had a mouse-selection copy on 'y' but
// nothing for the everyday "copy what I am looking at", and the walk chords
// existed only as ctrl+j / ctrl+k. The keymap layer resolves issues.copy /
// issues.selectPrev / issues.selectNext in the issues context and dispatches
// them here, mirroring httppane.CopyKeyCmd.

// CopyKeyCmd is issues.copy (cmd+c): a live mouse selection wins — that is
// what the pane's own 'y' copies — and otherwise the selected item's URL goes
// to the clipboard, falling back to "#42 Title" when the forge listing
// carries no URL.
func (m *Model) CopyKeyCmd() tea.Cmd {
	if cmd := m.copySelection(); cmd != nil {
		return cmd
	}
	text, what := m.selectedRef()
	if text == "" {
		return nil
	}
	msg := CopyMsg{Text: text, What: what}
	return func() tea.Msg { return msg }
}

// selectedRef is the copyable reference of the item under the cursor: its URL
// when the forge reported one, else its number and title.
func (m *Model) selectedRef() (text, what string) {
	if m.tab == TabPRs {
		pr := m.SelectedPR()
		if pr == nil {
			return "", ""
		}
		if pr.URL != "" {
			return pr.URL, "pull request URL"
		}
		return "#" + strconv.Itoa(pr.Number) + " " + pr.Title, "pull request title"
	}
	is := m.Selected()
	if is == nil {
		return "", ""
	}
	if is.URL != "" {
		return is.URL, "issue URL"
	}
	return "#" + strconv.Itoa(is.Number) + " " + is.Title, "issue title"
}

// StepSelection is issues.selectPrev / issues.selectNext (up / down and the
// ctrl+up / ctrl+down aliases, #2400, #2537): it walks the list cursor, or —
// with a detail view open — the shown item, which is what ctrl+j / ctrl+k
// already do there.
func (m *Model) StepSelection(delta int) tea.Cmd {
	if cmd, ok := m.arrowToOverlay(delta, false); ok {
		return cmd
	}
	switch {
	case m.detail && m.tab == TabIssues:
		return m.stepIssue(delta)
	case m.prDetail && m.tab == TabPRs:
		return m.stepPR(delta)
	}
	key := "down"
	if delta < 0 {
		key = "up"
	}
	m.navList(key)
	return nil
}

// SwitchTabCmd is issues.nextTab / issues.prevTab (right / left, #2537): the
// plain horizontal arrows walk the pane's two tabs, exactly like tab /
// shift+tab, from the list and from a detail view alike.
func (m *Model) SwitchTabCmd(delta int) tea.Cmd {
	if cmd, ok := m.arrowToOverlay(delta, true); ok {
		return cmd
	}
	m.switchTab(delta)
	return nil
}

// arrowToOverlay hands a bound arrow back to an open overlay (#2537). The
// keymap layer resolves the arrows before the pane sees them, so without this
// the filter overlay's match input and the pickers would lose their arrows to
// the list walk. horizontal picks left/right over up/down.
func (m *Model) arrowToOverlay(delta int, horizontal bool) (tea.Cmd, bool) {
	if m.ov == ovNone {
		return nil, false
	}
	code := tea.KeyUp
	switch {
	case horizontal && delta < 0:
		code = tea.KeyLeft
	case horizontal:
		code = tea.KeyRight
	case delta > 0:
		code = tea.KeyDown
	}
	return m.overlayKey(tea.KeyPressMsg{Code: code}), true
}
