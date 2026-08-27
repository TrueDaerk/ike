package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/ui"
)

// fileops.go implements the app side of the JetBrains-style file refactors
// (#175): file.rename (shift+f6) and file.move (f6). Both act on the
// explorer's selection when the explorer is focused and on the focused
// editor's file otherwise; the actual disk operation and its undo/redo live
// in the explorer's fileops, reached via explorer messages. Open editors
// follow the new path through FileMovedMsg instead of being closed.

// refactorTarget resolves the file a rename/move acts on: the explorer's
// selection when the explorer holds focus, else the focused editor's file.
func (m *Model) refactorTarget() (string, bool) {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return "", false
	}
	if inst.Kind() == pane.KindExplorer {
		path, _, ok := m.explorer().Selected()
		return path, ok
	}
	if inst.Kind() == pane.KindEditor {
		if ed := inst.Editor(); ed != nil && ed.HasFile() {
			return ed.Path(), true
		}
	}
	return "", false
}

// startRenameFile handles RenameFileMsg (file.rename): the explorer keeps its
// own inline prompt; for an editor the shell prompts for the new name.
func (m *Model) startRenameFile() tea.Cmd {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst != nil && inst.Kind() == pane.KindExplorer {
		exp := m.explorer()
		var cmd tea.Cmd
		*exp, cmd = exp.Update(explorer.RenameMsg{})
		return cmd
	}
	path, ok := m.refactorTarget()
	if !ok {
		return nil
	}
	m.renamePath = path
	m.renameInput = filepath.Base(path)
	m.renamePos = len([]rune(m.renameInput))
	m.renderRenamePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
	return nil
}

// renameOpen reports whether the shell currently shows the rename prompt.
func (m Model) renameOpen() bool { return m.renamePath != "" && m.shell.IsOpen() }

// renderRenamePrompt (re)fills the shell with the prompt for the current
// input; called on open and after every accepted key.
func (m *Model) renderRenamePrompt() {
	line := "> " + ui.CursorView(m.renameInput, m.renamePos)
	m.shell.SetContent(ui.ModelContent{
		Heading: "Rename " + displayPath(m.renamePath),
		Body: func() string {
			return line + "\n\nenter rename · esc cancel"
		},
	})
}

// updateRenamePrompt consumes every key while the rename prompt is open.
// Enter renames through the explorer's fileops so the operation lands on the
// shared undo/redo stack; every other key is line editing via ui.EditKey
// (#2002), which is what gives the field word motions, word/line kills and
// the macOS opt/cmd chords.
func (m Model) updateRenamePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.renamePath = ""
		m.renameInput = ""
		m.renamePos = 0
		m.shell.Close()
	}
	switch {
	case msg.Code == tea.KeyEscape:
		closePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		path := m.renamePath
		name := strings.TrimSpace(m.renameInput)
		closePrompt()
		if name == "" || name == filepath.Base(path) {
			return m, nil
		}
		exp := m.explorer()
		var cmd tea.Cmd
		*exp, cmd = exp.Update(explorer.RenamePathMsg{Path: path, Name: name})
		return m, cmd
	}
	if out, pos, handled, _ := ui.EditKey(msg, m.renameInput, m.renamePos); handled {
		m.renameInput, m.renamePos = out, pos
	}
	m.renderRenamePrompt()
	return m, nil
}

// startMoveFile handles MoveFileMsg (file.move): stash the source and open
// the palette locked to the directory picker.
func (m *Model) startMoveFile() {
	// The explorer's multi-select (#2166) moves as a batch: the picker asks
	// for one target directory and every marked entry goes there.
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindExplorer {
		if marked := m.explorer().MarkedPaths(); len(marked) > 0 {
			m.moveMany = marked
			m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: m.explorer().Root()}, '>')
			return
		}
	}
	path, ok := m.refactorTarget()
	if !ok {
		return
	}
	m.movePending = path
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: m.explorer().Root()}, '>')
}

// finishMoveFile handles the picked target directory: the pending source
// moves there through the explorer's fileops (undo/redo included).
func (m *Model) finishMoveFile(dir string) tea.Cmd {
	path, many := m.movePending, m.moveMany
	m.movePending, m.moveMany = "", nil
	if path == "" && len(many) == 0 {
		return nil
	}
	target := filepath.Clean(filepath.Join(m.explorer().Root(), filepath.FromSlash(dir)))
	exp := m.explorer()
	var cmd tea.Cmd
	if len(many) > 0 {
		// A multi-select move (#2166): one batch, one undo step.
		*exp, cmd = exp.Update(explorer.MoveManyMsg{Paths: many, TargetDir: target})
		return cmd
	}
	*exp, cmd = exp.Update(explorer.MoveToMsg{Path: path, TargetDir: target})
	return cmd
}

// followMovedFile re-points every editor showing a renamed/moved path (or a
// file under a renamed/moved directory) at its new location: buffers, undo
// history, cursors — everything survives; only the path changes. Both ends
// are stamped as own writes so the watcher's echo of the rename does not mark
// the followed buffers stale or reload them (which would drop their history).
func (m *Model) followMovedFile(msg explorer.FileMovedMsg) tea.Cmd {
	m.watcher.MarkSaved(msg.Old)
	m.watcher.MarkSaved(msg.New)
	prefix := msg.Old + string(os.PathSeparator)
	var cmds []tea.Cmd
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if !ed.HasFile() {
				continue
			}
			ep := ed.Path()
			var np string
			switch {
			case ep == msg.Old:
				np = msg.New
			case msg.IsDir && strings.HasPrefix(ep, prefix):
				np = msg.New + ep[len(msg.Old):]
			default:
				continue
			}
			m.watcher.MarkSaved(ep)
			m.watcher.MarkSaved(np)
			if cmd := ed.SetPath(np); cmd != nil {
				cmds = append(cmds, cmd)
			}
			m.watcher.Track(np)
		}
		// Restored tabs still waiting to be activated (#2177) follow the
		// move too — their file is the same document, just not read yet, and
		// a stale path would fail its load once the tab is opened.
		for idx := 0; idx < inst.TabCount(); idx++ {
			d, ok := inst.TabDeferredView(idx)
			if !ok {
				continue
			}
			switch {
			case d.Path == msg.Old:
				inst.RetargetDeferredTab(idx, msg.New)
			case msg.IsDir && strings.HasPrefix(d.Path, prefix):
				inst.RetargetDeferredTab(idx, msg.New+d.Path[len(msg.Old):])
			}
		}
	}
	// Project bookmarks (#55) follow the file: re-key the store so a rename
	// or move never leaves them dangling on the old path.
	m.renameBookmarks(msg.Old, msg.New)
	if key := m.activeEditorKey(); key != "" {
		if ed := m.activeWS().Panes.Get(key).Editor(); ed != nil && ed.HasFile() {
			m.explorer().SetActive(ed.Path())
		}
	}
	m.syncExplorerOpen()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	return tea.Batch(cmds...)
}
