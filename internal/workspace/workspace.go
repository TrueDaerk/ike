// Package workspace bundles the per-project UI state the root model owns —
// the pane registry, the split-tree layout, and focus bookkeeping — into one
// unit (Roadmap 0370, #776). M1 extracts the unit and a Manager holding the
// single active workspace; M2 grows the Manager into a background set so
// switching projects keeps terminals, runs and debug sessions alive.
package workspace

import (
	"sort"
	"time"

	"ike/internal/editor/register"
	"ike/internal/layout"
	"ike/internal/pane"
	"ike/internal/terminal"
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
	// ParkedAt is when this workspace last entered the background set (set
	// by Park, zero while active). The background LSP idle shutdown (#1521)
	// compares it at timer expiry: a workspace resumed and re-parked since
	// the timer was armed reads as freshly parked and is skipped.
	ParkedAt time.Time
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
	// regs is the app-wide register store (#1540): yanks, deletes and the
	// paste-from-history ring shared by every editor in every workspace. It
	// lives on the manager because a project switch rebuilds the root model
	// but carries the manager — model-owned state would reset the registers
	// on every switch.
	regs *register.Store
	// globalTools holds the detached global tool sessions (#1890), keyed by
	// tool name: a tool marked global in the config runs as one process-wide
	// instance, and while it is not attached to the active workspace's layout
	// its live terminal session parks here. The manager owns it because it
	// survives model rebuilds and never belongs to any workspace registry —
	// which is exactly what keeps switch, close and eviction from ending it.
	globalTools map[string]terminal.Model
	// closedGlobalTools records global tools explicitly closed and not
	// reopened since (#1903): a per-project layout.json saved while the tool
	// was open still lists it, and without this record a later switch into
	// that project would resurrect the closed tool from the stale entry. The
	// manager — stash plus this set — is the authority on whether a global
	// tool is open, never the per-project layouts.
	closedGlobalTools map[string]bool
}

// NewManager builds a manager with the given active workspace.
func NewManager(active *Workspace) *Manager {
	return &Manager{active: active, bg: map[string]*Workspace{}}
}

// Registers returns the manager-owned shared register store (#1540),
// allocating it on first use.
func (m *Manager) Registers() *register.Store {
	if m.regs == nil {
		m.regs = register.New()
	}
	return m.regs
}

// SetRegisters adopts an existing store as the shared one (#1540) — the first
// root model allocates the store before the manager exists and hands it over
// here; nil is ignored.
func (m *Manager) SetRegisters(s *register.Store) {
	if s != nil {
		m.regs = s
	}
}

// ParkGlobalTool stashes a global tool's live terminal session under its tool
// name (#1890) — detached from any workspace, it keeps running until taken
// back or closed at quit.
func (m *Manager) ParkGlobalTool(name string, t terminal.Model) {
	if m.globalTools == nil {
		m.globalTools = map[string]terminal.Model{}
	}
	m.globalTools[name] = t
}

// TakeGlobalTool removes and returns the parked global tool session for name;
// ok=false when none is parked (the tool is attached somewhere or was never
// opened).
func (m *Manager) TakeGlobalTool(name string) (terminal.Model, bool) {
	t, ok := m.globalTools[name]
	if ok {
		delete(m.globalTools, name)
	}
	return t, ok
}

// PeekGlobalTool returns the parked global tool session for name without
// removing it; ok=false when none is parked.
func (m *Manager) PeekGlobalTool(name string) (terminal.Model, bool) {
	t, ok := m.globalTools[name]
	return t, ok
}

// MarkGlobalToolClosed records that the named global tool was explicitly
// closed (#1903): the session is gone on purpose, and stale layout entries in
// other projects must not resurrect it on the next switch.
func (m *Manager) MarkGlobalToolClosed(name string) {
	if m.closedGlobalTools == nil {
		m.closedGlobalTools = map[string]bool{}
	}
	m.closedGlobalTools[name] = true
}

// ClearGlobalToolClosed forgets a recorded explicit close — every path that
// opens the tool again calls it, so the reopened tool restores normally.
func (m *Manager) ClearGlobalToolClosed(name string) {
	delete(m.closedGlobalTools, name)
}

// GlobalToolClosed reports whether the named global tool was explicitly
// closed and not reopened since (#1903).
func (m *Manager) GlobalToolClosed(name string) bool {
	return m.closedGlobalTools[name]
}

// GlobalToolNames lists the currently parked global tools, sorted for stable
// iteration — the quit path closes each one so no process outlives IKE.
func (m *Manager) GlobalToolNames() []string {
	names := make([]string, 0, len(m.globalTools))
	for name := range m.globalTools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
	w.ParkedAt = time.Now()
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
	w.ParkedAt = time.Time{}
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
