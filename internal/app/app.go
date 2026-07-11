// Package app wires the root bubbletea model for IKE: a dynamic tiled workspace
// that hosts the file explorer and N editor panes, owns focus and layout, routes
// the explorer's open-file message to the active editor (or a fresh split), and
// renders the status line. The pane set itself is dynamic (Roadmap 0037): a
// pane.Registry maps each layout leaf key to a live component instance, and focus
// is "the focused leaf" rather than a two-value enum.
package app

import (
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/backup"
	"ike/internal/callhier"
	"ike/internal/clipboard"
	"ike/internal/config"
	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/finder"
	"ike/internal/help"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/lang"
	"ike/internal/layout"
	ilsp "ike/internal/lsp"
	"ike/internal/menu"
	"ike/internal/nav"
	"ike/internal/overlay"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/project"
	"ike/internal/registry"
	"ike/internal/search"
	"ike/internal/settings"
	"ike/internal/terminal"
	"ike/internal/theme"
	"ike/internal/ui"
	"ike/internal/watch"
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
	paneChromeW   = 4 // horizontal: left+right border (2) + left+right padding (2)
	paneChromeH   = 3 // vertical: top+bottom border (2) + title row (1); no vertical padding
	paneContentX  = 2 // left border (1) + left padding (1) before pane content
	paneContentY  = 2 // top border (1) + title row (1) before pane content
	wheelLines    = 3 // rows a single mouse-wheel notch scrolls
)

// Model is the root model.
type Model struct {
	width  int
	height int
	// navHist is the session-scoped navigation history (Roadmap 0220): shared
	// by pointer across the value-model copies. navSkip suppresses recording
	// while nav.back/nav.forward themselves drive the open funnel.
	navHist *nav.History
	navSkip bool
	// panes is the registry of live pane instances (Roadmap 0037). It replaces the
	// two hard-coded explorer/editor fields and the two-value focus enum: focus is
	// the registry's focused key, which always names a layout leaf.
	panes *pane.Registry
	// recentEditor is the key of the most-recently-focused editor, used as the
	// Replace open-target when the explorer (not an editor) holds focus.
	recentEditor string
	// recent is the MRU file list behind the recent-files palette mode
	// (Roadmap 0230). Held by pointer so value-receiver open paths mutate the
	// one shared store; persisted with the session.
	recent *recentFiles
	// closedTabs is the reopen ring (0190, #158): the last few closed tabs'
	// paths and carets, newest last, popped by editor.tab.reopenClosed.
	closedTabs []closedTab
	host       *host.Host
	reg        *registry.Registry
	// toasts is the active notification stack (Roadmap 0130): drained from the
	// host after every Update pass, rendered bottom-right above the status line.
	toasts   []toast
	toastSeq int
	// history is the notification ring (#78): the newest historyCap entries,
	// newest first, browsable via the notifications.history command.
	history []histEntry
	// watcher is the external-file-change service (Roadmap 0140). It is
	// constructed with the model (so save epochs record from the start) but
	// only started by main.go via StartWatcher, keeping tests watcher-free.
	watcher *watch.Service
	// menu is the menu bar (Roadmap 0160, #90), rendered above the panes when
	// ui.menu_bar is enabled.
	menu *menu.Model
	// settings is the full-window settings panel (Roadmap 0160, #91); cfgOpts
	// names the layer files its edits write back to.
	settings *settings.Model
	cfgOpts  config.Options
	help     *help.Help
	// shell is the single active floating overlay (Roadmap 0035).
	shell *ui.Floating
	// conflictKey is the editor pane awaiting a save-conflict answer (Roadmap
	// 0140, #82) while the shell shows the prompt; "" when no conflict is open.
	conflictKey string
	// recovery holds the crash-recovery restore prompt (Roadmap 0210, #166) while
	// the shell shows it; recoveryPending carries snapshots found at startup until
	// the window is sized and the prompt can open. Both nil/empty when idle.
	recovery        *recoveryState
	recoveryPending []backup.Snapshot
	// onboarding holds the first-start LSP server-install dialog (#301) while
	// the shell shows it; onboardingPending flags it at startup until the
	// window is sized. Both nil/false when idle.
	onboarding        *onboardingState
	onboardingPending bool
	// backupSvc/backupDeb are the crash-recovery write side (Roadmap 0210,
	// #167): the change seam marks dirty buffers, one armed tick
	// (backupTickArmed) snapshots the ones that went quiet. backupIv caches the
	// debounce interval so a live reload can detect a change.
	backupSvc       *backup.Service
	backupDeb       *backup.Debouncer
	backupTickArmed bool
	backupIv        time.Duration
	// renamePath is the file being renamed by the file.rename prompt (#175)
	// while the shell shows it; renameInput/renamePos are the typed name and
	// its cursor. "" when no rename prompt is open.
	renamePath  string
	renameInput string
	renamePos   int
	// movePending is the file whose move target the palette's directory picker
	// is currently asking for (file.move, #175); "" when no move is pending.
	movePending string
	// lspRename is the open symbol-rename prompt (Roadmap 0100, #6); nil when
	// no rename is in flight.
	lspRename *lspRenameState
	// lspStatus holds the persistent per-language server state ("ready",
	// "disabled") behind the status line's server segment (#380). Keyed by
	// language ID; the segment renders only the focused buffer's language, so
	// stale text never follows the user into unrelated buffers.
	lspStatus map[string]string

	// symbols is the live workspace-symbol palette mode (0250 phase 2,
	// #295); symbolPriming marks a hook-priming goToClass run for the
	// search-everywhere seat, which must not open the palette.
	symbols       *symbolMode
	symbolPriming bool
	// terminalReturnFocus remembers the pane focused before terminal.toggle
	// moved focus into a terminal, so toggling again returns there (#97).
	terminalReturnFocus string
	// switchPending is the validated project root awaiting the unsaved-changes
	// answer (Roadmap 0090, #3) while the shell shows the save-all / discard /
	// cancel prompt; "" when no switch is gated.
	switchPending string

	// closePending is the close request awaiting the unsaved-changes guard
	// (#259); nil when no guard is open.
	closePending *pendingClose

	// explorerRatio remembers the hidden explorer's split ratio so
	// explorer.toggle restores the tree at its prior width (#268); 0 means
	// "use the default width".
	explorerRatio float64
	// callhier is the call-hierarchy tree overlay (lsp.callHierarchy, #173).
	callhier *callhier.Model
	// finder is the find-in-path overlay (Roadmap 0150); searcher is the
	// streaming scan service it drives.
	finder   *finder.Model
	searcher *search.Service
	// inFileSearchRecent is true while a committed in-file search ("/", "?",
	// cmd+f) is more recent than any find-in-path scan: f3/shift+f3 then repeat
	// the in-file search on the active editor instead of stepping retained
	// find-in-path results (#376). Any new scan activity flips it back.
	inFileSearchRecent bool
	// palette is the command palette overlay (Roadmap 0070): a modal input that
	// fronts registered commands (":") and file search ("@"). paletteKey is the
	// default key that opens it (the final binding is Roadmap 0080's).
	palette    *palette.Palette
	paletteKey string
	// themePal is the resolved color scheme (Roadmap 0110): [theme].name mapped
	// to a theme.Palette. Chrome renders from its ui slots; panes get it threaded
	// at construction and on config reloads.
	themePal *theme.Palette
	// themeOverride is the theme name chosen at runtime via a palette command,
	// "" when the theme still follows config. It persists in the session so the
	// choice survives a restart; a config edit only wins again once it is cleared.
	themeOverride string
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
	// pendingWheel accumulates queued mouse-wheel events so a fast scroll burst
	// applies in one update pass instead of one render per event (#238);
	// wheelFlushQueued records that a wheelFlushMsg is already in flight.
	pendingWheel     []wheelBatch
	wheelFlushQueued bool
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
	// bindings is the live binding-table holder (0081/40): help and the
	// palette's shortcut column read honest labels through it, following
	// every keymap reload.
	bindings *keymap.LiveBindings
	// whichKey holds the which-key hint rows while a chord prefix is pending
	// (0081/40); nil hides the overlay.
	whichKey []string
	// refs is the palette mode listing the latest find-references results
	// (lsp.references, #5); the ReferencesMsg handler fills it and opens the
	// palette locked to it.
	refs *refsMode
	// actions is the palette mode listing the latest code-action offer
	// (lsp.codeAction, #8), same pattern as refs.
	actions *actionsMode
	// pasteHist is the palette mode over the focused editor's yank/delete
	// history (#57), same pattern as refs.
	pasteHist *pasteHistMode
	// zoomed is the pane key rendered alone while pane.maximize is active
	// (#358); "" = normal layout. zoomSig is the tree's leaf signature at zoom
	// time — layout() drops the zoom when it changes. Not persisted.
	zoomed  string
	zoomSig string
	// zen hides the tab bar and status line on top of the zoom (#359);
	// zenKeepZoom remembers whether the editor was manually zoomed before zen,
	// so leaving zen restores that state instead of the full layout.
	zen         bool
	zenKeepZoom bool
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
	dragResize     dragKind = iota // dragging a divider to change a split ratio
	dragMove                       // dragging a pane title bar to relocate or spawn
	dragTab                        // dragging one tab label to move just that file (#305)
	dragTermSelect                 // dragging a text selection inside a terminal pane (#227)
)

// dragState holds the in-flight mouse gesture. For a resize it carries the
// divider being dragged; for a move it carries the source leaf key. curX/curY
// track the latest mouse cell so the move can render live feedback (which pane
// and drop zone the release would target, and whether it spawns or relocates).
type dragState struct {
	kind    dragKind
	divider layout.Divider
	srcPane string
	srcTab  int // dragTab: index of the grabbed tab (#305)
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
	return newWithHost(reg, cfg, host.New(cfg))
}

// newWithHost is NewWith with the host supplied. A project switch (Roadmap
// 0090, #3) rebuilds the model through here with the *live* host, so the seams
// wired to its pointer — the program sender, the LSP bridge's editor emitter,
// plugin captures — survive the re-root.
func newWithHost(reg *registry.Registry, cfg host.Config, h *host.Host) Model {
	h.SetConfig(cfg)
	applyPluginConfig(reg, cfg)
	themePal, themeWarning := resolveTheme(reg, cfg)
	panes := pane.NewRegistry(cfg)
	panes.SetPalette(themePal)
	panes.AddExplorer()
	edKey := panes.AddEditor()
	panes.SetFocused(pane.ExplorerKey)
	refs := &refsMode{}
	actions := &actionsMode{}
	symbols := &symbolMode{}
	pasteHist := &pasteHistMode{}
	bindings := &keymap.LiveBindings{}
	recent := &recentFiles{}
	m := Model{
		panes:        panes,
		recentEditor: edKey,
		recent:       recent,
		navHist:      &nav.History{},
		host:         h,
		reg:          reg,
		themePal:     themePal,
		bindings:     bindings,
		help:         help.New(reg, bindings, helpMinCol(cfg)),
		shell:        ui.New(shellConfig(cfg)),
		palette:      buildPalette(reg, cfg, refs, actions, bindings, recent, symbols, pasteHist),
		refs:         refs,
		lspStatus:    map[string]string{},
		symbols:      symbols,
		actions:      actions,
		pasteHist:    pasteHist,
		paletteKey:   paletteToggleKey(cfg),
		splitZone:    splitZone(cfg),
		focusKeys:    focusKeys(cfg),
		keys:         buildKeymap(cfg, bindings),
	}
	m.watcher = watch.New(m.host.Send)
	m.backupSvc = backupService()
	m.backupIv = backupInterval(cfg)
	m.backupDeb = backup.NewDebouncer(m.backupIv)
	m.searcher = search.New(m.host.Send)
	m.finder = finder.New(m.searcher)
	m.finder.SetPalette(themePal)
	m.finder.SetDisplayPath(displayPath)
	m.callhier = callhier.New()
	m.callhier.SetPalette(themePal)
	m.callhier.SetDisplayPath(displayPath)
	m.menu = menu.New(menu.Defaults(), m.commandInfo(reg))
	m.cfgOpts = config.Discover(".")
	pages := settings.BasePages(themeNames(reg))
	pages = append(pages, settings.Page{Title: "Keymap", Custom: settings.NewKeymapPage(m.cfgOpts, func(id string) bool {
		_, ok := reg.Command(id)
		return ok
	})})
	pages = append(pages, settings.Page{Title: "Toolchain", Custom: settings.NewToolchainPage(m.cfgOpts, ".", func() tea.Cmd {
		// An interpreter change respawns the servers against the new value.
		if c, ok := reg.Command("lsp.restart"); ok {
			return c.Run(m.host)
		}
		return nil
	})})
	pages = append(pages, settings.Page{Title: "Plugins", Custom: settings.NewPluginsPage(m.cfgOpts,
		func() []settings.PluginInfo {
			descs := reg.Describe()
			out := make([]settings.PluginInfo, len(descs))
			for i, d := range descs {
				out[i] = settings.PluginInfo{
					ID: d.ID, Enabled: d.Enabled, Commands: d.Commands,
					Panes: d.Panes, Keymaps: d.Keymaps, FileHandlers: d.FileHandlers,
					Hooks: d.Hooks, Themes: d.Themes, SettingsPages: d.SettingsPages,
				}
			}
			return out
		},
		func(id string, enable bool) tea.Cmd {
			// The toggle is user preference, not project state: user scope.
			write := config.WriteAndReload(m.cfgOpts, config.UserScope, "plugins."+id+".enabled", enable)
			// Enabling a language plugin kicks the missing-server install
			// (#131); the command re-reads config off disk, so it sees the
			// write regardless of reload ordering.
			if enable && strings.HasPrefix(id, "lang-") {
				if c, ok := reg.Command("lsp.installMissing"); ok {
					return tea.Batch(write, c.Run(m.host))
				}
			}
			return write
		},
	)})
	m.settings = settings.New(append(pages, reg.SettingsPages()...), m.cfgOpts)
	// Thread the startup palette through every chrome component; without this
	// the settings panel, command palette, shell, help, and menu render with
	// the default palette until the first theme switch (#384).
	m.applyTheme(themePal)
	// Restore a saved per-project layout if one is structurally sound; an unknown
	// or stale layout is dropped and the default is built on first size.
	m.restoreLayout(cfg)
	m.restoreSession()
	m.scanRecovery()
	m.scanOnboarding()
	m.wireEditorEmitters()
	if themeWarning != "" {
		m.host.Notify(host.Warn, themeWarning)
	}
	return m
}

