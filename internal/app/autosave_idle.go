package app

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/backup"
	"ike/internal/host"
	"ike/internal/pane"
)

// autosave_idle.go is the idle autosave mode (#731): with
// editor.auto_save = "idle", a dirty titled buffer writes itself after staying
// quiet for editor.auto_save_idle_ms. It rides the same change seam and
// debouncer shape as the crash-recovery snapshots (backup.go) — the SyncMsg
// hook marks the buffer, one armed tick saves the buffers that went quiet.
// "idle" is a superset of "focus": the on-blur save stays active too.

// autosaveIdleTickMsg wakes the model to save the buffers whose debounce expired.
type autosaveIdleTickMsg struct{}

// autosaveIdle reports whether editor.auto_save is in idle mode.
func (m *Model) autosaveIdle() bool {
	v, ok := m.host.Config().Get("editor.auto_save")
	return ok && v == "idle"
}

// autosaveIdleInterval reads the idle delay from cfg (validation clamps it).
func autosaveIdleInterval(cfg host.Config) time.Duration {
	d := 2000
	if cfg != nil {
		if v, ok := cfg.Get("editor.auto_save_idle_ms"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				d = n
			}
		}
	}
	return time.Duration(d) * time.Millisecond
}

// autosaveIdleOnSync is the change-seam hook: a dirty titled buffer (re)arms
// its idle deadline, a clean one cancels it. Untitled buffers are never
// marked — autosave has nowhere to write them (#731); crash recovery covers
// them instead. Keys are shared with the backup side (path, or the pane-scoped
// untitled token that is filtered out here).
func (m *Model) autosaveIdleOnSync(fromKey, path string) tea.Cmd {
	if m.autosaveIdleDeb == nil || !m.autosaveIdle() {
		return nil
	}
	origin := m.panes.Get(fromKey)
	if origin == nil || origin.Kind() != pane.KindEditor {
		return nil
	}
	ed := origin.EditorForPath(path)
	if ed == nil || !ed.HasFile() {
		return nil
	}
	if ed.Dirty() {
		m.autosaveIdleDeb.Mark(ed.Path(), time.Now())
		return m.armAutosaveIdleTick()
	}
	m.autosaveIdleDeb.Cancel(ed.Path())
	return nil
}

// armAutosaveIdleTick schedules one wake at the earliest pending deadline; the
// tick handler re-arms while marks remain (the backup.go pattern).
func (m *Model) armAutosaveIdleTick() tea.Cmd {
	if m.autosaveIdleTickArmed || m.autosaveIdleDeb == nil {
		return nil
	}
	next, ok := m.autosaveIdleDeb.Next()
	if !ok {
		return nil
	}
	m.autosaveIdleTickArmed = true
	d := time.Until(next)
	if d < 0 {
		d = 0
	}
	return tea.Tick(d, func(time.Time) tea.Msg { return autosaveIdleTickMsg{} })
}

// saveDueIdleBuffers autosaves every buffer whose idle deadline expired.
// Editor.Autosave applies its own guards (clean, stale, pathless), so a
// buffer that changed state since its mark is a no-op; the save goes through
// the normal path, so EventSave fires and the modified indicator clears.
func (m *Model) saveDueIdleBuffers(now time.Time) {
	if m.autosaveIdleDeb == nil || !m.autosaveIdle() {
		return
	}
	for _, key := range m.autosaveIdleDeb.Due(now) {
		for _, paneKey := range m.editorKeysForPath(key) {
			if inst := m.panes.Get(paneKey); inst != nil {
				if ed := inst.EditorForPath(key); ed != nil {
					ed.Autosave()
					break // one write per document; other views sync off EventSave
				}
			}
		}
	}
}

// reconfigureAutosaveIdle applies editor.auto_save edits on a live config
// reload: an interval change re-arms future marks, leaving idle mode drops
// the pending ones.
func (m *Model) reconfigureAutosaveIdle(cfg host.Config) {
	if iv := autosaveIdleInterval(cfg); m.autosaveIdleDeb == nil || iv != m.autosaveIdleIv {
		m.autosaveIdleDeb = backup.NewDebouncer(iv)
		m.autosaveIdleIv = iv
	}
	if !m.autosaveIdle() {
		m.autosaveIdleDeb = backup.NewDebouncer(m.autosaveIdleIv) // drop pending marks
	}
}
