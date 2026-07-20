package pane

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/clipboard"
	"ike/internal/debugpanel"
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
	// KindDebug is the debug tool window (0350, #580): a singleton
	// bottom-split panel with the frames list and the variables tree,
	// under key "debug".
	KindDebug
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
	ctxDebug    = "debug"
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
	dp   debugpanel.Model
	// dfEdit is the diff pane's edit-mode editor (0340, #496): non-nil while
	// the right column is a live editor of the underlying file.
	dfEdit *editor.Model

	// Editor state: the ordered tab list and the active index. A tab holds a
	// document editor or an embedded terminal (#573). cfg/pal/size and focus
	// are remembered so tabs created later match the live pane.
	tabs    []*Tab
	active  int
	useSeq  int // monotonic activation counter stamping tab recency (#742)
	cfg     host.Config
	pal     *theme.Palette
	w, h    int
	focused bool

	// Box render cache (#612): the app hands CachedBox a signature that includes
	// a hash of the freshly-computed content plus the chrome. While the signature
	// is unchanged, the whole bordered box — the expensive lipgloss composition
	// (border, padding, per-line width measurement) — is reused. The content is
	// always recomputed and re-hashed by the caller, so the cache can never go
	// stale: it only skips re-composing an identical box.
	bxSig   BoxSig
	bxBox   string
	bxValid bool

	// View cache (#615): the active editor tab's rendered content, reused while
	// its RenderVersion and the active tab index are unchanged — so a pane the
	// user is not touching skips its View() recomputation entirely.
	cvView  string
	cvVer   uint64
	cvTab   int
	cvValid bool
}

// BoxSig is the render-cache key for a pane's bordered box. Content is captured
// by hash (of the freshly rendered content) rather than by a change flag, so two
// equal signatures render byte-identical.
type BoxSig struct {
	ContentHash uint64
	Title       string
	W, H        int
	Border      string // border color, hex
}

