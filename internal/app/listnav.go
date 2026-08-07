package app

import "ike/internal/ui"

// listnav.go is the app-side adapter for the shared selection-list navigation
// (#1666). Every modal picker the root model hosts — pins, local history, VCS
// history, the recovery prompt, the onboarding and setup wizards — renders
// into the same floating shell, so they all take their page size from it and
// all get the same semantics: single steps wrap, page jumps clamp, home/end
// go to the extremes.

// pickerPage is the page-jump size for a list hosted in the floating shell:
// the shell's visible content rows, or a readable default before the first
// layout has run.
func (m Model) pickerPage() int {
	if rows := m.shell.ViewportRows(); rows > 0 {
		return rows
	}
	return 10
}

// pickerNav routes a navigation key at a shell-hosted picker's cursor and
// reports whether it consumed the key. onMove runs after a consumed key so
// the caller can re-render its content or follow the selection; it may be nil.
func (m Model) pickerNav(key string, cursor *int, n int, onMove func()) bool {
	if !ui.ListNav(key, cursor, n, m.pickerPage(), ui.NavFull) {
		return false
	}
	if onMove != nil {
		onMove()
	}
	return true
}
