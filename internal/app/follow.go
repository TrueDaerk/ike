package app

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/pane"
)

// follow.go is the app half of editor follow mode (#1928): while at least one
// editor view follows its file (tail -f), a single demand-armed tick drives
// the watch service's poll fallback — the mtime+size comparison over the
// tracked open buffers — so appends surface as the usual watch.EventMsg even
// where fsnotify under-reports (network mounts, files outside the watched
// root). The tick re-arms only while a follower remains, per the idle rules
// in wiki/architecture/performance.md; fsnotify keeps reporting on its own
// either way, the poll is the safety net that bounds the follow latency.

// followTickMsg is the follow poll deadline (#1928). gen names the model that
// armed it (#2194): both chains of a park/resume race would self-sustain while
// a view keeps following, so a tick from a departed model retires instead.
type followTickMsg struct{ gen int64 }

// armFollowTick schedules the next follow poll; at most one is in flight.
func (m *Model) armFollowTick() tea.Cmd {
	if m.followTickArmed {
		return nil
	}
	m.followTickArmed = true
	gen := m.modelGen
	return tea.Tick(followInterval(m.host.Config()), func(time.Time) tea.Msg {
		return followTickMsg{gen: gen}
	})
}

// followTick handles one elapsed poll deadline: polls the tracked files off
// the Update loop (changes arrive as watch.EventMsg through the service's
// send seam) and re-arms only while a view still follows.
func (m *Model) followTick() tea.Cmd {
	m.followTickArmed = false
	if m.watcher == nil || !m.anyFollowing() {
		return nil
	}
	w := m.watcher
	return tea.Batch(
		func() tea.Msg { w.Poll(); return nil },
		m.armFollowTick(),
	)
}

// anyFollowing reports whether any editor view of the active workspace
// follows its file. Parked workspaces stop their watcher anyway; a resumed
// one reconciles from disk (#1515) and re-arms through the next FollowMsg or
// editor event.
func (m Model) anyFollowing() bool {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.Following() {
				return true
			}
		}
	}
	return false
}

// followInterval reads the poll interval from cfg (validation clamps it).
func followInterval(cfg host.Config) time.Duration {
	d := 500
	if cfg != nil {
		if v, ok := cfg.Get("editor.follow_poll_ms"); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				d = n
			}
		}
	}
	return time.Duration(d) * time.Millisecond
}
