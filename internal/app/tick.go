package app

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// tickHook, when set, replaces tea.Tick for every timer the app schedules.
// It is nil in the binary; the package tests install a hook that defers the
// long timers (toast expiry, grace periods, idle timeouts) instead of sleeping
// through them when a test drains commands synchronously (#2997).
var tickHook func(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd

// tickCmd is the one place the app schedules a timer command. Every
// tea.Tick in this package goes through it so the tests own a single seam
// for simulated time; see tick_test.go.
func tickCmd(d time.Duration, fn func(time.Time) tea.Msg) tea.Cmd {
	if tickHook != nil {
		return tickHook(d, fn)
	}
	return tea.Tick(d, fn)
}
