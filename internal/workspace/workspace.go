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
	// Aux carries app-owned per-workspace extras that must survive a switch
	// live (#777) — the debug session state, notably. The workspace package
	// never inspects it.
	Aux any
}

// New builds a workspace over an existing pane registry.
func New(root string, panes *pane.Registry) *Workspace {
	return &Workspace{Root: root, Panes: panes}
}

// Manager owns the active workspace plus the background set (#777): parked
// workspaces stay fully alive — their PTYs, run and debug processes keep
// pumping through goroutines that never depended on being rendered — and a
// later switch back resumes the exact unit. The LRU cap and eviction land
// with M4 (#780).
type Manager struct {
	active *Workspace
	bg     map[string]*Workspace // parked workspaces, keyed by Root
	order  []string              // LRU order over bg: least-recently-used first
}

// NewManager builds a manager with the given active workspace.
func NewManager(active *Workspace) *Manager {
	return &Manager{active: active, bg: map[string]*Workspace{}}
}

// Active returns the current workspace (never nil for a managed model).
func (m *Manager) Active() *Workspace { return m.active }

// SetActive replaces the current workspace without parking the old one (the
// M1 seam; switches use Park+Resume instead).
func (m *Manager) SetActive(w *Workspace) { m.active = w }

// Park moves the active workspace into the background set under its Root and
// clears the active slot. A workspace without a Root cannot be resumed and is
// dropped instead of parked.
func (m *Manager) Park() {
	w := m.active
	m.active = nil
	if w == nil || w.Root == "" {
		return
	}
	if m.bg == nil {
		m.bg = map[string]*Workspace{}
	}
	m.touch(w.Root)
	m.bg[w.Root] = w
}

// Resume pops the parked workspace for root and makes it active, returning
// it; nil (and no state change) when none is parked there.
func (m *Manager) Resume(root string) *Workspace {
	w, ok := m.bg[root]
	if !ok {
		return nil
	}
	delete(m.bg, root)
	m.remove(root)
	m.active = w
	return w
}

// Peek returns the parked workspace for root without resuming it.
func (m *Manager) Peek(root string) *Workspace { return m.bg[root] }

// Background returns the parked roots, least-recently-used first.
func (m *Manager) Background() []string {
	return append([]string(nil), m.order...)
}

// Drop removes a parked workspace without resuming it and returns it (nil
// when absent) — the M4 eviction seam: the caller owns tearing it down.
func (m *Manager) Drop(root string) *Workspace {
	w, ok := m.bg[root]
	if !ok {
		return nil
	}
	delete(m.bg, root)
	m.remove(root)
	return w
}

// touch moves root to the most-recently-used end of the LRU order.
func (m *Manager) touch(root string) {
	m.remove(root)
	m.order = append(m.order, root)
}

// remove drops root from the LRU order.
func (m *Manager) remove(root string) {
	for i, r := range m.order {
		if r == root {
			m.order = append(m.order[:i], m.order[i+1:]...)
			return
		}
	}
}
