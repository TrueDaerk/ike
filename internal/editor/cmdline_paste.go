package editor

import "ike/internal/ui"

// cmdline_paste.go routes a bracketed paste into whichever editor-internal
// single-line input owns the keyboard (#1380). The ":" / "/" / "?" command
// line and the find/replace panel are editor state, not app overlays, so the
// app-level paste router (#1273) cannot see them — without this, cmd+v while
// the search bar was open dumped the block into the buffer at the cursor.
//
// Blocks are flattened by ui.PasteText exactly like every other one-line
// field: a single line pastes verbatim, a multi-line block is trimmed and
// joined with spaces, so a line break can never leak into the input (or,
// worse, into the buffer).

// pasteIntoPrompt delivers a paste to the active editor-internal input, if
// any; it reports whether the paste was consumed. The substitute-confirm
// prompt has no text input, but it still swallows the paste — like an app
// overlay without an input, it must never let the block fall through into
// the document underneath.
func (m *Model) pasteIntoPrompt(text string) bool {
	switch {
	case m.subConfirm != nil:
		return true // y/n/a/q/l prompt: no input, swallow
	case m.replPanel != nil:
		m.pasteReplacePanel(text)
		return true
	case m.mode == Command:
		m.pasteCmdline(text)
		return true
	}
	return false
}

// pasteCmdline inserts at the command-line cursor, then re-runs the same
// hooks a typed rune triggers: incremental search preview on the "/" line,
// path-suggest refresh on the ":" line, and the history-recall reset (#1171).
func (m *Model) pasteCmdline(text string) {
	out, ncur, changed := ui.PasteText(m.cmdline, m.cmdCur, text)
	if !changed {
		return
	}
	m.bumpRender()
	m.cmdline, m.cmdCur = out, ncur
	m.cmdHistIdx = -1
	if m.searching {
		m.searchPreview()
	} else {
		m.refreshCmdlineSuggest()
	}
}

// pasteReplacePanel appends to the panel's active field (the fields keep no
// cursor — typing appends too). Pasting into a preselected Find prefill
// replaces it wholesale, matching the replace-on-type behavior (#292).
func (m *Model) pasteReplacePanel(text string) {
	p := m.replPanel
	f := m.panelField()
	if p.preselect && p.field == 0 {
		*f = ""
	}
	p.preselect = false
	out, _, changed := ui.PasteText(*f, len([]rune(*f)), text)
	if !changed {
		return
	}
	m.bumpRender()
	*f = out
	if p.field == 0 {
		m.previewPanelFind()
	}
}
