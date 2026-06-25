// Package app wires the root bubbletea model for IKE: a dynamic tiled workspace
// that hosts the file explorer and N editor panes, owns focus and layout, routes
// the explorer's open-file message to the active editor (or a fresh split), and
// renders the status line. The pane set itself is dynamic (Roadmap 0037): a
// pane.Registry maps each layout leaf key to a live component instance, and focus
// is "the focused leaf" rather than a two-value enum.
package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/help"
	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/layout"
	"ike/internal/overlay"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/ui"
)

// Context ids the core panes advertise for context-scoped command/keymap
// resolution. Plugin panes advertise their own via plugin.ContextProvider.
const (
	ctxExplorer = "explorer"
	ctxEditor   = "editor"
)

const (
	explorerWidth = 30 // outer width of the explorer pane (border included)
	statusHeight  = 1
	paneChrome    = 4 // border (2) + padding (2) per pane, horizontal and vertical-ish
	paneContentX  = 2 // left border (1) + left padding (1) before pane content
	paneContentY  = 2 // top border (1) + title row (1) before pane content
	wheelLines    = 3 // rows a single mouse-wheel notch scrolls
)

// Model is the root model.
type Model struct {
	width  int
	height int
	// panes is the registry of live pane instances (Roadmap 0037). It replaces the
	// two hard-coded explorer/editor fields and the two-value focus enum: focus is
	// the registry's focused key, which always names a layout leaf.
	panes *pane.Registry
	// recentEditor is the key of the most-recently-focused editor, used as the
	// Replace open-target when the explorer (not an editor) holds focus.
	recentEditor string
	host         *host.Host
	reg          *registry.Registry
	help         *help.Help
	// shell is the single active floating overlay (Roadmap 0035).
	shell *ui.Floating
	// palette is the command palette overlay (Roadmap 0070): a modal input that
	// fronts registered commands (":") and file search ("@"). paletteKey is the
	// default key that opens it (the final binding is Roadmap 0080's).
	palette    *palette.Palette
	paletteKey string
	// lastEsc records that the previous key was an esc in a non-capturing context,
	// so a second esc opens the palette (esc-esc toggle).
	lastEsc bool
	// tree is the pure split-tree layout (Roadmap 0036/0037). Leaves are instance
	// keys resolved through panes.
	tree layout.Node
	// lay caches the rectangles + dividers computed from tree for the current
	// viewport, so mouse hit-testing and rendering share one geometry.
	lay layout.Layout
	// drag is the active mouse gesture (resize or move), nil between drags.
	drag *dragState
	// pendingScroll holds an editor viewport offset restored from a session that
	// must be applied once the editor has been sized (the first layout). Cleared
	// after it is applied. It targets the focused editor at restore time.
	pendingScroll *editorScroll
	// splitZone is the default orientation SplitFocused and explorer "open in new
	// pane" use, read once from config.
	splitZone layout.Zone
	// focusKeys maps a key string to the focus-move direction it triggers, built
	// once from config (keymap.bindings.focus_{left,right,up,down}) with Ctrl+arrow
	// defaults. Roadmap 0080 owns the final keymap; this is the binding-agnostic op
	// wired to a configurable default.
	focusKeys map[string]Direction
	// keys is the JetBrains-flavoured keybinding resolver (Roadmap 0080). It maps
	// IDE-level chords (in the focused pane's context) to registered command ids;
	// unbound or inert chords fall through to the existing dispatch.
	keys *keymap.Resolver
}

// editorScroll is a restored viewport framing awaiting the first layout.
type editorScroll struct {
	key  string
	top  int
	left int
}

// dragKind distinguishes the two mouse gestures.
type dragKind int

const (
	dragResize dragKind = iota // dragging a divider to change a split ratio
	dragMove                   // dragging a pane title bar to relocate or spawn
)

// dragState holds the in-flight mouse gesture. For a resize it carries the
// divider being dragged; for a move it carries the source leaf key. curX/curY
// track the latest mouse cell so the move can render live feedback (which pane
// and drop zone the release would target, and whether it spawns or relocates).
type dragState struct {
	kind    dragKind
	divider layout.Divider
	srcPane string
	curX    int
	curY    int
}

// New returns the initial root model rooted at the working directory, wired to
// the global plugin registry. It loads the merged configuration (defaults < user
// < project) from the working directory and backs the host with it.
func New() Model {
	cfg, _ := config.Load(config.Discover("."))
	config.Set(cfg)
	return NewWith(registry.Global(), host.FromConfig(cfg))
}

// NewWith returns a root model backed by an explicit registry and config. It
// applies per-plugin enable/disable flags before the registry is queried, builds
// the pane registry (explorer singleton + one editor), then restores any saved
// layout and session.
func NewWith(reg *registry.Registry, cfg host.Config) Model {
	applyPluginConfig(reg, cfg)
	panes := pane.NewRegistry(cfg)
	panes.AddExplorer()
	edKey := panes.AddEditor()
	panes.SetFocused(pane.ExplorerKey)
	m := Model{
		panes:        panes,
		recentEditor: edKey,
		host:         host.New(cfg),
		reg:          reg,
		help:         help.New(reg, reg, helpMinCol(cfg)),
		shell:        ui.New(shellConfig(cfg)),
		palette:      buildPalette(reg, cfg),
		paletteKey:   paletteToggleKey(cfg),
		splitZone:    splitZone(cfg),
		focusKeys:    focusKeys(cfg),
		keys:         buildKeymap(cfg),
	}
	// Restore a saved per-project layout if one is structurally sound; an unknown
	// or stale layout is dropped and the default is built on first size.
	m.restoreLayout(cfg)
	m.restoreSession()
	return m
}

// restoreLayout rebuilds the registry and tree from the saved layout store. A
// valid save with the explorer present exactly once replaces the default
// explorer+editor set: every non-explorer leaf becomes an editor whose file is
// reloaded best-effort (a missing file restores as an empty editor). Any
// structural breakage or a missing/duplicate explorer leaves the default intact.
func (m *Model) restoreLayout(cfg host.Config) {
	tree, ids, ok := loadLayout()
	if !ok {
		return
	}
	leaves := layout.Leaves(tree)
	explorers := 0
	for _, key := range leaves {
		if key == pane.ExplorerKey {
			explorers++
		} else if !isEditorKey(key) {
			return // unknown leaf kind / malformed key: fall back to default
		}
	}
	if explorers != 1 {
		return // explorer must be present exactly once
	}
	// The default set is replaced: a fresh registry with the explorer plus one
	// editor per non-explorer leaf, each loading its remembered file.
	panes := pane.NewRegistry(cfg)
	panes.AddExplorer()
	for _, key := range leaves {
		if key == pane.ExplorerKey {
			continue
		}
		inst := panes.AddEditorKey(key)
		if id, hasID := ids[key]; hasID && id.Path != "" {
			_ = inst.Editor().Load(id.Path) // best-effort: missing file → empty editor
		}
	}
	panes.SetFocused(pane.ExplorerKey)
	m.panes = panes
	m.recentEditor = firstEditorKey(leaves)
	m.tree = tree
}