// CachedBox returns the pane's bordered box, running compute only when sig
// differs from the last render. compute must be a pure function of the same
// inputs sig captures.
func (i *Instance) CachedBox(sig BoxSig, compute func() string) string {
	if i.bxValid && i.bxSig == sig {
		return i.bxBox
	}
	i.bxBox = compute()
	i.bxSig = sig
	i.bxValid = true
	return i.bxBox
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
	case KindEditor:
		// An editor pane whose active tab is a terminal (#573) resolves
		// under the terminal context, so terminal bindings apply while it
		// owns the keystrokes.
		if t := i.activeTab(); t != nil && t.IsTerminal() {
			return ctxTerminal
		}
		return ctxEditor
	case KindTerminal:
		return ctxTerminal
	case KindMarkdown:
		return ctxPreview
	case KindDiff:
		return ctxDiff
	case KindVCS:
		return ctxVCS
	case KindDebug:
		return ctxDebug
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

// Debug returns the underlying debug tool-window model. It is only valid for
// a debug instance; callers gate on Kind first.
func (i *Instance) Debug() *debugpanel.Model { return &i.dp }

// DiffEditor returns the diff pane's edit-mode editor, nil while browsing.
func (i *Instance) DiffEditor() *editor.Model { return i.dfEdit }

// StartDiffEdit mounts ed as the diff pane's editable right column (#496):
// keys route into it, the left column re-aligns per keystroke.
func (i *Instance) StartDiffEdit(ed *editor.Model) {
	if i.kind != KindDiff || ed == nil {
		return
	}
	i.dfEdit = ed
	i.df.SetEditMode(true)
	i.sizeDiffEditor()
	ed.SetFocused(i.focused)
	i.df.Rediff(ed.Text())
}

// StopDiffEdit returns the pane to read-only browsing; the last buffer state
// stays diffed.
func (i *Instance) StopDiffEdit() {
	if i.dfEdit == nil {
		return
	}
	i.df.Rediff(i.dfEdit.Text())
	i.dfEdit = nil
	i.df.SetEditMode(false)
}

// sizeDiffEditor fits the embedded editor into the split's right column.
func (i *Instance) sizeDiffEditor() {
	if i.dfEdit == nil {
		return
	}
	_, right := i.df.EditSplitWidths()
	i.dfEdit.SetSize(right, i.h)
}

// Editor returns the active tab's editor model. It is only valid for an editor
// instance; callers gate on Kind first. It is nil when the active tab hosts a
// terminal (#573), so callers must nil-check before dereferencing.
func (i *Instance) Editor() *editor.Model {
	if t := i.activeTab(); t != nil {
		return t.Editor()
	}
	return nil
}

// activeTab returns the active tab slot, or nil when no tabs exist.
func (i *Instance) activeTab() *Tab {
	if len(i.tabs) == 0 {
		return nil
	}
	return i.tabs[i.active]
}

// Tab returns the tab slot at idx, or nil when out of range.
func (i *Instance) Tab(idx int) *Tab {
	if idx < 0 || idx >= len(i.tabs) {
		return nil
	}
	return i.tabs[idx]
}

// TabTerminal returns the terminal model of tab idx, nil for editor tabs or
// an out-of-range index.
func (i *Instance) TabTerminal(idx int) *terminal.Model {
	if t := i.Tab(idx); t != nil {
		return t.Terminal()
	}
	return nil
}

// ActiveTerminal returns the terminal the instance's input currently reaches:
// the wrapped terminal for a terminal pane, the active tab's terminal for an
// editor pane hosting one (#573), nil otherwise.
func (i *Instance) ActiveTerminal() *terminal.Model {
	if i.kind == KindTerminal {
		return &i.term
	}
	if i.kind != KindEditor {
		return nil
	}
	if t := i.activeTab(); t != nil {
		return t.Terminal()
	}
	return nil
}

// TabCount reports how many tabs the editor instance holds (0 for explorers).
func (i *Instance) TabCount() int { return len(i.tabs) }

// IsEmptyEditor reports whether this pane is a reusable blank editor: a single
// editor tab that is empty per editor.Model.IsEmpty (no file, no text) — the
// shared emptiness predicate of the file-open and diff-open paths (#628, #641).
// Opening a file or a diff can take over such a pane in place instead of
// splitting a new one. A pathless tab that already holds typed scratch text is
// not reusable — its content would be lost.
func (i *Instance) IsEmptyEditor() bool {
	if i.kind != KindEditor || len(i.tabs) != 1 {
		return false
	}
	ed := i.Editor()
	return ed != nil && ed.IsEmpty()
}

// ActiveTab returns the index of the active tab.
func (i *Instance) ActiveTab() int { return i.active }

// TabEditor returns the editor model of tab idx, nil when out of range or
// when that tab hosts a terminal (#573).
func (i *Instance) TabEditor(idx int) *editor.Model {
	if idx < 0 || idx >= len(i.tabs) {
		return nil
	}
	return i.tabs[idx].Editor()
}

// Editors returns every editor tab's model in tab order; terminal tabs are
// skipped. Callers that iterate "all documents of this pane" (emitters,
// autosave sweeps, backup drops) use this instead of Editor, which only sees
// the active tab.
func (i *Instance) Editors() []*editor.Model {
	out := make([]*editor.Model, 0, len(i.tabs))
	for _, t := range i.tabs {
		if ed := t.Editor(); ed != nil {
			out = append(out, ed)
		}
	}
	return out
}

// TabForPath returns the index of the first tab showing path, or -1.
func (i *Instance) TabForPath(path string) int {
	for idx, t := range i.tabs {
		if ed := t.Editor(); ed != nil && ed.HasFile() && ed.Path() == path {
			return idx
		}
	}
	return -1
}

// EditorForPath returns the first tab's editor model showing path, or nil.
func (i *Instance) EditorForPath(path string) *editor.Model {
	if idx := i.TabForPath(path); idx >= 0 {
		return i.tabs[idx].Editor()
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
	i.tabs = append(i.tabs, newEditorTab(&ed))
	i.activate(len(i.tabs) - 1)
	return i.tabs[i.active].Editor()
}

// AddTerminalTab appends a tab hosting term, makes it active, and returns the
// hosted model (#573): run output opens next to the file tabs. Only valid on
// editor instances.
func (i *Instance) AddTerminalTab(term terminal.Model) *terminal.Model {
	if i.kind != KindEditor {
		return nil
	}
	term.SetPalette(i.pal)
	term.SetSize(i.w, i.h)
	i.tabs = append(i.tabs, newTerminalTab(&term))
	i.activate(len(i.tabs) - 1)
	return i.tabs[i.active].Terminal()
}

// DetachTerminal hands the live terminal model to the caller and leaves the
// instance with a session-less placeholder, so a following registry Close no
// longer ends the moved shell (#708): a terminal pane dropped on an editor's
// center zone becomes a terminal tab there. Only valid on terminal instances.
func (i *Instance) DetachTerminal() (terminal.Model, bool) {
	if i.kind != KindTerminal {
		return terminal.Model{}, false
	}
	t := i.term
	i.term = terminal.Model{}
	return t, true
}

// CloseTerminalTabs ends every terminal tab's session; the tab slots stay (a
// registry Close drops the whole instance right after, a project switch just
// stops the shells the new workspace does not carry over).
func (i *Instance) CloseTerminalTabs() {
	for _, t := range i.tabs {
		t.close()
	}
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

// activate switches the active index and re-asserts per-tab focus flags. The
// activated tab gets the next use-sequence stamp — the recency the tab-limit
// eviction orders by (#742).
func (i *Instance) activate(idx int) {
	i.active = idx
	if idx >= 0 && idx < len(i.tabs) {
		i.useSeq++
		i.tabs[idx].lastUsed = i.useSeq
	}
	for n, t := range i.tabs {
		t.setFocused(i.focused && n == i.active)
	}
}

// FileTabCount counts the pane's document tabs — terminal tabs (#573) are
// exempt from the tab limit (#742).
func (i *Instance) FileTabCount() int {
	n := 0
	for _, t := range i.tabs {
		if !t.IsTerminal() {
			n++
		}
	}
	return n
}

// EvictableLRUTab returns the least recently used tab the tab limit may close
// (#742): a file-backed, non-dirty document tab that is not active — dirty
// tabs, scratch tabs (nothing to reopen from) and terminals are exempt.
// ok=false when no tab is eligible, in which case the limit may be exceeded.
func (i *Instance) EvictableLRUTab() (idx int, ok bool) {
	best := -1
	for n, t := range i.tabs {
		if n == i.active || t.IsTerminal() {
			continue
		}
		ed := t.Editor()
		if ed == nil || !ed.HasFile() || ed.Dirty() {
			continue
		}
		if best < 0 || t.lastUsed < i.tabs[best].lastUsed {
			best = n
		}
	}
	return best, best >= 0
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
	rest := append([]*Tab{}, i.tabs[to:]...)
	i.tabs = append(append(i.tabs[:to:to], t), rest...)
	for n, tab := range i.tabs {
		if tab == activeTab {
			i.active = n
			break
		}
	}
	return true
}

// DetachTerminalTab removes tab idx without ending its session and returns
// the hosted terminal model (#707): a dragged terminal tab moves into another
// pane or splits off as its own terminal pane. Valid only on editor instances
// holding more than one tab with a terminal at idx.
func (i *Instance) DetachTerminalTab(idx int) (terminal.Model, bool) {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) || len(i.tabs) == 1 || !i.tabs[idx].IsTerminal() {
		return terminal.Model{}, false
	}
	t := *i.tabs[idx].term
	i.tabs = append(i.tabs[:idx], i.tabs[idx+1:]...)
	switch {
	case i.active > idx:
		i.active--
	case i.active == idx && i.active >= len(i.tabs):
		i.active = len(i.tabs) - 1
	}
	i.activate(i.active)
	return t, true
}

// CloseTab removes tab idx. The neighbour that slides into its position becomes
// active when the active tab itself closes (the last position falls back to its
// left neighbour). Closing the only tab is refused — the caller closes the pane
// instead, so an editor instance never exists with zero tabs.
func (i *Instance) CloseTab(idx int) bool {
	if i.kind != KindEditor || idx < 0 || idx >= len(i.tabs) || len(i.tabs) == 1 {
		return false
	}
	i.tabs[idx].close() // a terminal tab's session ends with its tab (#573)
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
			t.setSize(w, h)
		}
	case KindTerminal:
		i.term.SetSize(w, h)
	case KindMarkdown:
		i.md.SetSize(w, h)
	case KindDiff:
		i.w, i.h = w, h
		i.df.SetSize(w, h)
		i.sizeDiffEditor()
	case KindVCS:
		i.vp.SetSize(w, h)
	case KindDebug:
		i.dp.SetSize(w, h)
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
			t.setFocused(f && n == i.active)
		}
	case KindTerminal:
		i.term.SetFocused(f)
	case KindMarkdown:
		i.md.SetFocused(f)
	case KindDiff:
		i.df.SetFocused(f)
		if i.dfEdit != nil {
			i.dfEdit.SetFocused(f)
		}
	case KindVCS:
		i.vp.SetFocused(f)
	case KindDebug:
		i.dp.SetFocused(f)
	}
}

