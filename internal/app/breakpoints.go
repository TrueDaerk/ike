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

// breakpointHooks returns the editor-facing source and adjuster closures.
// They capture the store pointer, not the model value, so every view shares
// the live set.
func breakpointHooks(bpts *debug.Breakpoints) (source func(string) []int, adjust func(string, int, int)) {
	source = func(path string) []int { return bpts.Lines(bpKey(path)) }
	adjust = func(path string, cursorAfter, delta int) {
		bpts.AdjustEdit(bpKey(path), cursorAfter, delta)
	}
	return source, adjust
}

// toggleBreakpoint flips path:line (0-based) and persists the store.
func (m *Model) toggleBreakpoint(path string, line int) {
	on := m.bpts.Toggle(bpKey(path), line)
	if err := m.bpts.Save(); err != nil {
		m.host.Notify(host.Warn, "breakpoints not saved: "+err.Error())
	}
	state := "removed"
	if on {
		state = "set"
	}
	m.host.Notify(host.Info, "breakpoint "+state+" — "+displayPath(path)+":"+strconv.Itoa(line+1))
}

// toggleBreakpointAtCursor is the debug.toggleBreakpoint handler: the focused
// editor's file at the cursor line.
func (m *Model) toggleBreakpointAtCursor() {
	inst := m.panes.FocusedInstance()
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