// SetSender wires the program's Send into the host so background workers (the LSP
// bridge) can inject async results. main.go calls it once after tea.NewProgram.
func (m Model) SetSender(send func(tea.Msg)) { m.host.SetSender(send) }

// Host exposes the model's live host as the plugin-facing API. main.go binds
// it into the WASM host adapter (Roadmap 9900, #25) once the model exists;
// the pointer survives project switches (newWithHost carries it over).
func (m Model) Host() host.API { return m.host }

// StartWatcher starts the external-file-change watcher on root when
// files.watch is enabled (Roadmap 0140). main.go calls it once after
// SetSender; a project switch (Roadmap 0090) re-calls it with the new root,
// which restarts the watcher there.
func (m Model) StartWatcher(root string) {
	if v, ok := m.host.Config().Get("files.watch"); ok && v == "false" {
		return
	}
	_ = m.watcher.Start(root)
}

// editorEmitter adapts editor lifecycle events into host editor events, which the
// host fans out to the LSP bridge (registered via host.SetEditorEmitter). One
// stateless adapter is installed on every editor instance; it is a no-op when no
// bridge is registered. Save events additionally stamp the file watcher's save
// epoch (Roadmap 0140) so IKE's own writes never report as external changes.
type editorEmitter struct {
	host    *host.Host
	watcher *watch.Service
	nav     *nav.History // navigation history (Roadmap 0220); shared pointer
	key     string       // pane key of the editor this emitter is installed on
}

// Emit implements editor.Emitter. The editor and host event-kind constants share
// the same iota ordering (change/cursor/completion/save), so the kind maps
// directly.
func (e editorEmitter) Emit(ev editor.Event) {
	if ev.Kind == editor.EventJump {
		// An in-file jump departs (Roadmap 0220): record where the caret came
		// from. Nav-only — the landing follows as an ordinary cursor-move, so
		// the LSP bridge needs no forwarding of this kind.
		if e.nav != nil {
			e.nav.RecordJump(nav.Position{Path: ev.Path, Line: ev.Line, Col: ev.Col})
		}
		return
	}
	if ev.Kind == editor.EventSave && e.watcher != nil {
		e.watcher.MarkSaved(ev.Path)
	}
	if ev.Kind == editor.EventChange || ev.Kind == editor.EventSave {
		// Shared documents (#142): tell the other views of this file that the
		// document changed. Emit runs synchronously inside Update, and sending
		// into the program's own message loop from there deadlocks — so the
		// send goes through a goroutine. Flags are NOT carried here: delivery
		// order between goroutines is not guaranteed, so the root model reads
		// dirty/stale fresh from the originating pane when the message lands.
		msg := editor.SyncMsg{Path: ev.Path, FromKey: e.key}
		go e.host.Send(msg)
	}
	e.host.EmitEditor(host.EditorEvent{
		Kind:       int(ev.Kind),
		Path:       ev.Path,
		Line:       ev.Line,
		Col:        ev.Col,
		Text:       ev.Text,
		Sel:        int(ev.Sel),
		AnchorLine: ev.AnchorLine,
		AnchorCol:  ev.AnchorCol,
	})
}

// wireEditorEmitters installs the editor-emitter adapter on every editor pane, so
// edits flow to the LSP bridge. It is idempotent and re-run whenever editors are
// created.
func (m *Model) wireEditorEmitters() {
	for _, key := range m.panes.Keys() {
		m.installEmitter(key)
	}
}

// installEmitter wires the editor-emitter adapter onto every tab of one editor
// pane. It is idempotent, so re-running it after a tab is added is cheap.
func (m *Model) installEmitter(key string) {
	if inst := m.panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
		for _, ed := range inst.Editors() {
			ed.SetEmitter(editorEmitter{host: m.host, watcher: m.watcher, nav: m.navHist, key: key})
		}
	}
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
		} else if ids[key].Kind == "terminal" {
			continue // restored below as a fresh shell in the saved position (#96)
		} else if !isEditorKey(key) {
			return // unknown leaf kind / malformed key: fall back to default
		}
	}
	if explorers > 1 {
		return // more than one explorer leaf is malformed
	}
	// Zero explorer leaves is a valid save: the tree was hidden via
	// explorer.toggle (#268). The instance below registers regardless, so
	// the toggle can bring the leaf back.
	// The default set is replaced: a fresh registry with the explorer plus one
	// editor per non-explorer leaf, each rebuilding its remembered tab list
	// (#160). Files missing on disk are skipped; a pane whose every file
	// vanished restores as a single scratch tab, like before tabs existed.
	panes := pane.NewRegistry(cfg)
	panes.AddExplorer()
	first := map[string]*editor.Model{} // path → first restored view, for sharing
	restoreTab := func(ed *editor.Model, path string) bool {
		if prev, ok := first[path]; ok {
			// The same file across several tabs or leaves restores as one
			// shared document (#142), not divergent copies.
			ed.ShareDocumentWith(prev)
			return true
		}
		if err := ed.Load(path); err != nil {
			return false
		}
		first[path] = ed
		return true
	}
	for _, key := range leaves {
		if key == pane.ExplorerKey {
			continue
		}
		if id := ids[key]; id.Kind == "terminal" {
			// A terminal restores as a *fresh* shell in the saved position
			// (#96): no process resurrection, the origin dir respawns it.
			dir := id.Path
			if dir == "" {
				dir = "."
			}
			shell := ""
			if v, ok := cfg.Get("terminal.shell"); ok {
				shell = v
			}
			panes.AddTerminalKey(key, terminal.Shell(shell), dir, terminalEnv(), m.host.Send)
			continue
		}
		inst := panes.AddEditorKey(key)
		id, hasID := ids[key]
		if !hasID {
			continue
		}
		paths := id.Tabs
		if len(paths) == 0 && id.Path != "" {
			paths = []string{id.Path} // pre-tabs file: one remembered document
		}
		active := 0
		for i, p := range paths {
			if p == "" {
				continue
			}
			ed := inst.Editor()
			if ed.HasFile() {
				ed = inst.AddTab()
			}
			if !restoreTab(ed, p) {
				if inst.TabCount() > 1 {
					inst.CloseTab(inst.ActiveTab()) // missing file: drop the spare tab
				}
				continue
			}
			if i == id.Active {
				active = inst.ActiveTab()
			}
		}
		inst.ActivateTab(active)
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
	m.restoreTheme(s.Theme)
	m.recent.Set(s.RecentFiles)
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
	m.syncExplorerOpen()
	m.syncFocus()
}

// editorWithFile returns the key of an editor instance holding path in any of
// its tabs, activating that tab, or "" if none does.
func (m Model) editorWithFile(path string) string {
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst.Kind() != pane.KindEditor {
			continue
		}
		if idx := inst.TabForPath(path); idx >= 0 {
			inst.ActivateTab(idx)
			return key
		}
	}
	return ""
}

// snapshotSession captures the active editor + explorer state for persistence.
func (m Model) snapshotSession() sessionState {
	st := m.explorer().Snapshot()
	s := sessionState{
		Theme:       m.themeOverride,
		RecentFiles: m.recent.List(),
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
	m.backupCleanShutdown()
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
		// Hold the partial chord, surface the which-key hints (0081/40) and
		// arm the timeout; swallow the key meanwhile.
		prefix, conts := m.keys.PendingContinuations(keymap.Context(m.focusContext()))
		m.whichKey = append([]string{prefix + " —"}, keymap.FormatContinuations(conts, 12)...)
		return tea.Tick(keymap.TimeoutDuration, func(time.Time) tea.Msg {
			return keymapTimeoutMsg{}
		}), true
	case keymap.Resolved:
		m.whichKey = nil
		if c, ok := m.reg.Command(res.Command); ok {
			return c.Run(m.host), true
		}
		// A documented blocked default (0081/20 ledger): consume the chord and
		// say why it does nothing — a silent no-op is indistinguishable from a
		// typo'd binding (#267). Unregistered commands outside the ledger keep
		// falling through to the legacy dispatch.
		if reason, ok := keymap.BlockedReason(res.Command); ok {
			m.host.Notify(host.Info, res.Command+" is not available yet — "+reason)
			return nil, true
		}
	default:
		m.whichKey = nil
	}
	return nil, false
}

// buildKeymap constructs the keybinding resolver from config: the preset
// (keymap.preset, default JetBrains) overlaid by keymap.bindings.* overrides.
// Non-chord override keys (the focus_* stopgap sharing the same map) are ignored
// by the table builder.
func buildKeymap(cfg host.Config, bindings *keymap.LiveBindings) *keymap.Resolver {
	preset := keymap.PresetJetBrains
	leader := keymap.DefaultLeader
	overrides := map[string]string{}
	if cfg != nil {
		if v, ok := cfg.Get("keymap.preset"); ok {
			if p := strings.TrimSpace(v); p != "" {
				preset = p
			}
		}
		if v, ok := cfg.Get("keymap.leader"); ok {
			if l := strings.TrimSpace(v); l != "" {
				leader = l
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
	rows := append(keymap.Defaults(preset), keymap.LeaderRows(leader)...)
	table := keymap.BuildTable(rows, overrides, keymap.GOOS)
	if bindings != nil {
		bindings.Set(table)
	}
	return keymap.NewResolver(table)
}

// buildPalette wires the command palette: a ":" command mode reading the registry
// and an "@" file finder, tuned by the optional palette.* config keys.
func buildPalette(reg *registry.Registry, cfg host.Config, refs *refsMode, actions *actionsMode, bindings *keymap.LiveBindings, recent *recentFiles, symbols *symbolMode, pasteHist *pasteHistMode) *palette.Palette {
	pcfg := palette.Config{
		MaxResults:    paletteMaxResults(cfg),
		DefaultPrefix: paletteDefaultPrefix(cfg),
	}
	cmd := palette.NewCommandMode(reg, bindings, paletteHideOff(cfg))
	file := palette.NewFileMode()
	dir := palette.NewDirMode()
	proj := project.NewPickerMode(nil)
	mru := palette.NewRecentMode(recent.List)
	scr := palette.NewScratchMode(scratchList)
	all := palette.NewSearchAllMode(cmd, file, symbols)
	all.SetRecents(mru)
	return palette.New(pcfg, cmd, file, dir, proj, refs, actions, mru, all, symbols, scr, pasteHist)
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
		// Symmetric on purpose (#133): a live reload must re-enable a plugin
		// whose toggle flipped back, not just disable.
		if v, ok := cfg.Get("plugins." + id + ".enabled"); ok {
			reg.SetEnabled(id, v != "false")
		} else {
			reg.SetEnabled(id, true)
		}
	}
}

// terminalFocused reports whether the focused pane is a live terminal; a dead
// one (shell exited) falls back to normal key handling so ctrl+w can close it.
func (m Model) terminalFocused() bool {
	inst := m.panes.FocusedInstance()
	return inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().Running()
}

// terminalTitle renders the pane title: shell name plus the session's origin
// directory — the marker that keeps a terminal attributable after a project
// switch carried it along (#96).
func (m Model) terminalTitle(inst *pane.Instance) string {
	t := inst.Terminal()
	title := "TERMINAL"
	if s := t.ShellPath(); s != "" {
		title += " — " + filepath.Base(s)
	}
	if d := t.Dir(); d != "" {
		title += " · " + displayDir(d)
	}
	// The application's OSC 0/2 title (shells report the running command
	// here) takes the tail slot when it says more than the shell name (#97).
	if osc := t.Title(); osc != "" && osc != filepath.Base(t.ShellPath()) {
		title += " · " + osc
	}
	// Active interpreter mappings (#98): what php/python resolve to inside
	// this terminal, straight from the settings choice.
	for _, mp := range explicitMappings() {
		title += " · " + mp.Lang + "→" + project.CompactPath(mp.Interpreter)
	}
	return title
}

// displayDir shortens a directory for chrome: the base name when it is the
// working directory's base, the compacted path otherwise.
func displayDir(dir string) string {
	if cwd, err := os.Getwd(); err == nil && cwd == dir {
		return filepath.Base(dir)
	}
	return project.CompactPath(dir)
}

// terminalReservedKey handles the documented reserved set — the only keys a
// focused live terminal does NOT forward to the shell:
//
//	ctrl+tab    move focus to the next pane (the global escape hatch)
//	alt+f12     terminal.toggle — return focus to the previous pane (#97)
//
// The spatial focus moves (default ctrl+arrows, keymap.bindings.focus_*) and
// cmd+c over an active mouse selection are reserved in the caller (#228,
// #227). Everything else, including tab, ctrl+c, esc and the F-keys, belongs
// to the shell. shift+pgup/pgdn page the scrollback inside the pane itself.
func (m Model) terminalReservedKey(keys string) (bool, tea.Model, tea.Cmd) {
	switch keys {
	case "ctrl+tab":
		m.cycleFocus()
		return true, m, nil
	case "alt+f12":
		m.toggleTerminal()
		return true, m, nil
	}
	return false, m, nil
}

// currentTerminal returns the focused terminal instance, else the first
// terminal in pane order, else nil.
func (m Model) currentTerminal() *pane.Instance {
	if inst := m.panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindTerminal {
		return inst
	}
	for _, key := range m.panes.Keys() {
		if inst := m.panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal {
			return inst
		}
	}
	return nil
}

// toggleTerminal is the terminal.toggle state machine (#97, JetBrains
// alt+f12): no terminal → open one at the bottom split; one exists but is
// not focused → focus it (remembering where focus was); focused → return
// focus to the remembered pane (or the active editor as the fallback).
func (m *Model) toggleTerminal() {
	inst := m.currentTerminal()
	if inst == nil {
		m.terminalReturnFocus = m.panes.Focused()
		m.openTerminal()
		return
	}
	if m.panes.Focused() != inst.Key() {
		m.terminalReturnFocus = m.panes.Focused()
		m.setFocus(inst.Key())
		return
	}
	target := m.terminalReturnFocus
	if target == "" || !m.panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// explicitMappings collects the settings-page interpreter choices (#98):
// only [lang.<id>] interpreter entries — silent detection never injects, per
// the epic rule. The same lang.Interpreter seam the LSP toolchain reads.
func explicitMappings() []terminal.Mapping {
	c := config.Get()
	if c == nil {
		return nil
	}
	var out []terminal.Mapping
	for _, l := range lang.All() {
		explicit := c.Lang[l.ID]["interpreter"]
		if explicit == "" {
			continue
		}
		if path, source := lang.Interpreter(l.ID, ".", explicit); source == "config" && path != "" {
			out = append(out, terminal.Mapping{Lang: l.ID, Interpreter: path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lang < out[j].Lang })
	return out
}

// shimDir is the per-project shim directory, mirroring the state stores'
// IKE_CONFIG_DIR override.
func shimDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "shims")
	}
	return filepath.Join(".ike", "shims")
}

// terminalEnv regenerates the shims for the current settings and returns the
// spawn-environment overlay (nil when no explicit interpreter is chosen).
func terminalEnv() []string {
	mappings := explicitMappings()
	dir := shimDir()
	if ok, err := terminal.WriteShims(dir, mappings); err != nil || !ok {
		return nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return terminal.EnvOverlay(abs, mappings, os.Getenv("PATH"))
}

// openTerminal opens a fresh terminal pane rooted in the working directory
// (the project root), split below the active editor — the conventional
// JetBrains placement — falling back to the focused leaf when no editor
// exists.
func (m *Model) openTerminal() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.panes.Focused()
	}
	if target == "" || m.tree == nil {
		return
	}
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	key := m.panes.AddTerminal(terminal.Shell(shell), ".", terminalEnv(), m.host.Send)
	tree, ok := layout.SplitLeaf(m.tree, target, key, layout.ZoneBottom)
	if !ok {
		m.panes.Close(key)
		return
	}
	m.tree = tree
	m.setFocus(key)
	m.layout()
	saveLayout(m.tree, m.panes)
}

// explorer returns the singleton explorer model.
func (m Model) explorer() *explorer.Model {
	return m.panes.Get(pane.ExplorerKey).Explorer()
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.explorer().Init()}
	// Highlight any files restored from the previous session at startup, before
	// the user edits them, and announce each to the plugin hooks (#332): the
	// restore paths (restoreLayout/restoreSession) load editors directly via
	// editor.Load, bypassing openPath, so without this the LSP never learns about
	// files already open at launch and they get no diagnostics until reopened.
	// Init runs after main.go wires the sender, so the bridge's async results land.
	opened := map[string]bool{} // one EventFileOpened per file — shared tabs/leaves (#142)
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if !ed.HasFile() {
				continue
			}
			cmds = append(cmds, ed.Reparse())
			if path := ed.Path(); !opened[path] {
				opened[path] = true
				cmds = append(cmds, m.fireHooks(plugin.EventFileOpened, path)...)
			}
		}
	}
	return tea.Batch(cmds...)
}