// firstEditorKey returns the first editor leaf key in walk order, or "".
func firstEditorKey(leaves []string) string {
	for _, key := range leaves {
		if key != pane.ExplorerKey {
			return key
		}
	}
	return ""
}

// restoreSession re-applies the saved workspace: explorer expansion / hidden
// toggle / cursor, and the active editor's open file and cursor position. When
// the layout restore already reopened editors, the session only refocuses the
// editor holding the saved file and re-applies its cursor framing; otherwise it
// loads the saved file into the default editor (the 0095 single-editor path).
func (m *Model) restoreSession() {
	s, ok := loadSession()
	if !ok {
		return
	}
	m.explorer().Restore(explorer.State{
		Expanded:   s.Explorer.Expanded,
		ShowHidden: s.Explorer.ShowHidden,
		Cursor:     s.Explorer.Cursor,
	})
	if s.Editor != nil && s.Editor.Path != "" {
		key := m.editorWithFile(s.Editor.Path)
		if key == "" {
			// No layout-restored editor holds the file: load it into the active
			// editor (fresh launch, the common case).
			key = m.activeEditorKey()
			if key == "" {
				key = m.spawnEditor()
			}
			if err := m.panes.Get(key).Editor().Load(s.Editor.Path); err != nil {
				key = ""
			}
		}
		if key != "" {
			ed := m.panes.Get(key).Editor()
			ed.SetCursor(s.Editor.Line, s.Editor.Col)
			// Defer the viewport framing until the editor is sized.
			m.pendingScroll = &editorScroll{key: key, top: s.Editor.Top, left: s.Editor.Left}
			m.explorer().SetActive(s.Editor.Path)
			m.setFocus(key)
		}
	}
	m.syncFocus()
}

// editorWithFile returns the key of an editor instance currently holding path,
// or "" if none does.
func (m Model) editorWithFile(path string) string {
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst.Kind() == pane.KindEditor && inst.Editor().HasFile() && inst.Editor().Path() == path {
			return key
		}
	}
	return ""
}

// snapshotSession captures the active editor + explorer state for persistence.
func (m Model) snapshotSession() sessionState {
	st := m.explorer().Snapshot()
	s := sessionState{
		Explorer: explorerSession{
			Expanded:   st.Expanded,
			ShowHidden: st.ShowHidden,
			Cursor:     st.Cursor,
		},
	}
	if key := m.activeEditorKey(); key != "" {
		ed := m.panes.Get(key).Editor()
		if ed.HasFile() {
			line, col := ed.CursorPos()
			top, left := ed.ScrollOffset()
			s.Editor = &editorSession{Path: ed.Path(), Line: line, Col: col, Top: top, Left: left}
		}
	}
	return s
}

// quit persists the session and layout and returns the program-exit command.
func (m Model) quit() (tea.Model, tea.Cmd) {
	saveSession(m.snapshotSession())
	if m.tree != nil {
		saveLayout(m.tree, m.panes)
	}
	return m, tea.Quit
}

// shellConfig builds the floating shell configuration, reading optional tuning
// keys (margin, max width/height fraction) from cfg.
func shellConfig(cfg host.Config) ui.Config {
	c := ui.Config{
		DismissKeys: []string{"esc", "?", "f1", "q"},
		Accent:      "69",
	}
	if cfg == nil {
		return c
	}
	if v, ok := cfg.Get("overlay.margin"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			c.Margin = n
		}
	}
	if v, ok := cfg.Get("overlay.max_width_fraction"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.MaxWidthFrac = f
		}
	}
	if v, ok := cfg.Get("overlay.max_height_fraction"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.MaxHeightFrac = f
		}
	}
	return c
}

// splitZone reads the default split orientation (config "panes.split_zone":
// left/right/top/bottom), defaulting to a split-right.
func splitZone(cfg host.Config) layout.Zone {
	if cfg != nil {
		if v, ok := cfg.Get("panes.split_zone"); ok {
			switch strings.ToLower(v) {
			case "left":
				return layout.ZoneLeft
			case "top":
				return layout.ZoneTop
			case "bottom":
				return layout.ZoneBottom
			}
		}
	}
	return layout.ZoneRight
}

// focusKeys builds the key→direction map for spatial focus moves. Defaults are
// Ctrl+arrows (which terminals reliably deliver, unlike Cmd); each is overridable
// via keymap.bindings.focus_{left,right,up,down}. An empty override disables that
// direction's binding.
func focusKeys(cfg host.Config) map[string]Direction {
	defaults := map[Direction]string{
		DirLeft:  "ctrl+left",
		DirRight: "ctrl+right",
		DirUp:    "ctrl+up",
		DirDown:  "ctrl+down",
	}
	names := map[Direction]string{
		DirLeft:  "keymap.bindings.focus_left",
		DirRight: "keymap.bindings.focus_right",
		DirUp:    "keymap.bindings.focus_up",
		DirDown:  "keymap.bindings.focus_down",
	}
	out := map[string]Direction{}
	for dir, def := range defaults {
		key := def
		if cfg != nil {
			if v, ok := cfg.Get(names[dir]); ok {
				key = strings.TrimSpace(v)
			}
		}
		if key != "" {
			out[key] = dir
		}
	}
	return out
}

// keymapTimeoutMsg fires when a held partial multi-step chord exceeds the
// resolver timeout, so the root model can resolve or discard it.
type keymapTimeoutMsg struct{}

// resolveKeymap feeds one key to the keybinding resolver in the focused context.
// It returns (cmd, true) when the key is consumed by the keymap layer — either a
// resolved command to run or a partial chord to wait on — and (nil, false) when
// the key should fall through to the existing dispatch (no match, or an inert
// binding whose command id is not registered).
func (m *Model) resolveKeymap(k keymap.Key) (tea.Cmd, bool) {
	res := m.keys.Feed(k, keymap.Context(m.focusContext()))
	switch res.Status {
	case keymap.Pending:
		// Hold the partial chord and arm the timeout; swallow the key meanwhile.
		return tea.Tick(keymap.TimeoutDuration, func(time.Time) tea.Msg {
			return keymapTimeoutMsg{}
		}), true
	case keymap.Resolved:
		if c, ok := m.reg.Command(res.Command); ok {
			return c.Run(m.host), true
		}
	}
	return nil, false
}

