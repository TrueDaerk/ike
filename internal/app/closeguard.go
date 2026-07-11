package app

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/ui"
)

// closeguard.go is the unsaved-changes guard on the close path (#259):
// cmd+w / ctrl+w / :q on a dirty buffer open a floating-shell prompt —
// save and close, discard, or cancel — instead of silently dropping the
// edits. Shared documents (#142) still visible in another pane close
// without a prompt (nothing is lost), and :q! forces the close, vim-style.

// pendingClose remembers what asked to close while the guard is open: the
// pane key, the tab index (-1 = the whole pane) and the dirty documents the
// close would drop.
type pendingClose struct {
	key   string
	tab   int
	dirty []string // display names for the prompt body
}

// guardedCloseFocused closes the focused pane's active tab (the pane on its
// last tab) unless that would drop unsaved changes, in which case the guard
// prompt opens and the close waits for the user's answer.
func (m *Model) guardedCloseFocused() {
	inst := m.panes.FocusedInstance()
	if inst != nil && inst.Kind() == pane.KindEditor {
		idx := -1
		if inst.TabCount() > 1 {
			idx = inst.ActiveTab()
		}
		if dirty := m.dirtyOnClose(inst, idx); len(dirty) > 0 {
			m.openClosePrompt(inst.Key(), idx, dirty)
			return
		}
	}
	m.closeFocused()
}

// dirtyOnClose lists the documents that closing tab idx (or the whole pane,
// idx -1) of inst would lose: dirty buffers not shown by any other pane.
func (m *Model) dirtyOnClose(inst *pane.Instance, idx int) []string {
	tabs := []int{idx}
	if idx < 0 {
		tabs = tabs[:0]
		for i := 0; i < inst.TabCount(); i++ {
			tabs = append(tabs, i)
		}
	}
	var dirty []string
	for _, i := range tabs {
		ed := inst.TabEditor(i)
		if ed == nil || !ed.Dirty() {
			continue
		}
		if path := ed.Path(); path != "" {
			// A document shared with another pane (#142) survives this close.
			if len(m.editorKeysForPath(path)) > 1 {
				continue
			}
			dirty = append(dirty, filepath.Base(path))
			continue
		}
		dirty = append(dirty, "untitled")
	}
	return dirty
}

// openClosePrompt shows the guard for the pending close.
func (m *Model) openClosePrompt(key string, tab int, dirty []string) {
	m.closePending = &pendingClose{key: key, tab: tab, dirty: dirty}
	body := strings.Join(dirty, ", ") + " has unsaved changes.\n\n" +
		"  [s]   save, then close\n" +
		"  [d]   discard changes and close\n" +
		"  [esc] cancel — keep the file open"
	m.shell.SetContent(ui.ModelContent{
		Heading: "Unsaved changes",
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// closePromptOpen reports whether the guard currently owns the keyboard.
func (m Model) closePromptOpen() bool { return m.closePending != nil && m.shell.IsOpen() }

// updateClosePrompt consumes every key while the guard is open: s saves the
// dirty tabs then closes, d discards and closes, esc cancels. A failed save
// (read-only file) keeps the tab open with an error instead of closing.
func (m Model) updateClosePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	pending := m.closePending
	switch msg.String() {
	case "s":
		m.closePending = nil
		m.shell.Close()
		inst := m.panes.Get(pending.key)
		if inst == nil {
			return m, nil
		}
		var cmds []tea.Cmd
		for _, i := range pendingTabs(inst, pending.tab) {
			if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
				cmds = append(cmds, inst.UpdateTab(i, editor.ActionMsg{Action: "write"}))
			}
		}
		if len(m.dirtyOnClose(inst, pending.tab)) > 0 {
			// The write failed (read-only file, full disk): keep the tab open.
			m.host.Notify(host.Error, "not closed: save failed")
			return m, tea.Batch(cmds...)
		}
		m.closeFocused()
		return m, tea.Batch(cmds...)
	case "d":
		m.closePending = nil
		m.shell.Close()
		m.closeFocused()
		return m, nil
	case "esc":
		m.closePending = nil
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// pendingTabs expands a pendingClose tab index into the concrete tab list.
func pendingTabs(inst *pane.Instance, idx int) []int {
	if idx >= 0 {
		return []int{idx}
	}
	tabs := make([]int, inst.TabCount())
	for i := range tabs {
		tabs[i] = i
	}
	return tabs
}