// Update owns global keys (quit, focus switch), routes open/close messages, and
// forwards everything else to the focused pane.
// Update handles one message and then drains any notifications the handling
// raised (command Runs and routed updates call host.Notify synchronously), so
// a toast appears in the very frame its event produced. updateMsg holds the
// actual dispatch switch.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	start := time.Now()
	tm, cmd := m.updateMsg(msg)
	if took := time.Since(start); took > slowUpdateThreshold {
		// A stalled Update pass freezes the whole UI (#123); leave a trace so
		// the culprit is attributable after the fact (#125).
		logSlowUpdate(msg, took)
	}
	mm, ok := tm.(Model)
	if !ok {
		return tm, cmd
	}
	if tick := mm.drainNotifications(); tick != nil {
		cmd = tea.Batch(cmd, tick)
	}
	return mm, cmd
}

func (m Model) updateMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Wheel coalescing (#238): wheel events only accumulate; anything else
	// flushes the pending batch first so ordering against clicks, keys and
	// every other message is preserved.
	switch msg.(type) {
	case tea.MouseWheelMsg, wheelFlushMsg:
	default:
		if len(m.pendingWheel) > 0 {
			tm, cmd := m.flushWheel()
			mm, ok := tm.(Model)
			if !ok {
				return tm, cmd
			}
			tm2, cmd2 := mm.updateMsg(msg)
			return tm2, tea.Batch(cmd, cmd2)
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.shell.SetSize(m.width, m.height)
		m.palette.SetSize(m.width, m.height)
		m.finder.SetSize(m.width, m.height)
		m.callhier.SetSize(m.width, m.height)
		m.menu.SetWidth(m.width)
		{
			w, h := m.settingsSize()
			m.settings.SetSize(w, h)
		}
		// Now that the window is sized, surface any crash-recovery snapshots found
		// at startup (Roadmap 0210, #166), then the first-start LSP onboarding
		// dialog (#301) — recovery wins the shell when both are due.
		m.maybeOpenRecovery()
		m.maybeOpenOnboarding()
		return m, nil

	case tea.MouseClickMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mousePress})
	case tea.MouseReleaseMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mouseRelease})
	case tea.MouseMotionMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mouseMotion})
	case tea.MouseWheelMsg:
		return m.queueWheel(mouseEvent{Mouse: msg.Mouse(), action: mouseWheel})
	case wheelFlushMsg:
		return m.flushWheel()

	case explorer.OpenFileMsg:
		return m.openPath(msg.Path, msg.NewPane)

	case OpenFindInPathMsg:
		// project.findInPath (cmd+shift+f / palette): the find-in-path overlay
		// (Roadmap 0150), rooted at the working directory like the explorer.
		m.finder.SetSize(m.width, m.height)
		m.finder.Open(".")
		return m, nil

	case OpenReplaceInPathMsg:
		// project.replaceInPath (cmd+shift+r / palette): find-in-path plus the
		// replacement input, preview and apply keys (#86).
		m.finder.SetSize(m.width, m.height)
		m.finder.OpenReplace(".")
		return m, nil

	case finder.ReplaceRequestMsg:
		// Apply replacements: through open dirty buffers, on disk otherwise;
		// reports the summary notification.
		m.applyReplace(msg)
		return m, nil

	case search.BatchMsg, search.DoneMsg:
		// Streamed scan results (generation-filtered inside the finder). A scan
		// makes find-in-path the most recent search again for f3/shift+f3.
		m.inFileSearchRecent = false
		m.finder.Apply(msg)
		return m, nil

	case editor.SearchCommittedMsg:
		// A committed "/", "?" or cmd+f search: f3/shift+f3 repeat it until the
		// next find-in-path scan (#376).
		m.inFileSearchRecent = true
		return m, nil

	case finder.OpenLocationMsg:
		// A selected match: open the file with the cursor on the hit
		// (OpenLocationMsg lines are 1-based, openPathAt takes 0-based).
		return m.openPathAt(msg.Path, msg.Line-1, msg.Col)

	case MatchStepMsg:
		// search.nextMatch / search.prevMatch: when an in-file search is the
		// most recent one, repeat it on the active editor like n/N (#376);
		// otherwise walk the retained find-in-path results without the overlay.
		if m.inFileSearchRecent {
			if ed := m.activeEditor(); ed != nil && ed.HasSearch() {
				ed.RepeatSearch(msg.Delta < 0)
				return m, nil
			}
		}
		if it, ok := m.finder.Advance(msg.Delta); ok {
			return m.openPathAt(it.Path, it.Line-1, it.StartCol)
		}
		return m, nil

	case explorer.FileDeletedMsg:
		// The explorer removed a path; close any editor still showing it so a
		// deleted file does not linger in an open pane.
		m.closeEditorsForPath(msg.Path, msg.IsDir)
		return m, nil

	case explorer.FileMovedMsg:
		// A rename/move (or its undo/redo): open editors follow the new path
		// instead of closing (#175).
		return m, m.followMovedFile(msg)

	case RenameFileMsg:
		// file.rename (shift+f6 / palette): explorer prompt on the selection,
		// or the shell prompt for the focused editor's file.
		return m, m.startRenameFile()

	case MoveFileMsg:
		// file.move (f6 / palette): pick a target folder for the selection /
		// focused file via the palette's directory mode.
		m.startMoveFile()
		return m, nil

	case palette.MoveTargetMsg:
		return m, m.finishMoveFile(msg.Dir)

	case explorer.Msg:
		// File ops that open a modal prompt render it in the explorer pane,
		// but the prompt only receives keys while that pane holds focus. A
		// palette invocation leaves focus wherever it was — typed filenames
		// would execute as vim commands in the editor (#374) — so move focus
		// to the explorer first, re-showing it when hidden.
		switch msg.(type) {
		case explorer.NewFileMsg, explorer.NewDirMsg, explorer.DeleteMsg, explorer.RenameMsg:
			m.focusExplorer()
		}
		exp := m.explorer()
		var cmd tea.Cmd
		*exp, cmd = exp.Update(msg)
		return m, cmd

	case host.OpenFileRequest:
		return m.openPath(msg.Path, msg.NewPane)

	case CloseTabMsg:
		// editor.closeTab (cmd+w / palette): close the focused editor pane's
		// active tab, the pane itself on its last tab (#156); a no-op on the
		// explorer / last leaf, matching the hardcoded ctrl+w. Dirty buffers
		// open the unsaved-changes guard first (#259).
		m.guardedCloseFocused()
		return m, nil

	case TabStepMsg:
		// editor.tab.next / editor.tab.prev (alt+right / alt+left, #158).
		m.stepTab(msg.Delta)
		return m, nil
	case TabSelectMsg:
		// editor.tab.select1…9 (alt+1…alt+9): jump straight to a tab.
		m.selectTab(msg.Index)
		return m, nil
	case TabMoveMsg:
		// editor.tab.moveLeft / editor.tab.moveRight (alt+shift+arrows).
		m.moveTab(msg.Delta)
		return m, nil
	case TabReopenMsg:
		// editor.tab.reopenClosed (alt+shift+t): pop the reopen ring.
		return m.reopenClosedTab()

	case ShowKeymapHelpMsg:
		// palette.keymapHelp (f1, cmd+k cmd+s / palette): the cheatsheet overlay.
		m.openHelp()
		return m, nil

	case CyclePaneFocusMsg:
		// pane.switcher (ctrl+tab / palette): same cycle as the hardcoded tab.
		m.cycleFocus()
		return m, nil

	case SaveAllMsg:
		// editor.saveAll (cmd+shift+s / palette): write every dirty editor,
		// background tabs included.
		var cmds []tea.Cmd
		for _, key := range m.panes.Keys() {
			inst := m.panes.Get(key)
			if inst == nil || inst.Kind() != pane.KindEditor {
				continue
			}
			for i := 0; i < inst.TabCount(); i++ {
				if inst.TabEditor(i).Dirty() {
					cmds = append(cmds, inst.UpdateTab(i, editor.ActionMsg{Action: "write"}))
				}
			}
		}
		switch n := len(cmds); {
		case n == 0:
			// A silent no-op is indistinguishable from a dead chord (#275).
			m.host.Notify(host.Info, "nothing to save")
		case n == 1:
			m.host.Notify(host.Info, "saved 1 file")
		default:
			m.host.Notify(host.Info, "saved "+strconv.Itoa(n)+" files")
		}
		return m, tea.Batch(cmds...)

	case settings.VersionMsg:
		// Async interpreter version probes land in the toolchain page's cache.
		m.settings.Deliver(msg)
		return m, nil

	case settings.EnvMsg:
		// Python environment action finished (#132): show the result on the
		// page, and on success register the interpreter through write-back
		// (lang.Interpreter stays the single source of truth) and restart the
		// language's server against it.
		m.settings.Deliver(msg)
		if msg.Err != nil {
			m.host.Notify(host.Warn, "python environment: "+msg.Err.Error())
			return m, nil
		}
		m.host.Notify(host.Info, msg.Label+" — registered as project interpreter")
		cmds := []tea.Cmd{config.WriteAndReload(m.cfgOpts, config.ProjectScope, "lang."+msg.LangID+".interpreter", msg.Interpreter)}
		if c, ok := m.reg.Command("lsp.restart"); ok {
			cmds = append(cmds, c.Run(m.host))
		}
		return m, tea.Batch(cmds...)

	case SplitFocusedMsg:
		// pane.splitDown / pane.splitUp (cmd+k down / cmd+k up): split the
		// focused leaf with a fresh empty editor, no drag or file open needed.
		m.SplitFocused(msg.Zone)
		return m, nil

	case MaximizePaneMsg:
		// pane.maximize (cmd+k z / View menu, #358): tmux-style zoom toggle.
		m.toggleMaximize()
		return m, nil

	case ZenModeMsg:
		// view.zenMode (cmd+k shift+z / View menu, #359): maximize + no chrome.
		m.toggleZen()
		return m, nil

	case SplitViewMsg:
		// editor.splitViewRight / editor.splitViewDown (#147): second shared
		// view of the focused editor's document.
		return m.splitView(msg.Zone)

	case NewScratchMsg:
		// scratch.new[.<lang>] (#351): create under the scratch store, open
		// through the standard funnel.
		return m.newScratch(msg.Ext)

	case OpenSettingsMsg:
		// settings.open (cmd+, / menu / palette): the floating settings panel.
		w, h := m.settingsSize()
		m.settings.SetSize(w, h)
		m.settings.Open()
		return m, nil

	case ToggleMenuMsg:
		// menu.open (f10 / palette): open the first menu, or close an open one.
		if m.menuEnabled() {
			m.menu.Toggle()
		}
		return m, nil

	case menu.RunMsg:
		return m, m.RunCommand(msg.Command)

	case ShowNotificationHistoryMsg:
		// notifications.history (palette): the history ring in the floating shell.
		body := m.historyView()
		m.shell.SetContent(ui.ModelContent{Heading: "NOTIFICATIONS", Body: func() string { return body }})
		m.shell.SetSize(m.width, m.height)
		m.shell.Open()
		return m, nil

	case ToggleExplorerFocusMsg:
		// explorer.toggle (cmd+1 / space e): the JetBrains cmd+1 state machine
		// (#268) — focused tree hides, visible unfocused tree gains focus, a
		// hidden tree comes back at its remembered width and takes focus.
		m.toggleExplorer()
		return m, nil

	case TerminalNewMsg:
		// terminal.new (palette / menu): split the focused leaf with a fresh
		// shell session rooted in the project (Roadmap 0170, #95).
		m.openTerminal()
		return m, nil

	case TerminalToggleMsg:
		// terminal.toggle (alt+f12 / palette / menu): the JetBrains state
		// machine — create, focus, or return focus (#97).
		m.toggleTerminal()
		return m, nil

	case TerminalClearMsg:
		// terminal.clear: scrollback gone, screen repainted via ctrl+l.
		if inst := m.currentTerminal(); inst != nil {
			inst.Terminal().Clear()
		}
		return m, nil

	case terminal.OutputMsg:
		// The grid changed; returning repaints. The msg is send-coalesced.
		return m, nil

	case terminal.ExitedMsg:
		// The shell ended: close its pane like ctrl+w would; when the layout
		// refuses (last leaf), the pane stays showing [process exited].
		if m.panes.Has(msg.Key) {
			if m.closeKey(msg.Key) {
				m.setFocus(m.focusAfterClose())
				m.syncExplorerOpen()
				m.layout()
				saveLayout(m.tree, m.panes)
			}
		}
		return m, nil

	case GoToFileMsg:
		// project.goToFile (cmd+shift+o / palette): the centered fuzzy file
		// finder, locked to the "@" mode, from any context.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, '@')
		return m, nil

	case ShowRecentFilesMsg:
		// palette.recentFiles (cmd+e / leader m / menu): the MRU file list,
		// locked to its mode. The active file is excluded so opening the
		// palette and pressing enter jumps to the previously used file.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{
			ContextID:  m.focusContext(),
			Root:       ".",
			ActivePath: m.activeFilePath(),
		}, palette.RecentPrefix)
		return m, nil

	case ShowPasteHistoryMsg:
		// editor.pasteFromHistory (cmd+shift+v / Edit menu, #57): snapshot the
		// focused editor's yank/delete history into the picker.
		inst := m.panes.FocusedInstance()
		if inst == nil || inst.Kind() != pane.KindEditor {
			m.host.Notify(host.Info, "paste history needs a focused editor")
			return m, nil
		}
		hist := inst.Editor().RegisterHistory()
		if len(hist) == 0 {
			m.host.Notify(host.Info, "clipboard history is empty — yank or delete something first")
			return m, nil
		}
		m.pasteHist.Set(hist)
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, pasteHistPrefix)
		return m, nil

	case PasteHistoryEntryMsg:
		// A picker row was chosen: paste that entry into the focused editor
		// with Cmd+V semantics (it also becomes the current clipboard).
		if inst := m.panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor {
			inst.Editor().PasteHistoryEntry(msg.Index)
		}
		return m, nil

	case ShowScratchFilesMsg:
		// scratch.list (palette / File menu): the scratch store newest-first,
		// locked to its mode (#352); enter opens through the standard funnel.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{
			ContextID: m.focusContext(),
			Root:      ".",
		}, palette.ScratchPrefix)
		return m, nil

	case ShowSearchEverywhereMsg:
		// palette.searchEverywhere (cmd+shift+a / space space): one query
		// ranked across commands and files, locked to its mode. ActivePath
		// lets the empty-query recents listing exclude the open file (#263).
		// The workspace-symbol seat (#295) needs the bridge continuation;
		// prime it silently on the first open.
		var prime tea.Cmd
		if m.symbols.request == nil {
			if c, ok := m.reg.Command("project.goToClass"); ok {
				m.symbolPriming = true
				prime = c.Run(m.host)
			}
		}
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{
			ContextID:  m.focusContext(),
			Root:       ".",
			ActivePath: m.activeFilePath(),
		}, palette.SearchAllPrefix)
		return m, prime

	case project.OpenPickerMsg:
		// project.switch (alt+shift+p / palette / menu): the recent-projects
		// picker, locked to its mode; the selection lands as project.PickedMsg.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, project.PickerPrefix)
		return m, nil

	case project.PickedMsg:
		// Picker selection: validate off the Update loop; the result comes
		// back as SwitchProjectMsg or SwitchFailedMsg.
		return m, project.SwitchTo(msg.Path)

	case project.SwitchProjectMsg:
		return m.handleSwitchProject(msg)

	case project.UnsavedChangesMsg:
		m.openSwitchPrompt(msg.Root)
		return m, nil

	case project.SwitchFailedMsg:
		m.host.Notify(host.Error, "cannot switch project: "+msg.Err.Error())
		return m, nil

	case project.SwitchedMsg:
		m.host.Notify(host.Info, "switched to "+msg.Root)
		return m, nil

	case project.RecordedMsg:
		// History write-back after a switch. A failure is worth a toast; a
		// success reloads the config so the picker's in-memory history already
		// lists the just-recorded open.
		if msg.Err != nil {
			m.host.Notify(host.Warn, "could not record project history: "+msg.Err.Error())
			return m, nil
		}
		return m, config.Reload(m.cfgOpts)

	case SelectThemeMsg:
		// Session-only theme switch from the palette's "Theme: <name>" commands.
		m.selectTheme(msg.Name)
		return m, nil

	case config.ConfigReloadedMsg:
		// Live re-theme (Roadmap 0110): publish the fresh config and re-resolve
		// the palette so a [theme].name change lands without a restart.
		m.reloadConfig(msg.Config)
		return m, nil

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

	case highlight.SpansMsg:
		// Async Tree-sitter parse results route to every editor leaf owning the
		// path (background panes and shared-document views included); each pane
		// filters by its own document version.
		return m, m.routeToEditor(msg.Path, msg)

	case editor.SyncMsg:
		// A shared document changed in one pane (#142): every other view of the
		// same file re-clamps and mirrors the flags. Dirty/stale are read from
		// the originating pane *now* (not at emit time), so late or reordered
		// broadcasts always converge on the current document state.
		var skip *editor.Model
		if origin := m.panes.Get(msg.FromKey); origin != nil && origin.Kind() == pane.KindEditor {
			if ed := origin.EditorForPath(msg.Path); ed != nil {
				skip = ed
				msg.Dirty = ed.Dirty()
				msg.Stale = ed.Stale()
			}
		}
		var cmds []tea.Cmd
		// Crash-recovery write side (#167): the same seam drives the snapshot
		// debounce — dirty (re)arms it, clean cancels and drops the snapshot.
		if c := m.backupOnSync(msg.FromKey, msg.Path); c != nil {
			cmds = append(cmds, c)
		}
		// Deliver to every other view of the document — other panes and this
		// pane's background tabs alike; only the originating tab is skipped.
		for _, key := range m.editorKeysForPath(msg.Path) {
			if cmd := m.panes.Get(key).UpdateForPath(msg.Path, skip, msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case ilsp.DiagnosticsMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.CompletionMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.HoverMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.SignatureHelpMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.SemanticSpansMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.DocumentHighlightsMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.DefinitionMsg:
		// Navigate to a definition target and place the cursor there. Also the
		// activation msg of a references-list entry (references.go).
		return m.openPathAt(msg.Path, msg.Line, msg.Col)

	case NavBackMsg:
		// nav.back (Roadmap 0220): return to the previous recorded position.
		return m.navigateHistory(m.navHist.BackWhere, "no earlier position in the navigation history")
	case NavForwardMsg:
		return m.navigateHistory(m.navHist.ForwardWhere, "no later position in the navigation history")

	case ilsp.CodeActionsMsg:
		// lsp.codeAction: the offer opens as a locked palette list; picking an
		// entry dispatches actionPickedMsg below.
		m.actions.Set(msg)
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, actionsPrefix)
		return m, nil

	case actionPickedMsg:
		return m, m.actions.Run(msg)

	case ilsp.RenamePromptMsg:
		// lsp.rename: the server validated the position; prompt for the name.
		m.openLSPRenamePrompt(msg)
		return m, nil

	case ilsp.SymbolPromptMsg:
		// project.goToClass (cmd+o / leader S): install the bridge
		// continuation as the live mode's re-query hook (#295) and open the
		// palette locked to it — unless this run only primes the hook for
		// the search-everywhere seat.
		m.symbols.SetRequest(msg.Apply)
		if m.symbolPriming {
			m.symbolPriming = false
			return m, nil
		}
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, symbolsPrefix)
		return m, nil

	case ilsp.SymbolResultsMsg:
		// Workspace-symbol hits (#295): fresh rows into the live mode's
		// cache (stale queries are dropped there), then the open palette
		// recomputes. No provider stays an honest toast; zero hits render as
		// the palette's natural empty list.
		if msg.NoProvider {
			m.host.Notify(host.Warn, "no running language server supports workspace symbols")
			return m, nil
		}
		m.symbols.SetHits(msg.Query, msg.Hits)
		m.palette.Refresh()
		return m, nil

	case palette.LiveTickMsg:
		// A live mode's settled-query debounce fired (#295).
		return m, m.palette.LiveTick(msg)

	case ilsp.FormatEditsMsg:
		// lsp.format / rename / code actions: applied as one undo unit
		// (editor/textedit.go) through exactly ONE view. Views of the same
		// file alias one document (#142), so routing to every view — like
		// diagnostics or highlight spans — applied the edits once per view
		// (#366: rename z -> match1 became match1atch1 with a second view
		// open). The first view applies, the change-sync broadcast converges
		// the others, mirroring replace.go's single-view rule.
		if views := m.editorViewsForPath(msg.Path); len(views) > 0 {
			edits := make([]editor.TextEdit, len(msg.Edits))
			for i, e := range msg.Edits {
				edits[i] = editor.TextEdit{
					StartLine: e.StartLine, StartCol: e.StartCol,
					EndLine: e.EndLine, EndCol: e.EndCol,
					Text: e.Text,
				}
			}
			views[0].ApplyTextEdits(edits)
		}
		return m, nil

	case ilsp.ReferencesMsg:
		// lsp.references (alt+f7 / palette): nothing found is a toast, a single
		// usage navigates straight there, more open the results list.
		switch len(msg.Refs) {
		case 0:
			m.host.Notify(host.Info, "no usages found")
			return m, nil
		case 1:
			r := msg.Refs[0]
			return m.openPathAt(r.Path, r.Line, r.Col)
		}
		m.refs.Set(msg.Refs)
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, refsPrefix)
		return m, nil
	case ilsp.CallHierarchyMsg:
		// lsp.callHierarchy (#173): the prepared roots open the tree overlay;
		// nothing prepared never reaches here (the bridge toasts instead).
		m.callhier.SetSize(m.width, m.height)
		return m, m.callhier.Open(msg)
	case ilsp.CallHierarchyCallsMsg:
		// One lazy node expansion; stale replies are dropped inside.
		m.callhier.Apply(msg)
		return m, nil
	case ilsp.DefinitionCandidatesMsg:
		// lsp.definition with several targets (#279): pick, don't guess. The
		// list reuses the references rows; Enter navigates via DefinitionMsg.
		m.refs.Set(msg.Refs)
		m.refs.SetPlaceholder("Definitions — pick a target…")
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, refsPrefix)
		return m, nil
	case ilsp.ServerStatusMsg:
		// Persistent server state stays on the status line; transient events
		// (crash, restart, launch failure) surface as toasts (Roadmap 0130).
		// The Language Servers settings page tracks per-language state (#130).
		m.settings.Deliver(msg)
		if msg.Kind == ilsp.ServerEventError {
			// Unrecoverable failures (launch errors, failed installs, #131)
			// also leave a debug.log line for post-mortems (#125).
			logDiagnostic("lsp: " + msg.Text)
		}
		switch msg.Kind {
		case ilsp.ServerEventInfo:
			m.host.Notify(host.Info, msg.Text)
		case ilsp.ServerEventWarn:
			m.host.Notify(host.Warn, msg.Text)
		case ilsp.ServerEventError:
			m.host.Notify(host.Error, msg.Text)
		default:
			// Persistent state is tracked per language (#380): the status line
			// shows only the focused buffer's language, so "gopls not found"
			// no longer haunts a plain-text buffer. The host's global status
			// segment stays reserved for plugins (host.API.SetStatus).
			if msg.Lang != "" {
				m.lspStatus[msg.Lang] = msg.Text
			} else {
				m.host.SetStatus(msg.Text)
			}
		}
		return m, nil

	case watch.EventMsg:
		// External file changes (Roadmap 0140): directory events refresh the
		// explorer, file events go to the editor leaf owning the path.
		if msg.Kind == watch.DirChanged {
			if m.panes.Has(pane.ExplorerKey) {
				return m, m.panes.Get(pane.ExplorerKey).Update(msg)
			}
			return m, nil
		}
		if msg.Kind == watch.FileRemoved {
			if _, err := os.Stat(msg.Path); err == nil {
				// The file is back: a replace-in-place (write temp + rename,
				// git checkout) coalesced remove over create — a content change.
				msg.Kind = watch.FileChanged
			} else if ed := m.editorForPath(msg.Path); ed != nil {
				if !ed.Dirty() {
					// Externally deleted, nothing unsaved: same as the
					// explorer's delete flow — close the pane (#83). A dirty
					// buffer instead stays open, marked stale by the editor.
					m.closeEditorsForPath(msg.Path, false)
					return m, nil
				}
			}
		}
		return m, m.routeToEditor(msg.Path, msg)

	case editor.ConflictMsg:
		// Saving a stale buffer (Roadmap 0140, #82): prompt before overwriting
		// the external change.
		m.openConflictPrompt(msg.Path)
		return m, nil

	case editor.NoticeMsg:
		// Editor action feedback ("no comment syntax for this file") → toast.
		m.host.Notify(host.Info, msg.Text)
		return m, nil

	case editor.CloseMsg:
		// :q / :wq closes the focused editor leaf, mirroring CloseFocused;
		// :q! skips the unsaved-changes guard, vim-style (#259).
		if msg.Force {
			m.closeFocused()
		} else {
			m.guardedCloseFocused()
		}
		return m, nil

	case backupTickMsg:
		// A debounce deadline elapsed: snapshot the quiet dirty buffers and
		// re-arm while marks remain (Roadmap 0210, #167).
		m.backupTickArmed = false
		return m, tea.Batch(m.snapshotDueBackups(time.Now()), m.armBackupTick())

	case toastExpireMsg:
		m.expireToast(msg.id)
		return m, nil

	case keymapTimeoutMsg:
		// A held partial chord timed out: resolve it as an exact binding if one
		// exists (e.g. cmd+k alone → vcs.commit), else discard it. Either way
		// the which-key overlay goes.
		m.whichKey = nil
		if res := m.keys.Timeout(keymap.Context(m.focusContext())); res.Status == keymap.Resolved {
			if c, ok := m.reg.Command(res.Command); ok {
				return m, c.Run(m.host)
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		// Esc dismisses persistent error toasts but keeps its normal meaning
		// (pass-through) so it never costs an extra press elsewhere.
		if msg.Code == tea.KeyEscape {
			m.dismissErrorToasts()
		}
		// The settings panel is a full-window modal: it owns the keyboard.
		if m.settings.IsOpen() {
			return m, m.settings.Update(msg)
		}
		// An open menu dropdown owns the keyboard (arrows/enter/esc).
		if m.menu.IsOpen() {
			return m, m.menu.Update(msg)
		}
		if m.finder.IsOpen() {
			// The find-in-path overlay owns the keyboard like the palette.
			return m, m.finder.Update(msg)
		}
		if m.callhier.IsOpen() {
			// The call-hierarchy overlay owns the keyboard the same way (#173).
			return m, m.callhier.Update(msg)
		}
		if m.palette.IsOpen() {
			return m, m.palette.Update(msg)
		}
		// The crash-recovery prompt (Roadmap 0210, #166) owns the keyboard at
		// startup: r / d / s decide the highlighted file, j / k move, esc skips.
		if m.recoveryOpen() {
			return m.updateRecovery(msg)
		}
		// The first-start LSP onboarding dialog (#301) owns the keyboard the
		// same way: space toggles, enter installs, esc skips.
		if m.onboardingOpen() {
			return m.updateOnboarding(msg)
		}
		// The save-conflict prompt owns the keyboard ahead of the generic shell
		// handling: k / r / esc answer it, everything else is swallowed.
		if m.conflictOpen() {
			return m.updateConflict(msg)
		}
		// The unsaved-changes guard before a project switch (0090, #3) owns the
		// keyboard the same way: s / d / esc answer it.
		if m.switchPromptOpen() {
			return m.updateSwitchPrompt(msg)
		}
		// The unsaved-changes guard on a close (#259): s / d / esc answer it.
		if m.closePromptOpen() {
			return m.updateClosePrompt(msg)
		}
		// A focused terminal takes every key raw (vim/htop must see them all)
		// except the reserved set below; scrollback paging keys are handled by
		// the pane itself.
		if m.terminalFocused() {
			if handled, tm, cmd := m.terminalReservedKey(msg.String()); handled {
				return tm, cmd
			}
			// The spatial focus moves (default ctrl+arrows) escape the terminal
			// like every other pane (#228); keymap.bindings.focus_* overrides
			// apply, and a disabled direction stays with the shell.
			if dir, ok := m.focusKeys[msg.String()]; ok {
				m.FocusDir(dir)
				return m, nil
			}
			// cmd+c copies an active mouse selection (#227); without one the
			// key stays with the shell.
			if term := m.panes.FocusedInstance().Terminal(); term.HasSelection() {
				if k, ok := keymap.FromKeyMsg(msg); ok && k.Mods == keymap.ModMeta && k.Base == "c" {
					m.copyTerminalSelection(term)
					return m, nil
				}
			}
			return m.routeKey(msg)
		}
		// The rename prompt (#175) owns the keyboard the same way: typed
		// characters build the new name, enter applies, esc cancels.
		if m.renameOpen() {
			return m.updateRenamePrompt(msg)
		}
		// The symbol-rename prompt (0100, #6) mirrors it.
		if m.lspRenameOpen() {
			return m.updateLSPRenamePrompt(msg)
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
		// "?" stays a plain non-capturing key outside the chord table; f1
		// normally resolves through the keymap layer above (palette.keymapHelp)
		// and only lands here when that command is not registered.
		if keys == "?" || keys == "f1" {
			m.openHelp()
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
			// Quit routes through the unsaved-changes guard (#287) so a
			// dirty buffer prompts instead of being dropped.
			return m.guardedQuit()
		case "q":
			if m.quitKey() {
				return m.guardedQuit()
			}
		case "tab":
			m.cycleFocus()
			return m, nil
		case "ctrl+w":
			// Close the focused editor pane (no-op on the explorer / last leaf).
			// Roadmap 0080 owns the final keymap; this is the default binding.
			// Dirty buffers open the unsaved-changes guard first (#259).
			m.guardedCloseFocused()
			return m, nil
		}
		return m.routeKey(msg)
	}
	return m, nil
}

// openPath opens path honouring the open target: a registered FileHandler claims
// it first regardless of target; otherwise the file lands in the active editor's
// tab list (#156) — activating an existing tab, appending a new one, or filling
// a scratch tab — and NewPane splits off a fresh editor and loads there.
// EventFileOpened hooks fire either way.
func (m Model) openPath(path string, newPane bool) (tea.Model, tea.Cmd) {
	// Every open source spells paths differently (explorer: absolute, palette
	// modes: root-relative) — canonicalize first so the same file always
	// lands on its existing tab instead of a duplicate buffer (#272).
	path = canonicalPath(path)
	// Leaving one file for another is a navigation jump (Roadmap 0220);
	// same-file opens are handled by openPathAt, which knows the target line.
	if cur := m.currentNavPos(); cur.Path != "" && cur.Path != path {
		m.recordNavFrom(cur)
	}
	var cmds []tea.Cmd
	if h, ok := m.reg.ResolveHandler(path, readHead(path)); ok {
		cmds = append(cmds, h.Open(m.host, path))
	} else {
		key := m.activeEditorKey()
		if newPane || key == "" {
			key = m.spawnEditor()
		}
		if m.openInTab(key, path) {
			m.recent.Touch(path)  // MRU for the recent-files palette mode (0230)
			m.watcher.Track(path) // poll-fallback comparison for open buffers
			m.explorer().SetActive(path)
			m.syncExplorerOpen()
			m.setFocus(key)
			m.layout()
			saveLayout(m.tree, m.panes)
			cmds = append(cmds, m.panes.Get(key).Editor().Reparse())
		}
	}
	cmds = append(cmds, m.fireHooks(plugin.EventFileOpened, path)...)
	return m, tea.Batch(cmds...)
}

// openInTab lands path in the editor pane key's tab list (#156): a tab already
// showing the file is activated; a pathless scratch tab is filled in place
// (fresh panes keep today's behavior); otherwise a new tab is appended after
// autosaving the document being left (#174). It reports whether the file is
// now open and active in the pane.
func (m *Model) openInTab(key, path string) bool {
	inst := m.panes.Get(key)
	if idx := inst.TabForPath(path); idx >= 0 {
		m.activateTab(inst, idx)
		return true
	}
	added := false
	if inst.Editor().HasFile() {
		if m.autosaveEnabled() {
			// Leaving the active tab's document counts as leaving it (#174).
			inst.Editor().Autosave()
		}
		inst.AddTab()
		m.installEmitter(key)
		added = true
	}
	if err := m.loadOrShare(key, path); err != nil {
		if added {
			// The freshly appended tab never held the file: drop it again.
			inst.CloseTab(inst.ActiveTab())
		}
		return false
	}
	return true
}

// activateTab switches pane inst to tab idx, autosaving the document being
// left — a tab switch leaves the document just like a focus switch (#174).
func (m *Model) activateTab(inst *pane.Instance, idx int) {
	if idx == inst.ActiveTab() {
		return
	}
	if m.autosaveEnabled() {
		inst.Editor().Autosave()
	}
	inst.ActivateTab(idx)
	// Returning to a background tab counts as using its file (MRU, 0230).
	if ed := inst.Editor(); ed.HasFile() {
		m.recent.Touch(ed.Path())
	}
}

// activeFilePath is the focused (else most recent) editor's file, or "".
func (m Model) activeFilePath() string {
	if key := m.activeEditorKey(); key != "" {
		if ed := m.panes.Get(key).Editor(); ed.HasFile() {
			return ed.Path()
		}
	}
	return ""
}

// loadOrShare fills the active tab of editor pane key with path: when another
// tab — in this pane or any other — already shows that file, the tab becomes a
// second view of the same document (shared buffer + undo stack, #142) instead
// of loading a divergent copy; otherwise the file is read from disk.
func (m *Model) loadOrShare(key, path string) error {
	target := m.panes.Get(key).Editor()
	if src := m.editorForPath(path); src != nil && src != target {
		target.ShareDocumentWith(src)
		return nil
	}
	return target.Load(path)
}

// syncExplorerOpen refreshes the explorer's set of open files (every editor
// pane holding a file), so their rows render underlined + italic. Called after
// anything that opens or closes an editor.
func (m *Model) syncExplorerOpen() {
	var open []string
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() {
				open = append(open, ed.Path())
			}
		}
	}
	m.explorer().SetOpen(open)
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