// buildKeymap constructs the keybinding resolver from config: the preset
// (keymap.preset, default JetBrains) overlaid by keymap.bindings.* overrides.
// Non-chord override keys (the focus_* stopgap sharing the same map) are ignored
// by the table builder.
func buildKeymap(cfg host.Config) *keymap.Resolver {
	preset := keymap.PresetJetBrains
	overrides := map[string]string{}
	if cfg != nil {
		if v, ok := cfg.Get("keymap.preset"); ok {
			if p := strings.TrimSpace(v); p != "" {
				preset = p
			}
		}
		const pfx = "keymap.bindings."
		for _, key := range cfg.Keys() {
			if strings.HasPrefix(key, pfx) {
				if v, ok := cfg.Get(key); ok {
					overrides[strings.TrimPrefix(key, pfx)] = v
				}
			}
		}
	}
	table := keymap.BuildTable(keymap.Defaults(preset), overrides, keymap.GOOS)
	return keymap.NewResolver(table)
}

// buildPalette wires the command palette: a ":" command mode reading the registry
// and an "@" file finder, tuned by the optional palette.* config keys.
func buildPalette(reg *registry.Registry, cfg host.Config) *palette.Palette {
	pcfg := palette.Config{
		MaxResults:    paletteMaxResults(cfg),
		DefaultPrefix: paletteDefaultPrefix(cfg),
		Accent:        colorPaneFocus,
	}
	cmd := palette.NewCommandMode(reg, reg, paletteHideOff(cfg))
	file := palette.NewFileMode()
	return palette.New(pcfg, cmd, file)
}

// paletteMaxResults reads palette.max_results (rows shown), 0 if unset/invalid.
func paletteMaxResults(cfg host.Config) int {
	if cfg == nil {
		return 0
	}
	if v, ok := cfg.Get("palette.max_results"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// paletteDefaultPrefix reads palette.default_mode and returns its first rune, or
// 0 to let the palette default to its first mode.
func paletteDefaultPrefix(cfg host.Config) rune {
	if cfg == nil {
		return 0
	}
	if v, ok := cfg.Get("palette.default_mode"); ok {
		if r := []rune(strings.TrimSpace(v)); len(r) > 0 {
			return r[0]
		}
	}
	return 0
}

// paletteHideOff reports whether command mode hides off-context commands
// (palette.off_context == "hide") rather than ranking them last.
func paletteHideOff(cfg host.Config) bool {
	if cfg == nil {
		return false
	}
	if v, ok := cfg.Get("palette.off_context"); ok {
		return strings.EqualFold(strings.TrimSpace(v), "hide")
	}
	return false
}

// paletteToggleKey reads palette.toggle_key, defaulting to ctrl+p.
func paletteToggleKey(cfg host.Config) string {
	if cfg != nil {
		if v, ok := cfg.Get("palette.toggle_key"); ok {
			if k := strings.TrimSpace(v); k != "" {
				return k
			}
		}
	}
	return "ctrl+p"
}

// helpMinCol reads the optional help.min_column_width config value.
func helpMinCol(cfg host.Config) int {
	if cfg == nil {
		return 0
	}
	if v, ok := cfg.Get("help.min_column_width"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// applyPluginConfig reads "plugins.<id>.enabled" toggles.
func applyPluginConfig(reg *registry.Registry, cfg host.Config) {
	if cfg == nil {
		return
	}
	for _, id := range reg.PluginIDs() {
		if v, ok := cfg.Get("plugins." + id + ".enabled"); ok && v == "false" {
			reg.SetEnabled(id, false)
		}
	}
}

// explorer returns the singleton explorer model.
func (m Model) explorer() *explorer.Model {
	return m.panes.Get(pane.ExplorerKey).Explorer()
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return m.explorer().Init() }

// Update owns global keys (quit, focus switch), routes open/close messages, and
// forwards everything else to the focused pane.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.shell.SetSize(m.width, m.height)
		m.palette.SetSize(m.width, m.height)
		return m, nil

	case tea.MouseClickMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mousePress})
	case tea.MouseReleaseMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mouseRelease})
	case tea.MouseMotionMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mouseMotion})
	case tea.MouseWheelMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mouseWheel})

	case explorer.OpenFileMsg:
		return m.openPath(msg.Path, msg.NewPane)

	case explorer.FileDeletedMsg:
		// The explorer removed a path; close any editor still showing it so a
		// deleted file does not linger in an open pane.
		m.closeEditorsForPath(msg.Path, msg.IsDir)
		return m, nil

	case explorer.Msg:
		exp := m.explorer()
		var cmd tea.Cmd
		*exp, cmd = exp.Update(msg)
		return m, cmd

	case host.OpenFileRequest:
		return m.openPath(msg.Path, msg.NewPane)

	case palette.RunCommandMsg:
		return m, m.RunCommand(msg.ID)

	case palette.OpenFileMsg:
		return m.openPath(msg.Path, false)

	case host.OpenModalRequest:
		m.shell.SetContent(ui.ModelContent{Heading: msg.Title, Body: msg.View})
		m.shell.SetSize(m.width, m.height)
		m.shell.Open()
		return m, nil

	case editor.ActionMsg:
		// A registry command drives the focused editor through this message path.
		if key := m.activeEditorKey(); key != "" {
			cmd := m.panes.Get(key).Update(msg)
			return m, cmd
		}
		return m, nil

	case editor.CloseMsg:
		// :q / :wq closes the focused editor leaf, mirroring CloseFocused.
		m.closeFocused()
		return m, nil

	case keymapTimeoutMsg:
		// A held partial chord timed out: resolve it as an exact binding if one
		// exists (e.g. cmd+k alone → vcs.commit), else discard it.
		if res := m.keys.Timeout(keymap.Context(m.focusContext())); res.Status == keymap.Resolved {
			if c, ok := m.reg.Command(res.Command); ok {
				return m, c.Run(m.host)
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.palette.IsOpen() {
			return m, m.palette.Update(msg)
		}
		if m.shell.IsOpen() {
			m.shell.Update(msg)
			return m, nil
		}
		// A focused explorer with an open prompt (new-file name entry, delete/undo
		// confirmation) captures every key, ahead of the keymap and global layers,
		// so typed names and y/n answers reach the prompt intact.
		if m.explorerCapturing() {
			return m.routeKey(msg)
		}
		keys := msg.String()
		if keys == m.paletteKey {
			m.lastEsc = false
			m.openPalette()
			return m, nil
		}
		// esc-esc opens the palette from a non-capturing context; the first esc is
		// still forwarded (clears selection, etc.).
		if keys == "esc" && !m.editorCapturing() {
			if m.lastEsc {
				m.lastEsc = false
				m.openPalette()
				return m, nil
			}
			m.lastEsc = true
			return m.routeKey(msg)
		}
		m.lastEsc = false
		// "@" in an editor's normal mode opens a slimmed, file-only palette floated
		// over that editor pane.
		if keys == "@" && m.editorNormalMode() {
			m.openFilePaletteAnchored()
			return m, nil
		}
		// Keybinding layer (Roadmap 0080): resolve IDE-level chords to registered
		// commands before pane dispatch. In a text-capturing editor only modified
		// chords (or a chord already in progress) are eligible; plain letters always
		// reach the editor. Inert/unbound chords fall through unchanged.
		if k, ok := keymap.FromKeyMsg(msg); ok {
			eligible := !m.editorCapturing() ||
				k.Has(keymap.ModCtrl) || k.Has(keymap.ModAlt) || k.Has(keymap.ModMeta) ||
				m.keys.Pending()
			if eligible {
				if cmd, handled := m.resolveKeymap(k); handled {
					return m, cmd
				}
			}
		}
		if m.editorCapturing() {
			return m.routeKey(msg)
		}
		if keys == "?" || keys == "f1" {
			m.help.Snapshot()
			m.shell.SetContent(m.help)
			m.shell.SetSize(m.width, m.height)
			m.shell.Open()
			return m, nil
		}
		if k, ok := m.reg.ResolveKey(keys, m.focusContext()); ok {
			if k.Priority > plugin.CorePriority || !m.isCoreKey(keys) {
				return m, k.Action(m.host)
			}
		}
		if dir, ok := m.focusKeys[keys]; ok {
			m.FocusDir(dir)
			return m, nil
		}
		switch keys {
		case "ctrl+c":
			return m.quit()
		case "q":
			if m.quitKey() {
				return m.quit()
			}
		case "tab":
			m.cycleFocus()
			return m, nil
		case "ctrl+w":
			// Close the focused editor pane (no-op on the explorer / last leaf).
			// Roadmap 0080 owns the final keymap; this is the default binding.
			m.CloseFocused()
			return m, nil
		}
		return m.routeKey(msg)
	}
	return m, nil
}

