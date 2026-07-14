package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
	inst := m.panes.FocusedInstance()
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
	inst := m.panes.FocusedInstance()
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

// renamePromptCursor reverse-videos the cell the input cursor sits on, the
// same rendering the explorer's inline prompt uses.
var renamePromptCursor = lipgloss.NewStyle().Reverse(true)

// renderRenamePrompt (re)fills the shell with the prompt for the current
// input; called on open and after every accepted key.
func (m *Model) renderRenamePrompt() {
	r := []rune(m.renameInput)
	pos := m.renamePos
	before, after := string(r[:pos]), ""
	cur := " "
	if pos < len(r) {
		cur = string(r[pos])
		after = string(r[pos+1:])
	}
	line := "> " + before + renamePromptCursor.Render(cur) + after
	m.shell.SetContent(ui.ModelContent{
		Heading: "Rename " + displayPath(m.renamePath),
		Body: func() string {
			return line + "\n\nenter rename · esc cancel"
		},
	})
}

// updateRenamePrompt consumes every key while the rename prompt is open,
// mirroring the explorer prompt's line editing (arrows, home/end, backspace,
// delete). Enter renames through the explorer's fileops so the operation
// lands on the shared undo/redo stack.
func (m Model) updateRenamePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePrompt := func() {
		m.renamePath = ""
		m.renameInput = ""
		m.renamePos = 0
		m.shell.Close()
	}
	r := []rune(m.renameInput)
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
	case msg.Code == tea.KeyLeft:
		if m.renamePos > 0 {
			m.renamePos--
		}
	case msg.Code == tea.KeyRight:
		if m.renamePos < len(r) {
			m.renamePos++
		}
	case msg.Code == tea.KeyHome:
		m.renamePos = 0
	case msg.Code == tea.KeyEnd:
		m.renamePos = len(r)
	case msg.Code == tea.KeyBackspace:
		if m.renamePos > 0 {
			m.renameInput = string(append(r[:m.renamePos-1:m.renamePos-1], r[m.renamePos:]...))
			m.renamePos--
		}
	case msg.Code == tea.KeyDelete:
		if m.renamePos < len(r) {
			m.renameInput = string(append(r[:m.renamePos:m.renamePos], r[m.renamePos+1:]...))
		}
	case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		ins := []rune(msg.Text)
		m.renameInput = string(append(append(append([]rune{}, r[:m.renamePos]...), ins...), r[m.renamePos:]...))
		m.renamePos += len(ins)
	}
	m.renderRenamePrompt()
	return m, nil
}

// startMoveFile handles MoveFileMsg (file.move): stash the source and open
// the palette locked to the directory picker.
func (m *Model) startMoveFile() {
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
	path := m.movePending
	m.movePending = ""
	if path == "" {
		return nil
	}
	target := filepath.Clean(filepath.Join(m.explorer().Root(), filepath.FromSlash(dir)))
	exp := m.explorer()
	var cmd tea.Cmd
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
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
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
	}
	if key := m.activeEditorKey(); key != "" {
		if ed := m.panes.Get(key).Editor(); ed != nil && ed.HasFile() {
			m.explorer().SetActive(ed.Path())
		}
	}
	m.syncExplorerOpen()
	saveLayout(m.tree, m.panes)
	return tea.Batch(cmds...)
}
