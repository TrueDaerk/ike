package pane

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/clipboard"
	"ike/internal/diff"
	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/preview"
	"ike/internal/terminal"
	"ike/internal/theme"
	"ike/internal/vcspanel"
)

// Kind is the type of component an Instance wraps. The explorer is a singleton;
// editors are many.
type Kind int

const (
	// KindExplorer is the file-tree pane. Exactly one exists, under key "explorer".
	KindExplorer Kind = iota
	// KindEditor is a text editor pane. Any number may exist, tiled side by side.
	KindEditor
	// KindTerminal is an integrated terminal pane (Roadmap 0170); any number
	// may exist, each owning one shell session.
	KindTerminal
	// KindMarkdown is a rendered markdown preview pane (#62); any number may
	// exist, each bound to one source buffer path.
	KindMarkdown
	// KindDiff is a read-only diff viewer pane (#60); any number may exist,
	// each comparing two text versions.
	KindDiff
	// KindVCS is the VCS tool window (Roadmap 0330): a singleton bottom-split
	// panel with the changes list and the git log, under key "vcs".
	KindVCS
)

// Context ids an Instance advertises for context-scoped command/keymap
// resolution, matching the constants internal/app historically owned.
const (
	ctxExplorer = "explorer"
	ctxEditor   = "editor"
	ctxTerminal = "terminal"
	ctxPreview  = "preview"
	ctxDiff     = "diff"
	ctxVCS      = "vcs"
)

// Instance is one live pane: a stable key plus the component it drives. An
// explorer instance wraps the singleton explorer model. An editor instance
// hosts an ordered list of open documents — its tabs (#156) — with exactly one
// active tab; the pane renders and routes input to the active tab only, while
// the layout tree stays a pure split-tree of leaves. Component models are value
// types with pointer-receiver methods, so the Instance is held behind a pointer
// in the registry and its accessors hand out pointers into the tab slots.
type Instance struct {
	key  string
	kind Kind
	exp  explorer.Model
	term terminal.Model
	md   preview.Model
	df   diff.Model
	vp   vcspanel.Model

	// Editor state: the ordered tab list and the active index. cfg/pal/size
	// and focus are remembered so tabs created later match the live pane.
	tabs    []*editor.Model
	active  int
	cfg     host.Config
	pal     *theme.Palette
	w, h    int
	focused bool
}

// Key returns the instance's stable identity, the same string used as the
// layout leaf id and the persistence key.
func (i *Instance) Key() string { return i.key }

// Kind reports whether the instance is an explorer or an editor.
func (i *Instance) Kind() Kind { return i.kind }

// ContextID is the context id the instance advertises for command/keymap
// resolution: explorer panes resolve under "explorer", editors under "editor".
func (i *Instance) ContextID() string {
	switch i.kind {
	case KindExplorer:
		return ctxExplorer
	case KindTerminal:
		return ctxTerminal
	case KindMarkdown:
		return ctxPreview
	case KindDiff:
		return ctxDiff
	case KindVCS:
		return ctxVCS
	}
	return ctxEditor
}

// Explorer returns the underlying explorer model. It is only valid for an
// explorer instance; callers gate on Kind first.
func (i *Instance) Explorer() *explorer.Model { return &i.exp }

// Terminal returns the underlying terminal model. It is only valid for a
// terminal instance; callers gate on Kind first.
func (i *Instance) Terminal() *terminal.Model { return &i.term }

// Preview returns the underlying markdown preview model. It is only valid for
// a markdown instance; callers gate on Kind first.
func (i *Instance) Preview() *preview.Model { return &i.md }

// Diff returns the underlying diff viewer model. It is only valid for a diff
// instance; callers gate on Kind first.
func (i *Instance) Diff() *diff.Model { return &i.df }

// VCS returns the underlying VCS tool-window model. It is only valid for a
// vcs instance; callers gate on Kind first.
func (i *Instance) VCS() *vcspanel.Model { return &i.vp }

// Editor returns the active tab's editor model. It is only valid for an editor
// instance; callers gate on Kind first.
func (i *Instance) Editor() *editor.Model {
	if len(i.tabs) == 0 {
		return nil
	}
	return i.tabs[i.active]
}

// TabCount reports how many tabs the editor instance holds (0 for explorers).
func (i *Instance) TabCount() int { return len(i.tabs) }

// ActiveTab returns the index of the active tab.
func (i *Instance) ActiveTab() int { return i.active }