// openPath opens path honouring the open target: a registered FileHandler claims
// it first regardless of target; otherwise Replace loads into the active editor
// and NewPane splits off a fresh editor and loads there. EventFileOpened hooks
// fire either way.
func (m Model) openPath(path string, newPane bool) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if h, ok := m.reg.ResolveHandler(path, readHead(path)); ok {
		cmds = append(cmds, h.Open(m.host, path))
	} else {
		key := m.activeEditorKey()
		if newPane || key == "" {
			key = m.spawnEditor()
		}
		if err := m.panes.Get(key).Editor().Load(path); err == nil {
			m.explorer().SetActive(path)
			m.setFocus(key)
			m.layout()
			saveLayout(m.tree, m.panes)
		}
	}
	cmds = append(cmds, m.fireHooks(plugin.EventFileOpened, path)...)
	return m, tea.Batch(cmds...)
}

// fireHooks invokes every enabled hook subscribed to event.
func (m Model) fireHooks(event plugin.Event, payload any) []tea.Cmd {
	var cmds []tea.Cmd
	for _, h := range m.reg.Hooks(event) {
		if c := h.Notify(m.host, payload); c != nil {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

// RunCommand looks up and runs a registered command by id.
func (m Model) RunCommand(id string) tea.Cmd {
	if c, ok := m.reg.Command(id); ok {
		return c.Run(m.host)
	}
	return nil
}

// openPalette shows the centered command palette for the focused pane's context,
// rooted at the working directory for file search.
func (m *Model) openPalette() {
	m.palette.SetSize(m.width, m.height)
	m.palette.Open(palette.Context{ContextID: m.focusContext(), Root: "."})
}

// openFilePaletteAnchored opens the slimmed file-only palette floated over the
// focused editor pane (its top-left interior), falling back to the centered
// palette if the pane has no computed rectangle yet.
func (m *Model) openFilePaletteAnchored() {
	m.palette.SetSize(m.width, m.height)
	r, ok := m.lay.Panes[m.panes.Focused()]
	if !ok {
		m.openPalette()
		return
	}
	cx := palette.Context{ContextID: m.focusContext(), Root: "."}
	m.palette.OpenAnchored(cx, '@', r.X+1, r.Y+1, r.W-2)
}

// editorNormalMode reports whether the focused pane is an editor in normal mode
// (not capturing text), the context in which "@" opens the file finder.
func (m Model) editorNormalMode() bool {
	inst := m.panes.FocusedInstance()
	return inst != nil && inst.Kind() == pane.KindEditor &&
		inst.Editor().ModeName() == editor.Normal
}

// focusContext reports the context id advertised by the focused pane.
func (m Model) focusContext() string {
	if inst := m.panes.FocusedInstance(); inst != nil {
		return inst.ContextID()
	}
	return ctxExplorer
}

// isCoreKey reports whether keys is handled by a core binding in the current
// focus, so a plugin must out-prioritise it to take over.
func (m Model) isCoreKey(keys string) bool {
	if _, ok := m.focusKeys[keys]; ok {
		return true
	}
	switch keys {
	case "ctrl+c", "tab", "ctrl+w":
		return true
	case "q":
		return m.quitKey()
	}
	return false
}

// quitKey reports whether "q" should quit in the current focus: from the
// explorer, or from an editor while in normal mode (not typing into a file).
func (m Model) quitKey() bool {
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() == pane.KindExplorer {
		return true
	}
	return inst.Editor().ModeName() == editor.Normal
}

// readHead returns the leading bytes of path for content sniffing, or nil.
func readHead(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return buf[:n]
}

// editorCapturing reports whether the focused pane is an editor in a
// text-capturing mode, in which case global single-letter keys are not stolen.
func (m Model) editorCapturing() bool {
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		return false
	}
	return inst.Editor().Capturing()
}

// explorerCapturing reports whether the focused pane is the explorer with an
// open modal prompt, in which case keys go straight to it (see Update).
func (m Model) explorerCapturing() bool {
	inst := m.panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindExplorer {
		return false
	}
	return inst.Explorer().Prompting()
}

// routeKey forwards a key to the focused pane.
func (m Model) routeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	inst := m.panes.FocusedInstance()
	if inst == nil {
		return m, nil
	}
	cmd := inst.Update(msg)
	return m, cmd
}

// activeEditorKey returns the editor that should receive a Replace open or an
// editor action: the focused editor, else the most-recent editor, else the first
// editor in tree order, else "".
func (m Model) activeEditorKey() string {
	if inst := m.panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor {
		return m.panes.Focused()
	}
	if m.recentEditor != "" {
		if inst := m.panes.Get(m.recentEditor); inst != nil && inst.Kind() == pane.KindEditor {
			return m.recentEditor
		}
	}
	for _, key := range m.leafOrder() {
		if inst := m.panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			return key
		}
	}
	return ""
}

