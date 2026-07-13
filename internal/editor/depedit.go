package editor

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// depedit.go guards edits to dependency files (#565). A buffer whose path lives
// under a dependency directory (a vendored third-party tree, e.g. the source a
// go-to-definition jumped into) opens read-only: the first edit is intercepted,
// the host shows a confirmation, and confirming unlocks the buffer for the
// session and replays the blocked edit. The editor never opens the prompt
// itself — it only blocks the mutation and signals the host through the Cmd
// returned by Update.

// dependencyDirs are the path segments that mark a vendored dependency tree.
// The match is on any segment, so both an out-of-project interpreter
// (~/.pyenv/.../site-packages) and a project-local one (./.venv, ./node_modules)
// are covered — the intent is "not your source", not "outside the root".
var dependencyDirs = map[string]bool{
	".venv":            true,
	"venv":             true,
	"site-packages":    true,
	"dist-packages":    true,
	"__pypackages__":   true,
	".tox":             true,
	"node_modules":     true,
	"vendor":           true,
	"Pods":             true,
	"bower_components": true,
}

// dependencyDir reports whether path traverses a known dependency directory.
func dependencyDir(path string) bool {
	if path == "" {
		return false
	}
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if dependencyDirs[seg] {
			return true
		}
	}
	return false
}

// DepEditBlockedMsg is emitted (via the Update Cmd) when an edit to an
// unconfirmed dependency file is blocked. The host opens its confirmation
// prompt for Path and, on confirm, routes a ConfirmDepEditMsg to that editor.
type DepEditBlockedMsg struct{ Path string }

// ConfirmDepEditMsg is routed to the editor when the user accepts the
// dependency-file edit prompt: it unlocks the buffer and replays the blocked
// edit through Update, so the change reparses and scrolls like any other.
type ConfirmDepEditMsg struct{}

// IsDependencyFile reports whether this buffer is an unvalidated dependency file
// (read-only until the user confirms editing it).
func (m *Model) IsDependencyFile() bool { return m.depFile }

// blockDep reports whether an edit must be blocked: a dependency file whose edit
// has not yet been confirmed this session.
func (m *Model) blockDep() bool { return m.depFile && !m.depOK }

// stashDep records the blocked edit for replay and raises the one-shot signal so
// Update emits DepEditBlockedMsg. run re-executes the exact edit once confirmed.
func (m *Model) stashDep(run func(*Model)) {
	m.depPending = run
	m.depSignal = true
}

// takeDepSignal consumes the block signal, returning the Cmd that asks the host
// to open the confirmation prompt (nil when no edit was blocked this cycle).
func (m *Model) takeDepSignal() tea.Cmd {
	if !m.depSignal {
		return nil
	}
	m.depSignal = false
	path := m.path
	return func() tea.Msg { return DepEditBlockedMsg{Path: path} }
}

// ConfirmDepEdit unlocks the dependency buffer for the session and replays the
// edit that was blocked, so the confirmed keystroke lands. Called by the host
// when the user accepts the confirmation prompt.
func (m *Model) ConfirmDepEdit() {
	m.depOK = true
	run := m.depPending
	m.depPending = nil
	if run != nil {
		run(m)
	}
}

// CancelDepEdit drops the blocked edit, leaving the buffer unchanged and still
// locked. Called by the host when the user declines the prompt.
func (m *Model) CancelDepEdit() { m.depPending = nil }
