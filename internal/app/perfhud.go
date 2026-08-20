package app

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"ike/internal/host"
	"ike/internal/overlay"
	"ike/internal/perfhud"
	"ike/internal/plugin"
)

// perfhud.go is the app half of the built-in performance HUD (#1999): the
// toggle, the sampling tick, the overlay placement and the clipboard snapshot.
// The measurement hooks themselves sit at the three points that see everything
// — Update (message rates), render (frame cost) and renderPane (per-pane
// attribution) — each behind a perfhud.Enabled() check, so a hidden HUD costs
// one atomic load and nothing else. See wiki/architecture/performance.md.
//
// The HUD's open state deliberately lives in the perfhud collector rather than
// in the model: the model is rebuilt on a project switch, and an in-flight
// sampling tick has to find the HUD still open after that.

// TogglePerfHUDMsg shows or hides the performance HUD overlay. Dispatched by
// perf.hud (ctrl+alt+p / View menu / palette).
type TogglePerfHUDMsg struct{}

// PerfSnapshotMsg copies the current metrics to the system clipboard as a
// plain-text block. Dispatched by perf.snapshot.
type PerfSnapshotMsg struct{}

// perfTickMsg is the HUD's sampling deadline.
type perfTickMsg struct{}

// perfCommands builds the perf.* command family.
func perfCommands() []plugin.Command {
	return []plugin.Command{
		appCommand("perf.hud", "Performance HUD", TogglePerfHUDMsg{}),
		appCommand("perf.snapshot", "Copy Performance Snapshot", PerfSnapshotMsg{}),
	}
}

// togglePerfHUD flips the HUD. Turning it on starts collection and the
// sampling tick; turning it off stops both, so the hooks fall back to their
// single atomic load.
func (m *Model) togglePerfHUD() tea.Cmd {
	if perfhud.Enabled() {
		perfhud.SetEnabled(false)
		m.perfBox, m.perfBoxW = "", 0
		m.host.Notify(host.Info, "performance HUD off")
		return nil
	}
	perfhud.SetHistory(perfHUDHistory(m.host.Config()))
	perfhud.SetEnabled(true)
	m.host.Notify(host.Info, "performance HUD on — perf.snapshot copies the numbers")
	return m.armPerfTick()
}

// armPerfTick schedules the next sample; at most one is in flight and only
// while the HUD is on.
func (m *Model) armPerfTick() tea.Cmd {
	if m.perfTickArmed || !perfhud.Enabled() {
		return nil
	}
	m.perfTickArmed = true
	return tea.Tick(perfHUDInterval(m.host.Config()), func(time.Time) tea.Msg {
		return perfTickMsg{}
	})
}

// perfTick closes one measurement window and re-arms while the HUD is open.
func (m *Model) perfTick() tea.Cmd {
	// Counted before clearing the flag: the HUD's own tick was armed for the
	// window being closed, and it re-arms below — reporting it as gone for the
	// instant it is being handled would understate the HUD's cost.
	timers := m.armedTimers()
	m.perfTickArmed = false
	if !perfhud.Enabled() {
		return nil
	}
	latest := perfhud.Collect(timers)
	// The box is laid out here, once per sample, not once per frame: its
	// numbers cannot change in between, and a per-frame lipgloss pass over a
	// diagnostic overlay would be its own small regression.
	m.perfBox = perfhud.Render(latest, perfhud.History(), m.pal(), m.width)
	m.perfBoxW = m.width
	return m.armPerfTick()
}

// armedTimers counts the app's demand-armed tickers (the idle rules in
// wiki/architecture/performance.md): a number that should sit at zero in a
// quiet session and names a stuck debounce loop when it does not. The HUD's
// own tick is counted too — the HUD does not get to hide its cost.
func (m Model) armedTimers() int {
	n := 0
	for _, armed := range []bool{
		m.backupTickArmed,
		m.autosaveIdleTickArmed,
		m.followTickArmed,
		m.hoverIdleTickArmed,
		m.perfTickArmed,
	} {
		if armed {
			n++
		}
	}
	return n
}

// copyPerfSnapshot puts the current numbers on the clipboard. With the HUD
// open it copies the sample on screen rather than closing a fresh window: the
// slice of a second between the last tick and the keystroke is not a window
// anyone can reason about, and the point of the command is pasting what you
// just saw. With the HUD closed it takes a sample on the spot — the goroutine,
// heap and RSS gauges are honest either way, and the block says the rates are
// missing instead of printing measured-looking zeros.
func (m Model) copyPerfSnapshot() (tea.Model, tea.Cmd) {
	s, ok := perfhud.Latest()
	if !ok || !perfhud.Enabled() {
		s = perfhud.Collect(m.armedTimers())
	}
	clipboardWrite(perfhud.SnapshotText(s, perfhud.History()))
	if s.Live {
		m.host.Notify(host.Info, "performance snapshot copied to the clipboard")
	} else {
		m.host.Notify(host.Info, "performance snapshot copied (rates need the HUD — perf.hud)")
	}
	return m, nil
}

// compositePerfHUD places the HUD box in the workspace's top-right corner,
// below the menu bar when one is shown. It is drawn over everything but the
// toasts: a diagnostic overlay that a palette could hide would miss the very
// frames it is there to explain.
func (m Model) compositePerfHUD(base string) string {
	box := m.perfBox
	if box == "" || m.perfBoxW != m.width {
		// No cached box: either no sample has landed yet, or a resize (or a
		// project switch, which rebuilds the model) invalidated the cache and
		// the next sample has not re-laid it out yet.
		latest, ok := perfhud.Latest()
		if !ok {
			return base
		}
		box = perfhud.Render(latest, perfhud.History(), m.pal(), m.width)
	}
	if box == "" {
		return base
	}
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	x := m.width - w - 1
	y := 0
	if m.menuEnabled() {
		y = 1
	}
	if x < 0 || y+h > m.height {
		return base
	}
	return overlay.Place(base, box, x, y, m.width, m.height)
}

// perfHUDInterval reads the sampling interval from cfg (validation clamps it).
func perfHUDInterval(cfg host.Config) time.Duration {
	return time.Duration(perfSetting(cfg, "perf.hud_interval_ms", 1000)) * time.Millisecond
}

// perfHUDHistory derives the rolling-history length in samples from the
// configured seconds and the sampling interval, so "the last minute" stays a
// minute whatever the refresh rate is. Bounded so a fast interval with a long
// window cannot grow an unreasonable ring.
func perfHUDHistory(cfg host.Config) int {
	iv := perfSetting(cfg, "perf.hud_interval_ms", 1000)
	if iv < 1 {
		iv = 1000
	}
	n := perfSetting(cfg, "perf.hud_history_seconds", 60) * 1000 / iv
	if n < 8 {
		n = 8
	}
	if n > 600 {
		n = 600
	}
	return n
}

// perfSetting reads one integer setting, falling back to def.
func perfSetting(cfg host.Config, key string, def int) int {
	if cfg == nil {
		return def
	}
	v, ok := cfg.Get(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