// leafOrder returns the leaf keys in tree walk order, falling back to registry
// insertion order before the tree exists (e.g. during construction).
func (m Model) leafOrder() []string {
	if m.tree != nil {
		return layout.Leaves(m.tree)
	}
	return m.panes.Keys()
}

// setFocus focuses key and remembers it as the recent editor when it is one.
func (m *Model) setFocus(key string) {
	m.panes.SetFocused(key)
	if inst := m.panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
		m.recentEditor = key
	}
}

// syncFocus re-asserts the registry's focus marking across all instances.
func (m *Model) syncFocus() { m.panes.SetFocused(m.panes.Focused()) }

// cycleFocus moves focus to the next leaf in tree order (tab).
func (m *Model) cycleFocus() {
	order := m.leafOrder()
	if len(order) == 0 {
		return
	}
	cur := m.panes.Focused()
	idx := 0
	for i, k := range order {
		if k == cur {
			idx = (i + 1) % len(order)
			break
		}
	}
	m.setFocus(order[idx])
}

// SplitFocused adds a new editor instance and splits the focused leaf toward
// zone, moving focus to the new pane. It is a binding-agnostic op (Roadmap 0080
// binds keys; the mouse reaches it too).
func (m *Model) SplitFocused(zone layout.Zone) {
	target := m.panes.Focused()
	if target == "" || m.tree == nil {
		return
	}
	newKey := m.panes.AddEditor()
	tree, ok := layout.SplitLeaf(m.tree, target, newKey, zone)
	if !ok {
		m.panes.Close(newKey)
		return
	}
	m.tree = tree
	m.setFocus(newKey)
	m.layout()
	saveLayout(m.tree, m.panes)
}

// spawnEditor splits the active editor's leaf toward the default zone, returning
// the new editor's key. Used by open-in-new-pane and Replace-with-no-editor. The
// split target is the active editor (so opening from the explorer lands the new
// pane in the editor area, not beside the explorer); only when no editor exists
// does it fall back to the focused leaf.
func (m *Model) spawnEditor() string {
	target := m.activeEditorKey()
	if target == "" {
		target = m.panes.Focused()
	}
	newKey := m.panes.AddEditor()
	if m.tree == nil || target == "" {
		// Pre-layout: no tree to split yet; the default tree will adopt the key on
		// first layout only if it is the canonical first editor. Otherwise leave the
		// instance registered and let layout() build around it.
		return newKey
	}
	tree, ok := layout.SplitLeaf(m.tree, target, newKey, m.splitZone)
	if !ok {
		// Target not in the tree (e.g. focused leaf already gone): drop the spare.
		m.panes.Close(newKey)
		return m.panes.Focused()
	}
	m.tree = tree
	return newKey
}

// CloseFocused closes the focused editor leaf, collapsing its sibling up and
// refocusing it. It is a no-op on the explorer (a singleton) and on the last
// leaf, so the workspace never empties and context resolution never loses its
// explorer.
func (m *Model) CloseFocused() { m.closeFocused() }

func (m *Model) closeFocused() {
	if m.closeKey(m.panes.Focused()) {
		// Focus the leaf that now occupies the closed pane's position: the first
		// leaf in walk order is a safe, always-present choice (explorer at minimum).
		m.setFocus(m.focusAfterClose())
		m.layout()
		saveLayout(m.tree, m.panes)
	}
}

// closeKey removes the editor leaf named key from the layout and registry,
// reporting whether it closed one. It never closes the explorer or the last
// leaf, and leaves focus/layout/persistence to the caller (so a batch close can
// relayout once). recentEditor is repaired here since it is bookkeeping local to
// the close.
func (m *Model) closeKey(key string) bool {
	inst := m.panes.Get(key)
	if inst == nil || inst.Kind() == pane.KindExplorer || m.tree == nil {
		return false
	}
	tree, ok := layout.Close(m.tree, key)
	if !ok {
		return false // last leaf: never empty the workspace
	}
	m.tree = tree
	m.panes.Close(key)
	if m.recentEditor == key {
		m.recentEditor = firstEditorKey(layout.Leaves(m.tree))
	}
	return true
}

// closeEditorsForPath closes every editor leaf showing path (or, when isDir,
// any file beneath it), so deleting a file in the explorer does not leave a
// stale editor open on it. It relayouts and persists once if anything closed,
// and refocuses only when the focused leaf itself was removed.
func (m *Model) closeEditorsForPath(path string, isDir bool) {
	prefix := path + string(os.PathSeparator)
	closed := false
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor || !inst.Editor().HasFile() {
			continue
		}
		ep := inst.Editor().Path()
		if ep == path || (isDir && strings.HasPrefix(ep, prefix)) {
			if m.closeKey(key) {
				closed = true
			}
		}
	}
	if !closed {
		return
	}
	if !m.panes.Has(m.panes.Focused()) {
		m.setFocus(m.focusAfterClose())
	}
	m.layout()
	saveLayout(m.tree, m.panes)
}

// focusAfterClose picks the leaf to focus once the focused one is gone: the
// recent editor if it survived, else the first remaining leaf.
func (m *Model) focusAfterClose() string {
	leaves := layout.Leaves(m.tree)
	if m.recentEditor != "" && m.panes.Has(m.recentEditor) {
		return m.recentEditor
	}
	if len(leaves) > 0 {
		return leaves[0]
	}
	return pane.ExplorerKey
}

// FocusDir moves focus to the pane neighbouring the current one in dir, using
// the computed rectangles. A binding-agnostic op for 0080.
func (m *Model) FocusDir(dir Direction) {
	if best := focusTarget(m.lay.Panes, m.panes.Focused(), dir); best != "" {
		m.setFocus(best)
	}
}

