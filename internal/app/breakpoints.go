package app

import (
	"path/filepath"
	"strconv"
	"strings"

	"ike/internal/debug"
	"ike/internal/host"
	"ike/internal/pane"
)

// breakpoints.go wires the breakpoint store (0350, #577) into the app: the
// store lives on the root model, editors render it through an injected
// source, toggling happens via debug.toggleBreakpoint (ctrl+f8) or a gutter
// click, and edits shift lines through the editor's adjuster callback.
// Persisted per project in .ike/breakpoints.json on toggle and on file save.

// bpKey canonicalizes an editor path to the store's project-relative key, so
// the file travels with the repository.
func bpKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(projectRoot(), abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return abs
}

// bpHooks bundles the editor-facing breakpoint callbacks: the line sources
// the gutter renders (all lines, the disabled subset, and the conditional and
// logpoint subsets that pick their own glyph, #2245) plus the edit adjuster.
type bpHooks struct {
	source      func(string) []int
	disabled    func(string) []int
	conditional func(string) []int
	logpoints   func(string) []int
	adjust      func(string, int, int)
}

// breakpointHooks returns those closures. They capture the store pointer, not
// the model value, so every view shares the live set.
func breakpointHooks(bpts *debug.Breakpoints) bpHooks {
	return bpHooks{
		source:      func(path string) []int { return bpts.Lines(bpKey(path)) },
		disabled:    func(path string) []int { return bpts.DisabledLines(bpKey(path)) },
		conditional: func(path string) []int { return bpts.ConditionalLines(bpKey(path)) },
		logpoints:   func(path string) []int { return bpts.LogpointLines(bpKey(path)) },
		adjust: func(path string, cursorAfter, delta int) {
			bpts.AdjustEdit(bpKey(path), cursorAfter, delta)
		},
	}
}

// toggleBreakpoint flips path:line (0-based) and persists the store.
func (m *Model) toggleBreakpoint(path string, line int) {
	on := m.bpts.Toggle(bpKey(path), line)
	m.saveBreakpoints()
	state := "removed"
	if on {
		state = "set"
	}
	m.host.Notify(host.Info, "breakpoint "+state+" — "+displayPath(path)+":"+strconv.Itoa(line+1))
	// An open Breakpoints list (#1377) reflects the gutter toggle live.
	m.refreshBreakpointsPanel()
	// A live debug session (#579) sees the change immediately.
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	m.syncSessionBreakpoints(abs)
}

// toggleBreakpointAtCursor is the debug.toggleBreakpoint handler: the focused
// editor's file at the cursor line.
func (m *Model) toggleBreakpointAtCursor() {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		m.host.Notify(host.Info, "breakpoints need a focused editor")
		return
	}
	ed := inst.Editor()
	if ed == nil || !ed.HasFile() {
		m.host.Notify(host.Info, "breakpoints need an open file")
		return
	}
	line, _ := ed.CursorPos()
	m.toggleBreakpoint(ed.Path(), line)
}