// TabEditor returns the editor model of tab idx, or nil when out of range.
func (i *Instance) TabEditor(idx int) *editor.Model {
	if idx < 0 || idx >= len(i.tabs) {
		return nil
	}
	return i.tabs[idx]
}

// Editors returns every tab's editor model in tab order. Callers that iterate
// "all documents of this pane" (emitters, autosave sweeps, backup drops) use
// this instead of Editor, which only sees the active tab.
func (i *Instance) Editors() []*editor.Model {
	out := make([]*editor.Model, len(i.tabs))
	copy(out, i.tabs)
	return out
}

// TabForPath returns the index of the first tab showing path, or -1.
func (i *Instance) TabForPath(path string) int {
	for idx, t := range i.tabs {
		if t.HasFile() && t.Path() == path {
			return idx
		}
	}
	return -1
}

// EditorForPath returns the first tab's editor model showing path, or nil.
func (i *Instance) EditorForPath(path string) *editor.Model {
	if idx := i.TabForPath(path); idx >= 0 {
		return i.tabs[idx]
	}
	return nil
}

// AddTab appends a fresh empty tab, makes it active, and returns its editor
// model. The new tab inherits the pane's size, config, palette and focus. Only
// valid on editor instances.
func (i *Instance) AddTab() *editor.Model {
	if i.kind != KindEditor {
		return nil
	}
	ed := newEditorModel(i.cfg, i.pal)
	ed.SetSize(i.w, i.h)
	i.tabs = append(i.tabs, &ed)
	i.activate(len(i.tabs) - 1)
	return i.tabs[i.active]
}

// ActivateTab makes tab idx the active one, moving the pane's focus state onto
// it. It reports whether the index was valid.
func (i *Instance) ActivateTab(idx int) bool {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) {
		return false
	}
	i.activate(idx)
	return true
}

// activate switches the active index and re-asserts per-tab focus flags.
func (i *Instance) activate(idx int) {
	i.active = idx
	for n, t := range i.tabs {
		t.SetFocused(i.focused && n == i.active)
	}
}

// MoveTab reorders the tab at from to position to, keeping the same tab active.
// It reports whether both indexes were valid.
func (i *Instance) MoveTab(from, to int) bool {
	if i.kind != KindEditor || from < 0 || from >= len(i.tabs) || to < 0 || to >= len(i.tabs) {
		return false
	}
	if from == to {
		return true
	}
	activeTab := i.tabs[i.active]
	t := i.tabs[from]
	i.tabs = append(i.tabs[:from], i.tabs[from+1:]...)
	rest := append([]*editor.Model{}, i.tabs[to:]...)
	i.tabs = append(append(i.tabs[:to:to], t), rest...)
	for n, tab := range i.tabs {
		if tab == activeTab {
			i.active = n
			break
		}
	}
	return true
}

// CloseTab removes tab idx. The neighbour that slides into its position becomes
// active when the active tab itself closes (the last position falls back to its
// left neighbour). Closing the only tab is refused — the caller closes the pane
// instead, so an editor instance never exists with zero tabs.
func (i *Instance) CloseTab(idx int) bool {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) || len(i.tabs) == 1 {
		return false
	}
	i.tabs = append(i.tabs[:idx], i.tabs[idx+1:]...)
	switch {
	case i.active > idx:
		i.active--
	case i.active == idx && i.active >= len(i.tabs):
		i.active = len(i.tabs) - 1
	}
	i.activate(i.active)
	return true
}

// SetSize pushes an interior content size into the wrapped component. Editor
// instances size every tab, so switching tabs never renders through a stale
// viewport.
func (i *Instance) SetSize(w, h int) {
	switch i.kind {
	case KindExplorer:
		i.exp.SetSize(w, h)
	case KindEditor:
		i.w, i.h = w, h
		for _, t := range i.tabs {
			t.SetSize(w, h)
		}
	case KindTerminal:
		i.term.SetSize(w, h)
	case KindMarkdown:
		i.md.SetSize(w, h)
	case KindDiff:
		i.df.SetSize(w, h)
	case KindVCS:
		i.vp.SetSize(w, h)
	}
}

// SetFocused marks the wrapped component focused or blurred. For editors only
// the active tab ever carries focus.
func (i *Instance) SetFocused(f bool) {
	switch i.kind {
	case KindExplorer:
		i.exp.SetFocused(f)
	case KindEditor:
		i.focused = f
		for n, t := range i.tabs {
			t.SetFocused(f && n == i.active)
		}
	case KindTerminal:
		i.term.SetFocused(f)
	case KindMarkdown:
		i.md.SetFocused(f)
	case KindDiff:
		i.df.SetFocused(f)
	case KindVCS:
		i.vp.SetFocused(f)
	}
}