// focusTarget picks the pane to focus when moving from focused in dir. Among the
// panes lying in that direction it prefers those whose perpendicular span
// overlaps the current pane (so a focus-right from a top-left pane lands on the
// pane directly to its right, not a tall full-width pane below), then the
// nearest along the travel axis, then the best perpendicular alignment. Returns
// "" when there is no pane in that direction.
func focusTarget(panes map[string]layout.Rect, focused string, dir Direction) string {
	cur, ok := panes[focused]
	if !ok {
		return ""
	}
	cx, cy := cur.X+cur.W/2, cur.Y+cur.H/2
	best := ""
	bestScore := [3]int{1 << 30, 1 << 30, 1 << 30}
	for key, r := range panes {
		if key == focused {
			continue
		}
		tx, ty := r.X+r.W/2, r.Y+r.H/2
		if !inDirection(dir, cx, cy, tx, ty) {
			continue
		}
		// rank 0 = perpendicular spans overlap, 1 = not. primary = distance
		// along the travel axis; perp = perpendicular centre offset.
		rank, primary, perp := 1, 0, 0
		switch dir {
		case DirLeft, DirRight:
			if cur.Y < r.Y+r.H && r.Y < cur.Y+cur.H {
				rank = 0
			}
			primary, perp = abs(tx-cx), abs(ty-cy)
		default: // DirUp, DirDown
			if cur.X < r.X+r.W && r.X < cur.X+cur.W {
				rank = 0
			}
			primary, perp = abs(ty-cy), abs(tx-cx)
		}
		score := [3]int{rank, primary, perp}
		if score[0] < bestScore[0] ||
			(score[0] == bestScore[0] && score[1] < bestScore[1]) ||
			(score[0] == bestScore[0] && score[1] == bestScore[1] && score[2] < bestScore[2]) {
			bestScore, best = score, key
		}
	}
	return best
}

// Direction is a spatial focus-move direction for FocusDir.
type Direction int

const (
	DirLeft Direction = iota
	DirRight
	DirUp
	DirDown
)

func inDirection(dir Direction, cx, cy, tx, ty int) bool {
	switch dir {
	case DirLeft:
		return tx < cx
	case DirRight:
		return tx > cx
	case DirUp:
		return ty < cy
	default: // DirDown
		return ty > cy
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// bodyRect is the viewport the layout tree tiles.
func (m *Model) bodyRect() layout.Rect {
	return layout.Rect{X: 0, Y: 0, W: m.width, H: m.height - statusHeight}
}

// layout recomputes the layout geometry and pushes each leaf's interior size
// into its instance. The tree is built lazily on the first real window size so a
// default ratio can key off the actual width.
func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	if m.tree == nil {
		m.tree = layout.Default(m.width, explorerWidth)
	}
	m.lay = layout.Compute(m.tree, m.bodyRect())
	for key, r := range m.lay.Panes {
		inst := m.panes.Get(key)
		if inst == nil {
			continue
		}
		inst.SetSize(paneInterior(r.W), paneInterior(r.H))
		if inst.Kind() == pane.KindEditor && m.pendingScroll != nil && m.pendingScroll.key == key {
			inst.Editor().SetScroll(m.pendingScroll.top, m.pendingScroll.left)
			m.pendingScroll = nil
		}
	}
	m.syncFocus()
}

// paneInterior maps an outer pane dimension to the content area.
func paneInterior(outer int) int {
	if v := outer - paneChrome; v >= 1 {
		return v
	}
	return 1
}

// handleMouse runs the drag state machine: press hit-tests the layout to start a
// resize (divider) or move (title bar), motion updates the in-flight gesture, and
// release commits and persists. A title drag onto another pane relocates it
// (0036); a drag to the source pane's own edge spawns a fresh split there (0037).
func (m Model) handleMouse(msg mouseEvent) (tea.Model, tea.Cmd) {
	if m.shell.IsOpen() {
		return m, nil
	}
	shift := msg.Mod&tea.ModShift != 0
	if msg.action == mouseWheel {
		if p, ok := m.lay.PaneAt(msg.X, msg.Y); ok && p == pane.ExplorerKey {
			switch {
			case msg.Button == tea.MouseWheelLeft:
				m.explorer().ScrollXBy(-wheelLines)
			case msg.Button == tea.MouseWheelRight:
				m.explorer().ScrollXBy(wheelLines)
			case msg.Button == tea.MouseWheelUp && shift:
				m.explorer().ScrollXBy(-wheelLines)
			case msg.Button == tea.MouseWheelDown && shift:
				m.explorer().ScrollXBy(wheelLines)
			case msg.Button == tea.MouseWheelUp:
				m.explorer().ScrollBy(-wheelLines)
			case msg.Button == tea.MouseWheelDown:
				m.explorer().ScrollBy(wheelLines)
			}
		}
		return m, nil
	}
	switch msg.action {
	case mousePress:
		hit := m.lay.Hit(msg.X, msg.Y)
		switch hit.Kind {
		case layout.HitDivider:
			m.drag = &dragState{kind: dragResize, divider: *hit.Divider, curX: msg.X, curY: msg.Y}
		case layout.HitTitle:
			m.drag = &dragState{kind: dragMove, srcPane: hit.Pane, curX: msg.X, curY: msg.Y}
		case layout.HitPane:
			return m.paneClick(hit.Pane, msg)
		}
	case mouseMotion:
		if m.drag == nil {
			m.updateHover(msg)
			return m, nil
		}
		m.drag.curX, m.drag.curY = msg.X, msg.Y
		if m.drag.kind == dragResize {
			m.drag.divider.ResizeTo(msg.X, msg.Y)
			m.layout()
		}
	case mouseRelease:
		if m.drag == nil {
			return m, nil
		}
		if m.drag.kind == dragMove {
			m.commitMove(msg.X, msg.Y)
		}
		m.drag = nil
		saveLayout(m.tree, m.panes)
	}
	return m, nil
}

// commitMove applies a title-bar drag release: onto another pane it relocates the
// source (0036 move/swap); onto the source pane's own edge it spawns a fresh
// editor split there (0037); a drop in the source pane's interior is a no-op.
func (m *Model) commitMove(x, y int) {
	target, ok := m.lay.PaneAt(x, y)
	if !ok {
		return
	}
	if target != m.drag.srcPane {
		zone := layout.DropZone(m.lay.Panes[target], x, y)
		m.tree = layout.Move(m.tree, m.drag.srcPane, target, zone)
		m.layout()
		return
	}
	// Dropped on the source pane: spawn a split only when near an edge.
	if zone, near := edgeZone(m.lay.Panes[target], x, y); near {
		newKey := m.panes.AddEditor()
		if tree, ok := layout.SplitLeaf(m.tree, target, newKey, zone); ok {
			m.tree = tree
			m.setFocus(newKey)
			m.layout()
		} else {
			m.panes.Close(newKey)
		}
	}
}

// edgeBand is the fraction of a pane's span near an edge that, when a self-drop
// lands in it, spawns a split rather than being ignored.
const edgeBand = 0.30

// edgeZone reports the drop zone for (x,y) within r and whether the point lies in
// the outer edgeBand of that zone's axis (so a center drop does not spawn).
func edgeZone(r layout.Rect, x, y int) (layout.Zone, bool) {
	if r.W <= 0 || r.H <= 0 {
		return layout.ZoneRight, false
	}
	fx := (float64(x-r.X) + 0.5) / float64(r.W)
	fy := (float64(y-r.Y) + 0.5) / float64(r.H)
	z := layout.DropZone(r, x, y)
	switch z {
	case layout.ZoneLeft:
		return z, fx <= edgeBand
	case layout.ZoneRight:
		return z, fx >= 1-edgeBand
	case layout.ZoneTop:
		return z, fy <= edgeBand
	default:
		return z, fy >= 1-edgeBand
	}
}

// mouseAction is the kind of mouse event, recovered from the concrete v2 mouse
// message type (bubbletea v2 split the single MouseMsg into four types).
type mouseAction int

const (
	mousePress mouseAction = iota
	mouseRelease
	mouseMotion
	mouseWheel
)

// mouseEvent normalises the four v2 mouse messages into one value the drag state
// machine consumes: the embedded tea.Mouse carries X/Y/Button/Mod, and action
// records which message type it came from.
type mouseEvent struct {
	tea.Mouse
	action mouseAction
}

// updateHover sets (or clears) the explorer's hover highlight.
func (m *Model) updateHover(msg mouseEvent) {
	r, ok := m.lay.Panes[pane.ExplorerKey]
	if !ok {
		return
	}
	if p, in := m.lay.PaneAt(msg.X, msg.Y); in && p == pane.ExplorerKey {
		m.explorer().SetHoverAt(msg.X-(r.X+paneContentX), msg.Y-(r.Y+paneContentY))
		return
	}
	m.explorer().ClearHover()
}

// paneClick focuses the clicked leaf and forwards the interior click to it,
// translating the absolute mouse cell into the pane's content-local space.
func (m Model) paneClick(key string, msg mouseEvent) (tea.Model, tea.Cmd) {
	r, ok := m.lay.Panes[key]
	if !ok {
		return m, nil
	}
	inst := m.panes.Get(key)
	if inst == nil {
		return m, nil
	}
	m.setFocus(key)
	localX := msg.X - (r.X + paneContentX)
	localY := msg.Y - (r.Y + paneContentY)
	switch inst.Kind() {
	case pane.KindExplorer:
		var cmd tea.Cmd
		exp := inst.Explorer()
		*exp, cmd = exp.MouseClick(localX, localY)
		return m, cmd
	case pane.KindEditor:
		inst.Editor().MouseClick(localX, localY)
	}
	return m, nil
}

// View implements tea.Model. Under bubbletea v2 the alternate screen, mouse mode
// and keyboard enhancements (the kitty keyboard protocol) are declared on the
// View rather than via program options. Basic key disambiguation is requested by
// default; ReportEventTypes asks the terminal for key repeat and release events,
// which we deliberately ignore in Update (only KeyPressMsg is dispatched).
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.KeyboardEnhancements.ReportEventTypes = true
	return v
}

