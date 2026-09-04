package app

import (
	"strings"

	"ike/internal/host"
)

// compareWithClipboard runs diff.compareWithClipboard (#1477): open the diff
// view with the clipboard contents on the left (read-only) and the active
// buffer — or the visual selection, when one is active — on the right. An
// empty clipboard notifies instead of opening an empty diff.
func (m *Model) compareWithClipboard() {
	ed := m.activeEditor()
	if ed == nil {
		m.host.Notify(host.Info, "no editor to compare with the clipboard")
		return
	}
	clip := clipboardRead()
	if clip == "" {
		m.host.Notify(host.Info, "clipboard is empty")
		return
	}
	// Whole buffer by default; a visual selection narrows the right side to
	// the selected text (and drops the jump/edit link to the file).
	right := ed.Text()
	rightTitle := "buffer"
	rightPath := ""
	editable := false
	if ed.HasFile() {
		rightTitle = baseName(ed.Path())
		rightPath = ed.Path()
		editable = true
	}
	if sel, ok := ed.SelectionText(); ok {
		right = sel
		rightTitle += " (selection)"
		rightPath = ""
		editable = false
	}
	m.openClipboardDiffPane(rightTitle, rightPath, clip, right, editable)
}

// openClipboardDiffPane shows the clipboard (left) against text (right) in
// the reusable diff pane, following the openLocalHistoryDiffPane shape:
// reuse the single diff slot when one exists, otherwise open a titled viewer
// through openDiffLeaf (#2507).
func (m *Model) openClipboardDiffPane(rightTitle, rightPath, clip, right string, editable bool) {
	const leftTitle = "Clipboard"
	// Side labels (#2494): the clipboard against the buffer it is compared
	// with — the right title already names a narrowed selection.
	rightLabel := rightTitle + " (buffer)"
	if strings.HasSuffix(rightTitle, "(selection)") {
		rightLabel = rightTitle
	}
	if inst, hostKey, tabIdx, ok := m.diffSlot(); ok {
		inst.StopDiffEdit()
		inst.Diff().Retarget(leftTitle, rightTitle, "", rightPath, "", "", editable)
		inst.Diff().SetSideLabels("clipboard", rightLabel)
		inst.Diff().SetContents(clip, right)
		m.focusContentAt(hostKey, tabIdx)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		return
	}
	inst, ok := m.openDiffLeaf(func() string {
		return m.activeWS().Panes.AddDiffTitled(leftTitle, rightTitle, rightPath)
	})
	if !ok {
		return
	}
	if editable {
		inst.Diff().SetEditable(true)
	}
	inst.Diff().SetSideLabels("clipboard", rightLabel)
	inst.Diff().SetContents(clip, right)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}
