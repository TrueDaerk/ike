package app

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
	"ike/internal/editor"
	"ike/internal/host"
	"ike/internal/pane"
)

// backup.go is the crash-recovery write side (Roadmap 0210, #167). The change
// seam (editor.SyncMsg fires on every buffer change and save) marks dirty
// buffers on the debouncer; one armed tick snapshots the buffers that went
// quiet, off the Update loop; save, close-with-discard and clean quit remove
// their snapshots so leftovers at startup always mean a crash. The read side
// (restore prompt, startup GC) lives in recovery.go.

// backupTickMsg wakes the model to snapshot the buffers whose debounce expired.
type backupTickMsg struct{}

// untitledPrefix namespaces snapshot keys of pathless buffers by pane key.
const untitledPrefix = "untitled:"

// backupKey is a buffer's stable snapshot identity: the file path for titled
// buffers, a pane-scoped token for untitled ones.
func backupKey(ed *editor.Model, paneKey string) string {
	if ed.HasFile() {
		return ed.Path()
	}
	return untitledPrefix + paneKey
}

// backupEnabled reads backup.enable live from the config, so a settings toggle
// applies without a restart.
func (m *Model) backupEnabled() bool {
	v, ok := m.host.Config().Get("backup.enable")
	return !ok || v != "false"
}

// backupInterval reads the debounce interval from cfg (validation clamps it).
func backupInterval(cfg host.Config) time.Duration {
	d := 2000
	if cfg != nil {
		if v, ok := cfg.Get("backup.debounce_ms"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				d = n
			}
		}
	}
	return time.Duration(d) * time.Millisecond
}

// backupMaxAge reads the snapshot age bound from cfg (validation clamps it).
func backupMaxAge(cfg host.Config) time.Duration {
	days := 7
	if cfg != nil {
		if v, ok := cfg.Get("backup.max_age_days"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				days = n
			}
		}
	}
	return time.Duration(days) * 24 * time.Hour
}

// backupOnSync is the change-seam hook: the originating tab's dirty flag
// decides — dirty (re)arms the debounce, clean (just saved or reverted)
// cancels it and drops the snapshot. The tab is resolved by path, since a
// background tab (e.g. replace-in-buffer) can emit changes too.
func (m *Model) backupOnSync(fromKey, path string) tea.Cmd {
	if m.backupDeb == nil || !m.backupEnabled() {
		return nil
	}
	origin := m.panes.Get(fromKey)
	if origin == nil || origin.Kind() != pane.KindEditor {
		return nil
	}
	ed := origin.EditorForPath(path)
	if ed == nil {
		ed = origin.Editor() // pathless scratch buffer
	}
	key := backupKey(ed, fromKey)
	if ed.Dirty() {
		m.backupDeb.Mark(key, time.Now())
		return m.armBackupTick()
	}
	m.backupDeb.Cancel(key)
	svc := m.backupSvc
	return func() tea.Msg { _ = svc.Remove(key); return nil }
}

// armBackupTick schedules one wake at the earliest pending deadline. A single
// armed tick suffices: the tick handler re-arms while marks remain.
func (m *Model) armBackupTick() tea.Cmd {
	if m.backupTickArmed || m.backupDeb == nil {
		return nil
	}
	next, ok := m.backupDeb.Next()
	if !ok {
		return nil
	}
	m.backupTickArmed = true
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return backupTickMsg{} })
}

// snapshotDueBackups captures the text of every buffer whose debounce expired
// and writes the snapshots off the Update loop. Buffers that went clean or
// closed since their mark are skipped.
func (m *Model) snapshotDueBackups(now time.Time) tea.Cmd {
	if m.backupDeb == nil || !m.backupEnabled() {
		return nil
	}
	var docs []backup.Doc
	for _, key := range m.backupDeb.Due(now) {
		ed := m.backupEditorFor(key)
		if ed == nil || !ed.Dirty() {
			continue
		}
		d := backup.Doc{Key: key, Path: ed.Path(), Text: ed.Text()}
		if mtime, hash, ok := backup.BaseInfo(ed.Path()); ok {
			d.BaseMTime, d.BaseHash = mtime, hash
		}
		docs = append(docs, d)
	}
	if len(docs) == 0 {
		return nil
	}
	svc := m.backupSvc
	return func() tea.Msg {
		for _, d := range docs {
			_ = svc.Snapshot(d)
		}
		return nil
	}
}

// backupEditorFor resolves a snapshot key back to its live editor: by path for
// titled buffers, by pane key for untitled ones. nil when the buffer is gone.
func (m *Model) backupEditorFor(key string) *editor.Model {
	if pk, ok := strings.CutPrefix(key, untitledPrefix); ok {
		if inst := m.panes.Get(pk); inst != nil && inst.Kind() == pane.KindEditor {
			for _, ed := range inst.Editors() {
				if !ed.HasFile() {
					return ed
				}
			}
		}
		return nil
	}
	return m.editorForPath(key)
}

// backupDropOnClose removes the snapshots and pending marks of a closing editor
// pane's tabs — close-with-discard must not resurrect discarded edits at next
// launch. A shared document still shown in another tab or pane keeps its
// snapshot.
func (m *Model) backupDropOnClose(inst *pane.Instance, paneKey string) {
	if inst.Kind() != pane.KindEditor {
		return
	}
	for _, ed := range inst.Editors() {
		m.backupDropOnCloseTab(ed, paneKey)
	}
}

// backupDropOnCloseTab removes one closing tab's snapshot and pending mark,
// unless another tab — in this pane or any other — still shows the document.
func (m *Model) backupDropOnCloseTab(ed *editor.Model, paneKey string) {
	if m.backupDeb == nil {
		return
	}
	if ed.HasFile() && len(m.editorViewsForPath(ed.Path())) > 1 {
		return
	}
	key := backupKey(ed, paneKey)
	m.backupDeb.Cancel(key)
	_ = m.backupSvc.Remove(key)
}

// backupCleanShutdown removes this session's snapshots on a clean quit, so a
// leftover at startup always means a crash. Snapshots skipped at the restore
// prompt belong to no open pane and stay for the next launch.
func (m *Model) backupCleanShutdown() {
	if m.backupSvc == nil {
		return
	}
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			_ = m.backupSvc.Remove(backupKey(ed, key))
		}
	}
}

// reconfigureBackup applies [backup] changes on a live config reload: a new
// debounce interval takes over for future marks, and enable = false shuts the
// write side down, purges existing snapshots and withdraws any open prompt.
func (m *Model) reconfigureBackup(cfg host.Config) {
	if iv := backupInterval(cfg); m.backupDeb == nil || iv != m.backupIv {
		m.backupDeb = backup.NewDebouncer(iv)
		m.backupIv = iv
	}
	if m.backupEnabled() {
		return
	}
	m.backupDeb = backup.NewDebouncer(m.backupIv) // drop pending marks
	_, _ = m.backupSvc.Purge()
	m.recoveryPending = nil
	if m.recoveryOpen() {
		m.recovery = nil
		m.shell.Close()
	}
}