// render composes the full frame as a styled string: the pane tree, the status
// line, and any floating overlay (move ghost, palette, modal shell) on top.
func (m Model) render() string {
	if m.width == 0 {
		return "starting ike…"
	}
	body := m.renderNode(m.tree, m.bodyRect())
	base := lipgloss.JoinVertical(lipgloss.Left, body, m.statusLine())
	if box, x, y, ok := m.moveGhost(); ok {
		base = overlay.Place(base, box, x, y, m.width, m.height)
	}
	if m.palette.IsOpen() {
		v := m.palette.View()
		if m.palette.Anchored() {
			x, y := m.palette.AnchorPos()
			return overlay.Place(base, v, x, y, m.width, m.height)
		}
		return overlay.Center(base, v, m.width, m.height)
	}
	if m.shell.IsOpen() {
		return overlay.Center(base, m.shell.View(), m.width, m.height)
	}
	return base
}

// moveGhost computes the preview box for an in-flight move. Onto another pane it
// previews the relocation; onto the source pane's own edge it previews the spawn.
func (m Model) moveGhost() (box string, x, y int, ok bool) {
	d := m.drag
	if d == nil || d.kind != dragMove {
		return "", 0, 0, false
	}
	tgt, found := m.lay.PaneAt(d.curX, d.curY)
	if !found {
		return "", 0, 0, false
	}
	if tgt == d.srcPane {
		zone, near := edgeZone(m.lay.Panes[tgt], d.curX, d.curY)
		if !near {
			return "", 0, 0, false
		}
		gr := dropRect(m.lay.Panes[tgt], zone)
		if gr.W < 3 || gr.H < 3 {
			return "", 0, 0, false
		}
		return ghostBox(gr.W, gr.H, "new pane"), gr.X, gr.Y, true
	}
	gr := dropRect(m.lay.Panes[tgt], layout.DropZone(m.lay.Panes[tgt], d.curX, d.curY))
	if gr.W < 3 || gr.H < 3 {
		return "", 0, 0, false
	}
	return ghostBox(gr.W, gr.H, m.paneLabel(d.srcPane)), gr.X, gr.Y, true
}

// dropRect is the sub-rectangle of r the dragged pane would occupy for zone z.
func dropRect(r layout.Rect, z layout.Zone) layout.Rect {
	switch z {
	case layout.ZoneLeft:
		return layout.Rect{X: r.X, Y: r.Y, W: r.W / 2, H: r.H}
	case layout.ZoneRight:
		w := r.W / 2
		return layout.Rect{X: r.X + r.W - w, Y: r.Y, W: w, H: r.H}
	case layout.ZoneTop:
		return layout.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H / 2}
	default:
		h := r.H / 2
		return layout.Rect{X: r.X, Y: r.Y + r.H - h, W: r.W, H: h}
	}
}

// ghostBox renders the matte drop-preview box at size w×h with a centered label.
func ghostBox(w, h int, label string) string {
	inner := lipgloss.Place(w-2, h-2, lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Foreground(lipgloss.Color(colorGhost)).Render("⤴ "+label))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorGhost)).
		Faint(true).
		Render(inner)
}

