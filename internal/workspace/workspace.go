// Package workspace bundles the per-project UI state the root model owns —
// the pane registry, the split-tree layout, and focus bookkeeping — into one
// unit (Roadmap 0370, #776). M1 extracts the unit and a Manager holding the
// single active workspace; M2 grows the Manager into a background set so
// switching projects keeps terminals, runs and debug sessions alive.
package workspace

import (
	"ike/internal/layout"
	"ike/internal/pane"
)

// Workspace is one project's live UI state. The root model reaches panes and
// layout exclusively through its active Workspace, so a later Manager can
// swap the whole unit atomically on a project switch.
type Workspace struct {
	// Root is the absolute project root this workspace is anchored to ("" in
	// M1: the process cwd is the root by convention).
	Root string
	// Panes is the instance registry backing every leaf of Tree.
	Panes *pane.Registry
	// Tree is the pure split-tree layout; leaves are instance keys.
	Tree layout.Node
	// ReturnFocus remembers the pane focused before terminal.toggle / a tool
	// command moved focus, so a second toggle returns there.
	ReturnFocus string
}

// New builds a workspace over an existing pane registry.
func New(root string, panes *pane.Registry) *Workspace {
	return &Workspace{Root: root, Panes: panes}
}

// Manager owns the active workspace. M1 is single-workspace by design —
// the type exists so every call site is already manager-shaped when M2 adds
// the background map, LRU cap and switch orchestration.
type Manager struct {
	active *Workspace
}

// NewManager builds a manager with the given active workspace.
func NewManager(active *Workspace) *Manager {
	return &Manager{active: active}
}

// Active returns the current workspace (never nil for a managed model).
func (m *Manager) Active() *Workspace { return m.active }

// SetActive replaces the current workspace (M2: park the old one in the
// background set instead of dropping it).
func (m *Manager) SetActive(w *Workspace) { m.active = w }
