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
// close would drop. quit marks an app-quit request (#287) — s/d then act on
// every dirty editor instead of one pane.
type pendingClose struct {
	key   string
	tab   int
	dirty []string // display names for the prompt body
	quit  bool
}

// guardedCloseFocused closes the focused pane's active tab (the pane on its
// last tab) unless that would drop unsaved changes, in which case the guard
// prompt opens and the close waits for the user's answer.
func (m *Model) guardedCloseFocused() {
	inst := m.activeWS().Panes.FocusedInstance()
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

// guardedQuit quits the app unless that would drop live state (#287, #821):
// dirty buffers or running debug/run/tool activity in ANY in-memory
// workspace — the active one plus every parked background workspace — open
// the guard prompt: save everything and quit, discard and quit, or cancel.
// Idle shells never gate the quit (every session has one open).
func (m Model) guardedQuit() (tea.Model, tea.Cmd) {
	dirty, running := m.quitActivity()
	if len(dirty) > 0 || len(running) > 0 {
		m.openQuitPrompt(dirty, running)
		return m, nil
	}
	return m.quit()
}

// dirtyEverywhere lists every dirty document across all editor panes, deduped
// by path (shared documents count once — quitting loses them regardless).
func (m *Model) dirtyEverywhere() []string {
	var dirty []string
	seen := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			ed := inst.TabEditor(i)
			if ed == nil || !ed.Dirty() {
				continue
			}
			name := "untitled"
			if path := ed.Path(); path != "" {
				if seen[path] {
					continue
				}
				seen[path] = true
				name = filepath.Base(path)
			}
			dirty = append(dirty, name)
		}
	}
	return dirty
}

// openQuitPrompt shows the guard for a pending app quit, aggregating dirty
// buffers and running activity across every in-memory workspace (#821).
func (m *Model) openQuitPrompt(dirty, running []string) {
	m.closePending = &pendingClose{quit: true, dirty: dirty}
	var parts []string
	if len(running) > 0 {
		parts = append(parts, "still running:\n  "+strings.Join(running, "\n  "))
	}
	if len(dirty) > 0 {
		parts = append(parts, "unsaved changes: "+strings.Join(dirty, ", "))
	}
	body := strings.Join(parts, "\n") + "\n\n"
	if len(dirty) > 0 {
		body += "  [s]   save all, then quit\n"
	}
	body += "  [d]   quit — stop processes, discard unsaved changes\n" +
		"  [esc] cancel — keep ike running"
	heading := "Unsaved changes"
	if len(running) > 0 {
		heading = "Quit ike?"
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: heading,
		Body:    func() string { return body },
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
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
	if pending.quit {
		return m.updateQuitPrompt(msg)
	}
	switch msg.String() {
	case "s":
		m.closePending = nil
		m.shell.Close()
		inst := m.activeWS().Panes.Get(pending.key)
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
		m.resumePendingClose(pending)
		return m, tea.Batch(cmds...)
	case "d":
		m.closePending = nil
		m.shell.Close()
		m.resumePendingClose(pending)
		return m, nil
	case "esc":
		m.closePending = nil
		m.shell.Close()
		return m, nil
	}
	return m, nil
}

// resumePendingClose performs the close the guard was holding: a whole-pane
// pending close (tab -1 while the pane still holds several tabs — the
// pane.close path, #1128) closes the leaf outright; a tab pending closes the
// focused pane's active tab, as guardedCloseFocused queued it.
func (m *Model) resumePendingClose(p *pendingClose) {
	if p.tab < 0 {
		if inst := m.activeWS().Panes.Get(p.key); inst != nil && inst.TabCount() > 1 {
			m.closePane(p.key)
			return
		}
	}
	m.closeFocused()
}

// updateQuitPrompt is the quit flavor of the guard (#287): s writes every
// dirty buffer (staying open if any write fails), d quits discarding, esc
// cancels.
func (m Model) updateQuitPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s":
		if len(m.closePending.dirty) == 0 {
			return m, nil // running-only prompt: no save option offered
		}
		m.closePending = nil
		m.shell.Close()
		cmds := m.saveAllDirty()
		// Background workspaces save too (#821): the write path does not
		// depend on focus or rendering.
		for _, root := range m.ws.Background() {
			cmds = append(cmds, saveWorkspaceDirty(m.ws.Peek(root))...)
		}
		if dirty, _ := m.quitActivity(); len(dirty) > 0 {
			// A write failed (read-only file, full disk): stay running; the
			// batched editor cmds surface the write error.
			m.host.Notify(host.Error, "not quit: save failed")
			return m, tea.Batch(cmds...)
		}
		return m.quit()
	case "d":
		m.closePending = nil
		m.shell.Close()
		return m.quit()
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