// View renders the wrapped component's content (without pane chrome).
func (i *Instance) View() string {
	switch i.kind {
	case KindExplorer:
		return i.exp.View()
	case KindEditor:
		t := i.activeTab()
		if t == nil {
			return ""
		}
		if t.IsTerminal() {
			return t.view() // live terminal output — never cached
		}
		// Skip recomputing the editor's View when nothing it renders changed
		// (#615): a scroll of another pane, or an idle frame, reuses the cached
		// string. RenderVersion is a complete identity of everything View draws,
		// so this can never serve a stale frame.
		ver := t.ed.RenderVersion()
		if i.cvValid && i.cvTab == i.active && i.cvVer == ver {
			return i.cvView
		}
		v := t.view()
		i.cvView, i.cvVer, i.cvTab, i.cvValid = v, ver, i.active, true
		return v
	case KindTerminal:
		return i.term.View()
	case KindMarkdown:
		return i.md.View()
	case KindDiff:
		if i.dfEdit != nil {
			lines := strings.Split(i.dfEdit.View(), "\n")
			return i.df.RenderEditSplit(lines, i.dfEdit.ScrollTop(), i.h)
		}
		return i.df.View()
	case KindVCS:
		return i.vp.View()
	case KindDebug:
		return i.dp.View()
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
		cmd = i.tabs[i.active].update(msg)
	case KindTerminal:
		if k, ok := msg.(tea.KeyPressMsg); ok {
			cmd = i.term.Update(k)
		}
	case KindMarkdown:
		cmd = i.md.Update(msg)
	case KindDiff:
		if i.dfEdit != nil {
			// Edit mode (#496): keys belong to the embedded editor; ctrl+e
			// returns to read-only browsing. The left column re-aligns from
			// a re-diff after every message.
			if k, ok := msg.(tea.KeyPressMsg); ok && k.String() == "ctrl+e" {
				i.StopDiffEdit()
				return nil
			}
			ed := i.dfEdit
			*ed, cmd = ed.Update(msg)
			i.df.Rediff(ed.Text())
			return cmd
		}
		cmd = i.df.Update(msg)
	case KindVCS:
		cmd = i.vp.Update(msg)
	case KindDebug:
		cmd = i.dp.Update(msg)
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
		ed := t.Editor()
		if ed == nil || ed == skip || !ed.HasFile() || ed.Path() != path {
			continue
		}
		if cmd := t.update(msg); cmd != nil {
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
	return i.tabs[idx].update(msg)
}

// Init returns the wrapped component's initialisation command.
func (i *Instance) Init() tea.Cmd {
	switch i.kind {
	case KindExplorer:
		return i.exp.Init()
	case KindEditor:
		if ed := i.Editor(); ed != nil {
			return ed.Init()
		}
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
		i.tabs = []*Tab{newEditorTab(&ed)}
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
			t.setPalette(p)
		}
	case KindMarkdown:
		i.md.SetPalette(p)
	case KindDiff:
		i.df.SetPalette(p)
	case KindVCS:
		i.vp.SetPalette(p)
	case KindDebug:
		i.dp.SetPalette(p)
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
			t.configure(cfg)
		}
	}
}
