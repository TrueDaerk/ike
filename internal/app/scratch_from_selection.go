package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
)

// scratch_from_selection.go is the way *into* the scratch store from work in
// progress (#2339): "I want to take this block apart somewhere else".
//
// Before, that cost four steps — copy the selection, create a scratch, pick a
// language, paste — even though both the language and the content were already
// decided by the selection itself. This command decides them: the content is
// the selection, and the extension is inherited from the file the selection
// came from, so the language picker never opens.
//
// Inheriting the *extension* rather than a picked language is deliberate. The
// store has no whitelist — scratch.Create takes any extension — so a selection
// out of a file whose extension has no picker row (a `.tf`, a `.bazel`, an
// in-house suffix) still produces a scratch of that very type, which is the
// only answer that keeps the scratch classified like its source.

// newScratchFromSelection creates a scratch seeded with the active selection
// and opens it. Without a selection it says so instead of creating an empty
// scratch: a command that silently produces an empty file is indistinguishable
// from one that lost the selection.
func (m Model) newScratchFromSelection() (tea.Model, tea.Cmd) {
	text := m.activeSelectionText()
	if strings.TrimSpace(text) == "" {
		m.host.Notify(host.Info, "scratch: select some text first")
		return m, nil
	}
	return m.newScratch(m.selectionScratchExt(), text)
}

// selectionScratchExt is the extension the selection's source file lends to
// the new scratch, "txt" when there is none to inherit. It walks the same
// panes activeSelectionText walks, in the same order, so the extension always
// belongs to the buffer the text was taken from — a selection made in a
// terminal or a merge view has no source file and lands on plain text.
func (m Model) selectionScratchExt() string {
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil {
		if ext := instSelectionExt(inst); ext != "" {
			return ext
		}
	}
	if ed := m.activeEditor(); ed != nil {
		if ext := editorScratchExt(ed.Path(), ed.LangPath()); ext != "" {
			return ext
		}
	}
	return "txt"
}

// instSelectionExt is instSelectionText's extension twin for one pane
// instance: the extension of the editor that holds the selection, "" for a
// pane whose selection has no file behind it.
func instSelectionExt(inst *pane.Instance) string {
	if c := inst.ActiveContent(); c != nil {
		if ext := instSelectionExt(c); ext != "" {
			return ext
		}
	}
	switch inst.Kind() {
	case pane.KindEditor:
		if ed := inst.Editor(); ed != nil {
			if _, ok := ed.SelectionText(); ok {
				return editorScratchExt(ed.Path(), ed.LangPath())
			}
		}
	case pane.KindDiff:
		if ed := inst.DiffEditor(); ed != nil {
			if _, ok := ed.SelectionText(); ok {
				return editorScratchExt(ed.Path(), ed.LangPath())
			}
		}
	}
	return ""
}

// editorScratchExt picks the extension a buffer lends to a scratch: its file's
// own extension, or — for a file-less buffer that was given a type (#2033) —
// the synthetic language name's. "" when neither carries one, e.g. an untyped
// untitled buffer or a file recognized by base name only (Dockerfile).
func editorScratchExt(path, langPath string) string {
	for _, p := range []string{path, langPath} {
		if ext := strings.TrimPrefix(filepath.Ext(p), "."); ext != "" {
			return ext
		}
	}
	return ""
}