// openHelp shows the keymap cheatsheet overlay in the modal shell, scoped to
// the focused pane's context (global commands plus that context's own).
func (m *Model) openHelp() {
	// Honest blocked section (0081/40): bindings whose command has no owner
	// yet appear with their dependency instead of vanishing. Built live from
	// the effective table on every open.
	m.help.SetExtra(m.blockedHelpGroup())
	m.help.SetFilter("") // each open starts unfiltered (#271)
	m.help.Snapshot(m.focusContext())
	m.shell.SetContent(m.help)
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// blockedHelpGroup collects the blocked default bindings for the cheatsheet.
func (m Model) blockedHelpGroup() help.Group {
	g := help.Group{Label: "blocked (dependency not landed)"}
	if m.bindings == nil || m.bindings.Table() == nil {
		return g
	}
	seen := map[string]bool{}
	for _, b := range m.bindings.Table().Bindings() {
		reason, blocked := keymap.BlockedReason(b.Command)
		if !blocked || seen[b.Command] {
			continue
		}
		seen[b.Command] = true
		title := b.Title
		if title == "" {
			title = b.Command
		}
		g.Entries = append(g.Entries, help.Entry{ID: b.Command, Title: title, Shortcut: "✗ needs " + reason})
	}
	sort.Slice(g.Entries, func(i, j int) bool { return g.Entries[i].Title < g.Entries[j].Title })
	return g
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
	m.autosaveOnBlur(key)
	m.panes.SetFocused(key)
	if inst := m.panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
		m.recentEditor = key
		// The explorer's accent always tracks the focused editor's file, so
		// switching panes (click, focus cycling) moves the highlight with it.
		if inst.Editor().HasFile() {
			m.explorer().SetActive(inst.Editor().Path())
		}
	}
}