// renderNode walks the layout tree, rendering each leaf into its rectangle.
func (m Model) renderNode(n layout.Node, r layout.Rect) string {
	switch t := n.(type) {
	case *layout.Leaf:
		return m.renderPane(t.Pane, r)
	case *layout.Split:
		a, _, b := t.Children(r)
		if t.Orient == layout.Horizontal {
			return lipgloss.JoinHorizontal(lipgloss.Top,
				m.renderNode(t.A, a), dividerV(r.H), m.renderNode(t.B, b))
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderNode(t.A, a), dividerH(r.W), m.renderNode(t.B, b))
	}
	return ""
}

// Pane border colors.
const (
	colorPaneFocus  = "69"  // focused pane border
	colorPaneBlur   = "240" // unfocused pane border
	colorMoveSource = "203" // the pane currently being moved
	colorDropTarget = "220" // the pane a release would drop onto
	colorGhost      = "136" // matte gold, the drop-preview box
)

// renderPane renders a single leaf at its outer rectangle, resolving its key to
// an instance for title, content, and focus state. During a move drag the source
// pane and the hovered drop target are recolored. An unknown key (no instance)
// renders an empty titled box rather than crashing.
func (m Model) renderPane(key string, r layout.Rect) string {
	inst := m.panes.Get(key)
	var title, content string
	var focused bool
	if inst == nil {
		title = strings.ToUpper(key)
	} else {
		focused = m.panes.Focused() == key
		switch inst.Kind() {
		case pane.KindExplorer:
			title, content = "EXPLORER", inst.View()
		case pane.KindEditor:
			title, content = m.editorTitle(inst.Editor()), inst.View()
		}
	}

	border := colorPaneBlur
	if focused {
		border = colorPaneFocus
	}
	if d := m.drag; d != nil && d.kind == dragMove {
		if key == d.srcPane {
			border = colorMoveSource
			title = "⤴ " + title
		} else if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt == key && tgt != d.srcPane {
			border = colorDropTarget
			title = title + "  " + zoneArrow(layout.DropZone(r, d.curX, d.curY))
		}
	}
	return paneBox(title, content, r.W, r.H, border)
}

// zoneArrow is the short drop-zone marker shown in a target pane's title.
func zoneArrow(z layout.Zone) string {
	switch z {
	case layout.ZoneLeft:
		return "◧ left"
	case layout.ZoneRight:
		return "right ◨"
	case layout.ZoneTop:
		return "⬒ top"
	default:
		return "⬓ bottom"
	}
}

// dividerV renders the vertical gutter between two horizontally-arranged panes.
func dividerV(h int) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return style.Render(strings.TrimRight(strings.Repeat("│\n", h), "\n"))
}

// dividerH renders the horizontal gutter between two vertically-stacked panes.
func dividerH(w int) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return style.Render(strings.Repeat("─", w))
}

// editorTitle returns an editor pane title: file basename with a dirty marker.
func (m Model) editorTitle(ed *editor.Model) string {
	if !ed.HasFile() {
		return "EDITOR"
	}
	name := baseName(ed.Path())
	if ed.Dirty() {
		name += " *"
	}
	return name
}

// statusLine renders the bottom status bar: mode, file, dirty flag, cursor, and
// any active command line, reflecting the active editor.
func (m Model) statusLine() string {
	style := lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("252"))

	if d := m.drag; d != nil && d.kind == dragMove {
		hint := "MOVE " + m.paneLabel(d.srcPane)
		if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt != d.srcPane {
			hint += " → " + zoneArrow(layout.DropZone(m.lay.Panes[tgt], d.curX, d.curY)) + " of " + m.paneLabel(tgt)
		} else if zone, near := m.selfDropZone(d); near {
			hint += " → split " + zoneArrow(zone)
		} else {
			hint += "  (drop on a pane or this pane's edge)"
		}
		return style.Foreground(lipgloss.Color(colorDropTarget)).Render(" " + hint)
	}

	ed := m.activeEditor()
	if ed != nil {
		if cl := ed.CommandLine(); cl != "" {
			return style.Render(cl)
		}
	}
	if s := m.host.Status(); s != "" {
		return style.Render(" " + s)
	}

	mode, file, dirty := "NORMAL", "no file", ""
	line, col := 1, 1
	if ed != nil {
		mode = ed.ModeName().String()
		if ed.HasFile() {
			file = baseName(ed.Path())
		}
		if ed.Dirty() {
			dirty = " [+]"
		}
		line, col = ed.Cursor()
	}
	left := " " + mode + " │ " + file + dirty
	right := "Ln " + strconv.Itoa(line) + ", Col " + strconv.Itoa(col) + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return style.Render(left + strings.Repeat(" ", gap) + right)
}

// selfDropZone reports the spawn zone (and proximity) for a self-drop during a
// move drag, for the status hint.
func (m Model) selfDropZone(d *dragState) (layout.Zone, bool) {
	if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt == d.srcPane {
		return edgeZone(m.lay.Panes[tgt], d.curX, d.curY)
	}
	return layout.ZoneRight, false
}

// activeEditor returns the active editor model, or nil when no editor exists.
func (m Model) activeEditor() *editor.Model {
	if key := m.activeEditorKey(); key != "" {
		return m.panes.Get(key).Editor()
	}
	return nil
}

// paneBox renders a titled bordered box around content with the given border
// color. It hard-clamps to exactly width×height: the title is truncated to the
// interior so it never wraps, and MaxWidth/MaxHeight cap the rendered box so a
// narrow pane can never overflow its rectangle and push the whole tiling off
// screen (the layout assigns each leaf an exact rect; the renderer must honour it).
func paneBox(title, content string, width, height int, borderColor string) string {
	// Interior text width = outer width minus the two border columns and the two
	// padding columns. Truncate the title to it so it stays on one row.
	if inner := width - 4; inner >= 1 {
		title = ansi.Truncate(title, inner, "…")
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(width-2).
		Height(height-2).
		MaxWidth(width).
		MaxHeight(height).
		Padding(0, 1).
		BorderForeground(lipgloss.Color(borderColor))
	titleStyle := lipgloss.NewStyle().Bold(true)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), content))
}

func baseName(path string) string { return filepath.Base(path) }

// paneLabel is the human label for a leaf key used in the drag status hint.
func (m Model) paneLabel(key string) string {
	inst := m.panes.Get(key)
	if inst != nil && inst.Kind() == pane.KindEditor {
		return m.editorTitle(inst.Editor())
	}
	return strings.ToUpper(strings.SplitN(key, ":", 2)[0])
}