// View renders the wrapped component's content (without pane chrome).
func (i *Instance) View() string {
	switch i.kind {
	case KindExplorer:
		return i.exp.View()
	case KindEditor:
		return i.Editor().View()
	case KindTerminal:
		return i.term.View()
	case KindMarkdown:
		return i.md.View()
	case KindDiff:
		return i.df.View()
	case KindVCS:
		return i.vp.View()
	}
	return ""
}

// Update dispatches a message to the wrapped component — for editors, to the
// active tab — mutating it in place and returning any resulting command.
func (i *Instance) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch i.kind {
	case KindExplorer:
		i.exp, cmd = i.exp.Update(msg)
	case KindEditor:
		t := i.tabs[i.active]
		*t, cmd = t.Update(msg)
	case KindTerminal:
		if k, ok := msg.(tea.KeyPressMsg); ok {
			cmd = i.term.Update(k)
		}
	case KindMarkdown:
		cmd = i.md.Update(msg)
	case KindDiff:
		cmd = i.df.Update(msg)
	case KindVCS:
		cmd = i.vp.Update(msg)
	}
	return cmd
}

// UpdateForPath dispatches a message to every tab showing path except skip,
// batching the resulting commands. Background tabs share documents too (#142),
// so path-routed messages (sync, highlight, LSP results) must reach the tabs
// the active-tab Update never sees.
func (i *Instance) UpdateForPath(path string, skip *editor.Model, msg tea.Msg) tea.Cmd {
	if i.kind != KindEditor {
		return nil
	}
	var cmds []tea.Cmd
	for _, t := range i.tabs {
		if t == skip || !t.HasFile() || t.Path() != path {
			continue
		}
		var cmd tea.Cmd
		*t, cmd = t.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// UpdateTab dispatches a message to tab idx regardless of which tab is active.
func (i *Instance) UpdateTab(idx int, msg tea.Msg) tea.Cmd {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) {
		return nil
	}
	t := i.tabs[idx]
	var cmd tea.Cmd
	*t, cmd = t.Update(msg)
	return cmd
}

// Init returns the wrapped component's initialisation command.
func (i *Instance) Init() tea.Cmd {
	switch i.kind {
	case KindExplorer:
		return i.exp.Init()
	case KindEditor:
		return i.Editor().Init()
	}
	return nil
}

// newInstance builds an instance of the given kind, configuring it against cfg.
// The explorer is rooted at the working directory; editors start with a single
// tab holding an empty scratch buffer.
func newInstance(key string, kind Kind, cfg host.Config, pal *theme.Palette) *Instance {
	i := &Instance{key: key, kind: kind, cfg: cfg, pal: pal}
	switch kind {
	case KindExplorer:
		i.exp = explorer.New(".")
		i.exp.SetPalette(pal)
		i.exp.Configure(cfg)
	case KindEditor:
		ed := newEditorModel(cfg, pal)
		i.tabs = []*editor.Model{&ed}
	}
	return i
}

// newEditorModel constructs one tab's editor model configured against cfg.
func newEditorModel(cfg host.Config, pal *theme.Palette) editor.Model {
	ed := editor.New()
	ed.SetPalette(pal)
	ed.Configure(cfg)
	if c := clipboard.System(); c != nil {
		ed.SetClipboard(c)
	}
	return ed
}

// setPalette re-threads the active theme palette into the wrapped component.
func (i *Instance) setPalette(p *theme.Palette) {
	i.pal = p
	switch i.kind {
	case KindExplorer:
		i.exp.SetPalette(p)
	case KindEditor:
		for _, t := range i.tabs {
			t.SetPalette(p)
		}
	case KindMarkdown:
		i.md.SetPalette(p)
	case KindDiff:
		i.df.SetPalette(p)
	case KindVCS:
		i.vp.SetPalette(p)
	}
}

// configure re-applies configuration to the wrapped component.
func (i *Instance) configure(cfg host.Config) {
	i.cfg = cfg
	switch i.kind {
	case KindExplorer:
		i.exp.Configure(cfg)
	case KindEditor:
		for _, t := range i.tabs {
			t.Configure(cfg)
		}
	}
}