// autosaveOnBlur saves the editor pane focus is leaving (#174): every focus
// transition funnels through setFocus, so one hook covers Ctrl+arrows, the
// pane switcher, mouse clicks and the explorer toggle. Autosave itself skips
// clean, stale and pathless buffers.
func (m *Model) autosaveOnBlur(next string) {
	if !m.autosaveEnabled() {
		return
	}
	old := m.panes.Focused()
	if old == "" || old == next {
		return
	}
	if inst := m.panes.Get(old); inst != nil && inst.Kind() == pane.KindEditor {
		inst.Editor().Autosave()
	}
}

// autosaveEnabled reads editor.auto_save live from the config ("focus" unless
// explicitly "off"), so a settings change applies without restart.
func (m *Model) autosaveEnabled() bool {
	v, ok := m.host.Config().Get("editor.auto_save")
	return !ok || v != "off"
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
	m.installEmitter(newKey)
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

// splitView implements editor.splitViewRight/Down (#147): split the focused
// editor leaf toward zone and turn the new pane into a second live view of
// the same document (#142), with cursor and scroll copied from the source so
// both views start at the same spot; the new view keeps the focus JetBrains
// gives it. A pane without a file (scratch editor, explorer, terminal) is a
// no-op with a toast — there is no document to share.
func (m Model) splitView(zone layout.Zone) (tea.Model, tea.Cmd) {
	target := m.panes.Focused()
	inst := m.panes.Get(target)
	if inst == nil || inst.Kind() != pane.KindEditor || !inst.Editor().HasFile() {
		m.host.Notify(host.Info, "no file to split — open one first")
		return m, nil
	}
	src := inst.Editor()
	line, col := src.CursorPos()
	top, left := src.ScrollOffset()
	m.SplitFocused(zone)
	newKey := m.panes.Focused()
	if newKey == target {
		return m, nil // split failed (leaf vanished mid-flight); nothing changed
	}
	ed := m.panes.Get(newKey).Editor()
	ed.ShareDocumentWith(src)
	ed.SetCursor(line, col)
	ed.SetScroll(top, left)
	m.syncExplorerOpen()
	return m, ed.Reparse()
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
	m.installEmitter(newKey)
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

// CloseFocused closes the focused editor pane's active tab; the pane itself
// closes — collapsing its sibling up and refocusing it — only when its last
// tab goes (#156), preserving today's cmd+w feel for single-tab panes. It is
// a no-op on the explorer (a singleton) and on the last leaf, so the workspace
// never empties and context resolution never loses its explorer.
func (m *Model) CloseFocused() { m.guardedCloseFocused() }

func (m *Model) closeFocused() {
	if inst := m.panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor && inst.TabCount() > 1 {
		m.closeTab(inst, inst.ActiveTab())
		return
	}
	if m.closeKey(m.panes.Focused()) {
		// Focus the leaf that now occupies the closed pane's position: the first
		// leaf in walk order is a safe, always-present choice (explorer at minimum).
		m.setFocus(m.focusAfterClose())
		m.syncExplorerOpen()
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
	for _, ed := range inst.Editors() {
		m.rememberClosedTab(ed)
	}
	m.backupDropOnClose(inst, key)
	m.tree = tree
	m.panes.Close(key)
	if m.recentEditor == key {
		m.recentEditor = firstEditorKey(layout.Leaves(m.tree))
	}
	return true
}

// closeTab closes tab idx of editor pane inst, applying the same unsaved-
// changes guard as a pane close: the crash-backup snapshot is dropped unless
// another tab or pane still shows the document (#156). The caller guarantees
// the pane holds more than one tab; the pane's chrome, explorer accent and
// persisted layout follow the tab that takes over.
func (m *Model) closeTab(inst *pane.Instance, idx int) {
	ed := inst.TabEditor(idx)
	if ed == nil || inst.TabCount() <= 1 {
		return
	}
	m.rememberClosedTab(ed)
	m.backupDropOnCloseTab(ed, inst.Key())
	inst.CloseTab(idx)
	m.syncExplorerOpen()
	if next := inst.Editor(); next.HasFile() && inst.Key() == m.panes.Focused() {
		m.explorer().SetActive(next.Path())
	}
	saveLayout(m.tree, m.panes)
}

// closeEditorsForPath closes every editor leaf showing path (or, when isDir,
// any file beneath it), so deleting a file in the explorer does not leave a
// stale editor open on it. It relayouts and persists once if anything closed,
// and refocuses only when the focused leaf itself was removed.
// editorKeyForPath returns the key of the editor leaf currently showing path, or
// "" if none is open. Used to route async LSP/highlight results to the owning
// pane regardless of focus.
func (m Model) editorKeyForPath(path string) string {
	if keys := m.editorKeysForPath(path); len(keys) > 0 {
		return keys[0]
	}
	return ""
}

// editorKeysForPath returns every editor leaf holding path in any tab. Multiple
// panes and tabs can view the same (shared, #142) document, so async per-path
// messages must reach all of them, not just the first.
func (m Model) editorKeysForPath(path string) []string {
	var keys []string
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst != nil && inst.Kind() == pane.KindEditor && inst.TabForPath(path) >= 0 {
			keys = append(keys, key)
		}
	}
	return keys
}

// editorForPath returns the editor model of a tab showing path, preferring the
// active editor pane's tab, then the first match in pane order. nil when the
// file is open nowhere.
func (m Model) editorForPath(path string) *editor.Model {
	if key := m.activeEditorKey(); key != "" {
		if ed := m.panes.Get(key).EditorForPath(path); ed != nil {
			return ed
		}
	}
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		if ed := inst.EditorForPath(path); ed != nil {
			return ed
		}
	}
	return nil
}

// editorViewsForPath returns every tab's editor model showing path, across all
// panes — the per-view fan-out shared documents (#142) and tabs (#156) need.
func (m Model) editorViewsForPath(path string) []*editor.Model {
	var out []*editor.Model
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, ed := range inst.Editors() {
			if ed.HasFile() && ed.Path() == path {
				out = append(out, ed)
			}
		}
	}
	return out
}

