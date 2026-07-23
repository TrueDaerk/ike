package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/localhistory"
	"ike/internal/project"
	"ike/internal/textenc"
	"ike/internal/ui"
)

// localhistory.go wires Local History (#35, MVP #1023) into the app: every
// successful editor save records the saved file into the per-project snapshot
// store, and file.localHistory raises a floating picker over the focused
// file's snapshots — enter diffs a snapshot against the current buffer in the
// reusable diff pane (#60), r restores it into the buffer through the normal
// edit path (undoable, marks dirty; the file on disk is untouched until the
// next save).

// LocalHistoryMsg runs file.localHistory: show the focused file's snapshots.
type LocalHistoryMsg struct{}

// localHistorySnapshotMsg reports one buffer save (any flow: manual write,
// Save All, autosave — they all funnel through editor.saveAs, whose EventSave
// the emitter forwards). The handler snapshots the just-written file.
type localHistorySnapshotMsg struct{ path string }

// localHistoryDir returns the snapshot store directory, mirroring
// layoutFile(): IKE_CONFIG_DIR when set, else the project's ".ike" directory.
func localHistoryDir() string {
	base := ".ike"
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		base = d
	}
	return filepath.Join(base, "history")
}

// recordLocalHistory snapshots path's on-disk content (the bytes the save
// just wrote). Store errors and unreadable files are swallowed — snapshotting
// must never disrupt the save that triggered it.
func (m *Model) recordLocalHistory(path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	m.lhStore.Record(path, data)
}

// openLocalHistoryPicker shows the focused file's snapshots in the modal
// shell, newest-first.
func (m *Model) openLocalHistoryPicker() {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "no file for local history")
		return
	}
	path := ed.Path()
	entries := m.lhStore.List(path)
	if len(entries) == 0 {
		m.host.Notify(host.Info, "no local history for "+baseName(path)+" yet — snapshots record on save")
		return
	}
	m.lhPath = path
	m.lhEntries = entries
	m.lhSel = 0
	m.lhPicker = true
	m.shell.SetContent(ui.ModelContent{
		Heading: "LOCAL HISTORY — " + baseName(path),
		Body:    m.renderLocalHistoryPicker,
	})
	m.shell.Open()
}

// localHistoryOpen reports whether the shell currently shows the snapshot
// picker — the content check guards against another overlay having taken the
// shell over without the picker's own close path running (the pins pattern).
func (m Model) localHistoryOpen() bool {
	if !m.lhPicker || !m.shell.IsOpen() {
		return false
	}
	c, ok := m.shell.Content().(ui.ModelContent)
	return ok && strings.HasPrefix(c.Heading, "LOCAL HISTORY — ")
}

// renderLocalHistoryPicker draws the snapshot list plus the key hints.
func (m *Model) renderLocalHistoryPicker() string {
	var b strings.Builder
	now := time.Now()
	for i, e := range m.lhEntries {
		marker := "  "
		if i == m.lhSel {
			marker = "▍ "
		}
		fmt.Fprintf(&b, "%s%-8s %s\n", marker, project.RelTime(e.Time, now),
			e.Time.Local().Format("2006-01-02 15:04:05"))
	}
	b.WriteString("\nenter diff against current · r restore into buffer · j/k move · esc close")
	return b.String()
}

// updateLocalHistoryPicker consumes every key while the picker is open:
// navigation, diff, restore. Everything else is swallowed (the picker is
// modal).
func (m Model) updateLocalHistoryPicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closePicker := func() {
		m.lhPicker = false
		m.shell.Close()
	}
	switch msg.String() {
	case "esc", "q":
		closePicker()
		return m, nil
	case "j", "down":
		if m.lhSel < len(m.lhEntries)-1 {
			m.lhSel++
		}
		return m, nil
	case "k", "up":
		if m.lhSel > 0 {
			m.lhSel--
		}
		return m, nil
	case "enter":
		path, entry := m.lhPath, m.lhEntries[m.lhSel]
		closePicker()
		text, ok := m.localHistoryText(entry)
		if !ok {
			return m, nil
		}
		m.openLocalHistoryDiffPane(path, entry, text)
		return m, nil
	case "r":
		path, entry := m.lhPath, m.lhEntries[m.lhSel]
		closePicker()
		text, ok := m.localHistoryText(entry)
		if !ok {
			return m, nil
		}
		m.restoreLocalHistory(path, entry, text)
		return m, nil
	}
	return m, nil
}

// localHistoryText reads a snapshot blob and normalizes it to the buffer's
// native form (UTF-8, LF line endings, no final newline — the save path
// re-adds it) for diffing and restoring.
func (m *Model) localHistoryText(entry localhistory.Entry) (string, bool) {
	data, err := m.lhStore.Read(entry.Hash)
	if err != nil {
		m.host.Notify(host.Warn, "local history snapshot unreadable (pruned?)")
		return "", false
	}
	text, _, err := textenc.Decode(data, textenc.UTF8)
	if err != nil {
		m.host.Notify(host.Warn, "local history snapshot undecodable: "+err.Error())
		return "", false
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.TrimSuffix(text, "\n"), true
}

// openLocalHistoryDiffPane shows snapshot (left) against the live buffer
// (right) in the reusable diff pane, following the openDiffHeadPane shape:
// reuse the single diff slot when one exists, otherwise split a titled pane
// beside the editor.
func (m *Model) openLocalHistoryDiffPane(path string, entry localhistory.Entry, snapshot string) {
	leftTitle := baseName(path) + " @ " + project.RelTime(entry.Time, time.Now())
	right := readFileOrEmpty(path)
	if ed := m.editorForPath(path); ed != nil {
		right = ed.Text()
	}
	if key, ok := m.diffSlot(); ok {
		inst := m.activeWS().Panes.Get(key)
		inst.StopDiffEdit()
		inst.Diff().Retarget(leftTitle, baseName(path), "", path, "", "", true)
		inst.Diff().SetContents(snapshot, right)
		m.setFocus(key)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		return
	}
	key := m.activeWS().Panes.AddDiffTitled(leftTitle, baseName(path), path)
	if !m.placeDiffLeaf(key) {
		return
	}
	m.activeWS().Panes.Get(key).Diff().SetContents(snapshot, right)
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// restoreLocalHistory replaces the buffer of path with the snapshot text
// through the normal edit path — one history change, so a single undo brings
// the pre-restore content back and the buffer marks dirty. The file on disk
// is untouched until the user saves.
func (m *Model) restoreLocalHistory(path string, entry localhistory.Entry, snapshot string) {
	ed := m.editorForPath(path)
	if ed == nil {
		m.host.Notify(host.Warn, "no open buffer for "+baseName(path))
		return
	}
	if ed.Text() == snapshot {
		m.host.Notify(host.Info, "buffer already matches this snapshot")
		return
	}
	lines := strings.Split(ed.Text(), "\n")
	last := lines[len(lines)-1]
	ed.ApplyTextEdits([]editor.TextEdit{{
		StartLine: 0, StartCol: 0,
		EndLine: len(lines) - 1, EndCol: len([]rune(last)),
		Text: snapshot,
	}})
	m.host.Notify(host.Info, fmt.Sprintf("restored %s from %s — undo reverts, save writes it",
		baseName(path), project.RelTime(entry.Time, time.Now())))
}
