package app

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/pane"
	"ike/internal/ui"
)

// overlaypaste.go routes a paste into the focused overlay's text input (#1273).
//
// Before this, handlePaste bailed on overlayCapturesKeyboard() and dropped the
// block: bracketed paste reached no overlay at all, so nothing could be pasted
// into search-everywhere, the find-in-path query, a rename prompt or any other
// floating input. Cmd+V was equally dead — it maps to editor.paste, which no
// overlay handles. The clone dialog, new-project wizard, save-as, save-layout
// and JetBrains-import prompts share the same gap and are routed the same way
// (#1873).
//
// The fix is one router, ordered exactly like the KeyPressMsg guard chain in
// Update, so the surface that owns the keyboard is the surface that receives
// the paste. Overlays with no text input return false and the paste is dropped
// as before — a paste must never leak into the hidden editor underneath.
//
// Pasted blocks are flattened to a single line by ui.PasteText: every target
// here is a one-line field.

// routeOverlayPaste delivers text to the focused overlay's text input. handled
// reports that an overlay consumed it (including "an input was focused but the
// block flattened to nothing" — false there, so the caller drops it rather than
// routing further down).
func (m *Model) routeOverlayPaste(text string) (cmd tea.Cmd, handled bool) {
	switch {
	case m.settings.IsOpen():
		return nil, m.settings.Paste(text)
	case m.ctxMenu.IsOpen(), m.menuEnabled() && m.menu.IsOpen():
		return nil, false // navigation-only overlays
	case m.finder.IsOpen():
		return nil, m.finder.Paste(text)
	case m.todo.IsOpen(), m.undoTree.IsOpen(), m.callhier.IsOpen(), m.typehier.IsOpen():
		return nil, false // list overlays, no text input
	case m.palette.IsOpen():
		return m.palette.Paste(text)
	case m.recoveryOpen(), m.onboardingOpen(), m.conflictOpen(),
		m.revertPromptOpen(), m.depEditPromptOpen(), m.switchPromptOpen(),
		m.closePromptOpen():
		return nil, false // decision prompts, no text input
	case m.renameOpen():
		return nil, m.pasteRenamePrompt(text)
	case m.clonePromptOpen():
		return nil, m.pasteClonePrompt(text)
	case m.regexTesterOpen():
		// The only prompt whose paste keeps its line breaks: the test-text
		// area is exactly where a multi-line log excerpt belongs.
		return m.pasteRegexTester(text)
	case m.playNamePromptOpen():
		// The saved-filter name prompt (#1995) is checked before the
		// playground: it is a modal shell prompt opened from it, and the
		// mode's pane still holds the focus while it is up.
		return nil, m.pastePlayNamePrompt(text)
	case m.playFocused():
		// The playground query line is one line; a pasted program is flattened into
		// it like every other single-field prompt (#1936). An unfocused
		// playground (#1980) never captures — the paste belongs to the
		// focused pane then.
		return m.pastePlayground(text), true
	case m.newProjectPromptOpen():
		return nil, m.pasteNewProjectPrompt(text)
	case m.generateScratchOpen():
		return nil, m.pasteGenerateScratch(text)
	case m.saveAsOpen():
		return nil, m.pasteSaveAsPrompt(text)
	case m.bookmarkPromptOpen():
		return nil, m.pasteBookmarkPrompt(text)
	case m.layoutSavePromptOpen():
		return nil, m.pasteLayoutSavePrompt(text)
	case m.jbImportPromptOpen():
		return nil, m.pasteJBImportPrompt(text)
	case m.openAPIImportPromptOpen():
		return nil, m.pasteOpenAPIImportPrompt(text)
	case m.curlImportPromptOpen():
		return nil, m.pasteCurlImportPrompt(text)
	case m.httpSavePromptOpen():
		return nil, m.pasteHTTPSavePrompt(text)
	case m.lspRenameOpen():
		return nil, m.pasteLSPRenamePrompt(text)
	case m.explorerCapturing():
		inst := m.activeWS().Panes.FocusedInstance()
		if inst == nil || inst.Kind() != pane.KindExplorer {
			return nil, false
		}
		return nil, inst.Explorer().Paste(text)
	}
	return nil, false
}

// pasteRenamePrompt inserts into the file-rename prompt at its cursor.
func (m *Model) pasteRenamePrompt(text string) bool {
	out, ncur, changed := ui.PasteText(m.renameInput, m.renamePos, text)
	if !changed {
		return false
	}
	m.renameInput, m.renamePos = out, ncur
	m.renderRenamePrompt()
	return true
}

// pasteLSPRenamePrompt inserts into the symbol-rename prompt at its cursor.
func (m *Model) pasteLSPRenamePrompt(text string) bool {
	s := m.lspRename
	if s == nil {
		return false
	}
	out, ncur, changed := ui.PasteText(s.input, s.pos, text)
	if !changed {
		return false
	}
	s.input, s.pos = out, ncur
	m.renderLSPRenamePrompt()
	return true
}