// routeToEditor forwards an LSP/highlight result message to every tab owning
// path — background tabs included — or drops it if no editor shows that file.
func (m *Model) routeToEditor(path string, msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range m.editorKeysForPath(path) {
		if cmd := m.panes.Get(key).UpdateForPath(path, nil, msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// openPathAt opens path (reusing the standard open flow) and places the cursor at
// the 0-based line/col — the navigation half of go-to-definition.
func (m Model) openPathAt(path string, line, col int) (tea.Model, tea.Cmd) {
	// Canonicalize before the same-file compare and editorForPath below;
	// openPath normalizes again, which is harmless (#272).
	path = canonicalPath(path)
	// A same-file jump to another line is a navigation jump too (Roadmap
	// 0220); the different-file case records inside openPath below.
	if cur := m.currentNavPos(); cur.Path == path && cur.Line != line {
		m.recordNavFrom(cur)
	}
	model, cmd := m.openPath(path, false)
	mm, ok := model.(Model)
	if !ok {
		return model, cmd
	}
	if ed := mm.editorForPath(path); ed != nil {
		ed.SetCursor(line, col)
	}
	return mm, cmd
}

func (m *Model) closeEditorsForPath(path string, isDir bool) {
	prefix := path + string(os.PathSeparator)
	match := func(ed *editor.Model) bool {
		if !ed.HasFile() {
			return false
		}
		ep := ed.Path()
		return ep == path || (isDir && strings.HasPrefix(ep, prefix))
	}
	closed := false
	for _, key := range m.panes.Keys() {
		inst := m.panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		// Close matching tabs first (highest index first, so indexes stay
		// valid); the pane itself goes when its last tab matches too.
		for i := inst.TabCount() - 1; i >= 0 && inst.TabCount() > 1; i-- {
			if match(inst.TabEditor(i)) {
				m.closeTab(inst, i)
				closed = true
			}
		}
		if match(inst.Editor()) && m.closeKey(key) {
			closed = true
		}
	}
	if !closed {
		return
	}
	if !m.panes.Has(m.panes.Focused()) {
		m.setFocus(m.focusAfterClose())
	}
	m.syncExplorerOpen()
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

// bodyRect is the viewport the layout tree tiles: below the menu bar (when
// enabled), above the status line.
func (m *Model) bodyRect() layout.Rect {
	top := m.menuHeight()
	h := m.height - statusHeight - top
	if m.zen {
		// Zen (#359): the status line is hidden, its row joins the body.
		h = m.height - top
	}
	return layout.Rect{X: 0, Y: top, W: m.width, H: h}
}

// clickOutside reports a mouse press landing outside a centered overlay view
// (mirroring overlay.Center's placement). Non-press events never dismiss.
func clickOutside(msg mouseEvent, view string, tw, th int) bool {
	if msg.action != mousePress || view == "" {
		return false
	}
	w, h := lipgloss.Width(view), lipgloss.Height(view)
	return !inRect(msg.X, msg.Y, (tw-w)/2, (th-h)/2, w, h)
}

// inRect reports whether the cell (px, py) lies inside the rect at (x, y).
func inRect(px, py, x, y, w, h int) bool {
	return px >= x && px < x+w && py >= y && py < y+h
}

// settingsSize bounds the floating settings panel: most of the terminal, but
// never full-screen (capped like a JetBrains dialog) and never overflowing.
func (m Model) settingsSize() (w, h int) {
	w = m.width - 6
	if w > 110 {
		w = 110
	}
	h = m.height - 4
	if h > 32 {
		h = 32
	}
	return w, h
}

// menuHeight is the rows the menu bar occupies (0 when hidden via ui.menu_bar).
func (m Model) menuHeight() int {
	if m.menuEnabled() {
		return 1
	}
	return 0
}

// menuEnabled reads ui.menu_bar (default true).
func (m Model) menuEnabled() bool {
	v, ok := m.host.Config().Get("ui.menu_bar")
	return !ok || v != "false"
}

// commandInfo builds the menu's command-id resolver: registered ids are
// runnable and carry the same shortcut the cheatsheet shows; unregistered ids
// render disabled with the blocked-ledger dependency (or a generic hint) as
// the reason.
func (m Model) commandInfo(reg *registry.Registry) menu.InfoFunc {
	return func(id string) menu.Info {
		if c, ok := reg.Command(id); ok {
			info := menu.Info{Runnable: true, Shortcut: c.Shortcut}
			if s, ok := reg.Binding(id); ok {
				info.Shortcut = s
			}
			return info
		}
		hint := "not available yet"
		if reason, ok := keymap.BlockedReason(id); ok {
			hint = reason
		}
		return menu.Info{Hint: hint}
	}
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
	if m.zoomActive() {
		// Zoomed (#358): the one pane owns the whole body; no dividers.
		m.lay = layout.Layout{Panes: map[string]layout.Rect{m.zoomed: m.bodyRect()}}
	} else {
		m.lay = layout.Compute(m.tree, m.bodyRect())
	}
	for key, r := range m.lay.Panes {
		inst := m.panes.Get(key)
		if inst == nil {
			continue
		}
		inst.SetSize(paneInterior(r.W, paneChromeW), paneInterior(r.H, paneChromeH))
		if inst.Kind() == pane.KindEditor && m.pendingScroll != nil && m.pendingScroll.key == key {
			inst.Editor().SetScroll(m.pendingScroll.top, m.pendingScroll.left)
			m.pendingScroll = nil
		}
	}
	m.syncFocus()
}

// paneInterior maps an outer pane dimension to the content area, subtracting the
// chrome for that axis (paneChromeW horizontally, paneChromeH vertically).
func paneInterior(outer, chrome int) int {
	if v := outer - chrome; v >= 1 {
		return v
	}
	return 1
}

// handleMouse runs the drag state machine: press hit-tests the layout to start a
// resize (divider) or move (title bar), motion updates the in-flight gesture, and
// release commits and persists. A title drag onto another pane relocates it
// (0036); a drag to the source pane's own edge spawns a fresh split there (0037).
func (m Model) handleMouse(msg mouseEvent) (tea.Model, tea.Cmd) {
	// Floating overlays (#116): a click outside an open overlay dismisses it,
	// a click inside stays with the overlay (never leaks to the panes below).
	// The finder renders above every other overlay, so it hit-tests first (#424).
	if m.finder.IsOpen() {
		if clickOutside(msg, m.finder.View(), m.width, m.height) {
			m.finder.Close()
			return m, nil
		}
		switch {
		case msg.action == mousePress && msg.Button == tea.MouseLeft:
			v := m.finder.View()
			bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
			return m, m.finder.Click(msg.X-bx, msg.Y-by)
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelUp:
			m.finder.Wheel(-3)
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelDown:
			m.finder.Wheel(3)
		}
		return m, nil
	}
	if m.settings.IsOpen() {
		if clickOutside(msg, m.settings.View(), m.width, m.height) {
			m.settings.Close()
			return m, nil
		}
		if msg.action == mousePress && msg.Button == tea.MouseLeft {
			// Translate to panel-local coordinates (the box is centered).
			v := m.settings.View()
			bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
			return m, m.settings.Click(msg.X-bx, msg.Y-by)
		}
		return m, nil
	}
	if m.shell.IsOpen() {
		if clickOutside(msg, m.shell.View(), m.width, m.height) {
			m.shell.Close()
		}
		return m, nil
	}
	if m.palette.IsOpen() {
		if m.palette.Anchored() {
			ax, ay := m.palette.AnchorPos()
			v := m.palette.View()
			if msg.action == mousePress && !inRect(msg.X, msg.Y, ax, ay, lipgloss.Width(v), lipgloss.Height(v)) {
				m.palette.Close()
			}
		} else if clickOutside(msg, m.palette.View(), m.width, m.height) {
			m.palette.Close()
		}
		return m, nil
	}
	// Menu bar (Roadmap 0160): with a dropdown open, moving the mouse over an
	// entry selects it (hover follows focus, like keyboard navigation).
	if m.menuEnabled() && m.menu.IsOpen() && msg.action == mouseMotion {
		if idx, ok := m.menu.ItemAt(msg.X, msg.Y); ok {
			m.menu.Hover(idx)
		}
		return m, nil
	}
	// Clicks on the bar row open/switch menus; with a
	// dropdown open, a click runs the entry under it or closes the menu.
	if m.menuEnabled() && msg.action == mousePress && msg.Button == tea.MouseLeft {
		if m.menu.IsOpen() {
			if idx, ok := m.menu.ItemAt(msg.X, msg.Y); ok {
				return m, m.menu.Invoke(idx)
			}
			if msg.Y == 0 {
				if i, ok := m.menu.TitleAt(msg.X); ok {
					m.menu.OpenMenu(i)
					return m, nil
				}
			}
			m.menu.Close()
			return m, nil
		}
		if msg.Y == 0 {
			if i, ok := m.menu.TitleAt(msg.X); ok {
				m.menu.OpenMenu(i)
			}
			return m, nil
		}
	}
	shift := msg.Mod&tea.ModShift != 0
	if msg.action == mouseWheel {
		key, ok := m.lay.PaneAt(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		inst := m.panes.Get(key)
		if inst == nil {
			return m, nil
		}
		switch inst.Kind() {
		case pane.KindExplorer:
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
		case pane.KindTerminal:
			// The pane routes the wheel (#226): mouse-reporting children get
			// the event, alt-screen children arrow keys, a plain shell pages
			// the scrollback (#96) — up towards history, down back to live.
			if r, ok := m.lay.Panes[key]; ok {
				lx, ly := msg.X-(r.X+paneContentX), msg.Y-(r.Y+paneContentY)
				switch msg.Button {
				case tea.MouseWheelUp:
					inst.Terminal().MouseWheel(lx, ly, wheelLines)
				case tea.MouseWheelDown:
					inst.Terminal().MouseWheel(lx, ly, -wheelLines)
				}
			}
		case pane.KindEditor:
			// The wheel over the tab bar row cycles tabs (#159): up goes to
			// the previous tab, down to the next.
			if r, ok := m.lay.Panes[key]; ok && msg.Y == r.Y+1 &&
				(inst.TabCount() > 1 || m.tabsAlwaysShow()) {
				switch msg.Button {
				case tea.MouseWheelUp:
					m.cycleTabs(inst, -1)
				case tea.MouseWheelDown:
					m.cycleTabs(inst, 1)
				}
				return m, nil
			}
			// Scrolls the viewport regardless of mode (normal, insert,
			// visual, …); the cursor stays put until the user clicks or moves.
			// Horizontal wheel and shift+wheel scroll sideways (#230), like
			// the explorer.
			switch {
			case msg.Button == tea.MouseWheelLeft:
				inst.Editor().ScrollXBy(-wheelLines)
			case msg.Button == tea.MouseWheelRight:
				inst.Editor().ScrollXBy(wheelLines)
			case msg.Button == tea.MouseWheelUp && shift:
				inst.Editor().ScrollXBy(-wheelLines)
			case msg.Button == tea.MouseWheelDown && shift:
				inst.Editor().ScrollXBy(wheelLines)
			case msg.Button == tea.MouseWheelUp:
				inst.Editor().ScrollBy(-wheelLines)
			case msg.Button == tea.MouseWheelDown:
				inst.Editor().ScrollBy(wheelLines)
			}
		}
		return m, nil
	}
	switch msg.action {
	case mousePress:
		if m.explorerCapturing() {
			// The prompt floats centered within the explorer pane's own
			// content area, so it reads the same content-local coordinates
			// as a normal row click.
			if r, ok := m.lay.Panes[pane.ExplorerKey]; ok {
				m.explorer().PromptMouseClick(msg.X-(r.X+paneContentX), msg.Y-(r.Y+paneContentY))
			}
			return m, nil
		}
		hit := m.lay.Hit(msg.X, msg.Y)
		switch hit.Kind {
		case layout.HitDivider:
			m.drag = &dragState{kind: dragResize, divider: *hit.Divider, curX: msg.X, curY: msg.Y}
		case layout.HitTitle:
			// Clicks on a tab-bar segment act on that tab (#159): left-click
			// focuses it, middle-click closes it. The active tab's own
			// segment — and the row outside the segments — still starts a
			// pane move, keeping the title row as the drag handle.
			if key, idx, ok := m.tabBarHit(msg.X, msg.Y); ok {
				inst := m.panes.Get(key)
				if msg.Button == tea.MouseMiddle {
					m.closeBarTab(key, idx)
					return m, nil
				}
				if msg.Button == tea.MouseLeft {
					m.setFocus(key)
					m.switchTab(inst, idx)
					if inst.TabCount() > 1 {
						// Grabbing a tab label drags just that file
						// (#305); the whole-pane move below stays the
						// last-tab / off-segment behavior.
						m.drag = &dragState{kind: dragTab, srcPane: key, srcTab: idx, curX: msg.X, curY: msg.Y}
						return m, nil
					}
				}
			}
			// A click on the title band focuses the pane (#304); the drag
			// only commits once the pointer leaves the band (commitMove).
			m.setFocus(hit.Pane)
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
		switch m.drag.kind {
		case dragResize:
			m.drag.divider.ResizeTo(msg.X, msg.Y)
			m.layout()
		case dragTermSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				m.panes.Get(m.drag.srcPane).Terminal().MouseDrag(lx, ly)
			}
		}
	case mouseRelease:
		if m.drag == nil {
			return m, nil
		}
		switch m.drag.kind {
		case dragMove:
			m.commitMove(msg.X, msg.Y)
		case dragTab:
			m.commitTabMove(msg.X, msg.Y)
		case dragTermSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				m.panes.Get(m.drag.srcPane).Terminal().MouseRelease(lx, ly)
			}
			m.drag = nil
			return m, nil // a selection drag never moved the layout
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
	// A release still inside the source pane's own title band is a click,
	// not a drag (#304): the title rows double as the pane's top edge, so
	// without this guard a plain click would land in the top edgeZone and
	// spawn a surprise split. Dragging out of the band (any direction,
	// including onto another pane's title row) still commits.
	if r, ok := m.lay.Panes[m.drag.srcPane]; ok && target == m.drag.srcPane && y < r.Y+layout.TitleBarRows {
		return
	}
	if target != m.drag.srcPane {
		r := m.lay.Panes[target]
		zone := layout.DropZone(r, x, y)
		if inst := m.panes.Get(target); inst != nil && inst.Kind() == pane.KindEditor && m.dragCarriesFiles(m.drag) {
			zone = layout.DropZoneWithCenter(r, x, y)
		}
		if zone == layout.ZoneCenter {
			// Center drop on an editor merges the source pane's files into
			// the target's tab list instead of relocating the pane (#318).
			m.mergePaneTabs(m.drag.srcPane, target)
			return
		}
		m.tree = layout.Move(m.tree, m.drag.srcPane, target, zone)
		m.layout()
		return
	}
	// Dropped on the source pane: spawn a split only when near an edge.
	if zone, near := edgeZone(m.lay.Panes[target], x, y); near {
		newKey := m.panes.AddEditor()
		m.installEmitter(newKey)
		if tree, ok := layout.SplitLeaf(m.tree, target, newKey, zone); ok {
			m.tree = tree
			m.setFocus(newKey)
			m.layout()
		} else {
			m.panes.Close(newKey)
		}
	}
}

// commitTabMove applies a tab-label drag release (#305): only the grabbed
// file moves. Onto another editor pane it relocates the document (shared
// documents stay shared); onto the source pane's own edge it spawns a split
// holding just that file. A release still inside the source's title band is a
// click (the tab already switched on press); everything else is a no-op.
func (m *Model) commitTabMove(x, y int) {
	src := m.drag.srcPane
	inst := m.panes.Get(src)
	r, rok := m.lay.Panes[src]
	if inst == nil || !rok {
		return
	}
	ed := inst.TabEditor(m.drag.srcTab)
	if ed == nil || !ed.HasFile() {
		return
	}
	path := ed.Path()
	target, ok := m.lay.PaneAt(x, y)
	if !ok || (target == src && y < r.Y+layout.TitleBarRows) {
		return // dropped outside any pane, or a plain click (#304 semantics)
	}
	if target != src {
		tinst := m.panes.Get(target)
		if tinst == nil {
			return
		}
		if tinst.Kind() != pane.KindEditor {
			// A non-editor pane has no tab list to join, but its edge
			// zones still accept the file as a split next to it (#317),
			// mirroring the self-edge drop below.
			if zone, near := edgeZone(m.lay.Panes[target], x, y); near {
				m.splitTabTo(target, zone, path, ed)
			}
			return
		}
		// An editor target shows five zones (#318): the center merges the
		// file into its tab list, the edges split next to it like #317.
		if zone := layout.DropZoneWithCenter(m.lay.Panes[target], x, y); zone != layout.ZoneCenter {
			m.splitTabTo(target, zone, path, ed)
			return
		}
		m.openInTab(target, path)
		m.backupDropOnCloseTab(ed, src)
		inst.CloseTab(m.drag.srcTab)
		m.setFocus(target)
		m.syncExplorerOpen()
		m.layout()
		return
	}
	// Self-drop on an edge: split off a fresh pane holding just this file.
	if zone, near := edgeZone(r, x, y); near {
		m.splitTabTo(src, zone, path, ed)
	}
}

// splitTabTo finishes a tab drag by splitting pane target at zone into a fresh
// editor leaf holding path, then closing the dragged tab in the source pane.
func (m *Model) splitTabTo(target string, zone layout.Zone, path string, ed *editor.Model) {
	newKey := m.panes.AddEditor()
	m.installEmitter(newKey)
	tree, ok := layout.SplitLeaf(m.tree, target, newKey, zone)
	if !ok {
		m.panes.Close(newKey)
		return
	}
	m.tree = tree
	m.layout()
	m.openInTab(newKey, path)
	m.backupDropOnCloseTab(ed, m.drag.srcPane)
	m.panes.Get(m.drag.srcPane).CloseTab(m.drag.srcTab)
	m.setFocus(newKey)
	m.syncExplorerOpen()
	m.layout()
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

// wheelFlushMsg asks the model to apply the accumulated wheel batch (#238). It
// is emitted by queueWheel and travels through the same message queue as input
// events, so by the time it arrives every wheel event that was backed up behind
// it has been folded into pendingWheel — the whole burst then costs one update
// pass (and one render) instead of one per event.
type wheelFlushMsg struct{}

// wheelBatch is one run of identical wheel events (same cell, button and
// modifiers) waiting to be applied.
type wheelBatch struct {
	ev    mouseEvent
	count int
}

// queueWheel folds a wheel event into the pending batch and schedules a flush
// unless one is already in flight.
func (m Model) queueWheel(ev mouseEvent) (tea.Model, tea.Cmd) {
	if n := len(m.pendingWheel); n > 0 && m.pendingWheel[n-1].ev.Mouse == ev.Mouse {
		m.pendingWheel[n-1].count++
	} else {
		m.pendingWheel = append(m.pendingWheel, wheelBatch{ev: ev, count: 1})
	}
	if m.wheelFlushQueued {
		return m, nil
	}
	m.wheelFlushQueued = true
	return m, func() tea.Msg { return wheelFlushMsg{} }
}

// flushWheel replays the accumulated wheel events through handleMouse in one
// update pass. A stale flush — the batch was already applied inline by a
// non-wheel message — is a no-op.
func (m Model) flushWheel() (tea.Model, tea.Cmd) {
	batches := m.pendingWheel
	m.pendingWheel = nil
	m.wheelFlushQueued = false
	var tm tea.Model = m
	var cmds []tea.Cmd
	for _, b := range batches {
		for i := 0; i < b.count; i++ {
			mm, ok := tm.(Model)
			if !ok {
				return tm, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			tm, cmd = mm.handleMouse(b.ev)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	return tm, tea.Batch(cmds...)
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
	case pane.KindTerminal:
		// Left press: forward to a mouse-reporting child, else anchor a text
		// selection and track the drag (#227).
		if msg.Button == tea.MouseLeft {
			inst.Terminal().MousePress(localX, localY)
			m.drag = &dragState{kind: dragTermSelect, srcPane: key, curX: msg.X, curY: msg.Y}
		}
	}
	return m, nil
}

// termLocal translates a screen-cell mouse event into pane-content-local
// coordinates for the given terminal pane key.
func (m Model) termLocal(key string, msg mouseEvent) (x, y int, ok bool) {
	r, found := m.lay.Panes[key]
	if !found || m.panes.Get(key) == nil {
		return 0, 0, false
	}
	return msg.X - (r.X + paneContentX), msg.Y - (r.Y + paneContentY), true
}

// clipboardWrite is a seam over the system clipboard so tests don't clobber
// the user's real clipboard.
var clipboardWrite = func(text string) {
	if c := clipboard.System(); c != nil {
		_ = c.Write(text)
	}
}

// copyTerminalSelection writes the terminal's mouse selection to the system
// clipboard and drops the highlight (#227).
func (m *Model) copyTerminalSelection(term *terminal.Model) {
	clipboardWrite(term.SelectionText())
	term.ClearSelection()
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
	// Set the screen-wide default background/foreground at the renderer level.
	// A pane body's inner styled spans (syntax colors, selection) emit a full SGR
	// reset ("\x1b[m") after each span, which clears any background set by an
	// enclosing lipgloss style — so wrapping the composed frame in a Background
	// style leaves pane interiors, overlays, and the floating shell showing the
	// raw terminal background. Setting it here makes the terminal's *default*
	// background equal the palette background, so every reset falls back to it
	// instead of the terminal's own theme.
	v.BackgroundColor = m.pal().Background
	v.ForegroundColor = m.pal().Foreground
	return v
}

// compositeLSPPopups overlays the focused editor's completion or hover popup at
// the cursor cell. Only the editor knows the buffer-relative anchor; only the app
// knows the absolute screen geometry, so the placement is computed here.
func (m Model) compositeLSPPopups(base string) string {
	key := m.panes.Focused()
	inst := m.panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		return base
	}
	r, ok := m.lay.Panes[key]
	if !ok {
		return base
	}
	ed := inst.Editor()
	// The popups carry their own frame (#316), so they may overflow the owning
	// pane; cap their content width at the terminal instead of the pane
	// (frame + padding take 4 columns).
	ed.SetPopupMaxWidth(m.width - 4)
	top, left := ed.ScrollOffset()
	gw := ed.GutterWidth()
	contentX := r.X + paneContentX
	contentY := r.Y + paneContentY
	place := func(view string, col, line int) string {
		x := contentX + gw + (col - left)
		y := contentY + (line - top) + 1 // one row below the cursor
		// Clamp the box to the terminal (#316): the framed popup may extend
		// past the owning pane's borders, but shifts left instead of bleeding
		// across the screen edge and flips above the anchor row when it would
		// cross the bottom of the screen.
		w, h := lipgloss.Width(view), lipgloss.Height(view)
		if maxX := m.width - w; x > maxX {
			x = maxX
		}
		if x < 0 {
			x = 0
		}
		if y+h > m.height {
			y = contentY + (line - top) - h
		}
		if y < 0 {
			y = 0
		}
		return overlay.Place(base, view, x, y, m.width, m.height)
	}
	if ed.CompletionOpen() {
		col, line := ed.CompletionAnchor()
		return place(ed.CompletionView(), col, line)
	}
	if ed.SignatureOpen() {
		col, line := ed.SignatureAnchor()
		return place(ed.SignatureView(), col, line)
	}
	if ed.HoverOpen() {
		col, line := ed.HoverAnchor()
		return place(ed.HoverView(), col, line)
	}
	return base
}

// pal returns the active theme palette. A model built without NewWith (tests,
// zero values) falls back to the resolved default theme so chrome renderers
// never nil-check.
func (m Model) pal() *theme.Palette {
	if m.themePal != nil {
		return m.themePal
	}
	return theme.DefaultPalette()
}

// render composes the full frame as a styled string: the pane tree, the status
// line, and any floating overlay (move ghost, palette, modal shell) on top.
// The palette's background/foreground are painted behind and under the whole
// screen, regardless of the terminal's own theme, so unstyled text stays
// readable (nested styles elsewhere still win over these defaults).
func (m Model) render() string {
	if m.width == 0 {
		return "starting ike…"
	}
	body := ""
	if m.zoomed != "" {
		// Zoomed (#358): render only that pane; the tree survives untouched.
		body = m.renderPane(m.zoomed, m.bodyRect())
	} else {
		body = m.renderNode(m.tree, m.bodyRect())
	}
	rows := []string{body}
	if !m.zen {
		rows = append(rows, m.statusLine())
	}
	if m.menuEnabled() {
		rows = append([]string{m.menu.Bar()}, rows...)
	}
	base := lipgloss.JoinVertical(lipgloss.Left, rows...)
	if m.menu.IsOpen() {
		base = overlay.Place(base, m.menu.Dropdown(), m.menu.DropdownX(), 1, m.width, m.height)
	}
	if m.settings.IsOpen() {
		// The settings panel floats centered above the workspace (#115).
		base = overlay.Center(base, m.settings.View(), m.width, m.height)
	}
	if box, x, y, ok := m.moveGhost(); ok {
		base = overlay.Place(base, box, x, y, m.width, m.height)
	}
	base = m.compositeLSPPopups(base)
	base = m.compositeWhichKey(base)
	result := base
	switch {
	case m.finder.IsOpen():
		result = overlay.Center(base, m.finder.View(), m.width, m.height)
	case m.callhier.IsOpen():
		result = overlay.Center(base, m.callhier.View(), m.width, m.height)
	case m.palette.IsOpen():
		v := m.palette.View()
		if m.palette.Anchored() {
			x, y := m.palette.AnchorPos()
			result = overlay.Place(base, v, x, y, m.width, m.height)
		} else {
			result = overlay.Center(base, v, m.width, m.height)
		}
	case m.shell.IsOpen():
		result = overlay.Center(base, m.shell.View(), m.width, m.height)
	}
	result = m.compositeToasts(result)
	return lipgloss.NewStyle().
		Background(m.pal().Background).
		Foreground(m.pal().Foreground).
		Width(m.width).
		Height(m.height).
		Render(result)
}

// compositeWhichKey overlays the pending-chord hint rows (0081/40) as a
// small bottom-centered panel above the status line.
func (m Model) compositeWhichKey(base string) string {
	if len(m.whichKey) == 0 {
		return base
	}
	box := lipgloss.NewStyle().
		Background(m.pal().Panel).
		Foreground(m.pal().Foreground).
		Padding(0, 1).
		Render(strings.Join(m.whichKey, "\n"))
	w := lipgloss.Width(box)
	h := lipgloss.Height(box)
	x := (m.width - w) / 2
	y := m.height - h - 1 // one row above the status line
	if x < 0 || y < 0 {
		return base
	}
	return overlay.Place(base, box, x, y, m.width, m.height)
}

// moveGhost computes the preview box for an in-flight move. Onto another pane it
// previews the relocation; onto the source pane's own edge it previews the spawn.
func (m Model) moveGhost() (box string, x, y int, ok bool) {
	d := m.drag
	if d == nil || (d.kind != dragMove && d.kind != dragTab) {
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
		label := "new pane"
		if d.kind == dragTab {
			label = m.tabDragLabel(d)
		}
		return ghostBox(gr.W, gr.H, label, m.pal().Ghost), gr.X, gr.Y, true
	}
	zone, can := m.dropZoneFor(d, tgt, m.lay.Panes[tgt])
	if !can {
		return "", 0, 0, false
	}
	label := m.paneLabel(d.srcPane)
	if d.kind == dragTab {
		label = m.tabDragLabel(d)
	}
	if zone == layout.ZoneCenter {
		// The full-pane ghost with a merge label marks the center zone
		// (#318), distinct from the half-pane edge previews.
		label += " ⧉ merge as tab"
	}
	gr := dropRect(m.lay.Panes[tgt], zone)
	if gr.W < 3 || gr.H < 3 {
		return "", 0, 0, false
	}
	return ghostBox(gr.W, gr.H, label, m.pal().Ghost), gr.X, gr.Y, true
}

// dropZoneFor reports the drop zone to signal for the hovered target pane and
// whether a drop there would do anything: a dragged tab only lands in a
// non-editor pane's edge zones (#317), so its interior shows no target; an
// editor target whose drag carries files shows the five-zone set with the
// center merge zone (#318).
func (m Model) dropZoneFor(d *dragState, key string, r layout.Rect) (layout.Zone, bool) {
	inst := m.panes.Get(key)
	isEditor := inst != nil && inst.Kind() == pane.KindEditor
	if d.kind == dragTab && !isEditor {
		return edgeZone(r, d.curX, d.curY)
	}
	if isEditor && m.dragCarriesFiles(d) {
		return layout.DropZoneWithCenter(r, d.curX, d.curY), true
	}
	return layout.DropZone(r, d.curX, d.curY), true
}

// dragCarriesFiles reports whether the drag has files an editor target could
// merge as tabs (#318): a tab drag always carries one; a whole-pane move
// carries the source editor's open files (an empty editor, an explorer or a
// terminal pane keeps the plain relocate zones).
func (m Model) dragCarriesFiles(d *dragState) bool {
	if d.kind == dragTab {
		return true
	}
	inst := m.panes.Get(d.srcPane)
	if inst == nil || inst.Kind() != pane.KindEditor {
		return false
	}
	for _, ed := range inst.Editors() {
		if ed.HasFile() {
			return true
		}
	}
	return false
}

// mergePaneTabs finishes a whole-pane center drop (#318): every file of the
// source editor joins the target's tab list (openInTab dedupes onto existing
// tabs), then the emptied source pane closes.
func (m *Model) mergePaneTabs(src, target string) {
	inst := m.panes.Get(src)
	if inst == nil {
		return
	}
	for _, ed := range inst.Editors() {
		if ed.HasFile() {
			m.openInTab(target, ed.Path())
		}
	}
	m.closeKey(src)
	m.setFocus(target)
	m.syncExplorerOpen()
	m.layout()
}

// tabDragLabel is the ghost/status label for a tab drag: the dragged file's
// basename.
func (m Model) tabDragLabel(d *dragState) string {
	if inst := m.panes.Get(d.srcPane); inst != nil {
		if ed := inst.TabEditor(d.srcTab); ed != nil && ed.HasFile() {
			return baseName(ed.Path())
		}
	}
	return "tab"
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
	case layout.ZoneCenter:
		// The merge zone covers the whole target (#318): the full-pane ghost
		// is what visually distinguishes it from the half-pane edge zones.
		return r
	default:
		h := r.H / 2
		return layout.Rect{X: r.X, Y: r.Y + r.H - h, W: r.W, H: h}
	}
}

// ghostBox renders the matte drop-preview box at size w×h with a centered label.
func ghostBox(w, h int, label string, ghost color.Color) string {
	inner := lipgloss.Place(w-2, h-2, lipgloss.Center, lipgloss.Center,
		lipgloss.NewStyle().Foreground(ghost).Render("⤴ "+label))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ghost).
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
				m.renderNode(t.A, a), m.dividerV(r.H), m.renderNode(t.B, b))
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			m.renderNode(t.A, a), m.dividerH(r.W), m.renderNode(t.B, b))
	}
	return ""
}

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
			// The tab bar takes over the title row once the pane holds
			// multiple tabs (#157); paneBox draws it like any title.
			if bar, ok := m.tabBar(inst, r.W-paneChromeW); ok {
				title = bar
			}
		case pane.KindTerminal:
			title, content = m.terminalTitle(inst), inst.View()
		}
	}

	border := m.pal().Border
	if focused {
		border = m.pal().BorderFocus
	}
	if d := m.drag; d != nil && (d.kind == dragMove || d.kind == dragTab) {
		if key == d.srcPane {
			border = m.pal().MoveSource
			title = "⤴ " + title
		} else if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt == key && tgt != d.srcPane {
			if zone, can := m.dropZoneFor(d, tgt, r); can {
				border = m.pal().DropTarget
				title = title + "  " + zoneArrow(zone)
			}
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
	case layout.ZoneCenter:
		return "⧉ merge as tab"
	default:
		return "⬓ bottom"
	}
}

// dividerV renders the vertical gutter between two horizontally-arranged panes.
func (m Model) dividerV(h int) string {
	style := lipgloss.NewStyle().Foreground(m.pal().Border).Background(m.pal().Background)
	return style.Render(strings.TrimRight(strings.Repeat("│\n", h), "\n"))
}

// dividerH renders the horizontal gutter between two vertically-stacked panes.
func (m Model) dividerH(w int) string {
	style := lipgloss.NewStyle().Foreground(m.pal().Border).Background(m.pal().Background)
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
	if ed.Stale() {
		name += "!" // file changed on disk while dirty (Roadmap 0140)
	}
	return name
}

// statusLine renders the bottom status bar. With an editor focused it shows
// mode, file, dirty flag and cursor; with a terminal or the explorer focused
// it names that pane kind instead, so the line always says where input goes (#381).
func (m Model) statusLine() string {
	style := lipgloss.NewStyle().
		Width(m.width).
		Background(m.pal().Panel).
		Foreground(m.pal().Foreground)

	if d := m.drag; d != nil && (d.kind == dragMove || d.kind == dragTab) {
		hint := "MOVE " + m.paneLabel(d.srcPane)
		if d.kind == dragTab {
			if ed := m.panes.Get(d.srcPane).TabEditor(d.srcTab); ed != nil && ed.HasFile() {
				hint = "MOVE " + filepath.Base(ed.Path())
			}
		}
		if tgt, ok := m.lay.PaneAt(d.curX, d.curY); ok && tgt != d.srcPane {
			if zone, can := m.dropZoneFor(d, tgt, m.lay.Panes[tgt]); can {
				hint += " → " + zoneArrow(zone) + " of " + m.paneLabel(tgt)
			} else {
				hint += "  (drop on a pane or this pane's edge)"
			}
		} else if zone, near := m.selfDropZone(d); near {
			hint += " → split " + zoneArrow(zone)
		} else {
			hint += "  (drop on a pane or this pane's edge)"
		}
		return style.Foreground(m.pal().DropTarget).Render(" " + hint)
	}

	// A non-editor focus names itself instead of implying editor input (#381):
	// mirroring the active editor while a terminal owns the keystrokes made it
	// hard to tell where input goes.
	if inst := m.panes.FocusedInstance(); inst != nil && inst.Kind() != pane.KindEditor {
		left := " "
		switch inst.Kind() {
		case pane.KindTerminal:
			left += "TERMINAL"
			t := inst.Terminal()
			seg := ""
			if s := t.ShellPath(); s != "" {
				seg = filepath.Base(s)
			}
			if d := t.Dir(); d != "" {
				if seg != "" {
					seg += " · "
				}
				seg += displayDir(d)
			}
			if seg != "" {
				left += " │ " + seg
			}
			if !t.Running() {
				left += " [exited]"
			}
		default:
			left += "EXPLORER"
		}
		if s := m.host.Status(); s != "" {
			left += " │ " + s
		}
		return style.Render(left)
	}

	// The ":" / "/" command line renders inside the editor pane (vim-style),
	// not here — the status line keeps its segments while typing a command.
	ed := m.activeEditor()
	mode, file, dirty, diag := "NORMAL", "no file", "", ""
	line, col := 1, 1
	if ed != nil {
		mode = ed.ModeName().String()
		if ed.HasFile() {
			file = displayPath(ed.Path())
		}
		if ed.Dirty() {
			dirty = " [+]"
		}
		if ed.Stale() {
			dirty += " [disk changed]"
		}
		if errs, warns := ed.DiagnosticCounts(); errs > 0 || warns > 0 {
			diag = " │ " + strconv.Itoa(errs) + "E " + strconv.Itoa(warns) + "W"
		}
		line, col = ed.Cursor()
	}
	left := " " + mode + " │ " + file + dirty + diag
	// Persistent host status (plugin-set) is one more segment; it never
	// replaces the mode/file/cursor segments (Roadmap 0130).
	if s := m.host.Status(); s != "" {
		left += " │ " + s
	}
	// The LSP server segment is scoped to the focused buffer's language (#380):
	// blank for buffers whose language has no tracked server state.
	if s := m.focusedLangStatus(ed); s != "" {
		left += " │ " + s
	}
	right := "Ln " + strconv.Itoa(line) + ", Col " + strconv.Itoa(col) + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return style.Render(left + strings.Repeat(" ", gap) + right)
}

// focusedLangStatus returns the tracked server state for the focused editor's
// language (#380): the status line's server segment follows the buffer instead
// of echoing the last event globally. Empty when no file is open, the language
// is unknown, or no server state was ever reported for it.
func (m Model) focusedLangStatus(ed *editor.Model) string {
	if ed == nil || !ed.HasFile() {
		return ""
	}
	l, ok := lang.ByPath(ed.Path())
	if !ok {
		return ""
	}
	return m.lspStatus[l.ID]
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
func paneBox(title, content string, width, height int, borderColor color.Color) string {
	// Interior text width = outer width minus the two border columns and the two
	// padding columns. Truncate the title to it so it stays on one row.
	if inner := width - 4; inner >= 1 {
		title = ansi.Truncate(title, inner, "…")
	}
	// lipgloss v2 makes Width/Height border-inclusive totals, so the box must be
	// sized to the full rect (width × height). The content area is then
	// width-2(border)-2(padding) = width-4, which matches paneInterior(); using
	// width-2 here (the v1 convention) renders the box two columns too narrow and
	// wraps full-width pane lines, doubling their height.
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Width(width).
		Height(height).
		MaxWidth(width).
		MaxHeight(height).
		Padding(0, 1).
		BorderForeground(borderColor)
	titleStyle := lipgloss.NewStyle().Bold(true)
	return style.Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), content))
}

func baseName(path string) string { return filepath.Base(path) }

// canonicalPath normalizes a file path to its cleaned absolute form, so the
// tab and buffer lookups (TabForPath, editorForPath) treat every spelling of
// the same file as equal (#272).
func canonicalPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return filepath.Clean(path)
}

// displayPath renders a file path for the status line: relative to the project
// root (the working directory) when inside it, absolute when outside.
func displayPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// paneLabel is the human label for a leaf key used in the drag status hint.
func (m Model) paneLabel(key string) string {
	inst := m.panes.Get(key)
	if inst != nil && inst.Kind() == pane.KindEditor {
		return m.editorTitle(inst.Editor())
	}
	return strings.ToUpper(strings.SplitN(key, ":", 2)[0])
}
