// Package app wires the root bubbletea model for IKE: a dynamic tiled workspace
// that hosts the file explorer and N editor panes, owns focus and layout, routes
// the explorer's open-file message to the active editor (or a fresh split), and
// renders the status line. The pane set itself is dynamic (Roadmap 0037): a
// pane.Registry maps each layout leaf key to a live component instance, and focus
// is "the focused leaf" rather than a two-value enum.
package app

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/backup"
	"ike/internal/callhier"
	"ike/internal/clipboard"
	"ike/internal/commitui"
	"ike/internal/complete"
	"ike/internal/complete/emmet"
	"ike/internal/complete/mru"
	"ike/internal/complete/symbols"
	"ike/internal/complete/words"
	"ike/internal/config"
	"ike/internal/debug"
	"ike/internal/debugpanel"
	"ike/internal/diff"
	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/finder"
	"ike/internal/help"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/keymap"
	"ike/internal/lang"
	"ike/internal/largefile"
	"ike/internal/layout"
	"ike/internal/localhistory"
	ilsp "ike/internal/lsp"
	"ike/internal/market"
	"ike/internal/menu"
	"ike/internal/nav"
	"ike/internal/overlay"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/plugin"
	"ike/internal/preview"
	"ike/internal/problems"
	"ike/internal/project"
	"ike/internal/registry"
	"ike/internal/search"
	"ike/internal/settings"
	"ike/internal/structpanel"
	"ike/internal/terminal"
	"ike/internal/textenc"
	"ike/internal/theme"
	"ike/internal/todoindex"
	"ike/internal/toolcatalog"
	"ike/internal/tour"
	"ike/internal/ui"
	"ike/internal/undotree"
	"ike/internal/vcs"
	"ike/internal/vcspanel"
	"ike/internal/wasm"
	"ike/internal/watch"
	"ike/internal/workspace"
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
	// completeEngine is the local completion engine (0410, #851): word/symbol
	// sources register here; it fans out per completion trigger next to the
	// LSP bridge and its batches merge into the editor popup.
	completeEngine *complete.Engine
	// cfgDiagSeen dedupes config-diagnostic notifications (#793): each
	// distinct message toasts once per session, so a settings write that
	// reloads an unchanged-but-warned config does not re-toast. Lazily
	// initialized, shared by reference across value copies.
	cfgDiagSeen map[string]bool
	// toolRecent maps a tool name to the pane its instance was last opened
	// or focused in (#835), so the plain tool.<name> toggle targets the most
	// recent instance when multiple = true allows several. Session-local;
	// lazily initialized, shared by reference across value copies.
	toolRecent map[string]string
	// compMRU is the per-project recently-accepted-completions store (0410,
	// #854), injected into every editor for the popup's MRU ranking boost.
	compMRU *mru.Store
	// bpts is the per-project breakpoint store (0350, #577): loaded at start,
	// rendered by editors through an injected source, persisted on toggle and
	// on file save.
	bpts *debug.Breakpoints
	// dbg is the live DAP session's state (0350, #579), nil while no
	// session runs; a pointer so Update's value copies share it.
	dbg *debugState
	// dbgLaunching guards the launch/auto-install window before dbg is set:
	// a second debug.start (e.g. a terminal delivering the chord twice) must
	// not spawn a rival adapter that then tears down the first — one session
	// at a time (#579).
	dbgLaunching bool
	// dbgLaunchGen invalidates in-flight launch work (#636): a debug.stop
	// during the launching window bumps it, and the deferred post-install
	// retry only fires when its message still carries the current generation.
	dbgLaunchGen int
	navSkip      bool
	// ws manages the active workspace (Roadmap 0370, #776): the pane registry,
	// the split tree and the terminal return-focus live behind it so a project
	// switch can later swap the whole unit atomically. Focus is the registry's
	// focused key, which always names a layout leaf.
	ws *workspace.Manager
	// recentEditor is the key of the most-recently-focused editor, used as the
	// Replace open-target when the explorer (not an editor) holds focus.
	recentEditor string
	// closedFileViews collects the file paths whose editor view disappeared
	// during the current Update pass (tab close, pane close, tab-limit
	// eviction, drag). The Update wrapper drains it once the whole operation
	// settled and fires EventBufferClosed for paths with no view left in any
	// in-memory workspace (#827) — a dragged tab's file, re-opened elsewhere
	// in the same pass, never fires.
	closedFileViews []string
	// recent is the MRU file list behind the recent-files palette mode
	// (Roadmap 0230). Held by pointer so value-receiver open paths mutate the
	// one shared store; persisted with the session.
	recent *recentFiles
	// closedTabs is the reopen ring (0190, #158): the last few closed tabs'
	// paths and carets, newest last, popped by editor.tab.reopenClosed.
	closedTabs []closedTab
	// largeToasted remembers which paths already raised the one-time
	// large-file toast (#149), so re-activating the tab stays quiet. Held as
	// a map so value-receiver open paths mutate the shared set.
	largeToasted map[string]bool
	host         *host.Host
	reg          *registry.Registry
	// toasts is the active notification stack (Roadmap 0130): drained from the
	// host after every Update pass, rendered bottom-right above the status line.
	toasts   []toast
	toastSeq int
	// history is the notification ring (#78): the newest historyCap entries,
	// newest first, browsable via the notifications.history command.
	history []histEntry
	// notifUnseen counts history entries added since the history view was last
	// opened, shown as the status line's counter segment (#101).
	notifUnseen int
	// caps accumulates the terminal capability reports until the startup
	// verdict toasts any deficiencies (#720). Value state is fine: the
	// reports and the verdict all flow through Update's model copies.
	caps termCaps
	// toolchainSeg caches the status line's toolchain label per language ID
	// (#101): resolving an interpreter stats the filesystem and scans PATH, too
	// costly per frame. Shared by pointer across the value-model copies (like
	// largeToasted); its keys are dropped on config reload.
	toolchainSeg map[string]string
	// vcs is the git status state (Roadmap 0320): the latest snapshot plus
	// refresh scheduling, shared by pointer across value-model copies. A nil
	// snapshot means "not a git repository".
	vcs *vcsState
	// watcher is the external-file-change service (Roadmap 0140). It is
	// constructed with the model (so save epochs record from the start) but
	// only started by main.go via StartWatcher, keeping tests watcher-free.
	watcher *watch.Service
	// menu is the menu bar (Roadmap 0160, #90), rendered above the panes when
	// ui.menu_bar is enabled.
	menu *menu.Model
	// ctxMenu is the right-click context menu (#1020): a floating dropdown
	// anchored at the click cell, dispatching registry commands like the bar.
	ctxMenu *menu.Context
	// settings is the full-window settings panel (Roadmap 0160, #91); cfgOpts
	// names the layer files its edits write back to.
	settings *settings.Model
	// marketPage is kept aside so opening the panel can prefetch the catalog
	// (Roadmap 0310, #446).
	marketPage *settings.MarketplacePage
	cfgOpts    config.Options
	help       *help.Help
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
	// setupQueue is the post-tour setup flow (#713): the step names still to
	// run after the tour finishes; themePick/toolchainInfo hold the open
	// step dialogs while the shell shows them.
	setupQueue    []string
	themePick     *themePickState
	toolchainInfo *toolchainInfoState
	toolSetup     *toolSetupState
	// tour holds the welcome tour (#657) while the shell shows it; tourPending
	// flags the first-run auto-open (#658) until the window is sized.
	tour        *tour.Tour
	tourPending bool
	// backupSvc/backupDeb are the crash-recovery write side (Roadmap 0210,
	// #167): the change seam marks dirty buffers, one armed tick
	// (backupTickArmed) snapshots the ones that went quiet. backupIv caches the
	// debounce interval so a live reload can detect a change.
	backupSvc       *backup.Service
	backupDeb       *backup.Debouncer
	backupTickArmed bool
	backupIv        time.Duration

	// Idle autosave (#731): same debouncer shape as backup, but the tick
	// saves the quiet dirty buffers instead of snapshotting them.
	autosaveIdleDeb       *backup.Debouncer
	autosaveIdleTickArmed bool
	autosaveIdleIv        time.Duration
	// renamePath is the file being renamed by the file.rename prompt (#175)
	// while the shell shows it; renameInput/renamePos are the typed name and
	// its cursor. "" when no rename prompt is open.
	renamePath  string
	renameInput string

	// saveAsKey is the pane whose untitled buffer the save-as prompt (#730)
	// names while the shell shows it; saveAsClose carries the ":wq" intent.
	saveAsKey   string
	saveAsInput string
	saveAsPos   int
	saveAsClose bool
	saveAsErr   string
	renamePos   int
	// movePending is the file whose move target the palette's directory picker
	// is currently asking for (file.move, #175); "" when no move is pending.
	movePending string
	// jbImportOpen marks the JetBrains keymap import prompt (#677) while the
	// shell shows it; jbImportInput/jbImportPos are the typed path and cursor.
	jbImportOpen  bool
	jbImportInput string
	jbImportPos   int
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
	// The terminal return-focus (#97) moved into the workspace (#776):
	// m.activeWS().ReturnFocus.
	// vcsReturnFocus is the same dance for the VCS tool window (0330, #482).
	vcsReturnFocus string
	// problemsReturnFocus is the same dance for the Problems tool window
	// (#1024); probStore is its session-wide per-file diagnostics store, fed
	// from every publish — files without an open editor included.
	problemsReturnFocus string
	probStore           *problems.Store
	// structReturnFocus is the same dance for the Structure tool window
	// (#1025); structReqPath is the last path a documentSymbol refresh was
	// issued for (the request dedup), and structForce marks a save-triggered
	// refresh that must re-request the unchanged path.
	structReturnFocus string
	structReqPath     string
	structForce       bool
	// switchPending is the validated project root awaiting the unsaved-changes
	// answer (Roadmap 0090, #3) while the shell shows the save-all / discard /
	// cancel prompt; "" when no switch is gated.
	switchPending string
	// evictPending is the busy LRU background workspace root awaiting the
	// eviction-guard answer (0370 M4, #780).
	evictPending string
	// debugMapPending is the server directory of a #832 path-mapping hint
	// awaiting the user's answer ("" when no prompt is open): a listening
	// debug session accepted a request whose entry file does not resolve
	// locally, and mapping it to the project root was offered.
	debugMapPending string
	// wsClosePending is the busy close-from-list guard state (#821): the
	// background workspace whose teardown awaits the user's answer.
	wsClosePending *pendingWsClose

	// closePending is the close request awaiting the unsaved-changes guard
	// (#259); nil when no guard is open.
	closePending *pendingClose
	// termClosePending is true while the busy-terminal close guard (#986)
	// owns the keyboard.
	termClosePending bool

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
	// todo is the TODO/FIXME index overlay (#61); todoSearch is its own scan
	// service — separate from searcher so the index and the finder never cancel
	// each other, with results wrapped in todoindex.ScanMsg so the finder can
	// never mistake them for its own generations.
	todo       *todoindex.Model
	todoSearch *search.Service
	// undoTree is the undo-tree overlay (#59): the focused editor's change
	// tree; jumps route back into that editor as HistoryJumpMsg.
	undoTree *undotree.Model
	// commitUI is the commit dialog (Roadmap 0320, #465): the changed-files
	// list with stage toggles plus the commit message pane; the in-progress
	// message survives close/reopen until a commit succeeds.
	commitUI *commitui.Model
	// revertPending is the file awaiting the vcs.revertFile confirmation
	// (#466) while the shell shows the prompt; "" when none.
	revertPending string
	// depEditPending is the dependency file awaiting the edit confirmation
	// (#565) while the shell shows the prompt; "" when none. Confirming replays
	// the blocked edit on the active editor via ConfirmDepEdit.
	depEditPending string
	// inFileSearchRecent is true while a committed in-file search ("/", "?",
	// cmd+f) is more recent than any find-in-path scan: f3/shift+f3 then repeat
	// the in-file search on the active editor instead of stepping retained
	// find-in-path results (#376). Any new scan activity flips it back.
	inFileSearchRecent bool
	// palette is the command palette overlay (Roadmap 0070): a modal input that
	// fronts registered commands (":") and file search ("@"). paletteKey is the
	// default key that opens it (the final binding is Roadmap 0080's).
	palette     *palette.Palette
	cmdUsage    *palette.Usage       // most-used command ranking (#773)
	winSizes    *ui.WinSizes         // persisted floating-window resize deltas (#774)
	floatDrag   *floatResizeDrag     // live mouse resize of a floating window (#933)
	pins        *pinStore            // harpoon-style pinned file slots (#788)
	toolHide    *toolHideSnapshot    // hide-all-tool-windows snapshot (#791)
	termShiftAt time.Time            // last bare-shift tap in a terminal (#973)
	pinSel      int                  // pin-picker selection
	pinPicker   bool                 // pin picker owns the modal shell
	lhStore     *localhistory.Store  // local-history snapshot store (#1023)
	lhSel       int                  // local-history picker selection
	lhPicker    bool                 // local-history picker owns the modal shell
	lhPath      string               // file the open picker lists
	lhEntries   []localhistory.Entry // its snapshots, newest-first
	paletteKey  string
	// themePal is the resolved color scheme (Roadmap 0110): [theme].name mapped
	// to a theme.Palette. Chrome renders from its ui slots; panes get it threaded
	// at construction and on config reloads.
	themePal *theme.Palette
	// lastEsc records that the previous key was an esc in a non-capturing context,
	// so a second esc opens the palette (esc-esc toggle).
	lastEsc bool
	// The split-tree layout (Roadmap 0036/0037) lives in the workspace (#776):
	// m.activeWS().Tree. Leaves are instance keys resolved through the panes.
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
	// diffPick tracks diff.files' two-step file picking (#60): 0 idle, 1
	// picking the left (old) file, 2 the right (new). diffLeft holds the
	// first pick while the second is chosen.
	diffPick int
	diffLeft string
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
	dragEditSelect                 // dragging a text selection inside an editor pane (#977)
	dragEditScroll                 // dragging the editor scrollbar thumb (#1022)
	dragExplScroll                 // dragging the explorer scrollbar thumb (#1036)
	dragDebugTerm                  // dragging a selection in the debug panel's embedded terminal (#676)
	dragDebugDiv                   // dragging a column separator inside the debug panel (#691)
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
	sep     int // dragDebugDiv: which column separator is grabbed (#691)
	curX    int
	curY    int
	startX  int // press cell, for the move/tab engage threshold (#559)
	startY  int
}

// moveEngageCols is how far a move/tab drag must travel horizontally before the
// gesture engages; any vertical travel engages immediately (rows are taller
// than columns, so one row is already a deliberate motion).
const moveEngageCols = 3

// engaged reports whether a move/tab drag has traveled past the threshold that
// separates a deliberate drag from a plain click (#559). Until then no move
// feedback renders and a release commits nothing. Other drag kinds engage on
// press.
func (d *dragState) engaged() bool {
	if d.kind != dragMove && d.kind != dragTab {
		return true
	}
	return abs(d.curY-d.startY) >= 1 || abs(d.curX-d.startX) >= moveEngageCols
}

// New returns the initial root model rooted at the working directory, wired to
// the global plugin registry. It loads the merged configuration (defaults < user
// < project) from the working directory and backs the host with it.
func New() Model {
	cfg, diags := config.Load(config.Discover("."))
	config.Set(cfg)
	m := NewWith(registry.Global(), host.FromConfig(cfg))
	m.notifyConfigDiags(diags)
	return m
}

// notifyConfigDiags surfaces config-load diagnostics as warning notifications
// (0380, #793): a broken settings file or an unknown key must be visible, not
// silently skipped. Each distinct message toasts once per session.
func (m *Model) notifyConfigDiags(diags []config.Diagnostic) {
	if len(diags) == 0 {
		return
	}
	if m.cfgDiagSeen == nil {
		m.cfgDiagSeen = map[string]bool{}
	}
	for _, d := range diags {
		text := "config: " + d.Field + ": " + d.Message
		if d.Source != "" {
			text = "config: " + d.Source + " " + d.Field + ": " + d.Message
		}
		if m.cfgDiagSeen[text] {
			continue
		}
		m.cfgDiagSeen[text] = true
		m.host.Notify(host.Warn, text)
	}
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
	return buildModel(reg, cfg, h, nil)
}

// buildModel is the constructor body. When mgr is non-nil (a seamless project
// switch, #777) and it holds a parked workspace for the current root, that
// workspace — live panes, split tree, running terminals/runs and the debug
// session stashed in Aux — resumes as-is and the layout/session restore from
// disk is skipped; everything not part of the workspace unit (config layer,
// theme, watcher, MRU, breakpoints) still re-resolves against the new cwd.
func buildModel(reg *registry.Registry, cfg host.Config, h *host.Host, mgr *workspace.Manager) Model {
	h.SetConfig(cfg)
	applyPluginConfig(reg, cfg)
	themePal, themeWarning := resolveTheme(reg, cfg)
	root, _ := os.Getwd()
	// The local completion engine (#851) listens to editor events next to the
	// LSP bridge; registration by name keeps a project switch idempotent. The
	// word (#852) and symbol (#853) indexes start their one-shot project
	// scans in the background.
	engine := complete.NewEngine(h.Send)
	engine.Register(words.New(root))
	engine.Register(symbols.New(root))
	engine.Register(emmet.New())
	h.SetEditorEmitter("complete", engine)
	var resumed *workspace.Workspace
	if mgr != nil {
		resumed = mgr.Resume(root)
	}
	var panes *pane.Registry
	edKey := ""
	if resumed != nil {
		panes = resumed.Panes
		for _, key := range panes.Keys() {
			if inst := panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
				edKey = key
				break
			}
		}
	} else {
		panes = pane.NewRegistry(cfg)
		panes.SetPalette(themePal)
		panes.AddExplorer()
		edKey = panes.AddEditor()
		panes.SetFocused(pane.ExplorerKey)
	}
	refs := &refsMode{}
	actions := &actionsMode{}
	symbols := &symbolMode{}
	pasteHist := &pasteHistMode{}
	bindings := &keymap.LiveBindings{}
	recent := &recentFiles{}
	vcsSt := &vcsState{draft: &vcs.MessageDraft{}} // shared before the literal: the branch picker mode reads it
	cmdUsage := palette.LoadUsage(usageFile())     // most-used ranking (#773)
	winSizes := ui.LoadWinSizes(winSizeFile())     // resizable floats (#774)
	wsMgr := wsManager(mgr, resumed, root, panes)  // hoisted: the palette's recent-projects sources read it (#820)
	m := Model{
		cmdUsage:       cmdUsage,
		winSizes:       winSizes,
		pins:           loadPins(),                          // pinned file slots (#788)
		lhStore:        localhistory.New(localHistoryDir()), // local history (#1023)
		completeEngine: engine,
		ws:             wsMgr,
		recentEditor:   edKey,
		recent:         recent,
		largeToasted:   map[string]bool{},
		toolchainSeg:   map[string]string{},
		navHist:        &nav.History{},
		compMRU:        mru.Load(mru.DefaultFile()),
		bpts:           debug.Load(),
		host:           h,
		reg:            reg,
		themePal:       themePal,
		bindings:       bindings,
		help:           help.New(reg, bindings, helpMinCol(cfg)),
		shell:          ui.New(shellConfig(cfg)),
		vcs:            vcsSt,
		palette:        buildPalette(reg, cfg, refs, actions, bindings, recent, symbols, pasteHist, vcsSt, cmdUsage, wsMgr),
		refs:           refs,
		lspStatus:      map[string]string{},
		symbols:        symbols,
		actions:        actions,
		pasteHist:      pasteHist,
		paletteKey:     paletteToggleKey(cfg),
		splitZone:      splitZone(cfg),
		focusKeys:      focusKeys(cfg),
		keys:           buildKeymap(cfg, bindings),
	}
	m.shell.SetSizeStore(winSizes)            // resizable modal shell (#774)
	m.palette.SetSizeStore(winSizes)          // resizable palette box (#774)
	m.shell.SetMaxWidth(popupMaxWidth())      // centered-popup width cap (#932)
	highlight.SetRainbow(rainbowConfigured()) // rainbow brackets (#789)
	m.palette.SetMaxWidth(popupMaxWidth())
	m.watcher = watch.New(m.host.Send)
	m.backupSvc = backupService()
	m.backupIv = backupInterval(cfg)
	m.backupDeb = backup.NewDebouncer(m.backupIv)
	m.autosaveIdleIv = autosaveIdleInterval(cfg)
	m.autosaveIdleDeb = backup.NewDebouncer(m.autosaveIdleIv)
	m.searcher = search.New(m.host.Send)
	m.finder = finder.New(m.searcher)
	m.finder.SetPalette(themePal)
	m.finder.SetDisplayPath(displayPath)
	m.probStore = problems.NewStore()
	m.todoSearch = search.New(func(msg tea.Msg) { h.Send(todoindex.ScanMsg{Inner: msg}) })
	m.todo = todoindex.New(m.todoSearch, ".", todoPatterns(cfg))
	m.todo.SetPalette(themePal)
	m.todo.SetDisplayPath(displayPath)
	m.callhier = callhier.New()
	m.callhier.SetPalette(themePal)
	m.undoTree = undotree.New()
	m.undoTree.SetPalette(themePal)
	m.commitUI = commitui.New()
	m.commitUI.SetPalette(themePal)
	m.commitUI.SetDraft(vcsSt.draft)
	m.callhier.SetDisplayPath(displayPath)
	m.menu = menu.New(menu.Defaults(), m.commandInfo(reg))
	m.ctxMenu = menu.NewContext(m.commandInfo(reg))
	m.ctxMenu.SetPalette(themePal)
	m.cfgOpts = config.Discover(".")
	pages := settings.BasePages(themeNames(reg))
	pages = append(pages, settings.Page{Section: "TOOLS", Title: "Keymap", Custom: settings.NewKeymapPage(m.cfgOpts, func(id string) bool {
		_, ok := reg.Command(id)
		return ok
	}, func() []settings.CommandEntry {
		// Every registered command — including configured tools (#741),
		// whose tool.<name> commands the registry rebuilds per query — so
		// the page can offer never-bound ids for binding (#771).
		cmds := reg.Commands()
		out := make([]settings.CommandEntry, len(cmds))
		for i, c := range cmds {
			out[i] = settings.CommandEntry{ID: c.ID, Title: c.Title}
		}
		return out
	})})
	// The [[tools.custom]] list editor (#755): custom TUI tool panes (#741).
	pages = append(pages, settings.Page{Title: "Tools", Custom: settings.NewToolsPage(m.cfgOpts)})
	// The [[debug.php.path_mappings]] list editor (#832): the PHP listen
	// mode's (#823) docroot↔project mappings.
	pages = append(pages, settings.Page{Title: "PHP Debug Mappings", Custom: settings.NewDebugMapPage(m.cfgOpts)})
	pages = append(pages, settings.Page{Title: "Toolchain", Custom: settings.NewToolchainPage(m.cfgOpts, ".", func() tea.Cmd {
		// An interpreter change respawns the servers against the new value.
		if c, ok := reg.Command("lsp.restart"); ok {
			return m.dispatchCommand("lsp.restart", c)
		}
		return nil
	})})
	pages = append(pages, settings.Page{Section: "PLUGINS", Title: "Plugins", Custom: settings.NewPluginsPage(m.cfgOpts,
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
					return tea.Batch(write, m.dispatchCommand("lsp.installMissing", c))
				}
			}
			return write
		},
	)})
	// The marketplace page (Roadmap 0310, #446): production engine over the
	// conventional plugins dir, catalog fetch through the market client.
	marketClient := market.NewClient()
	m.marketPage = settings.NewMarketplacePage(
		market.NewEngine(marketClient, wasm.DefaultDir()),
		marketClient.FetchIndex,
	)
	pages = append(pages, settings.Page{Title: "Marketplace", Custom: m.marketPage})
	m.settings = settings.New(append(pages, reg.SettingsPages()...), m.cfgOpts)
	// Thread the startup palette through every chrome component; without this
	// the settings panel, command palette, shell, help, and menu render with
	// the default palette until the first theme switch (#384).
	m.applyTheme(themePal)
	// Restore a saved per-project layout if one is structurally sound; an unknown
	// or stale layout is dropped and the default is built on first size. A
	// resumed workspace (#777) is already live — restoring from disk would
	// replace its running panes with placeholders.
	if resumed == nil {
		m.restoreLayout(cfg)
		m.restoreSession()
	} else if extras, ok := resumed.Aux.(wsExtras); ok {
		// The debug session parked with the workspace re-attaches (#777).
		m.dbg = extras.dbg
		m.dbgLaunching = extras.dbgLaunching
		m.dbgLaunchGen = extras.dbgLaunchGen
		resumed.Aux = nil
	}
	// restoreLayout replaces m.activeWS().Panes with a fresh registry that never saw the
	// applyTheme above (#722): without re-threading, every restored pane
	// (explorer file colors, editor highlight captures) renders the default
	// dark theme's tokens — near-white identifiers on a light theme's
	// background. Idempotent when no layout was restored.
	m.activeWS().Panes.SetPalette(themePal)
	m.scanRecovery()
	m.scanTour()
	m.scanOnboarding()
	m.wireEditorEmitters()
	if themeWarning != "" {
		m.host.Notify(host.Warn, themeWarning)
	}
	return m
}

// wsManager resolves the model's workspace manager (#777): a resumed switch
// keeps the carried-over manager (its active slot already holds the parked
// workspace), a fresh-root switch registers a new workspace on the carried
// manager, and a plain start builds a single-workspace manager.
func wsManager(mgr *workspace.Manager, resumed *workspace.Workspace, root string, panes *pane.Registry) *workspace.Manager {
	if mgr == nil {
		return workspace.NewManager(workspace.New(root, panes))
	}
	if resumed == nil {
		mgr.SetActive(workspace.New(root, panes))
	}
	return mgr
}

// wsExtras is the app-owned per-workspace state stashed in Workspace.Aux
// while parked (#777): live state that cannot be reloaded from disk.
type wsExtras struct {
	dbg          *debugState
	dbgLaunching bool
	dbgLaunchGen int
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
	// The initial git status load (Roadmap 0320) piggybacks on the same
	// lifecycle: main.go-only, so tests stay free of the developer repo's
	// live git state (mirroring the watcher-free-tests rule above). The
	// invalidate goes through the debounce and runs even with files.watch
	// disabled.
	go m.host.Send(vcsInvalidateMsg{})
	if v, ok := m.host.Config().Get("files.watch"); ok && v == "false" {
		return
	}
	// Large files are never content-hashed by the poll fallback (#149):
	// mtime+size alone decide for them.
	m.watcher.SetHashLimit(largefile.LimitsFrom(m.host.Config().Get).MaxBytes)
	_ = m.watcher.Start(root)
}

// editorEmitter adapts editor lifecycle events into host editor events, which the
// host fans out to the LSP bridge (registered via host.SetEditorEmitter). One
// stateless adapter is installed on every editor instance; it is a no-op when no
// bridge is registered. Save events additionally stamp the file watcher's save
// epoch (Roadmap 0140) so IKE's own writes never report as external changes.
// todoSavedMsg reports one buffer save to the TODO index (#61). The editor
// emitter sends it from a goroutine; the root model answers with the index's
// single-file rescan command.
type todoSavedMsg struct{ path string }

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
	if ev.Kind == editor.EventSave && ev.Path != "" {
		// The TODO index rescans the saved file (#61). Same goroutine
		// indirection as the SyncMsg below: Emit runs inside Update, so a
		// direct send into the program's own loop would deadlock.
		go e.host.Send(todoSavedMsg{path: ev.Path})
		// Local History (#1023): snapshot the just-written file. Every save
		// flow (manual write, Save All, autosave) funnels through the editor
		// save path, so this one hook captures them all.
		go e.host.Send(localHistorySnapshotMsg{path: ev.Path})
		// The save also invalidates the git status snapshot (Roadmap 0320);
		// IKE's own writes are watcher-suppressed (MarkSaved above), so this
		// is the only refresh trigger for in-IDE saves.
		go e.host.Send(vcsInvalidateMsg{})
	}
	if ev.Kind == editor.EventCursorMove && ev.Path != "" {
		// Markdown previews follow the cursor (#62). Same goroutine indirection
		// as the SyncMsg below: Emit runs inside Update, so a direct send into
		// the program's own loop would deadlock. The handler is a cheap no-op
		// when no preview pane is bound to the path.
		go e.host.Send(preview.CursorMsg{Path: ev.Path, Line: ev.Line})
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
		Kind:         int(ev.Kind),
		Path:         ev.Path,
		Line:         ev.Line,
		Col:          ev.Col,
		Text:         ev.Text,
		Sel:          int(ev.Sel),
		AnchorLine:   ev.AnchorLine,
		AnchorCol:    ev.AnchorCol,
		Large:        ev.Large,
		Char:         ev.Char,
		CompletionID: ev.CompletionID,
	})
}

// wireEditorEmitters installs the editor-emitter adapter on every editor pane, so
// edits flow to the LSP bridge. It is idempotent and re-run whenever editors are
// created.
func (m *Model) wireEditorEmitters() {
	for _, key := range m.activeWS().Panes.Keys() {
		m.installEmitter(key)
	}
}

// installEmitter wires the editor-emitter adapter onto every tab of one editor
// pane. It is idempotent, so re-running it after a tab is added is cheap.
func (m *Model) installEmitter(key string) {
	if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
		source, adjust := breakpointHooks(m.bpts)
		for _, ed := range inst.Editors() {
			ed.SetEmitter(editorEmitter{host: m.host, watcher: m.watcher, nav: m.navHist, key: key})
			ed.SetBreakpointSource(source)
			ed.SetBreakpointAdjuster(adjust)
			ed.SetCompletionMRU(m.compMRU)
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
		} else if ids[key].Kind == "tool" {
			continue // restored below restarting the configured tool (#741)
		} else if ids[key].Kind == "markdown" {
			continue // restored below re-reading the source file (#62)
		} else if ids[key].Kind == "diff" {
			continue // restored below re-reading both files (#60; fix #490)
		} else if ids[key].Kind == "vcs" {
			continue // restored below as the empty singleton panel (0330)
		} else if ids[key].Kind == "debug" {
			continue // restored below as the empty singleton panel (#580)
		} else if ids[key].Kind == "structure" {
			continue // restored below as the empty singleton panel (#1025)
		} else if !isEditorKey(key) && !isTerminalKey(key) {
			// A terminal-shaped key may carry an editor identity: a
			// converted tab host (#836) restores as an editor pane below.
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
		if id := ids[key]; id.Kind == "tool" {
			// A tool pane restores by restarting its configured program
			// (#741); a tool no longer configured degrades to a fresh shell
			// in the saved position rather than breaking the layout.
			if entry, ok := toolEntry(id.Tool); ok {
				dir := entry.Cwd
				if dir == "" {
					dir = "."
				}
				argv := append([]string{entry.Command}, entry.Args...)
				panes.AddToolKey(key, entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send)
			} else {
				shell := ""
				if v, ok := cfg.Get("terminal.shell"); ok {
					shell = v
				}
				panes.AddTerminalKey(key, terminal.Shell(shell), ".", terminalEnv(), m.host.Send)
			}
			continue
		}
		if id := ids[key]; id.Kind == "vcs" {
			// The VCS panel restores empty in its saved slot; the first
			// status snapshot re-feeds it (0330, #482). Path carries the
			// active tab (#504) — a restored Log view lazy-loads with the
			// snapshot via EnsureLogLoaded.
			p := panes.Get(panes.AddVCS()).VCS()
			p.SetDraft(m.vcs.draft)
			if id.Path == "log" {
				p.SetTab(vcspanel.TabLog)
			}
			continue
		}
		if id := ids[key]; id.Kind == "debug" {
			// The debug panel restores empty (#580): sessions never
			// resurrect, the next stop re-feeds it.
			panes.AddDebug()
			continue
		}
		if id := ids[key]; id.Kind == "problems" {
			// The Problems panel restores empty in its saved slot (#1024):
			// diagnostics are session state; the live store re-feeds it as
			// the language servers publish.
			p := panes.Get(panes.AddProblems()).Problems()
			p.SetDisplayPath(displayPath)
			p.SetStore(m.probStore)
			continue
		}
		if id := ids[key]; id.Kind == "structure" {
			// The Structure panel restores empty (#1025); the first
			// buffer-change sync re-requests the symbols.
			panes.AddStructure()
			continue
		}
		if id := ids[key]; id.Kind == "diff" {
			// A diff pane restores from the two files on disk (#60); a
			// revision-backed side re-reads its blob via git instead (#508).
			// A vanished side restores as empty rather than breaking the
			// layout.
			if id.Rev != "" || id.Rev2 != "" {
				inst := panes.AddDiffRevKey(key, id.Path, id.Path2, id.Rev, id.Rev2)
				left := revContentOrFile(id.Rev, id.Path, id.Path2)
				right := revContentOrFile(id.Rev2, id.Path2, id.Path2)
				inst.Diff().SetContents(left, right)
				continue
			}
			inst := panes.AddDiffKey(key, id.Path, id.Path2)
			inst.Diff().SetContents(readFileOrEmpty(id.Path), readFileOrEmpty(id.Path2))
			continue
		}
		if id := ids[key]; id.Kind == "markdown" {
			// A preview restores from the file on disk (#62); live re-binding to
			// an editor buffer resumes with the first change event. A vanished
			// file restores as an empty preview rather than breaking the layout.
			inst := panes.AddMarkdownKey(key, id.Path)
			if data, err := os.ReadFile(id.Path); err == nil {
				inst.Preview().SetSourceImmediate(string(data))
			}
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
		// Tool sessions hosted as tabs (#836) restart their configured
		// program in place, like dedicated tool panes (#741); a tool no
		// longer configured restores as nothing. A pane that held only
		// tool tabs drops its placeholder empty editor tab again.
		wasEmpty := inst.IsEmptyEditor()
		toolTabs := 0
		for _, tool := range id.Tools {
			entry, ok := toolEntry(tool)
			if !ok {
				continue
			}
			dir := entry.Cwd
			if dir == "" {
				dir = "."
			}
			argv := append([]string{entry.Command}, entry.Args...)
			inst.AddTerminalTab(panes.NewToolSession(entry.Name, argv, dir, toolSpawnEnv(m.pal()), m.host.Send))
			toolTabs++
		}
		if wasEmpty && toolTabs > 0 {
			inst.CloseTab(0)
			active = inst.ActiveTab()
		}
		inst.ActivateTab(active)
	}
	panes.SetFocused(pane.ExplorerKey)
	m.activeWS().Panes = panes
	m.recentEditor = firstEditorKey(leaves)
	m.activeWS().Tree = tree
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
	// s.Theme (the pre-#667 per-project runtime override) is deliberately
	// ignored: the theme is a user setting now, resolved from config alone.
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
			// The pane's active tab can be a terminal (#573), leaving
			// Editor() nil (#931) — treat that like a failed load.
			if ed := m.activeWS().Panes.Get(key).Editor(); ed == nil || ed.Load(s.Editor.Path) != nil {
				key = ""
			}
		}
		if key != "" {
			ed := m.activeWS().Panes.Get(key).Editor()
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
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
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
		RecentFiles: m.recent.List(),
		Explorer: explorerSession{
			Expanded:   st.Expanded,
			ShowHidden: st.ShowHidden,
			Cursor:     st.Cursor,
		},
	}
	if key := m.activeEditorKey(); key != "" {
		// activeEditorKey guarantees an editor-kind pane, not an editor model:
		// the pane's active tab can be a terminal (#573, #836), in which case
		// Editor() is nil (#931) — skip the editor part of the snapshot.
		if ed := m.activeWS().Panes.Get(key).Editor(); ed != nil && ed.HasFile() {
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
	m.persistUndoAll()
	if m.activeWS().Tree != nil {
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
	m.backupCleanShutdown()
	return m, tea.Quit
}

// persistUndoAll writes the undo history of every open document (#148), one
// write per path — views of a shared document alias one history, so the first
// view covers them all.
func (m Model) persistUndoAll() {
	seen := map[string]bool{}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for i := 0; i < inst.TabCount(); i++ {
			ed := inst.TabEditor(i)
			if ed == nil || !ed.HasFile() || seen[ed.Path()] {
				continue
			}
			seen[ed.Path()] = true
			ed.PersistUndo()
		}
	}
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
			return m.dispatchCommand(res.Command, c), true
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
	if bindings != nil {
		bindings.Set(table)
	}
	return keymap.NewResolver(table)
}

// buildPalette wires the command palette: a ":" command mode reading the registry
// and an "@" file finder, tuned by the optional palette.* config keys.
func buildPalette(reg *registry.Registry, cfg host.Config, refs *refsMode, actions *actionsMode, bindings *keymap.LiveBindings, recent *recentFiles, symbols *symbolMode, pasteHist *pasteHistMode, vcsSt *vcsState, usage *palette.Usage, wsMgr *workspace.Manager) *palette.Palette {
	pcfg := palette.Config{
		MaxResults:    paletteMaxResults(cfg),
		DefaultPrefix: paletteDefaultPrefix(cfg),
	}
	cmd := palette.NewCommandMode(reg, bindings, paletteHideOff(cfg))
	cmd.SetUsage(usage)
	file := palette.NewFileMode()
	dir := palette.NewDirMode()
	proj := project.NewPickerMode(nil)
	mru := palette.NewRecentMode(recent.List)
	// The Recent Files dialog grows a Recent Projects column (#778): entries
	// from project.history (current project excluded), whose activation goes
	// through the normal validated seamless-switch path (project.PickedMsg).
	// Background workspaces still open in memory (#777) are marked with "●"
	// and closable in place via the aux action (#820).
	openInMemory := func(path string) bool { return wsMgr != nil && wsMgr.Peek(path) != nil }
	proj.SetOpen(openInMemory)
	mru.SetProjects(func() []palette.Item {
		cur, _ := os.Getwd()
		var items []palette.Item
		for _, e := range project.History(config.Get()) {
			if e.Path == cur {
				continue
			}
			it := palette.Item{
				Title: e.Name,
				Msg:   project.PickedMsg{Path: e.Path},
				Badge: project.RelTime(e.LastOpened, time.Now()),
			}
			if openInMemory(e.Path) {
				it.Badge = "●"
				it.Aux = project.CloseWorkspaceMsg{Path: e.Path}
			} else {
				// Unloaded entries prune from the history (#842), like in
				// the project picker.
				it.Aux = project.RemoveFromHistoryMsg{Path: e.Path}
			}
			items = append(items, it)
		}
		return items
	})
	scr := palette.NewScratchMode(scratchList)
	all := palette.NewSearchAllMode(cmd, file, symbols)
	all.SetRecents(mru)
	branches := newBranchMode(func() []vcs.Branch { return vcsSt.branches })
	reverts := newRevertsMode(func() (string, []vcs.RevertSnapshot) { return vcsSt.revertsPath, vcsSt.reverts })
	openPath := palette.NewOpenPathMode()
	return palette.New(pcfg, cmd, file, dir, proj, refs, actions, mru, all, symbols, scr, pasteHist, branches, reverts, openPath)
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

// paletteToggleKey reads palette.toggle_key. Empty means no toggle chord: the
// palette opens via esc-esc, "@" and searchEverywhere; ctrl+p is bound to
// lsp.parameterInfo by default (#523).
func paletteToggleKey(cfg host.Config) string {
	if cfg != nil {
		if v, ok := cfg.Get("palette.toggle_key"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
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

// terminalFocused reports whether input currently goes to a live terminal —
// a terminal pane, or an editor pane whose active tab hosts one (#573); a
// dead one (shell exited) falls back to normal key handling so ctrl+w can
// close it.
func (m Model) terminalFocused() bool {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return false
	}
	if inst.Kind() != pane.KindTerminal && inst.Kind() != pane.KindEditor {
		return false
	}
	t := inst.ActiveTerminal()
	return t != nil && t.Running()
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
	if d := t.Cwd(); d != "" { // live cwd via OSC 7 (#770), start dir until reported
		title += " · " + displayDir(d)
	}
	// The application's OSC 0/2 title (shells report the running command
	// here) takes the tail slot when it says more than the shell name (#97).
	if osc := t.Title(); osc != "" && osc != filepath.Base(t.ShellPath()) {
		title += " · " + osc
	}
	// Active interpreter mappings (#98, #652): what php/python resolve to
	// inside new terminals — only mappings that actually inject (venv,
	// PATH prepend or shim), from the cache terminalEnv maintains
	// (recomputing detection per render would fork subprocesses).
	for _, mp := range activeMappings() {
		title += " · " + mp.Lang + "→" + project.CompactPath(mp.Interpreter)
	}
	return title
}

// displayDir shortens a directory for chrome: the base name when it is the
// working directory's base, the compacted path otherwise.
func displayDir(dir string) string {
	if cwd, err := cachedGetwd(); err == nil && cwd == dir {
		return filepath.Base(dir)
	}
	return project.CompactPath(dir)
}

// terminalReservedKey handles the documented reserved set — the only keys a
// focused live terminal does NOT forward to the shell:
//
//	ctrl+tab    move focus to the next pane (the global escape hatch)
//	alt+f12     terminal.toggle — return focus to the previous pane (#97)
//	cmd+t       new sibling terminal tab in the focused pane (#729)
//
// The spatial focus moves (default ctrl+arrows, keymap.bindings.focus_*),
// cmd+c over an active mouse selection, cmd+v (system-clipboard paste), and
// the global navigation allowlist (terminalGlobalCommands + the configured
// palette.toggle_key, #805) are reserved in the caller (#228, #227, #727).
// Everything else, including tab, ctrl+c, esc and the F-keys, belongs
// to the shell. shift+pgup/pgdn page the scrollback inside the pane itself.
func (m Model) terminalReservedKey(keys string) (bool, tea.Model, tea.Cmd) {
	// Canonicalize the chord (#981): bubbletea encodes the Command key as
	// super+/meta+ tokens, which ParseKey folds onto the logical cmd form the
	// cases below use. Deliberately NOT platform-folded to ctrl — inside a
	// terminal ctrl+t/ctrl+d belong to the shell on every platform.
	if k, err := keymap.ParseKey(keys); err == nil {
		keys = k.String()
	}
	switch keys {
	case "ctrl+tab":
		m.cycleFocus()
		return true, m, nil
	case "alt+f12":
		m.toggleTerminal()
		return true, m, nil
	case "cmd+t":
		// iTerm-style: cmd+t inside a terminal spawns a sibling terminal
		// (#729); outside terminals the chord keeps its global binding —
		// the reserved set only fires while a live terminal is focused.
		m.newTerminalSibling()
		return true, m, nil
	case "cmd+d":
		// iTerm-style: cmd+d splits the focused terminal's pane to the
		// right with a fresh terminal (#982); outside terminals the chord
		// keeps its global binding (editor.duplicateLine).
		m.newTerminalSplitRight()
		return true, m, nil
	case "cmd+w":
		// cmd+w closes the focused terminal (#986): an idle shell gets an
		// EOF (it exits and the regular exit path closes the pane/tab); a
		// busy one raises the confirmation guard first. ctrl+w stays with
		// the shell (delete word); outside terminals cmd+w keeps its
		// global binding (editor.closeTab).
		m.requestTerminalClose()
		return true, m, nil
	}
	return false, m, nil
}

// newTerminalSplitRight splits the focused terminal's pane to the right with a
// fresh terminal pane and focuses it (#982, iTerm's cmd+d) — the same for a
// dedicated terminal pane and an editor pane hosting a terminal tab (#573).
func (m *Model) newTerminalSplitRight() {
	key := m.activeWS().Panes.Focused()
	if m.activeWS().Panes.Get(key) == nil || m.activeWS().Tree == nil {
		return
	}
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	nkey := m.activeWS().Panes.AddTerminal(terminal.Shell(shell), ".", terminalEnv(), m.host.Send)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, key, nkey, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(nkey)
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(nkey)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// mouseChordKey maps the dedicated mouse navigation buttons (#816) onto the
// synthetic keymap bases the default table binds to nav.back / nav.forward.
func mouseChordKey(b tea.MouseButton) (keymap.Key, bool) {
	switch b {
	case tea.MouseBackward:
		return keymap.Key{Base: "mouse-back"}, true
	case tea.MouseForward:
		return keymap.Key{Base: "mouse-forward"}, true
	}
	return keymap.Key{}, false
}

// terminalGlobalCommands are the commands whose chords a focused live
// terminal does NOT forward to the shell (#805): the IDE's global entry
// points — palette and project switching — must stay reachable without first
// focusing an editor. Resolved through the live binding table, so rebinds
// move the reserved chord along. esc-esc deliberately stays with the shell:
// forwarding escapes to vim/lazygit while also opening the palette would
// cause side effects there.
var terminalGlobalCommands = map[string]bool{
	"palette.searchEverywhere": true,
	"palette.recentFiles":      true,
	"project.switch":           true,
	// #973: IDE-level chords the shell can never meaningfully use.
	"settings.open":         true,
	"project.goToFile":      true,
	"project.goToClass":     true,
	"project.findInPath":    true,
	"project.replaceInPath": true,
	"explorer.toggle":       true,
	"window.hideAllTools":   true,
	"nav.pins":              true,
	"nav.pinGoto1":          true,
	"nav.pinGoto2":          true,
	"nav.pinGoto3":          true,
	"nav.pinGoto4":          true,
	"todo.list":             true,
	"vcs.panel":             true,
	"problems.toggle":       true,
	"structure.toggle":      true,
	"notifications.history": true,
	// #997: tab switching stays reachable from a focused terminal/tool pane
	// (the shell never meaningfully sees ctrl+cmd+arrows). The secondary
	// ctrl+alt+arrow bindings stay with the shell — see terminalShellChords.
	"editor.tab.next": true,
	"editor.tab.prev": true,
	// #934: zen must toggle (and untoggle) with a terminal or tool pane
	// focused; the shell never meaningfully sees the zen chord.
	"view.zenMode": true,
}

// terminalShellChords are chords that stay with the shell even when they
// resolve to an allowlisted command (#997): alt-modified arrows are common
// readline word/line navigation, so only the ctrl+cmd tab chords are
// reserved and the ctrl+alt secondaries keep reaching the shell.
var terminalShellChords = map[string]bool{
	"ctrl+alt+left":  true,
	"ctrl+alt+right": true,
}

// doubleTapWindow is how close two bare shift taps must be to count as the
// double-shift chord (#973), mirroring JetBrains' double-tap timing.
const doubleTapWindow = 600 * time.Millisecond

// isBareShift reports a bare shift modifier press (no base key).
func isBareShift(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "shift", "leftshift", "rightshift":
		return true
	}
	return false
}

// terminalGlobalChord resolves a single-step chord against the live binding
// table and dispatches it when it maps to an allowlisted global command
// (#805). The double-shift tap is detected explicitly (#973); other
// multi-step chords (cmd+k sequences) cannot be intercepted without
// buffering shell input and are left to the shell.
func (m *Model) terminalGlobalChord(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	if m.paletteKey != "" && msg.String() == m.paletteKey {
		m.openPalette()
		return true, nil
	}
	// Double-shift (#973): two bare shift taps in quick succession open
	// Search Everywhere. Unlike esc esc (which vim/lazygit need), a bare
	// modifier press means nothing to the shell, so intercepting the second
	// tap is side-effect-free; the taps themselves still forward.
	if isBareShift(msg) {
		if time.Since(m.termShiftAt) < doubleTapWindow {
			m.termShiftAt = time.Time{}
			if c, okc := m.reg.Command("palette.searchEverywhere"); okc {
				return true, m.dispatchCommand("palette.searchEverywhere", c)
			}
		}
		m.termShiftAt = time.Now()
		return false, nil
	}
	m.termShiftAt = time.Time{}
	k, ok := keymap.FromKeyMsg(msg)
	if ok {
		table := m.bindings.Table()
		if table == nil {
			return false, nil
		}
		chord := keymap.Chord{Steps: []keymap.Key{k}}
		if terminalShellChords[chord.String()] {
			return false, nil
		}
		if b, found := table.Lookup(chord, keymap.Context(m.focusContext())); found && terminalGlobalCommands[b.Command] {
			if c, okc := m.reg.Command(b.Command); okc {
				return true, m.dispatchCommand(b.Command, c)
			}
		}
	}
	return false, nil
}

// newTerminalSibling opens a terminal tab next to the focused one (#729,
// iTerm's cmd+t): a terminal tab hosted by an editor pane gets a sibling tab
// in the same pane (#573); a dedicated single-session terminal pane converts
// into a tab host first (#983, the same in-place conversion a tab drop does,
// #836) so its live shell becomes the first tab and the new one the second.
// The new session is focused either way.
func (m *Model) newTerminalSibling() {
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return
	}
	if inst.Kind() == pane.KindTerminal && !inst.ConvertToTabHost() {
		return
	}
	if inst.Kind() != pane.KindEditor {
		return
	}
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	tkey := m.activeWS().Panes.MintTerminalKey()
	term := terminal.New(tkey, terminal.Shell(shell), ".", 80, 24, terminalEnv(), m.host.Send)
	inst.AddTerminalTab(term)
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// activeWS returns the active workspace (Roadmap 0370, #776): the single
// access path to the pane registry, split tree and terminal return-focus.
func (m Model) activeWS() *workspace.Workspace { return m.ws.Active() }

// currentTerminal returns the focused regular terminal instance, else the
// first regular terminal in pane order, else nil. Tool panes (#741) never
// count (#772).
func (m Model) currentTerminal() *pane.Instance {
	// Custom tool panes (#741) reuse the terminal machinery but are not
	// regular terminals: terminal.toggle/clear must not treat them as the
	// terminal to focus or clear (#772).
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() == "" {
		return inst
	}
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().Tool() == "" {
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
		m.activeWS().ReturnFocus = m.activeWS().Panes.Focused()
		m.openTerminal()
		return
	}
	if m.activeWS().Panes.Focused() != inst.Key() {
		m.activeWS().ReturnFocus = m.activeWS().Panes.Focused()
		m.setFocus(inst.Key())
		return
	}
	target := m.activeWS().ReturnFocus
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = m.activeEditorKey()
	}
	if target == "" || !m.activeWS().Panes.Has(target) {
		target = pane.ExplorerKey
	}
	m.setFocus(target)
}

// effectiveMappings collects the effective interpreter per registered
// language (#98, #652): the explicit [lang.<id>] interpreter setting beats
// project detection — the same lang.Interpreter seam LSP, debug and the
// statusline read. Detection runs against the working directory (the project
// root by convention, like explicit settings always did).
func effectiveMappings() []terminal.Mapping {
	c := config.Get()
	var out []terminal.Mapping
	for _, l := range lang.All() {
		explicit := ""
		if c != nil {
			explicit = c.Lang[l.ID]["interpreter"]
		}
		if path, source := lang.Interpreter(l.ID, ".", explicit); path != "" {
			// Detection against "." can yield relative paths; PATH
			// entries must survive the shell changing directories.
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
			out = append(out, terminal.Mapping{Lang: l.ID, Interpreter: path, Source: source})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lang < out[j].Lang })
	return out
}

// termActive caches the mappings the last terminalEnv run actually injected,
// for the pane-title indicator: titles render every frame and must not
// re-run toolchain detection (which can fork version managers).
var termActive struct {
	sync.Mutex
	mappings []terminal.Mapping
}

func activeMappings() []terminal.Mapping {
	termActive.Lock()
	defer termActive.Unlock()
	return termActive.mappings
}

// shimDir is the per-project shim directory, mirroring the state stores'
// IKE_CONFIG_DIR override.
func shimDir() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "shims")
	}
	return filepath.Join(".ike", "shims")
}

// terminalEnv plans the toolchain activation for the effective mappings
// (#652), regenerates/sweeps the shims accordingly and returns the
// spawn-environment overlay — nil when nothing injects (no explicit setting
// and no project-local detection difference). It applies to NEW terminals;
// running sessions keep their environment.
func terminalEnv() []string {
	plan := terminal.PlanActivation(effectiveMappings(), os.Getenv("PATH"))
	dir := shimDir()
	if _, err := terminal.WriteShims(dir, plan.Shims); err != nil {
		return nil
	}
	termActive.Lock()
	termActive.mappings = plan.Active
	termActive.Unlock()
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return plan.Overlay(abs, os.Getenv("PATH"))
}

// openTerminal opens a fresh terminal pane rooted in the working directory
// (the project root), split below the active editor — the conventional
// JetBrains placement — falling back to the focused leaf when no editor
// exists.
func (m *Model) openTerminal() {
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	key := m.activeWS().Panes.AddTerminal(terminal.Shell(shell), ".", terminalEnv(), m.host.Send)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneBottom)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(key)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// openTerminalTab opens a fresh shell session as a new tab of the active
// editor pane (#573), so the terminal sits next to the files it belongs to.
// Without an editor pane it falls back to the classic bottom-split terminal.
func (m *Model) openTerminalTab() {
	target := m.activeEditorKey()
	if target == "" {
		m.openTerminal()
		return
	}
	inst := m.activeWS().Panes.Get(target)
	shell := ""
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		shell = v
	}
	key := m.activeWS().Panes.MintTerminalKey()
	term := terminal.New(key, terminal.Shell(shell), ".", 80, 24, terminalEnv(), m.host.Send)
	inst.AddTerminalTab(term)
	m.setFocus(target)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// openMarkdownPreview opens a rendered preview pane for the active editor's
// markdown buffer, split to its right (#62). The editor keeps focus — the
// preview follows the typing, it does not receive it. A preview already bound
// to the buffer is focused instead of duplicated; a non-markdown buffer is a
// no-op with a toast.
func (m *Model) openMarkdownPreview() {
	target := m.activeEditorKey()
	if target == "" || m.activeWS().Tree == nil {
		m.host.Notify(host.Info, "markdown preview needs an open markdown file")
		return
	}
	ed := m.activeWS().Panes.Get(target).Editor()
	if ed == nil || !ed.HasFile() || !isMarkdownPath(ed.Path()) {
		m.host.Notify(host.Info, "markdown preview needs an open markdown file")
		return
	}
	path := ed.Path()
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindMarkdown && inst.Preview().Path() == path {
			m.setFocus(key)
			return
		}
	}
	key := m.activeWS().Panes.AddMarkdownPreview(path)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, layout.ZoneRight)
	if !ok {
		m.activeWS().Panes.Close(key)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	pv := m.activeWS().Panes.Get(key).Preview()
	pv.SetSourceImmediate(ed.Text())
	line, _ := ed.CursorPos()
	pv.SetCursorLine(line)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// openDiffPane splits the focused leaf with a read-only diff viewer comparing
// the files at leftPath and rightPath (#60). The new pane takes focus so n/N
// and enter work immediately; an unreadable file diffs as empty text.
func (m *Model) openDiffPane(leftPath, rightPath string) {
	// The same file pair re-opens by focusing the existing pane with fresh
	// contents (#509).
	if key, ok := m.findDiffPane(leftPath, rightPath, "", ""); ok {
		m.activeWS().Panes.Get(key).Diff().SetContents(readFileOrEmpty(leftPath), readFileOrEmpty(rightPath))
		m.setFocus(key)
		return
	}
	// Single diff window (#513): retarget the existing pane instead of
	// splitting another one.
	if key, ok := m.diffSlot(); ok {
		inst := m.activeWS().Panes.Get(key)
		inst.StopDiffEdit()
		inst.Diff().Retarget(baseName(leftPath), baseName(rightPath), leftPath, rightPath, "", "", true)
		inst.Diff().SetContents(readFileOrEmpty(leftPath), readFileOrEmpty(rightPath))
		m.setFocus(key)
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
		return
	}
	key := m.activeWS().Panes.AddDiff(leftPath, rightPath)
	if !m.placeDiffLeaf(key) {
		return
	}
	m.activeWS().Panes.Get(key).Diff().SetContents(readFileOrEmpty(leftPath), readFileOrEmpty(rightPath))
	m.setFocus(key)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// diffSlot returns the diff pane to reuse in single-window mode (#513): the
// first open diff pane, unless config diff.windows = "multi" restores the
// split-per-open behavior.
func (m Model) diffSlot() (string, bool) {
	if v, ok := m.host.Config().Get("diff.windows"); ok && v == "multi" {
		return "", false
	}
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindDiff {
			return key, true
		}
	}
	return "", false
}

// findDiffPane locates an open diff pane matching the identity: the file
// pair plus the per-side revisions ("" = working tree). Re-opening the same
// diff focuses it instead of splitting a duplicate (#509).
func (m Model) findDiffPane(leftPath, rightPath, leftRev, rightRev string) (string, bool) {
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindDiff {
			continue
		}
		d := inst.Diff()
		lr, rr := d.Revs()
		if d.LeftPath() == leftPath && d.RightPath() == rightPath && lr == leftRev && rr == rightRev {
			return key, true
		}
	}
	return "", false
}

// revContentOrFile resolves one restored diff side (#508): a revision reads
// its blob at blobPath via git, a file-backed side reads path from disk;
// failures degrade to empty text like readFileOrEmpty.
func revContentOrFile(rev, path, blobPath string) string {
	if rev == "" {
		return readFileOrEmpty(path)
	}
	root, err := vcs.DetectRoot(".")
	if err != nil {
		return ""
	}
	content, err := vcs.RevContent(root, rev, blobPath)
	if err != nil {
		return ""
	}
	return content
}

// readFileOrEmpty reads path, degrading a missing or unreadable file to the
// empty text so a diff side never breaks the pane.
func readFileOrEmpty(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// isMarkdownPath reports whether path names a markdown document.
func isMarkdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

// previewsForPath returns every markdown preview instance bound to path.
func (m Model) previewsForPath(path string) []*pane.Instance {
	var out []*pane.Instance
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindMarkdown && inst.Preview().Path() == path {
			out = append(out, inst)
		}
	}
	return out
}

// explorer returns the singleton explorer model.
func (m Model) explorer() *explorer.Model {
	return m.activeWS().Panes.Get(pane.ExplorerKey).Explorer()
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.explorer().Init()}
	// The TODO index's initial full scan (#61): Init runs after main.go wires
	// the sender (and again after a project switch), so the streamed results
	// land and the status-line count is live without opening the overlay.
	m.todo.Rescan()
	// Highlight any files restored from the previous session at startup, before
	// the user edits them, and announce each to the plugin hooks (#332): the
	// restore paths (restoreLayout/restoreSession) load editors directly via
	// editor.Load, bypassing openPath, so without this the LSP never learns about
	// files already open at launch and they get no diagnostics until reopened.
	// Init runs after main.go wires the sender, so the bridge's async results land.
	opened := map[string]bool{} // one EventFileOpened per file — shared tabs/leaves (#142)
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
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
	// EventBufferClosed fires here, once the whole pass settled (#827): a tab
	// drag closes its source view and reopens the file elsewhere within one
	// message, so only now is "no view left" decidable.
	if closed := mm.drainClosedFileViews(); closed != nil {
		cmd = tea.Batch(cmd, closed)
	}
	// The Structure pane follows the focused buffer here (#1025), once the
	// pass settled: cursor follow is a cheap in-place highlight, a buffer
	// switch issues the documentSymbol refresh (deduplicated per path).
	if sync := mm.structureSyncCmd(); sync != nil {
		cmd = tea.Batch(cmd, sync)
	}
	// An armed explorer reveal (#1042) drains here once the pass settled:
	// SetActive's call sites (focus changes, tab switches, the CLI open flow)
	// cannot dispatch Cmds, so auto-reveal / Reveal() only mark the model and
	// the expansion scans start now.
	if reveal := mm.explorer().PendingRevealCmd(); reveal != nil {
		cmd = tea.Batch(cmd, reveal)
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
	// Terminal capability probing (#720): collect the async reports, then a
	// grace tick draws the verdict. A terminal without the Kitty protocol
	// never sends KeyboardEnhancementsMsg, so the tick treats silence as
	// "unsupported" and toasts the specific deficiency.
	case tea.KeyboardEnhancementsMsg:
		m.caps.kitty = msg.SupportsKeyDisambiguation()
		return m, nil

	case tea.ColorProfileMsg:
		m.caps.profile = msg.Profile
		m.caps.profileSeen = true
		// The profile report is the "running under a real bubbletea program"
		// signal (it always arrives at startup, before any user input), so it
		// also schedules the verdict tick. Deliberately not done in Init: the
		// test harness (sized()) executes Init's commands synchronously, and a
		// tea.Tick there would sleep the grace period and toast capability
		// warnings into unrelated tests.
		var tick tea.Cmd
		if !m.caps.scheduled {
			m.caps.scheduled = true
			tick = termCheckTick()
		}
		return m, tick

	case termCheckMsg:
		return m, m.runTermCheck()

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.shell.SetSize(m.width, m.height)
		m.palette.SetSize(m.width, m.height)
		m.finder.SetSize(m.width, m.height)
		m.todo.SetSize(m.width, m.height)
		m.callhier.SetSize(m.width, m.height)
		m.undoTree.SetSize(m.width, m.height)
		m.commitUI.SetSize(m.width, m.height)
		m.menu.SetWidth(m.width)
		{
			w, h := m.settingsSize()
			m.settings.SetSize(w, h)
		}
		// Now that the window is sized, surface any crash-recovery snapshots found
		// at startup (Roadmap 0210, #166), then the first-run welcome tour
		// (#658), then the first-start LSP onboarding dialog (#301) — recovery
		// wins the shell, and the LSP dialog queues behind the tour (its
		// maybeOpen refuses while the shell is open; closeTour re-triggers it).
		m.maybeOpenRecovery()
		tourCmd := m.maybeOpenTour()
		m.maybeOpenOnboarding()
		return m, tourCmd

	case tea.MouseClickMsg:
		// The dedicated back/forward buttons (#816) resolve through the
		// keymap as synthetic chords — rebindable like keys, default
		// nav.back / nav.forward — regardless of the hovered pane. Unbound
		// presses are swallowed: no pane expects button 4/5.
		if k, isNav := mouseChordKey(msg.Button); isNav {
			if cmd, handled := m.resolveKeymap(k); handled {
				return m, cmd
			}
			return m, nil
		}
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mousePress})
	case tea.MouseReleaseMsg:
		if _, isNav := mouseChordKey(msg.Button); isNav {
			return m, nil // the press acted; the release must not leak into panes
		}
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mouseRelease})
	case tea.MouseMotionMsg:
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mouseMotion})
	case tea.MouseWheelMsg:
		return m.queueWheel(mouseEvent{Mouse: msg.Mouse(), action: mouseWheel})
	case wheelFlushMsg:
		return m.flushWheel()

	case coalescedInputMsg:
		// A folded mouse burst from the input coalescer (#602): applied in one
		// pass so a scroll/drag storm costs a single render, never queuing up
		// behind (or ahead of) keystrokes.
		return m.applyCoalescedInput(msg)

	case tea.PasteMsg:
		// Bracketed paste (#603): the terminal delivers the whole pasted block as
		// one message. Insert it in a single pass (one edit, one undo unit) rather
		// than letting it arrive as per-character key input.
		return m.handlePaste(msg.Content)

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

	case OpenTodoIndexMsg:
		// todo.list (cmd+k t / palette): the TODO/FIXME index overlay (#61),
		// rooted at the working directory like the finder.
		m.todo.SetSize(m.width, m.height)
		cur := ""
		if ed := m.activeEditor(); ed != nil && ed.HasFile() {
			cur = ed.Path()
		}
		m.todo.Open(cur)
		return m, nil

	case todoindex.ScanMsg:
		// The TODO index's streamed scan results (#61), wrapped so the finder
		// never ingests them (both services count generations independently).
		m.todo.Apply(msg.Inner)
		return m, nil

	case todoSavedMsg:
		// A buffer save (#61): rescan just the saved file off the update loop.
		// The save is also the persistence point for edit-shifted breakpoints
		// (#577) — cheap, and the on-disk lines match the saved file.
		_ = m.bpts.Save()
		// The Structure pane re-requests the saved buffer's symbols (#1025);
		// the Update wrapper's sync issues the actual request.
		if sp := m.structPanel(); sp != nil && sp.Path() == msg.path {
			m.structForce = true
		}
		return m, m.todo.RescanFile(msg.path)

	case todoindex.FileScanMsg:
		m.todo.ApplyFileScan(msg)
		return m, nil

	case todoindex.OpenLocationMsg:
		// A selected tag: open the file with the cursor on it (1-based lines,
		// openPathAt takes 0-based).
		return m.openPathAt(msg.Path, msg.Line-1, msg.Col)

	case problems.OpenLocationMsg:
		// A Problems row activation (#1024): open the file with the cursor on
		// the diagnostic (already 0-based, like definition targets).
		return m.openPathAt(msg.Path, msg.Line, msg.Col)

	case editor.OpenUndoTreeMsg:
		// editor.undoTree (palette): the undo-tree overlay (#59) over the
		// focused editor's change tree.
		if ed := m.activeEditor(); ed != nil {
			m.undoTree.SetSize(m.width, m.height)
			m.undoTree.Open(ed.HistoryTree())
		}
		return m, nil

	case undotree.JumpMsg:
		// A selected state (#59): restore the focused editor's buffer to it,
		// then refresh the overlay so the current marker follows the jump.
		if key := m.activeEditorKey(); key != "" {
			cmd := m.activeWS().Panes.Get(key).Update(editor.HistoryJumpMsg{Seq: msg.Seq})
			if ed := m.activeEditor(); ed != nil {
				m.undoTree.SetNodes(ed.HistoryTree())
			}
			return m, cmd
		}
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

	case explorer.HiddenToggledMsg:
		// Persist the show-hidden toggle immediately so it survives a kill/crash,
		// not only a clean quit (#629).
		saveSession(m.snapshotSession())
		return m, nil

	case RenameFileMsg:
		// file.rename (shift+f6 / palette): explorer prompt on the selection,
		// or the shell prompt for the focused editor's file.
		return m, m.startRenameFile()

	case MoveFileMsg:
		// file.move (f6 / palette): pick a target folder for the selection /
		// focused file via the palette's directory mode.
		m.startMoveFile()
		return m, nil

	case ImportJetBrainsKeymapMsg:
		// keymap.importJetBrains (palette, #677): prompt for the exported
		// XML's path, then translate it into keymap.bindings.* overrides.
		m.startJBImport()
		return m, nil

	case jbImportDoneMsg:
		// The finished import: toast the summary and apply the config reload.
		return m, m.finishJBImport(msg)

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

	case ForceCodeInsightMsg:
		// editor.forceCodeInsight (palette): override the large-file
		// degradation (#149) for the focused document.
		return m.forceCodeInsight()

	case ShowKeymapHelpMsg:
		// palette.keymapHelp (f1, cmd+k cmd+s / palette): the cheatsheet overlay.
		m.openHelp()
		return m, nil

	case ShowWelcomeTourMsg:
		// help.welcomeTour (palette): the paged welcome tour (#657).
		m.openTour()
		return m, nil

	case CommandExecutedMsg:
		// The command-executed signal (#679): the tour ticks a matching
		// try-it task (#680). A suspended tour resumes lazily via
		// maybeResumeTour once the covering overlay is gone.
		if m.tour != nil {
			m.tour.NoteExecuted(msg.ID)
		}
		return m, nil

	case CyclePaneFocusMsg:
		// pane.switcher (ctrl+tab / palette): same cycle as the hardcoded tab.
		m.cycleFocus()
		return m, nil

	case SaveAllMsg:
		// editor.saveAll (cmd+shift+s / palette): write every dirty editor,
		// background tabs included.
		var cmds []tea.Cmd
		for _, key := range m.activeWS().Panes.Keys() {
			inst := m.activeWS().Panes.Get(key)
			if inst == nil || inst.Kind() != pane.KindEditor {
				continue
			}
			for i := 0; i < inst.TabCount(); i++ {
				if ed := inst.TabEditor(i); ed != nil && ed.Dirty() {
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
		return m, m.settings.Deliver(msg)

	case settings.PackagesMsg, settings.OutdatedMsg, settings.PkgActionMsg:
		// Package listing (#569 — previously unrouted), available upgrades
		// and finished package actions (#571) land in the toolchain page.
		return m, m.settings.Deliver(msg)

	case settings.WizardTickMsg, settings.WizardDataMsg:
		// Venv-wizard internals (#884): spinner ticks and async data fetches
		// route back into the open sub-panel, which may chain follow-ups.
		return m, m.settings.Deliver(msg)

	case settings.MarketCatalogMsg:
		// A finished marketplace catalog fetch (Roadmap 0310, #446).
		_ = m.settings.Deliver(msg)
		if msg.Err != nil {
			m.host.Notify(host.Warn, "marketplace: "+msg.Err.Error())
		}
		return m, nil

	case settings.MarketActionMsg:
		// A finished marketplace install/update/remove; the page shows the
		// detail, the toast carries the headline.
		_ = m.settings.Deliver(msg)
		if msg.Err != nil {
			m.host.Notify(host.Warn, "marketplace: "+msg.Action+" "+msg.Name+": "+msg.Err.Error())
		} else if msg.Action == "remove" {
			m.host.Notify(host.Info, "marketplace: removed "+msg.Name)
		} else {
			done := map[string]string{"install": "installed", "update": "updated"}[msg.Action]
			m.host.Notify(host.Info, "marketplace: "+done+" "+msg.Name+" — restart to load")
		}
		return m, nil

	case settings.EnvMsg:
		// Python environment action finished (#132): show the result on the
		// page, and on success register the interpreter through write-back
		// (lang.Interpreter stays the single source of truth) and restart the
		// language's server against it.
		deliverCmd := m.settings.Deliver(msg)
		if msg.Err != nil {
			m.host.Notify(host.Warn, "python environment: "+msg.Err.Error())
			return m, deliverCmd
		}
		m.host.Notify(host.Info, msg.Label+" — registered as project interpreter")
		cmds := []tea.Cmd{deliverCmd, config.WriteAndReload(m.cfgOpts, config.ProjectScope, "lang."+msg.LangID+".interpreter", msg.Interpreter)}
		if c, ok := m.reg.Command("lsp.restart"); ok {
			cmds = append(cmds, m.dispatchCommand("lsp.restart", c))
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

	case HideToolWindowsMsg:
		// window.hideAllTools (#791): hide every tool window / restore.
		m.toggleToolWindows()
		return m, nil

	case ZenModeMsg:
		// view.zenMode (ctrl+alt+f / View menu, #359): maximize + no chrome.
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

	case OpenPythonEnvWizardMsg:
		// python.newEnvironment (palette, #884): open settings on the
		// Toolchain page with the venv wizard pushed.
		w, h := m.settingsSize()
		m.settings.SetSize(w, h)
		m.settings.OpenPythonEnvWizard()
		return m, nil

	case OpenSettingsMsg:
		// settings.open (cmd+, / menu / palette): the floating settings panel.
		// Opening prefetches the marketplace catalog once (no-op when it is
		// already loaded, in flight, or unconfigured).
		w, h := m.settingsSize()
		m.settings.SetSize(w, h)
		m.settings.Open()
		return m, m.marketPage.RefreshCmd()

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
		// Opening marks everything seen — the status line counter resets (#101).
		m.notifUnseen = 0
		body := m.historyView()
		m.shell.SetContent(ui.ModelContent{Heading: "NOTIFICATIONS", Body: func() string { return body }})
		m.shell.SetSize(m.width, m.height)
		m.shell.Open()
		return m, nil

	case ToggleExplorerFocusMsg:
		// explorer.toggle (cmd+1): the JetBrains cmd+1 state machine
		// (#268) — focused tree hides, visible unfocused tree gains focus, a
		// hidden tree comes back at its remembered width and takes focus.
		m.toggleExplorer()
		return m, nil

	case TerminalNewMsg:
		// terminal.new (palette / menu): split the focused leaf with a fresh
		// shell session rooted in the project (Roadmap 0170, #95).
		m.openTerminal()
		return m, nil

	case TerminalNewTabMsg:
		// terminal.newTab (palette / menu, #573): open a shell in a new tab
		// of the active editor pane, next to the file tabs.
		m.openTerminalTab()
		return m, nil

	case RunFileMsg:
		// run.file (shift+f10 / Run menu / palette, #576): run the active
		// file through its run configuration.
		m.runCurrentFile()
		return m, nil

	case RunRerunMsg:
		// run.rerun (Run menu / palette, #576): repeat the last run.
		m.rerunLast()
		return m, nil

	case DebugToggleBreakpointMsg:
		// debug.toggleBreakpoint (ctrl+f8 / Run menu / palette, #577).
		m.toggleBreakpointAtCursor()
		return m, nil

	case DebugStartMsg:
		// debug.start (shift+f9 / Run menu / palette, #579).
		m.startDebug()
		return m, nil

	case DebugStopMsg:
		m.stopDebugSession(true)
		return m, nil

	case DebugListenMsg:
		// debug.listen (palette / Run menu, #823): toggle the persistent
		// Xdebug listener for web/request debugging through php-fpm.
		m.toggleDebugListen()
		return m, nil

	case DebugStepOverMsg:
		m.debugStep("over")
		return m, nil
	case DebugStepIntoMsg:
		m.debugStep("into")
		return m, nil
	case DebugStepOutMsg:
		m.debugStep("out")
		return m, nil
	case DebugContinueMsg:
		m.debugStep("continue")
		return m, nil

	case debugEventMsg:
		// Raw adapter events (initialized, stopped, output, terminated, …).
		m.handleDebugEvent(msg.ev)
		return m, nil

	case debugStoppedMsg:
		// The stop context arrived: jump to the top frame, mark its line,
		// and feed the tool window (#580).
		if top := m.applyDebugStop(msg); top != nil {
			col := top.Column - 1
			if col < 0 {
				col = 0
			}
			model, cmd := m.openPathAt(top.Source.Path, top.Line-1, col)
			mm, ok := model.(Model)
			if !ok {
				return model, cmd
			}
			mm.markPausedLine(canonicalPath(top.Source.Path), top.Line-1)
			mm.openDebugPanel()
			if p := mm.debugPanel(); p != nil {
				p.SetFrames(msg.frames)
			}
			mm.fetchScopes(top.ID)
			return mm, cmd
		}
		return m, nil

	case debugScopesMsg:
		if p := m.debugPanel(); p != nil {
			p.SetScopes(msg.scopes)
		}
		return m, nil

	case debugVarsMsg:
		if p := m.debugPanel(); p != nil {
			p.SetChildren(msg.ref, msg.vars)
		}
		return m, nil

	case debugpanel.SelectFrameMsg:
		// A frame was activated in the tool window: show that frame's state
		// — navigate the editor to its location and re-scope the variables.
		m.fetchScopes(msg.Frame.ID)
		if msg.Frame.Source.Path != "" {
			col := msg.Frame.Column - 1
			if col < 0 {
				col = 0
			}
			return m.openPathAt(msg.Frame.Source.Path, msg.Frame.Line-1, col)
		}
		return m, nil

	case debugpanel.ExpandVarMsg:
		m.fetchVariables(msg.Ref)
		return m, nil

	case debugpanel.SetVarMsg:
		// A variable value was edited in the tool window (#627): push it to the
		// adapter, then refetch the container so the panel shows the new value.
		m.setDebugVariable(msg.Ref, msg.Name, msg.Value)
		return m, nil

	case debugRunInTerminalMsg:
		// debugpy asked us to launch the debuggee in a terminal it can read
		// stdin from (#625): spawn it and answer with the pid.
		m.runDebuggeeInTerminal(msg)
		return m, nil

	case debugErrMsg:
		m.dbgLaunching = false
		m.host.Notify(host.Error, "debug: "+msg.err.Error())
		return m, nil

	case debugInstallResultMsg:
		// The adapter-runtime auto-install finished (#589): success retries
		// the pending launch once; failure surfaces the manual command.
		if msg.gen != m.dbgLaunchGen {
			// The launch was cancelled by debug.stop while installing (#636):
			// drop the retry silently — the stop already notified.
			return m, nil
		}
		if msg.err != nil {
			m.dbgLaunching = false
			m.host.Notify(host.Error, "debug: install failed: "+msg.err.Error())
			return m, nil
		}
		m.host.Notify(host.Info, "debug: adapter runtime installed — starting session")
		m.launchOrInstall(msg.root, msg.cfg, true)
		return m, nil

	case debugEndedMsg:
		m.finishDebugSession(msg)
		return m, nil

	case MarkdownPreviewMsg:
		// markdown.preview (cmd+k m / palette): split the active editor with a
		// rendered live preview of its markdown buffer (#62).
		m.openMarkdownPreview()
		return m, nil

	case DiffFilesMsg:
		// diff.files (palette): compare two files picked one after the other
		// via the "@" finder (#60); the picks land as palette.OpenFileMsg and
		// are intercepted below while diffPick is armed.
		m.diffPick = 1
		m.diffLeft = ""
		m.host.Notify(host.Info, "diff: pick the left (old) file")
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, '@')
		return m, nil

	case diff.JumpMsg:
		// enter on a hunk: open the diff's right-hand file with the cursor on
		// the hunk's first line (JumpMsg lines are 1-based, openPathAt 0-based).
		return m.openPathAt(msg.Path, msg.Line-1, 0)

	case preview.RenderTickMsg:
		// A preview's debounce timer fired: route it to the owning pane, which
		// renders only when the tick is still the newest one.
		if inst := m.activeWS().Panes.Get(msg.Key); inst != nil && inst.Kind() == pane.KindMarkdown {
			return m, inst.Update(msg)
		}
		return m, nil

	case preview.CursorMsg:
		// The source editor's cursor moved: scroll every preview of the buffer
		// to follow (#62).
		for _, inst := range m.previewsForPath(msg.Path) {
			inst.Preview().SetCursorLine(msg.Line)
		}
		return m, nil

	case TerminalToggleMsg:
		// terminal.toggle (alt+f12 / palette / menu): the JetBrains state
		// machine — create, focus, or return focus (#97).
		m.toggleTerminal()
		return m, nil

	case VCSPanelToggleMsg:
		// vcs.panel (0330, #482): same state machine for the VCS tool window.
		m.toggleVCSPanel()
		return m, nil

	case ProblemsToggleMsg:
		// problems.toggle (#1024): same state machine for the Problems pane.
		m.toggleProblemsPanel()
		return m, nil

	case StructureToggleMsg:
		// structure.toggle (#1025): same state machine for the Structure
		// tool window; the Update wrapper's sync issues the first refresh.
		m.toggleStructurePanel()
		return m, nil

	case ilsp.DocumentSymbolsMsg:
		// A documentSymbol reply (#1025) fills the open Structure pane.
		m.applyDocumentSymbols(msg)
		return m, nil

	case structpanel.NavigateMsg:
		// Enter / double-click on a symbol row (#1025): jump the editor to
		// the symbol through the standard open funnel, so nav history records
		// it like a definition jump.
		return m.openPathAt(msg.Path, msg.Line, msg.Col)

	case diff.EditRequestMsg:
		// 'e' in a diff pane (0340, #496): mount a live editor as the right
		// column. Revision-only diffs (the log's parent-vs-commit view) stay
		// read-only with a hint.
		inst := m.activeWS().Panes.Get(msg.Key)
		if inst == nil || inst.Kind() != pane.KindDiff || inst.DiffEditor() != nil {
			return m, nil
		}
		if !inst.Diff().Editable() || msg.Path == "" {
			m.host.Notify(host.Info, "this diff compares revisions — read-only")
			return m, nil
		}
		ed := editor.New()
		ed.SetPalette(m.themePal)
		ed.Configure(m.host.Config())
		if c := clipboard.System(); c != nil {
			ed.SetClipboard(c)
		}
		if prev := m.editorForPath(msg.Path); prev != nil {
			// The file is open elsewhere: edit the same document (#142), so
			// the tab and the diff column never diverge.
			ed.ShareDocumentWith(prev)
		} else if err := ed.Load(msg.Path); err != nil {
			m.host.Notify(host.Error, "edit: "+err.Error())
			return m, nil
		}
		ed.SetEmitter(editorEmitter{host: m.host, watcher: m.watcher, nav: m.navHist, key: msg.Key})
		inst.StartDiffEdit(&ed)
		m.host.Notify(host.Info, "editing "+displayPath(msg.Path)+" — ctrl+e returns to the diff")
		return m, ed.Reparse()

	case DiffStepMsg:
		// diff.nextChange / diff.prevChange (F7 / shift+F7, 0340 #495): step
		// the focused diff pane's hunk; a non-diff focus is a quiet no-op
		// (the bindings are diff-scoped, so this is belt and braces).
		if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindDiff {
			inst.Diff().StepHunk(msg.Delta)
		}
		return m, nil

	case ToolOpenMsg:
		// tool.<name> (#741): open the configured TUI tool pane, focus it
		// when it exists, return focus when it is already focused. New
		// (tool.<name>.new, #835) spawns another instance for multiple-
		// enabled tools.
		m.openTool(msg.Name, msg.New)
		return m, nil

	case ShowToolSetupMsg:
		// tools.setup (#751–#753): reopen the tool-pane setup dialog any time.
		if !m.openToolSetup() {
			m.host.Notify(host.Info, "all recommended tools are already configured — manage them in Settings → Tools")
		}
		return m, nil

	case toolcatalog.InstallResultMsg:
		// A tool install from the setup dialog or the Tools settings page
		// finished (#751–#753, #759).
		if msg.Err != nil {
			text := "installing " + msg.Name + " failed: " + msg.Err.Error()
			if msg.Detail != "" {
				text += " (" + msg.Detail + ")"
			}
			m.host.Notify(host.Error, text)
		} else {
			m.host.Notify(host.Info, msg.Name+" is ready — open it with the tool."+toolSlug(msg.Name)+" command")
		}
		return m, nil

	case TerminalClearMsg:
		// terminal.clear: scrollback gone, screen repainted via ctrl+l.
		if inst := m.currentTerminal(); inst != nil {
			inst.Terminal().Clear()
		}
		return m, nil

	case terminal.OutputMsg:
		// The grid changed; returning repaints. The msg is send-coalesced.
		// The completion popup (#740) recomputes here: the shell has echoed
		// the keystrokes, so the cursor row reads current.
		if t := m.terminalModelForSession(msg.Key); t != nil {
			t.OnOutput()
		}
		return m, nil

	case terminal.ExitedMsg:
		// The shell ended: close its pane like ctrl+w would; when the layout
		// refuses (last leaf), the pane stays showing [process exited]. A
		// command session (#576) stays open instead — its output is the point
		// of the run; terminal tabs (#573) stay open the same way.
		key := m.terminalPaneForSession(msg.Key)
		// Command sessions (#576) stay open — their output is the point of
		// the run. Tool panes (#741) stay open too (#810): the footer offers
		// restart-in-place (r / click) and close (ctrl+w / click), so
		// quitting the tool no longer destroys the pane's layout slot.
		if key != "" && m.activeWS().Panes.Get(key).Terminal().IsCommand() {
			return m, nil
		}
		if key != "" {
			if m.closeKey(key) {
				m.setFocus(m.focusAfterClose())
				m.syncExplorerOpen()
				m.layout()
				saveLayout(m.activeWS().Tree, m.activeWS().Panes)
			}
		}
		return m, nil

	case GoToFileMsg:
		// project.goToFile (cmd+shift+o / palette): the centered fuzzy file
		// finder, locked to the "@" mode, from any context.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, '@')
		return m, nil

	case OpenFilePathMsg:
		// file.openPath (palette / File menu, #999): the filesystem path
		// picker for files outside the workspace, locked to its mode.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, palette.OpenPathPrefix)
		return m, nil

	case palette.OpenPathDescendMsg:
		// Enter on a directory candidate (#999): re-open the picker with the
		// accepted directory as the query, so enter descends like tab.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLockedWith(palette.Context{ContextID: m.focusContext(), Root: "."}, palette.OpenPathPrefix, msg.Query)
		return m, nil

	case ShowRecentFilesMsg:
		// palette.recentFiles (cmd+e / menu): the MRU file list,
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
		inst := m.activeWS().Panes.FocusedInstance()
		if inst == nil || inst.Kind() != pane.KindEditor {
			m.host.Notify(host.Info, "paste history needs a focused editor")
			return m, nil
		}
		ed := inst.Editor()
		if ed == nil {
			m.host.Notify(host.Info, "paste history needs a focused editor")
			return m, nil
		}
		hist := ed.RegisterHistory()
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
		if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor {
			if ed := inst.Editor(); ed != nil {
				ed.PasteHistoryEntry(msg.Index)
			}
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
		// palette.searchEverywhere (cmd+shift+a / shift shift): one query
		// ranked across commands and files, locked to its mode. ActivePath
		// lets the empty-query recents listing exclude the open file (#263).
		// The workspace-symbol seat (#295) needs the bridge continuation;
		// prime it silently on the first open.
		var prime tea.Cmd
		if m.symbols.request == nil {
			if c, ok := m.reg.Command("project.goToClass"); ok {
				m.symbolPriming = true
				// Deliberately NOT dispatchCommand (#679): this is silent
				// internal priming of the workspace-symbol bridge, not a user
				// invocation of project.goToClass — the executed signal would
				// lie to observers (e.g. tick a tour task the user never did).
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

	case project.CloseWorkspaceMsg:
		// Close-from-list (#820): unload the background workspace without
		// switching — sessions terminated, memory freed; the history entry
		// stays. The active workspace cannot be closed this way, and a busy
		// one goes through the #821 confirm guard.
		if m.activeWS() != nil && m.activeWS().Root == msg.Path {
			m.host.Notify(host.Info, "cannot close the active project from the list")
			return m, nil
		}
		w := m.ws.Peek(msg.Path)
		if w == nil {
			return m, nil // already gone (evicted meanwhile)
		}
		if act := collectActivity(w); act.busy() {
			m.palette.Close() // the guard prompt owns the keyboard next
			m.openWsClosePrompt(msg.Path, act)
			return m, nil
		}
		// Idle: palette stays open, badge disappears.
		return m, m.finishWorkspaceClose(msg.Path)

	case project.RemoveFromHistoryMsg:
		// Prune-from-list (#842): the aux action of an unloaded recent
		// project deletes its history entry; the write runs off-loop.
		return m, project.RemoveFromHistoryCmd(config.Discover("."), msg.Path)

	case project.RemovedFromHistoryMsg:
		if msg.Err != nil {
			m.host.Notify(host.Warn, "could not remove project from history: "+msg.Err.Error())
			return m, nil
		}
		// Reload so the still-open picker re-lists without the entry.
		return m, config.Reload(m.cfgOpts)

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
		// Theme switch from the palette's "Theme: <name>" commands. The choice
		// is a user preference, not a project trait (#667): it writes
		// theme.name to the user scope — exactly what the Settings page does —
		// and the config reload applies the palette live everywhere.
		return m, m.selectTheme(msg.Name)

	case config.ConfigReloadedMsg:
		// Live re-theme (Roadmap 0110): publish the fresh config and re-resolve
		// the palette so a [theme].name change lands without a restart. Load
		// diagnostics — parse errors, unknown keys, clamp warnings — surface
		// as notifications, deduped per session (#793). An open palette
		// recomputes so config-backed lists (recent projects, #842) reflect
		// the reload immediately; Refresh is a no-op while closed.
		m.reloadConfig(msg.Config)
		m.notifyConfigDiags(msg.Diags)
		m.settings.NoteReloadDiags(msg.Diags) // inline in the panel too (#891)
		m.palette.Refresh()
		// Rainbow brackets (#789): a toggle flip re-parses every open editor
		// so the change lands without waiting for the next edit.
		if before := highlight.RainbowEnabled(); before != rainbowConfigured() {
			highlight.SetRainbow(!before)
			var cmds []tea.Cmd
			for _, key := range m.activeWS().Panes.Keys() {
				if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
					for _, ed := range inst.Editors() {
						cmds = append(cmds, ed.Reparse())
					}
				}
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case palette.RunCommandMsg:
		// A palette-window selection — never a keybind invocation — bumps the
		// most-used counter (#773).
		m.cmdUsage.Bump(msg.ID)
		return m, m.RunCommand(msg.ID)

	case palette.OpenFileMsg:
		switch m.diffPick {
		case 1:
			// First diff.files pick (#60): remember the left file and re-open
			// the picker for the right one.
			m.diffLeft = msg.Path
			m.diffPick = 2
			m.host.Notify(host.Info, "diff: pick the right (new) file")
			m.palette.SetSize(m.width, m.height)
			m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, '@')
			return m, nil
		case 2:
			// Second pick: both sides known, open the diff pane.
			left := m.diffLeft
			m.diffPick, m.diffLeft = 0, ""
			m.openDiffPane(left, msg.Path)
			return m, nil
		}
		return m.openPath(msg.Path, false)

	case host.OpenModalRequest:
		m.shell.SetContent(ui.ModelContent{Heading: msg.Title, Body: msg.View})
		m.shell.SetSize(m.width, m.height)
		m.shell.Open()
		return m, nil

	case editor.ActionMsg:
		// A registry command drives the focused editor through this message path.
		if key := m.activeEditorKey(); key != "" {
			cmd := m.activeWS().Panes.Get(key).Update(msg)
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
		if origin := m.activeWS().Panes.Get(msg.FromKey); origin != nil && origin.Kind() == pane.KindEditor {
			if ed := origin.EditorForPath(msg.Path); ed != nil {
				skip = ed
				msg.Dirty = ed.Dirty()
				msg.Stale = ed.Stale()
				msg.Large = ed.LargeFile()
				msg.Hash = ed.DiskHash()
				msg.EOL = textenc.LineEnding(ed.LineEnding())
				msg.Enc = textenc.Encoding(ed.EncodingName())
				msg.MixedEOL = ed.MixedEOL()
			}
		}
		var cmds []tea.Cmd
		// Crash-recovery write side (#167): the same seam drives the snapshot
		// debounce — dirty (re)arms it, clean cancels and drops the snapshot.
		if c := m.backupOnSync(msg.FromKey, msg.Path); c != nil {
			cmds = append(cmds, c)
		}
		// Idle autosave (#731) rides the same seam: dirty (re)arms the idle
		// deadline, clean cancels it.
		if c := m.autosaveIdleOnSync(msg.FromKey, msg.Path); c != nil {
			cmds = append(cmds, c)
		}
		// Deliver to every other view of the document — other panes and this
		// pane's background tabs alike; only the originating tab is skipped.
		for _, key := range m.editorKeysForPath(msg.Path) {
			if cmd := m.activeWS().Panes.Get(key).UpdateForPath(msg.Path, skip, msg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		// Markdown previews of the document re-render debounced off the same
		// seam (#62), pulling the text fresh from the originating editor.
		if previews := m.previewsForPath(msg.Path); len(previews) > 0 {
			src := skip
			if src == nil {
				if key := m.editorWithFile(msg.Path); key != "" {
					src = m.activeWS().Panes.Get(key).EditorForPath(msg.Path)
				}
			}
			if src != nil {
				text := src.Text()
				line, _ := src.CursorPos()
				for _, inst := range previews {
					if cmd := inst.Preview().SetSource(text); cmd != nil {
						cmds = append(cmds, cmd)
					}
					inst.Preview().SetCursorLine(line)
				}
			}
		}
		return m, tea.Batch(cmds...)

	case ilsp.DiagnosticInfoMsg:
		// lsp.diagnosticInfo (#739): show the caret line's diagnostics in the
		// hover popup — message, severity, source and rule code, so a false
		// positive can be judged and attributed to its server.
		if ed := m.activeEditor(); ed != nil {
			if !ed.ShowDiagnostics() {
				m.host.Notify(host.Info, "no diagnostics on this line")
			}
		}
		return m, nil

	case ilsp.DiagnosticsMsg:
		// The Problems store (#1024) keeps every published set — opened in an
		// editor or not — so the tool window aggregates project-wide.
		m.probStore.Set(msg.Path, msg.Diagnostics)
		m.refreshProblemsPanel()
		return m, m.routeToEditor(msg.Path, msg)

	case ilsp.DiagnosticsBatchMsg:
		// Coalesced diagnostics (#597): route each document's set to its editor
		// leaf in one Update pass, so a workspace publish storm re-renders once
		// instead of once per file. Unopened paths route to nothing (cheap) but
		// still land in the Problems store (#1024).
		var cmds []tea.Cmd
		for _, d := range msg.Items {
			m.probStore.Set(d.Path, d.Diagnostics)
			if cmd := m.routeToEditor(d.Path, d); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		m.refreshProblemsPanel()
		return m, tea.Batch(cmds...)
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
	case ilsp.InlayHintsMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.DefinitionMsg:
		// Navigate to a definition target and place the cursor there. Also the
		// activation msg of a references-list entry (references.go).
		return m.openPathAt(msg.Path, msg.Line, msg.Col)

	case PinSlotMsg:
		// nav.pinSlotN (#788): pin the active file to a harpoon slot.
		m.pinCurrent(msg.Slot)
		return m, nil
	case PinJumpMsg:
		return m.pinJump(msg.Slot)
	case PinPickerMsg:
		m.openPinPicker(0)
		return m, nil

	case LocalHistoryMsg:
		// file.localHistory (#1023): list the focused file's snapshots.
		m.openLocalHistoryPicker()
		return m, nil
	case localHistorySnapshotMsg:
		// A buffer save: record the written bytes into the snapshot store.
		m.recordLocalHistory(msg.path)
		return m, nil

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
		// project.goToClass (cmd+o): install the bridge
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
		_ = m.settings.Deliver(msg)
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

	case watch.TruncatedMsg:
		// The recursive watch hit its cap (#1011): tell the user external
		// changes below the unwatched remainder go unnoticed; open buffers
		// stay covered by the poll fallback.
		m.host.Notify(host.Info, fmt.Sprintf(
			"large project: file watching capped at %d directories — external changes elsewhere may go unnoticed", msg.Watched))
		return m, nil

	case watch.EventMsg:
		// External file changes (Roadmap 0140): directory events refresh the
		// explorer, file events go to the editor leaf owning the path. Every
		// event also invalidates the git status snapshot (Roadmap 0320); the
		// debounce collapses bursts into one refresh.
		vcsCmd := m.scheduleVCSRefresh()
		// On-disk changes refresh the symbol completion index (#853); repo
		// metadata and settings files are not index material.
		if m.completeEngine != nil && msg.Kind != watch.GitChanged && msg.Kind != watch.ConfigChanged {
			m.completeEngine.NotifyFileChanged(msg.Path)
		}
		if msg.Kind == watch.ConfigChanged {
			// The project settings file changed externally (0380, #795):
			// re-run the reload pipeline — theme, keymap, editor behavior
			// re-apply live, diagnostics toast via the normal path. No VCS
			// refresh: .ike is not part of the working tree view.
			return m, config.Reload(m.cfgOpts)
		}
		if msg.Kind == watch.GitChanged {
			// Repository metadata changed under .git (#738): an external commit,
			// branch switch, staging or pull — e.g. inside a lazygit tool pane.
			// Only the snapshot refresh; there is no project file to route.
			return m, vcsCmd
		}
		if msg.Kind == watch.DirChanged {
			if m.activeWS().Panes.Has(pane.ExplorerKey) {
				return m, tea.Batch(m.activeWS().Panes.Get(pane.ExplorerKey).Update(msg), vcsCmd)
			}
			return m, vcsCmd
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
					return m, vcsCmd
				}
			}
		}
		return m, tea.Batch(m.routeToEditor(msg.Path, msg), vcsCmd)

	case vcsInvalidateMsg:
		// Something changed the working tree (a buffer save, a mutating VCS
		// command): refresh the git status snapshot after the debounce.
		return m, m.scheduleVCSRefresh()

	case vcsTickMsg:
		// The git status debounce expired (Roadmap 0320): run the refresh.
		m.vcs.tickArmed = false
		return m, m.startVCSRefresh()

	case vcs.SnapshotMsg:
		return m, m.applyVCSSnapshot(msg)

	case vcs.MarksMsg:
		// Recomputed gutter diff markers (#464): to every view of the path.
		return m, m.routeToEditor(msg.Path, msg)

	case OpenCommitMsg:
		// vcs.commit (#465): the commit dialog over the current snapshot,
		// with a background refresh for fresh truth.
		if m.vcs.snap == nil {
			m.host.Notify(host.Info, "not a git repository")
			return m, nil
		}
		m.commitUI.SetSize(m.width, m.height)
		m.commitUI.Open(commitRows(m.vcs.snap))
		return m, m.scheduleVCSRefresh()

	case commitui.ToggleMsg:
		if snap := m.vcs.snap; snap != nil {
			if msg.Stage {
				return m, vcs.StageCmd(snap.Root, msg.Path)
			}
			return m, vcs.UnstageCmd(snap.Root, msg.Path)
		}
		return m, nil

	case vcspanel.ToggleMsg:
		// The tool window's staging toggles reuse the 0320 ops (#483).
		if snap := m.vcs.snap; snap != nil {
			if msg.Stage {
				return m, vcs.StageCmd(snap.Root, msg.Path)
			}
			return m, vcs.UnstageCmd(snap.Root, msg.Path)
		}
		return m, nil

	case vcspanel.SubmitMsg:
		if snap := m.vcs.snap; snap != nil {
			return m, vcs.CommitCmd(snap.Root, msg.Message)
		}
		return m, nil

	case vcspanel.HintMsg:
		m.host.Notify(host.Info, msg.Text)
		return m, nil

	case vcspanel.LogRequestMsg:
		// The panel's log window loads through the root model (#484).
		if snap := m.vcs.snap; snap != nil {
			return m, vcs.LogCmd(snap.Root, msg.Offset, msg.Limit)
		}
		return m, nil

	case vcs.LogMsg:
		if p := m.vcsPanel(); p != nil {
			p.ApplyLog(msg)
		}
		return m, nil

	case vcspanel.ShowRequestMsg:
		if snap := m.vcs.snap; snap != nil {
			return m, vcs.ShowCmd(snap.Root, msg.Hash)
		}
		return m, nil

	case vcs.ShowMsg:
		if p := m.vcsPanel(); p != nil {
			p.ApplyShow(msg)
		}
		return m, nil

	case vcspanel.OpenCommitDiffMsg:
		if snap := m.vcs.snap; snap != nil {
			return m, vcs.FileAtCmd(snap.Root, msg.Hash, msg.Path, msg.OldPath)
		}
		return m, nil

	case vcs.FileAtMsg:
		if msg.Err != nil {
			m.host.Notify(host.Error, "diff: "+msg.Err.Error())
			return m, nil
		}
		m.openCommitDiffPane(msg)
		return m, nil

	case vcspanel.OpenDiffMsg:
		// enter on a changes row (#483): the file's diff against HEAD. Rows
		// carry repo-relative paths; untracked files have no HEAD side.
		snap := m.vcs.snap
		if snap == nil {
			return m, nil
		}
		abs := filepath.Join(snap.Root, filepath.FromSlash(msg.Path))
		if snap.Status(abs) == vcs.StatusUntracked {
			m.host.Notify(host.Info, "untracked file — there is no HEAD version to diff against")
			return m, nil
		}
		return m, vcs.HeadDiffCmd(snap.Root, abs)

	case commitui.SubmitMsg:
		if snap := m.vcs.snap; snap != nil {
			return m, vcs.CommitCmd(snap.Root, msg.Message)
		}
		return m, nil

	case commitui.HintMsg:
		m.host.Notify(host.Info, msg.Text)
		return m, nil

	case vcs.OpDoneMsg:
		// A stage/unstage finished: surface failures, then let the refresh
		// re-derive the dialog rows from git's actual state.
		if msg.Err != nil {
			m.host.Notify(host.Error, msg.Op+" failed: "+msg.Err.Error())
		}
		return m, m.scheduleVCSRefresh()

	case OpenBranchPickerMsg:
		// vcs.branches (#467): fetch a fresh list, then open the picker.
		if m.vcs.snap == nil {
			m.host.Notify(host.Info, "not a git repository")
			return m, nil
		}
		return m, vcs.BranchesCmd(m.vcs.snap.Root)

	case vcs.BranchesMsg:
		if msg.Err != nil {
			m.host.Notify(host.Error, "branches: "+msg.Err.Error())
			return m, nil
		}
		m.vcs.branches = msg.Branches
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, branchesPrefix)
		return m, nil

	case CheckoutBranchMsg:
		snap := m.vcs.snap
		if snap == nil {
			return m, nil
		}
		if msg.Name == snap.Branch {
			m.host.Notify(host.Info, "already on "+msg.Name)
			return m, nil
		}
		return m, vcs.CheckoutCmd(snap.Root, msg.Name)

	case vcs.CheckoutDoneMsg:
		if msg.Err != nil {
			// Dirty-tree collisions surface as git's own "would be
			// overwritten by checkout" line.
			m.host.Notify(host.Error, "checkout failed: "+msg.Err.Error())
			return m, m.scheduleVCSRefresh()
		}
		m.host.Notify(host.Info, "switched to "+msg.Branch)
		return m, tea.Batch(m.scheduleVCSRefresh(), m.vcsPanelLogReload())

	case DiffHeadMsg:
		return m.diffAgainstHead()

	case vcs.HeadDiffMsg:
		if msg.Err != nil {
			m.host.Notify(host.Error, "diff: "+msg.Err.Error())
			return m, nil
		}
		m.openDiffHeadPane(msg.Path, msg.Head)
		return m, nil

	case ToggleBlameMsg:
		// vcs.blameLine (#468): flip the focused document's annotation and
		// fetch the blame map when it just turned on.
		ed := m.activeEditor()
		if ed == nil || !ed.HasFile() {
			m.host.Notify(host.Info, "no file to annotate")
			return m, nil
		}
		if !ed.ToggleBlame() {
			return m, nil
		}
		if m.vcs.snap == nil {
			ed.ToggleBlame() // back off: nothing to annotate outside a repo
			m.host.Notify(host.Info, "not a git repository")
			return m, nil
		}
		return m, vcs.BlameCmd(m.vcs.snap.Root, ed.Path())

	case vcs.BlameMsg:
		if msg.Err != nil {
			// Untracked files etc.: plain hint, annotation stays empty.
			m.host.Notify(host.Info, "blame: "+msg.Err.Error())
		}
		return m, m.routeToEditor(msg.Path, msg)

	case UpdateProjectMsg:
		return m.updateProject()

	case vcs.UpdateDoneMsg:
		switch {
		case msg.Err != nil:
			m.host.Notify(host.Error, "update failed: "+msg.Err.Error())
		case msg.UpToDate:
			m.host.Notify(host.Info, "already up to date")
		default:
			m.host.Notify(host.Info, "updated: "+strconv.Itoa(msg.Commits)+" commits, "+strconv.Itoa(msg.Files)+" files")
		}
		return m, tea.Batch(m.scheduleVCSRefresh(), m.vcsPanelLogReload())

	case RevertActiveFileMsg:
		return m.revertActiveFile()

	case RevertHunkMsg:
		return m.revertActiveHunk()

	case UndoRevertMsg:
		return m.openRevertHistory()

	case RestoreRevertMsg:
		return m.restoreRevert(msg)

	case vcs.RevertHunkHeadMsg:
		return m.applyRevertHunk(msg)

	case vcs.RevertInfoMsg:
		if msg.Err != nil {
			m.host.Notify(host.Error, "revert: "+msg.Err.Error())
			return m, nil
		}
		if msg.Changed == 0 {
			m.host.Notify(host.Info, "no changes to revert")
			return m, nil
		}
		m.openRevertPrompt(msg.Path, msg.Changed)
		return m, nil

	case vcs.RevertDoneMsg:
		if msg.Err != nil {
			m.host.Notify(host.Error, "revert failed: "+msg.Err.Error())
			return m, m.scheduleVCSRefresh()
		}
		m.host.Notify(host.Info, "reverted to HEAD: "+displayPath(msg.Path))
		// The open buffer reloads to the restored content, discarding any
		// unsaved edits with it — that is what the prompt confirmed.
		var reload tea.Cmd
		if ed := m.editorForPath(msg.Path); ed != nil {
			reload = ed.ResolveConflictReload()
		}
		return m, tea.Batch(reload, m.scheduleVCSRefresh())

	case vcs.CommitDoneMsg:
		if msg.Err != nil {
			// Hook failures etc. keep the dialog (and message) for a retry.
			m.host.Notify(host.Error, "commit failed: "+msg.Err.Error())
			return m, m.scheduleVCSRefresh()
		}
		m.commitUI.ClearMessage()
		m.commitUI.Close()
		m.host.Notify(host.Info, "committed "+msg.Hash+" — "+msg.Summary)
		return m, tea.Batch(m.scheduleVCSRefresh(), m.vcsPanelLogReload())

	case editor.ConflictMsg:
		// Saving a stale buffer (Roadmap 0140, #82): prompt before overwriting
		// the external change.
		m.openConflictPrompt(msg.Path)
		return m, nil

	case editor.DepEditBlockedMsg:
		// First edit to a dependency file (#565): confirm before it is unlocked.
		m.openDepEditPrompt(msg.Path)
		return m, nil

	case editor.NoticeMsg:
		// Editor action feedback ("no comment syntax for this file") → toast.
		m.host.Notify(host.Info, msg.Text)
		return m, nil

	case editor.SaveAsPromptMsg:
		// Saving an untitled buffer has no path (#730): prompt for one.
		m.startSaveAsPrompt(msg.CloseAfter)
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

	case autosaveIdleTickMsg:
		// An idle deadline elapsed: save the quiet dirty buffers and re-arm
		// while marks remain (#731).
		m.autosaveIdleTickArmed = false
		m.saveDueIdleBuffers(time.Now())
		return m, m.armAutosaveIdleTick()

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
				return m, m.dispatchCommand(res.Command, c)
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
			// Resize chords (#774) adjust the panel size — unless a page is
			// capturing keys verbatim (chord capture, text input).
			if ddw, ddh, ok := ui.ResizeDelta(msg.String()); ok && !m.settings.Capturing() {
				m.winSizes.Adjust("settings", ddw, ddh)
				w, h := m.settingsSize()
				m.settings.SetSize(w, h)
				return m, nil
			}
			return m, m.settings.Update(msg)
		}
		// An open context menu owns the keyboard (arrows/enter/esc), #1020.
		if m.ctxMenu.IsOpen() {
			return m, m.ctxMenu.Update(msg)
		}
		// An open menu dropdown owns the keyboard (arrows/enter/esc).
		if m.menu.IsOpen() {
			return m, m.menu.Update(msg)
		}
		if m.finder.IsOpen() {
			// The find-in-path overlay owns the keyboard like the palette.
			return m, m.finder.Update(msg)
		}
		if m.todo.IsOpen() {
			// The TODO index overlay owns the keyboard the same way (#61).
			return m, m.todo.Update(msg)
		}
		if m.undoTree.IsOpen() {
			// The undo-tree overlay owns the keyboard the same way (#59).
			return m, m.undoTree.Update(msg)
		}
		if m.commitUI.IsOpen() {
			// The commit dialog owns the keyboard the same way (0320, #465).
			return m, m.commitUI.Update(msg)
		}
		if m.callhier.IsOpen() {
			// The call-hierarchy overlay owns the keyboard the same way (#173).
			return m, m.callhier.Update(msg)
		}
		if m.palette.IsOpen() {
			cmd := m.palette.Update(msg)
			if !m.palette.IsOpen() && cmd == nil && m.diffPick != 0 {
				// The picker was dismissed mid diff.files flow (#60): abandon
				// the pending picks so a later "@" open is a plain file open.
				m.diffPick = 0
				m.diffLeft = ""
			}
			return m, cmd
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
		// The post-tour setup dialogs (#713) own the keyboard the same way.
		if m.themePickOpen() {
			return m.updateThemePick(msg)
		}
		if m.toolchainInfoOpen() {
			return m.updateToolchainInfo(msg)
		}
		if m.toolSetupOpen() {
			return m.updateToolSetup(msg)
		}
		// A tour suspended behind a try-it overlay (#680) resumes as soon as
		// the screen is free — this key then behaves as if the tour never
		// left (paging, closing, or the next try-it pass-through).
		m.maybeResumeTour()
		// The welcome tour (#657) pages host-level — the shell scroller must
		// never see its space/arrow keys. On a page with an unfinished try-it
		// task (#680), non-paging keys are not consumed and fall through to
		// normal key handling below.
		if m.tourOpen() {
			if tm, cmd, consumed := m.updateTour(msg); consumed {
				return tm, cmd
			}
		}
		// The save-conflict prompt owns the keyboard ahead of the generic shell
		// handling: k / r / esc answer it, everything else is swallowed.
		if m.conflictOpen() {
			return m.updateConflict(msg)
		}
		// The pinned-files picker (#788) owns the keyboard the same way.
		if m.pinPickerOpen() {
			return m.updatePinPicker(msg)
		}
		// The local-history picker (#1023) owns the keyboard the same way.
		if m.localHistoryOpen() {
			return m.updateLocalHistoryPicker(msg)
		}
		// The revert-file confirmation (0320, #466): enter / esc answer it.
		if m.revertPromptOpen() {
			return m.updateRevertPrompt(msg)
		}
		// The dependency-file edit confirmation (#565): enter / esc answer it.
		if m.depEditPromptOpen() {
			return m.updateDepEditPrompt(msg)
		}
		// The unsaved-changes guard before a project switch (0090, #3) owns the
		// keyboard the same way: s / d / esc answer it.
		if m.switchPromptOpen() {
			return m.updateSwitchPrompt(msg)
		}
		// The background-workspace eviction guard (0370 M4, #780): e / esc.
		if m.evictPromptOpen() {
			return m.updateEvictPrompt(msg)
		}
		// The busy close-from-list guard (#821): s / d / esc answer it.
		if m.wsClosePromptOpen() {
			return m.updateWsClosePrompt(msg)
		}
		// The PHP path-mapping suggestion (#832): m / esc answer it.
		if m.debugMapPromptOpen() {
			return m.updateDebugMapPrompt(msg)
		}
		// The unsaved-changes guard on a close (#259): s / d / esc answer it.
		if m.closePromptOpen() {
			return m.updateClosePrompt(msg)
		}
		// The busy-terminal close guard (#986): enter / esc answer it.
		if m.termClosePromptOpen() {
			return m.updateTermClosePrompt(msg)
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
			// key stays with the shell. cmd+v pastes the system clipboard
			// through the bracketed-paste path (#727) — under the Kitty
			// protocol the host terminal delivers cmd+v as a key event, so
			// the bracketed-paste route (#603) never fires for it.
			if k, ok := keymap.FromKeyMsg(msg); ok && k.Mods == keymap.ModMeta {
				term := m.activeWS().Panes.FocusedInstance().ActiveTerminal()
				switch {
				case k.Base == "c" && term.HasSelection():
					m.copyTerminalSelection(term)
					return m, nil
				case k.Base == "v":
					if text := clipboardRead(); text != "" {
						term.PasteText(text)
					}
					return m, nil
				}
			}
			// Global navigation chords (palette, project switch) stay with the
			// IDE (#805); everything else belongs to the shell.
			if handled, cmd := m.terminalGlobalChord(msg); handled {
				return m, cmd
			}
			return m.routeKey(msg)
		}
		// The rename prompt (#175) owns the keyboard the same way: typed
		// characters build the new name, enter applies, esc cancels.
		if m.renameOpen() {
			return m.updateRenamePrompt(msg)
		}
		// The untitled save-as prompt (#730) mirrors it.
		if m.saveAsOpen() {
			return m.updateSaveAsPrompt(msg)
		}
		// The JetBrains keymap import prompt (#677) mirrors it, plus tab
		// path completion.
		if m.jbImportPromptOpen() {
			return m.updateJBImportPrompt(msg)
		}
		// The symbol-rename prompt (0100, #6) mirrors it.
		if m.lspRenameOpen() {
			return m.updateLSPRenamePrompt(msg)
		}
		if m.shell.IsOpen() && !m.tourOpen() {
			// The tour never reaches this branch: its keys are handled (or
			// deliberately passed through, #680) above, and the shell scroller
			// must not swallow a try-it chord.
			m.shell.Update(msg)
			return m, nil
		}
		// A focused explorer with an open prompt (new-file name entry, delete/undo
		// confirmation) captures every key, ahead of the keymap and global layers,
		// so typed names and y/n answers reach the prompt intact.
		if m.explorerCapturing() {
			return m.routeKey(msg)
		}
		// While the debug panel edits a variable value it captures every key
		// (incl. enter/esc/plain letters), like an editor in insert mode (#627).
		// Routed ahead of the esc-esc detector: an esc the editor consumes to
		// cancel must not arm the double-esc palette (#640).
		if m.debugPanelEditing() {
			m.lastEsc = false
			return m.routeKey(msg)
		}
		// A running debuggee terminal embedded in the debug panel's Output
		// column takes keys raw like a terminal pane (#676): plain letters
		// must reach the debuggee's stdin, not the keymap. shift+tab leaves
		// the column (panel-side); the spatial focus moves leave the pane.
		if m.debugPanelTermCapturing() {
			m.lastEsc = false
			if dir, ok := m.focusKeys[msg.String()]; ok {
				m.FocusDir(dir)
				return m, nil
			}
			// cmd+v pastes the system clipboard into the embedded debuggee
			// terminal, mirroring the terminal-pane path (#727).
			if k, ok := keymap.FromKeyMsg(msg); ok && k.Mods == keymap.ModMeta && k.Base == "v" {
				if term := m.activeWS().Panes.FocusedInstance().Debug().Terminal(); term != nil {
					if text := clipboardRead(); text != "" {
						term.PasteText(text)
					}
				}
				return m, nil
			}
			return m.routeKey(msg)
		}
		keys := msg.String()
		if m.paletteKey != "" && keys == m.paletteKey {
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
				cmd := k.Action(m.host)
				// A binding that aliases a registered command emits the
				// command-executed signal like every other dispatch (#679).
				if k.CommandID != "" {
					cmd = tea.Batch(cmd, m.commandExecuted(k.CommandID))
				}
				return m, cmd
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
// a scratch tab — and NewPane splits off a fresh editor and loads there
// (unless the active editor is empty, which is reused in place, #641).
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
		key := m.fileEditorKey()
		// A file already open in ANY editor pane focuses that pane's tab
		// instead of opening a duplicate in the current pane (#930) — the
		// #272 same-pane dedupe extended across panes, like re-opened diffs
		// (#509). An explicit new-pane open keeps its meaning.
		if !newPane {
			if k := m.editorWithFile(path); k != "" {
				key = k
			}
		}
		// NewPane with an empty active editor reuses it instead of splitting —
		// otherwise the blank pane is stranded beside the new one, the exact
		// scenario the diff path already guards against (#628, #641).
		if key == "" || (newPane && !m.activeWS().Panes.Get(key).IsEmptyEditor()) {
			key = m.spawnEditor()
		}
		if m.openInTab(key, path) {
			m.notifyLargeFile(m.activeWS().Panes.Get(key).Editor())
			m.recent.Touch(path)  // MRU for the recent-files palette mode (0230)
			m.watcher.Track(path) // poll-fallback comparison for open buffers
			m.explorer().SetActive(path)
			m.syncExplorerOpen()
			m.setFocus(key)
			m.layout()
			saveLayout(m.activeWS().Tree, m.activeWS().Panes)
			cmds = append(cmds, m.activeWS().Panes.Get(key).Editor().Reparse())
			// Gutter diff markers for the fresh buffer (Roadmap 0320, #464).
			cmds = append(cmds, m.vcsMarksCmd(m.activeWS().Panes.Get(key).Editor()))
		}
	}
	cmds = append(cmds, m.fireHooks(plugin.EventFileOpened, path)...)
	return m, tea.Batch(cmds...)
}

// notifyLargeFile raises the one-time large-file toast (#149) for a freshly
// opened flagged document; later opens and tab switches of the same path stay
// quiet.
func (m Model) notifyLargeFile(ed *editor.Model) {
	if ed == nil || !ed.HasFile() || !ed.InsightOff() || m.largeToasted[ed.Path()] {
		return
	}
	m.largeToasted[ed.Path()] = true
	m.host.Notify(host.Warn, "large file: highlighting and language features disabled")
}

// forceCodeInsight handles editor.forceCodeInsight (#149): it lifts the
// large-file degradation for the focused document — highlighting reparses in
// every view of it, and the file-opened hook re-fires so the LSP bridge
// didOpens past its gate.
func (m Model) forceCodeInsight() (tea.Model, tea.Cmd) {
	ed := m.activeEditor()
	if ed == nil || !ed.HasFile() {
		return m, nil
	}
	if !ed.LargeFile() {
		m.host.Notify(host.Info, "code insight is already enabled for this file")
		return m, nil
	}
	path := ed.Path()
	var cmds []tea.Cmd
	if c := ed.ForceCodeInsight(); c != nil {
		cmds = append(cmds, c)
	}
	// Shared views of the document (#142) resume highlighting too.
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		for _, other := range inst.Editors() {
			if other != ed && other.HasFile() && other.Path() == path {
				if c := other.Reparse(); c != nil {
					cmds = append(cmds, c)
				}
			}
		}
	}
	cmds = append(cmds, m.fireHooks(plugin.EventFileOpened, path)...)
	m.host.Notify(host.Info, "code insight enabled for "+filepath.Base(path))
	return m, tea.Batch(cmds...)
}

// openInTab lands path in the editor pane key's tab list (#156): a tab already
// showing the file is activated; an empty scratch tab (no file, no text —
// editor.IsEmpty, the predicate shared with the diff path, #641) is filled in
// place (fresh panes keep today's behavior); otherwise a new tab is appended
// after autosaving the document being left (#174) — a pathless tab with typed
// text keeps its content that way. It reports whether the file is now open and
// active in the pane.
func (m *Model) openInTab(key, path string) bool {
	inst := m.activeWS().Panes.Get(key)
	if idx := inst.TabForPath(path); idx >= 0 {
		m.activateTab(inst, idx)
		return true
	}
	added := false
	// A terminal-hosting active tab (#573) has no document to fill: append a
	// fresh tab for the file, like a file-backed or scratch-text active tab.
	if ed := inst.Editor(); ed == nil || !ed.IsEmpty() {
		if ed != nil && m.autosaveEnabled() {
			// Leaving the active tab's document counts as leaving it (#174).
			ed.Autosave()
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
		// Surface the failure (#999): a mistyped open-path pick or a
		// vanished file otherwise fails silently.
		m.host.Notify(host.Error, "cannot open "+displayPath(path)+": "+err.Error())
		return false
	}
	if added {
		m.enforceTabLimit(inst)
	}
	return true
}

// enforceTabLimit applies the editor.tabs.limit cap (#742, the JetBrains tab
// limit) to a pane after a file open appended a tab: while the pane holds
// more document tabs than the limit, the least recently used non-dirty file
// tab closes, landing in the reopen ring (#158) so it stays restorable.
// Dirty, scratch and terminal tabs are exempt — when nothing is eligible the
// limit is exceeded rather than data risked. 0 (or negative) disables.
func (m *Model) enforceTabLimit(inst *pane.Instance) {
	limit := 0
	if c := config.Get(); c != nil {
		limit = c.Editor.Tabs.Limit
	}
	if limit <= 0 {
		return
	}
	for inst.FileTabCount() > limit {
		idx, ok := inst.EvictableLRUTab()
		if !ok {
			return
		}
		if ed := inst.TabEditor(idx); ed != nil {
			m.rememberClosedTab(ed)
			if ed.HasFile() {
				m.noteClosedFileView(ed.Path())
			}
		}
		if !inst.CloseTab(idx) {
			return
		}
	}
}

// activateTab switches pane inst to tab idx, autosaving the document being
// left — a tab switch leaves the document just like a focus switch (#174).
func (m *Model) activateTab(inst *pane.Instance, idx int) {
	if idx == inst.ActiveTab() {
		return
	}
	if ed := inst.Editor(); ed != nil && m.autosaveEnabled() {
		ed.Autosave()
	}
	inst.ActivateTab(idx)
	// Returning to a background tab counts as using its file (MRU, 0230).
	if ed := inst.Editor(); ed != nil && ed.HasFile() {
		m.recent.Touch(ed.Path())
	}
}

// activeFilePath is the focused (else most recent) editor's file, or "".
func (m Model) activeFilePath() string {
	if key := m.activeEditorKey(); key != "" {
		if ed := m.activeWS().Panes.Get(key).Editor(); ed != nil && ed.HasFile() {
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
	target := m.activeWS().Panes.Get(key).Editor()
	if target == nil {
		return fmt.Errorf("no editor tab to load %s into", path)
	}
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
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
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

// noteClosedFileView records that one editor view of path disappeared during
// this Update pass (#827); pathless views (scratch tabs) are ignored. The
// Update wrapper drains the collected paths via drainClosedFileViews.
func (m *Model) noteClosedFileView(path string) {
	if path != "" {
		m.closedFileViews = append(m.closedFileViews, path)
	}
}

// drainClosedFileViews fires EventBufferClosed for every recorded path whose
// last editor view is gone — the close-side mirror of the EventFileOpened
// dedup over shared tabs/leaves (#142). Parked workspaces count as open: the
// LSP document belongs to the file, not to one workspace's view of it.
func (m *Model) drainClosedFileViews() tea.Cmd {
	if len(m.closedFileViews) == 0 {
		return nil
	}
	paths := m.closedFileViews
	m.closedFileViews = nil
	var cmds []tea.Cmd
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		if m.pathOpenAnywhere(path) {
			continue
		}
		cmds = append(cmds, m.fireHooks(plugin.EventBufferClosed, path)...)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// pathOpenAnywhere reports whether any editor tab in any in-memory workspace
// (active or parked) still shows path.
func (m Model) pathOpenAnywhere(path string) bool {
	shows := func(w *workspace.Workspace) bool {
		if w == nil || w.Panes == nil {
			return false
		}
		for _, key := range w.Panes.Keys() {
			inst := w.Panes.Get(key)
			if inst != nil && inst.Kind() == pane.KindEditor && inst.TabForPath(path) >= 0 {
				return true
			}
		}
		return false
	}
	if shows(m.ws.Active()) {
		return true
	}
	for _, root := range m.ws.Background() {
		if shows(m.ws.Peek(root)) {
			return true
		}
	}
	return false
}

// CommandExecutedMsg is the in-app command-executed signal (#679): it is
// delivered through the normal Update loop whenever a registered command is
// dispatched (palette, keybinding, or internal invocation), carrying the
// command id. App-internal consumers (e.g. the interactive tour) observe it
// with a plain switch case — no plugin hook registration needed.
type CommandExecutedMsg struct {
	ID string
}

// commandExecuted builds the command-executed signal for a dispatched
// command id: the plugin EventCommandExecuted hooks plus the in-app
// CommandExecutedMsg. It fires at dispatch time — the command's own tea.Cmd
// may still be running — and is a cheap batch when no hook subscribes.
func (m Model) commandExecuted(id string) tea.Cmd {
	cmds := append(m.fireHooks(plugin.EventCommandExecuted, id),
		func() tea.Msg { return CommandExecutedMsg{ID: id} })
	return tea.Batch(cmds...)
}

// dispatchCommand runs a registered command and emits the executed signal.
// Every command dispatch path — palette RunCommand, keymap resolution, and
// inline invocations — funnels through it (#679), so "command X ran" is
// observable regardless of how it was triggered.
func (m Model) dispatchCommand(id string, c registry.OwnedCommand) tea.Cmd {
	return tea.Batch(c.Run(m.host), m.commandExecuted(id))
}

// RunCommand looks up and runs a registered command by id.
func (m Model) RunCommand(id string) tea.Cmd {
	if c, ok := m.reg.Command(id); ok {
		return m.dispatchCommand(id, c)
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
	r, ok := m.lay.Panes[m.activeWS().Panes.Focused()]
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
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindEditor {
		return false
	}
	// The active tab can be a terminal (#573): no editor, no normal mode (#931).
	ed := inst.Editor()
	return ed != nil && ed.ModeName() == editor.Normal
}

// focusContext reports the context id advertised by the focused pane.
func (m Model) focusContext() string {
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil {
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
// Panes without an editor tab (diff, preview, VCS — #529) keep the key.
func (m Model) quitKey() bool {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() == pane.KindExplorer {
		return true
	}
	if inst.Kind() != pane.KindEditor {
		return false
	}
	ed := inst.Editor()
	return ed != nil && ed.ModeName() == editor.Normal
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
// A diff pane counts while its edit-mode editor (#496) captures text (#529).
func (m Model) editorCapturing() bool {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return false
	}
	if inst.Kind() == pane.KindDiff {
		ed := inst.DiffEditor()
		return ed != nil && ed.Capturing()
	}
	if inst.Kind() != pane.KindEditor {
		return false
	}
	ed := inst.Editor()
	return ed != nil && ed.Capturing()
}

// explorerCapturing reports whether the focused pane is the explorer with an
// open modal prompt, in which case keys go straight to it (see Update).
func (m Model) explorerCapturing() bool {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindExplorer {
		return false
	}
	return inst.Explorer().Prompting()
}

// routeKey forwards a key to the focused pane.
func (m Model) routeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return m, nil
	}
	cmd := inst.Update(msg)
	return m, cmd
}

// activeEditorKey returns the editor that should receive a Replace open or an
// editor action: the focused editor, else the most-recent editor, else the first
// editor in tree order, else "". The key names an editor-KIND pane, not an
// editor model: the pane's active tab can be a terminal (#573, #836), in which
// case Instance.Editor() is nil — callers needing the model must nil-check
// (#931).
// fileEditorKey returns the editor pane a *file* open lands in (#998): like
// activeEditorKey, but a pane only qualifies when it actually edits files —
// an empty editor or at least one editor tab. A terminal-only tab host
// (a converted terminal pane, #983) or a dedicated terminal/tool pane never
// takes a file tab; "" makes the caller spawn a fresh editor pane.
func (m Model) fileEditorKey() string {
	editsFiles := func(inst *pane.Instance) bool {
		return inst != nil && inst.Kind() == pane.KindEditor &&
			(inst.IsEmptyEditor() || len(inst.Editors()) > 0)
	}
	if inst := m.activeWS().Panes.FocusedInstance(); editsFiles(inst) {
		return m.activeWS().Panes.Focused()
	}
	if m.recentEditor != "" && editsFiles(m.activeWS().Panes.Get(m.recentEditor)) {
		return m.recentEditor
	}
	for _, key := range m.leafOrder() {
		if editsFiles(m.activeWS().Panes.Get(key)) {
			return key
		}
	}
	return ""
}

func (m Model) activeEditorKey() string {
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor {
		return m.activeWS().Panes.Focused()
	}
	if m.recentEditor != "" {
		if inst := m.activeWS().Panes.Get(m.recentEditor); inst != nil && inst.Kind() == pane.KindEditor {
			return m.recentEditor
		}
	}
	for _, key := range m.leafOrder() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			return key
		}
	}
	return ""
}

// leafOrder returns the leaf keys in tree walk order, falling back to registry
// insertion order before the tree exists (e.g. during construction).
func (m Model) leafOrder() []string {
	if m.activeWS().Tree != nil {
		return layout.Leaves(m.activeWS().Tree)
	}
	return m.activeWS().Panes.Keys()
}

// setFocus focuses key and remembers it as the recent editor when it is one.
func (m *Model) setFocus(key string) {
	m.autosaveOnBlur(key)
	m.activeWS().Panes.SetFocused(key)
	if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
		m.recentEditor = key
		// The explorer's accent always tracks the focused editor's file, so
		// switching panes (click, focus cycling) moves the highlight with it.
		if ed := inst.Editor(); ed != nil && ed.HasFile() {
			m.explorer().SetActive(ed.Path())
		}
	}
	// The Problems pane's current-file scope tracks the same file (#1024).
	m.syncProblemsActive()
}

// autosaveOnBlur saves the editor pane focus is leaving (#174): every focus
// transition funnels through setFocus, so one hook covers Ctrl+arrows, the
// pane switcher, mouse clicks and the explorer toggle. Autosave itself skips
// clean, stale and pathless buffers.
func (m *Model) autosaveOnBlur(next string) {
	if !m.autosaveEnabled() {
		return
	}
	old := m.activeWS().Panes.Focused()
	if old == "" || old == next {
		return
	}
	if inst := m.activeWS().Panes.Get(old); inst != nil && inst.Kind() == pane.KindEditor {
		if ed := inst.Editor(); ed != nil {
			ed.Autosave()
		}
	}
}

// autosaveEnabled reads editor.auto_save live from the config ("focus" unless
// explicitly "off"), so a settings change applies without restart.
func (m *Model) autosaveEnabled() bool {
	v, ok := m.host.Config().Get("editor.auto_save")
	return !ok || v != "off"
}

// syncFocus re-asserts the registry's focus marking across all instances.
func (m *Model) syncFocus() { m.activeWS().Panes.SetFocused(m.activeWS().Panes.Focused()) }

// cycleFocus moves focus to the next leaf in tree order (tab).
func (m *Model) cycleFocus() {
	order := m.leafOrder()
	if len(order) == 0 {
		return
	}
	cur := m.activeWS().Panes.Focused()
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
	target := m.activeWS().Panes.Focused()
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	newKey := m.activeWS().Panes.AddEditor()
	m.installEmitter(newKey)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, zone)
	if !ok {
		m.activeWS().Panes.Close(newKey)
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(newKey)
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// splitView implements editor.splitViewRight/Down (#147): split the focused
// editor leaf toward zone and turn the new pane into a second live view of
// the same document (#142), with cursor and scroll copied from the source so
// both views start at the same spot; the new view keeps the focus JetBrains
// gives it. A pane without a file (scratch editor, explorer, terminal) is a
// no-op with a toast — there is no document to share.
func (m Model) splitView(zone layout.Zone) (tea.Model, tea.Cmd) {
	target := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(target)
	if inst == nil || inst.Kind() != pane.KindEditor {
		m.host.Notify(host.Info, "no file to split — open one first")
		return m, nil
	}
	src := inst.Editor()
	if src == nil || !src.HasFile() {
		m.host.Notify(host.Info, "no file to split — open one first")
		return m, nil
	}
	line, col := src.CursorPos()
	top, left := src.ScrollOffset()
	m.SplitFocused(zone)
	newKey := m.activeWS().Panes.Focused()
	if newKey == target {
		return m, nil // split failed (leaf vanished mid-flight); nothing changed
	}
	ed := m.activeWS().Panes.Get(newKey).Editor()
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
		target = m.activeWS().Panes.Focused()
	}
	newKey := m.activeWS().Panes.AddEditor()
	m.installEmitter(newKey)
	if m.activeWS().Tree == nil || target == "" {
		// Pre-layout: no tree to split yet; the default tree will adopt the key on
		// first layout only if it is the canonical first editor. Otherwise leave the
		// instance registered and let layout() build around it.
		return newKey
	}
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, m.splitZone)
	if !ok {
		// Target not in the tree (e.g. focused leaf already gone): drop the spare.
		m.activeWS().Panes.Close(newKey)
		return m.activeWS().Panes.Focused()
	}
	m.activeWS().Tree = tree
	return newKey
}

// CloseFocused closes the focused editor pane's active tab; the pane itself
// closes — collapsing its sibling up and refocusing it — only when its last
// tab goes (#156), preserving today's cmd+w feel for single-tab panes. It is
// a no-op on the explorer (a singleton) and on the last leaf, so the workspace
// never empties and context resolution never loses its explorer.
func (m *Model) CloseFocused() { m.guardedCloseFocused() }

func (m *Model) closeFocused() {
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor && inst.TabCount() > 1 {
		m.closeTab(inst, inst.ActiveTab())
		return
	}
	if m.closeKey(m.activeWS().Panes.Focused()) {
		// Focus the leaf that now occupies the closed pane's position: the first
		// leaf in walk order is a safe, always-present choice (explorer at minimum).
		m.setFocus(m.focusAfterClose())
		m.syncExplorerOpen()
		m.layout()
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
}

// closeKey removes the editor leaf named key from the layout and registry,
// reporting whether it closed one. It never closes the explorer or the last
// leaf, and leaves focus/layout/persistence to the caller (so a batch close can
// relayout once). recentEditor is repaired here since it is bookkeeping local to
// the close.
func (m *Model) closeKey(key string) bool {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() == pane.KindExplorer || m.activeWS().Tree == nil {
		return false
	}
	tree, ok := layout.Close(m.activeWS().Tree, key)
	if !ok {
		return false // last leaf: never empty the workspace
	}
	for _, ed := range inst.Editors() {
		m.rememberClosedTab(ed)
		ed.PersistUndo() // undo survives the close (#148); no-op while dirty
		if ed.HasFile() {
			m.noteClosedFileView(ed.Path())
		}
	}
	m.backupDropOnClose(inst, key)
	m.activeWS().Tree = tree
	m.activeWS().Panes.Close(key)
	if m.recentEditor == key {
		m.recentEditor = firstEditorKey(layout.Leaves(m.activeWS().Tree))
	}
	return true
}

// closeTab closes tab idx of editor pane inst, applying the same unsaved-
// changes guard as a pane close: the crash-backup snapshot is dropped unless
// another tab or pane still shows the document (#156). The caller guarantees
// the pane holds more than one tab; the pane's chrome, explorer accent and
// persisted layout follow the tab that takes over.
func (m *Model) closeTab(inst *pane.Instance, idx int) {
	if inst.TabCount() <= 1 {
		return
	}
	if ed := inst.TabEditor(idx); ed != nil {
		m.rememberClosedTab(ed)
		ed.PersistUndo() // undo survives the close (#148); no-op while dirty
		m.backupDropOnCloseTab(ed, inst.Key())
		if ed.HasFile() {
			m.noteClosedFileView(ed.Path())
		}
	}
	// A terminal tab (#573) has no document bookkeeping; CloseTab ends its
	// session.
	inst.CloseTab(idx)
	m.syncExplorerOpen()
	if next := inst.Editor(); next != nil && next.HasFile() && inst.Key() == m.activeWS().Panes.Focused() {
		m.explorer().SetActive(next.Path())
	}
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
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
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
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
		if ed := m.activeWS().Panes.Get(key).EditorForPath(path); ed != nil {
			return ed
		}
	}
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
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
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
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
		if cmd := m.activeWS().Panes.Get(key).UpdateForPath(path, nil, msg); cmd != nil {
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
		// Navigation landings frame the target near the top edge (#996);
		// every jump surface (definition, usages, nav history, goto-line,
		// CLI targets) funnels through here.
		ed.JumpTo(line, col)
	}
	return mm, cmd
}

func (m *Model) closeEditorsForPath(path string, isDir bool) {
	prefix := path + string(os.PathSeparator)
	match := func(ed *editor.Model) bool {
		if ed == nil || !ed.HasFile() {
			return false
		}
		ep := ed.Path()
		return ep == path || (isDir && strings.HasPrefix(ep, prefix))
	}
	closed := false
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
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
	if !m.activeWS().Panes.Has(m.activeWS().Panes.Focused()) {
		m.setFocus(m.focusAfterClose())
	}
	m.syncExplorerOpen()
	m.layout()
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// focusAfterClose picks the leaf to focus once the focused one is gone: the
// recent editor if it survived, else the first remaining leaf.
func (m *Model) focusAfterClose() string {
	leaves := layout.Leaves(m.activeWS().Tree)
	if m.recentEditor != "" && m.activeWS().Panes.Has(m.recentEditor) {
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
	if best := focusTarget(m.lay.Panes, m.activeWS().Panes.Focused(), dir); best != "" {
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

// popupMaxWidth reads ui.popup_max_width (#932): the outer width cap for
// centered popups on large terminals; 0 disables. Falls back to the compiled
// default when no config is loaded (tests, early startup).
func popupMaxWidth() int {
	if c := config.Get(); c != nil {
		return c.UI.PopupMaxWidth
	}
	return 110
}

// rainbowConfigured reads editor.rainbow_brackets (#789, default on).
func rainbowConfigured() bool {
	if c := config.Get(); c != nil {
		return c.Editor.RainbowBrackets
	}
	return true
}

// settingsSize bounds the floating settings panel: most of the terminal, but
// never full-screen (capped like a JetBrains dialog, ui.popup_max_width #932)
// and never overflowing.
func (m Model) settingsSize() (w, h int) {
	w = m.width - 6
	if cap := popupMaxWidth(); cap > 0 && w > cap {
		w = cap
	}
	h = m.height - 4
	if h > 32 {
		h = 32
	}
	// User resize (#774): the stored delta adjusts the computed default,
	// re-clamped to the terminal so a shrunken window stays inside.
	dw, dh := m.winSizes.Get("settings")
	if dw != 0 || dh != 0 {
		w = ui.ClampDelta(w, dw, 40, m.width-2)
		h = ui.ClampDelta(h, dh, 10, m.height-2)
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
	if m.activeWS().Tree == nil {
		m.activeWS().Tree = layout.Default(m.width, explorerWidth)
	}
	if m.zoomActive() {
		// Zoomed (#358): the one pane owns the whole body; no dividers.
		m.lay = layout.Layout{Panes: map[string]layout.Rect{m.zoomed: m.bodyRect()}}
	} else {
		m.lay = layout.Compute(m.activeWS().Tree, m.bodyRect())
	}
	for key, r := range m.lay.Panes {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		inst.SetSize(paneInterior(r.W, paneChromeW), paneInterior(r.H, paneChromeH))
		if inst.Kind() == pane.KindEditor && m.pendingScroll != nil && m.pendingScroll.key == key {
			if ed := inst.Editor(); ed != nil {
				ed.SetScroll(m.pendingScroll.top, m.pendingScroll.left)
			}
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
// floatResizeDrag tracks a live mouse resize of a floating window (#933):
// press on the window's border ring grabs an edge (sx or sy set) or corner
// (both), motion applies pointer deltas as size deltas, release persists.
type floatResizeDrag struct {
	kind         string // which float: "settings", "palette", "shell"
	sx, sy       int    // grow direction of the grabbed edge/corner (−1/0/+1)
	lastX, lastY int    // last applied pointer cell
}

// applyFloatResize applies one resize step to the dragged float. Deltas go
// through the shared size store un-persisted (Nudge); each float re-clamps
// live against the terminal bounds exactly as it does for the key resize
// (#774), so a drag can never push a window off-screen.
func (m *Model) applyFloatResize(kind string, ddw, ddh int) {
	switch kind {
	case "settings":
		m.winSizes.Nudge("settings", ddw, ddh)
		w, h := m.settingsSize()
		m.settings.SetSize(w, h)
	case "palette":
		m.palette.AdjustSize(ddw, ddh)
	case "shell":
		m.shell.AdjustSize(ddw, ddh)
	}
}

func (m Model) handleMouse(msg mouseEvent) (tea.Model, tea.Cmd) {
	// An active float resize drag (#933) owns the mouse until release: each
	// motion step applies the pointer delta along the grabbed edge/corner as a
	// size delta (motion events are already folded by the input coalescer, so
	// this runs at most once per rendered frame), release persists the store.
	if m.floatDrag != nil {
		switch msg.action {
		case mouseMotion:
			d := m.floatDrag
			ddw, ddh := (msg.X-d.lastX)*d.sx, (msg.Y-d.lastY)*d.sy
			if ddw != 0 || ddh != 0 {
				d.lastX, d.lastY = msg.X, msg.Y
				m.applyFloatResize(d.kind, ddw, ddh)
			}
			return m, nil
		case mouseRelease:
			m.floatDrag = nil
			m.winSizes.Flush()
			return m, nil
		case mousePress:
			m.floatDrag = nil // stray press: drop the drag, fall through
		}
	}
	// The context menu (#1020) is the topmost transient popup: hover follows
	// the pointer, a left press inside invokes the entry, any press outside
	// dismisses — and never leaks to the panes below.
	if m.ctxMenu.IsOpen() {
		switch msg.action {
		case mouseMotion:
			if idx, ok := m.ctxMenu.ItemAt(msg.X, msg.Y); ok {
				m.ctxMenu.Hover(idx)
			}
		case mousePress:
			if msg.Button == tea.MouseLeft {
				if idx, ok := m.ctxMenu.ItemAt(msg.X, msg.Y); ok {
					return m, m.ctxMenu.Invoke(idx)
				}
			}
			m.ctxMenu.Close()
		}
		return m, nil
	}
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
			m.finder.Wheel(-wheelLines * msg.ticks())
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelDown:
			m.finder.Wheel(wheelLines * msg.ticks())
		}
		return m, nil
	}
	if m.todo.IsOpen() {
		// The TODO index overlay hit-tests like the finder above (#61).
		if clickOutside(msg, m.todo.View(), m.width, m.height) {
			m.todo.Close()
			return m, nil
		}
		switch {
		case msg.action == mousePress && msg.Button == tea.MouseLeft:
			v := m.todo.View()
			bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
			return m, m.todo.Click(msg.X-bx, msg.Y-by)
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelUp:
			m.todo.Wheel(-wheelLines * msg.ticks())
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelDown:
			m.todo.Wheel(wheelLines * msg.ticks())
		}
		return m, nil
	}
	if m.undoTree.IsOpen() {
		// The undo-tree overlay hit-tests like the TODO index above (#59).
		if clickOutside(msg, m.undoTree.View(), m.width, m.height) {
			m.undoTree.Close()
			return m, nil
		}
		switch {
		case msg.action == mousePress && msg.Button == tea.MouseLeft:
			v := m.undoTree.View()
			bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
			return m, m.undoTree.Click(msg.X-bx, msg.Y-by)
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelUp:
			m.undoTree.Wheel(-wheelLines * msg.ticks())
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelDown:
			m.undoTree.Wheel(wheelLines * msg.ticks())
		}
		return m, nil
	}
	if m.settings.IsOpen() {
		if clickOutside(msg, m.settings.View(), m.width, m.height) {
			m.settings.Close()
			return m, nil
		}
		switch {
		case msg.action == mousePress && msg.Button == tea.MouseLeft:
			// Translate to panel-local coordinates (the box is centered).
			v := m.settings.View()
			w, h := lipgloss.Width(v), lipgloss.Height(v)
			bx, by := (m.width-w)/2, (m.height-h)/2
			// The border ring starts a mouse resize (#933); anything inside
			// is a content click.
			if sx, sy, ok := ui.ResizeZone(msg.X-bx, msg.Y-by, w, h); ok {
				m.floatDrag = &floatResizeDrag{kind: "settings", sx: sx, sy: sy, lastX: msg.X, lastY: msg.Y}
				return m, nil
			}
			return m, m.settings.Click(msg.X-bx, msg.Y-by)
		case msg.action == mouseMotion:
			// Hover affordance (#885), menu-bar parity.
			v := m.settings.View()
			bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
			m.settings.Hover(msg.X-bx, msg.Y-by)
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelUp:
			v := m.settings.View()
			bx := (m.width - lipgloss.Width(v)) / 2
			m.settings.Wheel(msg.X-bx, -wheelLines*msg.ticks())
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelDown:
			v := m.settings.View()
			bx := (m.width - lipgloss.Width(v)) / 2
			m.settings.Wheel(msg.X-bx, wheelLines*msg.ticks())
		}
		return m, nil
	}
	if m.shell.IsOpen() {
		if clickOutside(msg, m.shell.View(), m.width, m.height) {
			m.shell.Close()
			return m, nil
		}
		if msg.action == mousePress && msg.Button == tea.MouseLeft {
			v := m.shell.View()
			w, h := lipgloss.Width(v), lipgloss.Height(v)
			bx, by := (m.width-w)/2, (m.height-h)/2
			if sx, sy, ok := ui.ResizeZone(msg.X-bx, msg.Y-by, w, h); ok {
				m.floatDrag = &floatResizeDrag{kind: "shell", sx: sx, sy: sy, lastX: msg.X, lastY: msg.Y}
			}
		}
		return m, nil
	}
	if m.palette.IsOpen() {
		v := m.palette.View()
		bx, by := (m.width-lipgloss.Width(v))/2, (m.height-lipgloss.Height(v))/2
		if m.palette.Anchored() {
			bx, by = m.palette.AnchorPos()
		}
		if msg.action == mousePress {
			if !inRect(msg.X, msg.Y, bx, by, lipgloss.Width(v), lipgloss.Height(v)) {
				m.palette.Close()
				return m, nil
			}
			// A left press inside the box hits the row layout (#820): rows
			// activate, the "✕" zone runs the aux action (close workspace).
			if msg.Button == tea.MouseLeft {
				// The border ring starts a mouse resize (#933) — centered
				// palettes only; an anchored box derives its geometry from
				// its anchor and is not user-sizable.
				if !m.palette.Anchored() {
					w, h := lipgloss.Width(v), lipgloss.Height(v)
					if sx, sy, ok := ui.ResizeZone(msg.X-bx, msg.Y-by, w, h); ok {
						m.floatDrag = &floatResizeDrag{kind: "palette", sx: sx, sy: sy, lastX: msg.X, lastY: msg.Y}
						return m, nil
					}
				}
				return m, m.palette.Click(msg.X-bx, msg.Y-by)
			}
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
		// One coalesced batch arrives as a single event carrying its tick
		// count (#669); every consumer scrolls by the whole distance at once.
		lines := wheelLines * msg.ticks()
		key, ok := m.lay.PaneAt(msg.X, msg.Y)
		if !ok {
			return m, nil
		}
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			return m, nil
		}
		switch inst.Kind() {
		case pane.KindExplorer:
			switch {
			case msg.Button == tea.MouseWheelLeft:
				m.explorer().ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelRight:
				m.explorer().ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp && shift:
				m.explorer().ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelDown && shift:
				m.explorer().ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp:
				m.explorer().ScrollBy(-lines)
			case msg.Button == tea.MouseWheelDown:
				m.explorer().ScrollBy(lines)
			}
		case pane.KindMarkdown:
			// The wheel scrolls the rendered document (#62); the next cursor
			// move in the source editor re-syncs the view.
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Preview().ScrollBy(-lines)
			case tea.MouseWheelDown:
				inst.Preview().ScrollBy(lines)
			}
		case pane.KindDiff:
			// The wheel scrolls the diff by visual rows (#60).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Diff().ScrollBy(-lines)
			case tea.MouseWheelDown:
				inst.Diff().ScrollBy(lines)
			}
		case pane.KindVCS:
			// The wheel scrolls the tool window's active list (#503).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.VCS().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.VCS().Wheel(lines)
			}
		case pane.KindDebug:
			// The wheel scrolls the debug panel's focused column (#626).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Debug().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Debug().Wheel(lines)
			}
		case pane.KindProblems:
			// The wheel scrolls the Problems list (#1024).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Problems().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Problems().Wheel(lines)
			}
		case pane.KindStructure:
			// The wheel scrolls the symbol list (#1025).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Structure().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Structure().Wheel(lines)
			}
		case pane.KindTerminal:
			// The pane routes the wheel (#226): mouse-reporting children get
			// the event, alt-screen children arrow keys, a plain shell pages
			// the scrollback (#96) — up towards history, down back to live.
			if r, ok := m.lay.Panes[key]; ok {
				lx, ly := msg.X-(r.X+paneContentX), msg.Y-(r.Y+paneContentY)
				switch msg.Button {
				case tea.MouseWheelUp:
					inst.Terminal().MouseWheel(lx, ly, lines)
				case tea.MouseWheelDown:
					inst.Terminal().MouseWheel(lx, ly, -lines)
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
			// An active terminal tab (#573) routes the wheel like a terminal
			// pane: mouse-reporting children get the event, alt-screen
			// children arrow keys, a plain shell pages the scrollback.
			if term := inst.ActiveTerminal(); term != nil {
				if r, ok := m.lay.Panes[key]; ok {
					lx, ly := msg.X-(r.X+paneContentX), msg.Y-(r.Y+paneContentY)
					switch msg.Button {
					case tea.MouseWheelUp:
						term.MouseWheel(lx, ly, lines)
					case tea.MouseWheelDown:
						term.MouseWheel(lx, ly, -lines)
					}
				}
				return m, nil
			}
			// Scrolls the viewport regardless of mode (normal, insert,
			// visual, …); the cursor stays put until the user clicks or moves.
			// Horizontal wheel and shift+wheel scroll sideways (#230), like
			// the explorer.
			switch {
			case msg.Button == tea.MouseWheelLeft:
				inst.Editor().ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelRight:
				inst.Editor().ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp && shift:
				inst.Editor().ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelDown && shift:
				inst.Editor().ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp:
				inst.Editor().ScrollBy(-lines)
			case msg.Button == tea.MouseWheelDown:
				inst.Editor().ScrollBy(lines)
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
				inst := m.activeWS().Panes.Get(key)
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
						m.drag = &dragState{kind: dragTab, srcPane: key, srcTab: idx, curX: msg.X, curY: msg.Y, startX: msg.X, startY: msg.Y}
						return m, nil
					}
				}
			}
			// A click on the title band focuses the pane (#304); the drag
			// only commits once the pointer leaves the band (commitMove).
			m.setFocus(hit.Pane)
			m.drag = &dragState{kind: dragMove, srcPane: hit.Pane, curX: msg.X, curY: msg.Y, startX: msg.X, startY: msg.Y}
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
				if term := m.activeWS().Panes.Get(m.drag.srcPane).ActiveTerminal(); term != nil {
					term.MouseDrag(lx, ly)
				}
			}
		case dragEditSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil {
					if ed := inst.Editor(); ed != nil {
						ed.MouseDrag(lx, ly)
					}
				}
			}
		case dragEditScroll:
			if _, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil {
					if ed := inst.Editor(); ed != nil {
						ed.ScrollbarDrag(ly)
					}
				}
			}
		case dragExplScroll:
			// The explorer thumb follows the pointer (#1036).
			if _, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindExplorer {
					inst.Explorer().ScrollbarDrag(ly)
				}
			}
		case dragDebugTerm:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindDebug {
					inst.Debug().TermDrag(lx, ly)
				}
			}
		case dragDebugDiv:
			if lx, _, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindDebug {
					inst.Debug().ResizeSeparator(m.drag.sep, lx)
				}
			}
		}
	case mouseRelease:
		if m.drag == nil {
			return m, nil
		}
		m.drag.curX, m.drag.curY = msg.X, msg.Y
		switch m.drag.kind {
		case dragMove, dragTab:
			// A release before the drag traveled the engage threshold is a
			// plain click (#559): the press already focused the pane / tab,
			// so there is nothing to commit or persist.
			if !m.drag.engaged() {
				m.drag = nil
				return m, nil
			}
			if m.drag.kind == dragMove {
				m.commitMove(msg.X, msg.Y)
			} else {
				m.commitTabMove(msg.X, msg.Y)
			}
		case dragTermSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if term := m.activeWS().Panes.Get(m.drag.srcPane).ActiveTerminal(); term != nil {
					term.MouseRelease(lx, ly)
				}
			}
			m.drag = nil
			return m, nil // a selection drag never moved the layout
		case dragEditSelect:
			m.drag = nil
			return m, nil // the editor selection is already in place; nothing to commit
		case dragEditScroll:
			m.drag = nil
			return m, nil // the viewport already followed the thumb; nothing to commit
		case dragExplScroll:
			m.drag = nil
			return m, nil // the tree already followed the thumb; nothing to commit
		case dragDebugTerm:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindDebug {
					inst.Debug().TermRelease(lx, ly)
				}
			}
			m.drag = nil
			return m, nil // a selection drag never moved the layout
		case dragDebugDiv:
			m.drag = nil
			return m, nil // column ratios are panel-local, nothing to persist
		}
		m.drag = nil
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
	return m, nil
}

// commitMove applies a title-bar drag release: onto another pane it relocates the
// source (0036 move/swap); onto the source pane's own edge it spawns a fresh
// editor split there (0037); a drop in the source pane's interior is a no-op.
func (m *Model) commitMove(x, y int) {
	// The workspace's outermost strip docks the pane full-span against that
	// edge (#811): top/bottom → full width, left/right → full height. Checked
	// before the pane hit-test — the strip lies on the edge panes' borders.
	if zone, ok := m.dockZoneAt(x, y); ok {
		m.activeWS().Tree = layout.Dock(m.activeWS().Tree, m.drag.srcPane, zone, m.dockRatio(m.drag.srcPane, zone))
		m.layout()
		return
	}
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
		if inst := m.activeWS().Panes.Get(target); canHostTabs(inst) && (m.dragCarriesFiles(m.drag) || m.dragCarriesTerminal(m.drag)) {
			zone = layout.DropZoneWithCenter(r, x, y)
		}
		if zone == layout.ZoneCenter {
			// Center drop on a tab host merges the source pane's files into
			// the target's tab list instead of relocating the pane (#318); a
			// terminal pane moves its live session there as a terminal tab
			// (#708). A terminal/tool target converts into a tab host first
			// (#836), its running session becoming the first tab.
			if !m.ensureTabHost(target) {
				return
			}
			if m.dragCarriesTerminal(m.drag) {
				m.adoptTerminalPane(m.drag.srcPane, target)
				return
			}
			m.mergePaneTabs(m.drag.srcPane, target)
			return
		}
		m.activeWS().Tree = layout.Move(m.activeWS().Tree, m.drag.srcPane, target, zone)
		m.layout()
		return
	}
	// Dropped on the source pane: spawn a split only when near an edge.
	if zone, near := edgeZone(m.lay.Panes[target], x, y); near {
		newKey := m.activeWS().Panes.AddEditor()
		m.installEmitter(newKey)
		if tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, zone); ok {
			m.activeWS().Tree = tree
			m.setFocus(newKey)
			m.layout()
		} else {
			m.activeWS().Panes.Close(newKey)
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
	inst := m.activeWS().Panes.Get(src)
	r, rok := m.lay.Panes[src]
	if inst == nil || !rok {
		return
	}
	if tab := inst.Tab(m.drag.srcTab); tab != nil && tab.IsTerminal() {
		m.commitTerminalTabMove(x, y, inst, r)
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
		tinst := m.activeWS().Panes.Get(target)
		if tinst == nil {
			return
		}
		if !canHostTabs(tinst) {
			// A pane that cannot host tabs still accepts the file as a
			// split next to it in its edge zones (#317), mirroring the
			// self-edge drop below.
			if zone, near := edgeZone(m.lay.Panes[target], x, y); near {
				m.splitTabTo(target, zone, path, ed)
			}
			return
		}
		// A tab-hosting target shows five zones (#318): the center merges
		// the file into its tab list (a terminal/tool target converts
		// first, #836), the edges split next to it like #317.
		if zone := layout.DropZoneWithCenter(m.lay.Panes[target], x, y); zone != layout.ZoneCenter {
			m.splitTabTo(target, zone, path, ed)
			return
		}
		if !m.ensureTabHost(target) {
			return
		}
		m.openInTab(target, path)
		m.backupDropOnCloseTab(ed, src)
		m.noteClosedFileView(path) // no-op fire: the target tab still shows it
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

// commitTerminalTabMove applies a terminal tab's drag release (#707),
// mirroring commitTabMove: another editor's center zone moves the live
// session into that pane's tab list; any edge zone — an editor's, a
// non-editor pane's (#317 semantics) or the source pane's own — splits the
// session off as its own terminal pane. The shell never restarts.
func (m *Model) commitTerminalTabMove(x, y int, inst *pane.Instance, r layout.Rect) {
	src := m.drag.srcPane
	target, ok := m.lay.PaneAt(x, y)
	if !ok || (target == src && y < r.Y+layout.TitleBarRows) {
		return // dropped outside any pane, or a plain click (#304 semantics)
	}
	if target == src {
		if zone, near := edgeZone(r, x, y); near {
			m.splitTerminalTabTo(src, zone)
		}
		return
	}
	tinst := m.activeWS().Panes.Get(target)
	if tinst == nil {
		return
	}
	if !canHostTabs(tinst) {
		if zone, near := edgeZone(m.lay.Panes[target], x, y); near {
			m.splitTerminalTabTo(target, zone)
		}
		return
	}
	if zone := layout.DropZoneWithCenter(m.lay.Panes[target], x, y); zone != layout.ZoneCenter {
		m.splitTerminalTabTo(target, zone)
		return
	}
	if !m.ensureTabHost(target) {
		return
	}
	term, ok := inst.DetachTerminalTab(m.drag.srcTab)
	if !ok {
		return
	}
	tinst.AddTerminalTab(term)
	m.setFocus(target)
	m.layout()
}

// splitTerminalTabTo finishes a terminal tab's drag by splitting pane target
// at zone into a fresh terminal pane hosting the dragged tab's live session
// (#707). When the split is refused the tab is re-adopted, never dropped.
func (m *Model) splitTerminalTabTo(target string, zone layout.Zone) {
	inst := m.activeWS().Panes.Get(m.drag.srcPane)
	if inst == nil {
		return
	}
	term, ok := inst.DetachTerminalTab(m.drag.srcTab)
	if !ok {
		return
	}
	newKey := m.activeWS().Panes.AddTerminalPaneFrom(term)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, zone)
	if !ok {
		if t, ok := m.activeWS().Panes.Get(newKey).DetachTerminal(); ok {
			inst.AddTerminalTab(t)
		}
		m.activeWS().Panes.Close(newKey) // session-less after the detach: harmless
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(newKey)
	m.layout()
}

// splitTabTo finishes a tab drag by splitting pane target at zone into a fresh
// editor leaf holding path, then closing the dragged tab in the source pane.
func (m *Model) splitTabTo(target string, zone layout.Zone, path string, ed *editor.Model) {
	newKey := m.activeWS().Panes.AddEditor()
	m.installEmitter(newKey)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, zone)
	if !ok {
		m.activeWS().Panes.Close(newKey)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	m.openInTab(newKey, path)
	m.backupDropOnCloseTab(ed, m.drag.srcPane)
	m.noteClosedFileView(path) // no-op fire: the split-off leaf still shows it
	m.activeWS().Panes.Get(m.drag.srcPane).CloseTab(m.drag.srcTab)
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
	// count is the number of identical coalesced wheel ticks this event
	// stands for (#669); 0 and 1 both mean a single tick. Wheel consumers
	// multiply their per-tick line delta by it instead of the event being
	// replayed count times.
	count int
}

// ticks normalises count for consumers: a plain (unbatched) event is one tick.
func (e mouseEvent) ticks() int {
	if e.count < 1 {
		return 1
	}
	return e.count
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

// flushWheel applies the accumulated wheel batches through handleMouse in one
// update pass: each batch is delivered ONCE carrying its tick count (#669) —
// consumers multiply their line delta — instead of being replayed per event,
// which for terminal panes meant one PTY write per tick and a child working
// off the burst for seconds. A stale flush — the batch was already applied
// inline by a non-wheel message — is a no-op.
func (m Model) flushWheel() (tea.Model, tea.Cmd) {
	batches := m.pendingWheel
	m.pendingWheel = nil
	m.wheelFlushQueued = false
	var tm tea.Model = m
	var cmds []tea.Cmd
	for _, b := range batches {
		mm, ok := tm.(Model)
		if !ok {
			return tm, tea.Batch(cmds...)
		}
		ev := b.ev
		ev.count = b.count
		var cmd tea.Cmd
		tm, cmd = mm.handleMouse(ev)
		if cmd != nil {
			cmds = append(cmds, cmd)
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
	if inst := m.activeWS().Panes.Get(pane.ExplorerKey); inst != nil {
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
	inst := m.activeWS().Panes.Get(key)
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
		// A right click opens the node context menu (#1040): the row under
		// the pointer is selected first, so the menu's actions target it.
		if msg.Button == tea.MouseRight {
			if exp.ContextClick(localX, localY) {
				m.ctxMenu.Open(explorerContextItems(), msg.X, msg.Y, m.width, m.height)
			}
			return m, nil
		}
		// A left press on the scrollbar thumb starts a drag (#1036), like
		// the editor scrollbar; track presses jump inside ScrollbarPress.
		if msg.Button == tea.MouseLeft && exp.ScrollbarHit(localX, localY) {
			if exp.ScrollbarPress(localY) {
				m.drag = &dragState{kind: dragExplScroll, srcPane: key, curX: msg.X, curY: msg.Y}
			}
			return m, nil
		}
		// shift+click extends the contiguous multi-select to the clicked
		// row (#1044); a plain click below collapses it and selects.
		if msg.Button == tea.MouseLeft && msg.Mod&tea.ModShift != 0 {
			exp.ShiftClick(localX, localY)
			return m, nil
		}
		*exp, cmd = exp.MouseClick(localX, localY)
		return m, cmd
	case pane.KindEditor:
		// An active terminal tab (#573) takes the click like a terminal pane:
		// forward to a mouse-reporting child, else anchor a text selection.
		if term := inst.ActiveTerminal(); term != nil {
			if msg.Button == tea.MouseLeft {
				term.MousePress(localX, localY)
				m.drag = &dragState{kind: dragTermSelect, srcPane: key, curX: msg.X, curY: msg.Y}
			}
			return m, nil
		}
		// A right click opens the context menu at the pointer (#1020): the
		// caret moves to the clicked cell first unless the click lands inside
		// the current selection (the menu then acts on the selection).
		if msg.Button == tea.MouseRight {
			if ed := inst.Editor(); ed != nil && ed.HasFile() {
				ed.ContextClick(localX, localY)
				m.ctxMenu.Open(editorContextItems(), msg.X, msg.Y, m.width, m.height)
			}
			return m, nil
		}
		// A left press on the scrollbar column (#1022) outranks any content
		// click at that x: on the thumb it starts a drag, on the track it
		// jumps the viewport to the proportional position.
		if ed := inst.Editor(); ed != nil && msg.Button == tea.MouseLeft && ed.ScrollbarHit(localX, localY) {
			if ed.ScrollbarPress(localY) {
				m.drag = &dragState{kind: dragEditScroll, srcPane: key, curX: msg.X, curY: msg.Y}
			}
			return m, nil
		}
		// A left click in the gutter toggles a breakpoint on that line
		// (0350, #577), JetBrains-style.
		if ed := inst.Editor(); ed != nil && ed.HasFile() && msg.Button == tea.MouseLeft && msg.Mod&tea.ModAlt == 0 {
			if line, ok := ed.GutterHit(localX, localY); ok {
				m.toggleBreakpoint(ed.Path(), line)
				return m, nil
			}
		}
		// alt+click toggles a secondary caret (#145); cmd+click navigates to
		// the clicked symbol's definition (#859) — cursor first (the click
		// emits the cursor move the LSP bridge reads), then the same command
		// F4 runs, which also records nav history via the DefinitionMsg
		// funnel; a plain click moves the cursor and collapses the caret set.
		if msg.Mod&tea.ModAlt != 0 {
			inst.Editor().AltClick(localX, localY)
		} else if msg.Mod&(tea.ModSuper|tea.ModMeta) != 0 && msg.Button == tea.MouseLeft {
			ed := inst.Editor()
			ed.MouseClick(localX, localY)
			if ed.HasFile() {
				if c, ok := m.reg.Command("lsp.definition"); ok {
					return m, m.dispatchCommand("lsp.definition", c)
				}
			}
		} else {
			inst.Editor().MouseClick(localX, localY)
			// Track the press so motion events extend a selection (#977):
			// char-wise from a plain press, word-/line-wise after a
			// double/triple click.
			if msg.Button == tea.MouseLeft {
				m.drag = &dragState{kind: dragEditSelect, srcPane: key, curX: msg.X, curY: msg.Y}
			}
		}
	case pane.KindTerminal:
		// Left press: forward to a mouse-reporting child, else anchor a text
		// selection and track the drag (#227). A finished tool pane's footer
		// actions (#810) take the click first.
		if msg.Button == tea.MouseLeft {
			switch inst.Terminal().DeadActionHit(localX, localY) {
			case "restart":
				inst.Terminal().Restart()
				return m, nil
			case "close":
				if m.closeKey(key) {
					m.setFocus(m.focusAfterClose())
					m.syncExplorerOpen()
					m.layout()
					saveLayout(m.activeWS().Tree, m.activeWS().Panes)
				}
				return m, nil
			}
			inst.Terminal().MousePress(localX, localY)
			m.drag = &dragState{kind: dragTermSelect, srcPane: key, curX: msg.X, curY: msg.Y}
		}
	case pane.KindVCS:
		// Tool-window clicks (#503): tabs, row select/activate, staging
		// checkboxes; emitted messages route like the key-driven ones.
		if msg.Button == tea.MouseLeft {
			return m, inst.VCS().Click(localX, localY)
		}
	case pane.KindProblems:
		// Problems-list clicks (#1024): a click selects, a double-click on
		// the row opens the diagnostic's location, mirroring the VCS panel.
		if msg.Button == tea.MouseLeft {
			return m, inst.Problems().Click(localX, localY)
		}
	case pane.KindStructure:
		// Structure-pane clicks (#1025): a row click selects, a double-click
		// navigates; the emitted message routes like the key-driven enter.
		if msg.Button == tea.MouseLeft {
			return m, inst.Structure().Click(localX, localY)
		}
	case pane.KindDebug:
		// Debug-panel clicks (#626): select a frame/variable, double-click to
		// activate (frame select / variable expand); messages route like keys.
		// A press on the embedded debuggee terminal (#676) also tracks a
		// selection drag, like a terminal pane.
		if msg.Button == tea.MouseLeft {
			// A press on a column separator starts a resize drag (#691),
			// mirroring the layout divider gesture; it never selects a row.
			if sep := inst.Debug().SeparatorHit(localX); sep >= 0 {
				m.drag = &dragState{kind: dragDebugDiv, srcPane: key, sep: sep, curX: msg.X, curY: msg.Y}
				return m, nil
			}
			if inst.Debug().OutputTermHit(localX, localY) {
				m.drag = &dragState{kind: dragDebugTerm, srcPane: key, curX: msg.X, curY: msg.Y}
			}
			return m, inst.Debug().Click(localX, localY)
		}
	}
	return m, nil
}

// editorContextItems is the editor pane's right-click menu (#1020): the
// JetBrains staples, each referencing a registered command so availability
// and shortcuts resolve through the same InfoFunc as the menu bar (LSP
// entries render disabled while no server backs them).
func editorContextItems() []menu.Item {
	return []menu.Item{
		{Title: "Cut", Command: "editor.cut"},
		{Title: "Copy", Command: "editor.copy"},
		{Title: "Paste", Command: "editor.paste"},
		{Title: "Go to Definition", Command: "lsp.definition"},
		{Title: "Find Usages", Command: "lsp.references"},
		{Title: "Reformat File", Command: "lsp.format"},
	}
}

// explorerContextItems is the explorer node's right-click menu (#1040): the
// existing file-op commands, resolved through the same InfoFunc as the menu
// bar so availability and shortcuts stay in sync.
func explorerContextItems() []menu.Item {
	return []menu.Item{
		{Title: "New File", Command: "explorer.newFile"},
		{Title: "New Directory", Command: "explorer.newFolder"},
		{Title: "Rename", Command: "explorer.rename"},
		{Title: "Delete", Command: "explorer.delete"},
		{Title: "Refresh", Command: "explorer.refresh"},
		{Title: "Expand All", Command: "explorer.expandAll"},
		{Title: "Reveal Open File", Command: "explorer.reveal"},
	}
}

// termLocal translates a screen-cell mouse event into pane-content-local
// coordinates for the given terminal pane key.
func (m Model) termLocal(key string, msg mouseEvent) (x, y int, ok bool) {
	r, found := m.lay.Panes[key]
	if !found || m.activeWS().Panes.Get(key) == nil {
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

// clipboardRead is the matching read-side seam (#727).
var clipboardRead = func() string {
	if c := clipboard.System(); c != nil {
		if text, err := c.Read(); err == nil {
			return text
		}
	}
	return ""
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
// default, which is all we need — Update only ever dispatches KeyPressMsg.
//
// ReportEventTypes (repeat + release reporting) is deliberately left OFF: we
// ignore those events anyway, and requesting them makes a full Kitty terminal
// (e.g. Ghostty) emit a release after every key. ultraviolet's
// parseKittyKeyboardExt mis-parses the release of a CSI-`~` function key (F7/F8/
// F9…, first param is the key number, not 1) as a *second* KeyPressEvent, so a
// single F8 tap stepped the debugger twice (#622). Legacy `~` keys carry no
// event type without the flag, so leaving it off is a clean fix.
func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
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
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		return base
	}
	r, ok := m.lay.Panes[key]
	if !ok {
		return base
	}
	ed := inst.Editor()
	if ed == nil {
		return base // active tab hosts a terminal (#573): no popups
	}
	// The popups carry their own frame (#316), so they may overflow the owning
	// pane; cap their content width at the terminal instead of the pane
	// (frame + padding take 4 columns).
	ed.SetPopupMaxWidth(m.width - 4)
	top, _ := ed.ScrollOffset()
	gw := ed.GutterWidth()
	contentX := r.X + paneContentX
	contentY := r.Y + paneContentY
	place := func(view string, col, line int) string {
		// DisplayOffset (not col-left): tabs expand and inlay hints (#171)
		// inject virtual text, so the buffer column alone under-counts the
		// cells renderLine drew before the anchor.
		x := contentX + gw + ed.DisplayOffset(line, col)
		// DisplayRow (not line-top): collapsed folds and soft wrap (#64)
		// change how many screen rows sit between the scroll top and the
		// anchor line.
		y := contentY + ed.DisplayRow(line, col) + 1 // one row below the cursor
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

// renderNanos holds the wall-clock cost of the last full-frame composition. The
// input coalescer reads it to pace scroll re-injection under a render budget
// (#610), so an expensive fullscreen frame throttles fps instead of pegging a
// core.
var renderNanos atomic.Int64

// render composes the full frame as a styled string: the pane tree, the status
// line, and any floating overlay (move ghost, palette, modal shell) on top.
// The palette's background/foreground are painted behind and under the whole
// screen, regardless of the terminal's own theme, so unstyled text stays
// readable (nested styles elsewhere still win over these defaults).
func (m Model) render() string {
	if m.width == 0 {
		return "starting ike…"
	}
	start := time.Now()
	defer func() { renderNanos.Store(int64(time.Since(start))) }()
	body := ""
	if m.zoomed != "" {
		// Zoomed (#358): render only that pane; the tree survives untouched.
		body = m.renderPane(m.zoomed, m.bodyRect())
	} else {
		body = m.renderNode(m.activeWS().Tree, m.bodyRect())
	}
	rows := []string{body}
	if !m.zen {
		rows = append(rows, m.statusLine())
	}
	if m.menuEnabled() {
		rows = append([]string{m.menu.Bar()}, rows...)
	}
	// The body (renderNode) and the status/menu rows are each already exactly
	// m.width wide, so stack them by plain join instead of lipgloss measuring the
	// whole body to pad it (#612).
	base := joinV(rows...)
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
	if m.ctxMenu.IsOpen() {
		// The right-click context menu (#1020) floats at its clamped anchor.
		x, y := m.ctxMenu.Pos()
		base = overlay.Place(base, m.ctxMenu.View(), x, y, m.width, m.height)
	}
	result := base
	switch {
	case m.finder.IsOpen():
		result = overlay.Center(base, m.finder.View(), m.width, m.height)
	case m.todo.IsOpen():
		result = overlay.Center(base, m.todo.View(), m.width, m.height)
	case m.undoTree.IsOpen():
		result = overlay.Center(base, m.undoTree.View(), m.width, m.height)
	case m.commitUI.IsOpen():
		result = overlay.Center(base, m.commitUI.View(), m.width, m.height)
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
	// The palette wash paints the theme background/foreground under the whole
	// frame. The frame is already composed at exactly width x height (#612),
	// so the wash must not re-run lipgloss's Wrap/align/width-measurement over
	// the entire screen — that alone was ~52% of every frame's CPU and ~68%
	// of its allocations (#1095). Styling without Width/Height applies the
	// colours per line and skips all of that; the padded variant stays as the
	// defensive fallback for a frame that is not full-height (cheap check —
	// counting newlines, no grapheme scanning).
	wash := lipgloss.NewStyle().
		Background(m.pal().Background).
		Foreground(m.pal().Foreground)
	if lipgloss.Height(result) == m.height {
		return wash.Render(result)
	}
	return wash.Width(m.width).Height(m.height).Render(result)
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

// dockBand is the outer strip of the workspace body (in cells) that triggers
// full-span edge docking during a whole-pane move (#811): exactly the
// outermost row/column, so pane-relative drops just inside stay reachable.
const dockBand = 1

// dockZoneAt maps a whole-pane drag position onto an outer-edge dock zone
// (#811). Only dragMove docks — tab drags carry a document, not a pane.
// Corners prefer the horizontal edges (top/bottom).
func (m Model) dockZoneAt(x, y int) (layout.Zone, bool) {
	if m.drag == nil || m.drag.kind != dragMove || m.zoomActive() {
		return 0, false
	}
	b := m.bodyRect()
	inX := x >= b.X && x < b.X+b.W
	inY := y >= b.Y && y < b.Y+b.H
	switch {
	case inX && y >= b.Y && y < b.Y+dockBand:
		return layout.ZoneTop, true
	case inX && y >= b.Y+b.H-dockBand && y < b.Y+b.H:
		return layout.ZoneBottom, true
	case inY && x >= b.X && x < b.X+dockBand:
		return layout.ZoneLeft, true
	case inY && x >= b.X+b.W-dockBand && x < b.X+b.W:
		return layout.ZoneRight, true
	}
	return 0, false
}

// dockMaxShare caps the docked pane's share of the workspace along the dock
// axis. A pane usually spans (nearly) the full workspace along the axis it
// docks to — a full-height editor docked to the bottom would otherwise claim
// ~90% of the height. Docking is a tool-window gesture; a third of the
// workspace is the JetBrains-ish extent.
const dockMaxShare = 1.0 / 3.0

// dockRatio derives the docked pane's share of the workspace along the dock
// axis: its current extent when that is already modest, capped at
// dockMaxShare (layout.Dock enforces the lower bound).
func (m Model) dockRatio(key string, zone layout.Zone) float64 {
	r, ok := m.lay.Panes[key]
	b := m.bodyRect()
	if !ok || b.W <= 0 || b.H <= 0 {
		return 0.3
	}
	share := float64(r.W) / float64(b.W)
	if zone == layout.ZoneTop || zone == layout.ZoneBottom {
		share = float64(r.H) / float64(b.H)
	}
	if share > dockMaxShare {
		share = dockMaxShare
	}
	return share
}

// dockPreviewRect is the full-span rect a dock drop would occupy (#811).
func (m Model) dockPreviewRect(zone layout.Zone, ratio float64) layout.Rect {
	b := m.bodyRect()
	if ratio < 0.1 {
		ratio = 0.1
	}
	if ratio > 0.9 {
		ratio = 0.9
	}
	switch zone {
	case layout.ZoneTop:
		return layout.Rect{X: b.X, Y: b.Y, W: b.W, H: int(float64(b.H) * ratio)}
	case layout.ZoneBottom:
		h := int(float64(b.H) * ratio)
		return layout.Rect{X: b.X, Y: b.Y + b.H - h, W: b.W, H: h}
	case layout.ZoneLeft:
		return layout.Rect{X: b.X, Y: b.Y, W: int(float64(b.W) * ratio), H: b.H}
	default: // ZoneRight
		w := int(float64(b.W) * ratio)
		return layout.Rect{X: b.X + b.W - w, Y: b.Y, W: w, H: b.H}
	}
}

// moveGhost computes the preview box for an in-flight move. Onto another pane it
// previews the relocation; onto the source pane's own edge it previews the spawn;
// onto the workspace's outer strip it previews the full-span dock (#811).
func (m Model) moveGhost() (box string, x, y int, ok bool) {
	d := m.drag
	if d == nil || (d.kind != dragMove && d.kind != dragTab) || !d.engaged() {
		return "", 0, 0, false
	}
	if zone, docks := m.dockZoneAt(d.curX, d.curY); docks {
		gr := m.dockPreviewRect(zone, m.dockRatio(d.srcPane, zone))
		if gr.W < 3 || gr.H < 3 {
			return "", 0, 0, false
		}
		return ghostBox(gr.W, gr.H, m.paneLabel(d.srcPane)+" — dock "+dockName(zone), m.pal().Ghost), gr.X, gr.Y, true
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
	inst := m.activeWS().Panes.Get(key)
	isHost := canHostTabs(inst)
	if d.kind == dragTab && !isHost {
		return edgeZone(r, d.curX, d.curY)
	}
	if isHost && (m.dragCarriesFiles(d) || m.dragCarriesTerminal(d)) {
		return layout.DropZoneWithCenter(r, d.curX, d.curY), true
	}
	return layout.DropZone(r, d.curX, d.curY), true
}

// canHostTabs reports whether the pane can take a merged tab (#836): an
// editor pane natively, a terminal/tool pane after conversion. Explorer and
// the viewer/tool-window kinds stay edge-only targets.
func canHostTabs(inst *pane.Instance) bool {
	return inst != nil && (inst.Kind() == pane.KindEditor || inst.Kind() == pane.KindTerminal)
}

// ensureTabHost makes the target pane tab-hosting in place (#836): editors
// already are; a terminal/tool pane converts, its live session becoming the
// first tab. Reports whether the pane can now take tabs.
func (m *Model) ensureTabHost(key string) bool {
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return false
	}
	if inst.Kind() == pane.KindEditor {
		return true
	}
	return inst.ConvertToTabHost()
}

// dragCarriesTerminal reports whether the drag moves a whole terminal pane
// (#708): an editor target then shows the center merge zone that adopts the
// live session as a terminal tab.
func (m Model) dragCarriesTerminal(d *dragState) bool {
	if d.kind != dragMove {
		return false
	}
	inst := m.activeWS().Panes.Get(d.srcPane)
	return inst != nil && inst.Kind() == pane.KindTerminal
}

// dragCarriesFiles reports whether the drag has files an editor target could
// merge as tabs (#318): a tab drag always carries one; a whole-pane move
// carries the source editor's open files (an empty editor, an explorer or a
// terminal pane keeps the plain relocate zones).
func (m Model) dragCarriesFiles(d *dragState) bool {
	if d.kind == dragTab {
		return true
	}
	inst := m.activeWS().Panes.Get(d.srcPane)
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
	inst := m.activeWS().Panes.Get(src)
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

// adoptTerminalPane finishes a terminal pane's center drop on an editor pane
// (#708): the live shell session moves into the target's tab list as a
// terminal tab (no restart), then the vacated terminal pane closes.
func (m *Model) adoptTerminalPane(src, target string) {
	sinst, tinst := m.activeWS().Panes.Get(src), m.activeWS().Panes.Get(target)
	if sinst == nil || tinst == nil || tinst.Kind() != pane.KindEditor {
		return
	}
	term, ok := sinst.DetachTerminal()
	if !ok {
		return
	}
	tinst.AddTerminalTab(term)
	m.closeKey(src)
	m.setFocus(target)
	m.layout()
}

// tabDragLabel is the ghost/status label for a tab drag: the dragged file's
// basename.
func (m Model) tabDragLabel(d *dragState) string {
	if inst := m.activeWS().Panes.Get(d.srcPane); inst != nil {
		if tab := inst.Tab(d.srcTab); tab != nil && tab.IsTerminal() {
			return tab.Title()
		}
		if ed := inst.TabEditor(d.srcTab); ed != nil && ed.HasFile() {
			return baseName(ed.Path())
		}
	}
	return "tab"
}

// terminalPaneForSession resolves the terminal pane hosting session sess. The
// pane key usually is the session key, but a terminal tab split into its own
// pane (#707) keeps its original session key under a freshly minted pane key.
func (m Model) terminalPaneForSession(sess string) string {
	if inst := m.activeWS().Panes.Get(sess); inst != nil && inst.Kind() == pane.KindTerminal {
		return sess
	}
	for _, k := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(k); inst != nil && inst.Kind() == pane.KindTerminal && inst.Terminal().SessionKey() == sess {
			return k
		}
	}
	return ""
}

// terminalModelForSession resolves a session key to its live terminal model —
// dedicated terminal panes and editor-hosted terminal tabs (#573) alike; nil
// when the session's pane is gone.
func (m Model) terminalModelForSession(sess string) *terminal.Model {
	for _, k := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(k)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			if inst.Terminal().SessionKey() == sess {
				return inst.Terminal()
			}
		case pane.KindEditor:
			for i := 0; i < inst.TabCount(); i++ {
				if t := inst.TabTerminal(i); t != nil && t.SessionKey() == sess {
					return t
				}
			}
		}
	}
	return nil
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
// The composition uses joinH/joinV rather than lipgloss.Join* (#612): every leaf
// box is already exactly its rect's width×height (paneBox clamps it), so the
// panes can be stitched by direct line placement — no per-line StringWidth
// re-measurement, which profiling showed dominated a fullscreen scroll.
func (m Model) renderNode(n layout.Node, r layout.Rect) string {
	switch t := n.(type) {
	case *layout.Leaf:
		return m.renderPane(t.Pane, r)
	case *layout.Split:
		a, b := t.Children(r)
		if t.Orient == layout.Horizontal {
			return joinH(r.H, m.renderNode(t.A, a), m.renderNode(t.B, b))
		}
		return joinV(m.renderNode(t.A, a), m.renderNode(t.B, b))
	}
	return ""
}

// joinH stitches equal-height columns side by side by concatenating the same
// line index of each — no width measurement, since each column's lines are
// already exactly their own width. rows is the expected line count (the shared
// rect height); if any column disagrees it falls back to lipgloss, which pads
// defensively (should not happen — paneBox produces exactly rows lines).
func joinH(rows int, cols ...string) string {
	split := make([][]string, len(cols))
	for i, c := range cols {
		split[i] = strings.Split(c, "\n")
		if len(split[i]) != rows {
			return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
		}
	}
	var sb strings.Builder
	for row := 0; row < rows; row++ {
		if row > 0 {
			sb.WriteByte('\n')
		}
		for _, lines := range split {
			sb.WriteString(lines[row])
		}
	}
	return sb.String()
}

// joinV stacks blocks vertically. Each block already fills the shared width, so
// stacking is a plain newline join — no padding, no measurement.
func joinV(blocks ...string) string {
	return strings.Join(blocks, "\n")
}

// renderPane renders a single leaf at its outer rectangle, resolving its key to
// an instance for title, content, and focus state. During a move drag the source
// pane and the hovered drop target are recolored. An unknown key (no instance)
// renders an empty titled box rather than crashing.
func (m Model) renderPane(key string, r layout.Rect) string {
	inst := m.activeWS().Panes.Get(key)
	// Title (chrome) is computed without touching the content, so a cached pane
	// never calls inst.View() (#612). Content is pulled lazily inside paneBox.
	var title string
	var focused bool
	if inst == nil {
		title = strings.ToUpper(key)
	} else {
		focused = m.activeWS().Panes.Focused() == key
		switch inst.Kind() {
		case pane.KindExplorer:
			title = "EXPLORER"
		case pane.KindEditor:
			title = m.editorTitle(inst.Editor())
			if term := inst.ActiveTerminal(); term != nil {
				// The active tab hosts a terminal (#573): title it like a
				// terminal pane, from the tab's own label. A tool session
				// (#741) keeps its tool chrome (#836).
				if term.Tool() != "" {
					title = "⚙ " + strings.ToUpper(term.Tool())
				} else {
					title = "TERMINAL — " + inst.Tab(inst.ActiveTab()).Title()
				}
			}
			// The tab bar takes over the title row once the pane holds
			// multiple tabs (#157); paneBox draws it like any title.
			if bar, ok := m.tabBar(inst, r.W-paneChromeW); ok {
				title = bar
			}
		case pane.KindTerminal:
			// A tool pane (#741) is chromed as the tool, not as a terminal:
			// no shell, no directory, no OSC title, no interpreter mappings.
			if tool := inst.Terminal().Tool(); tool != "" {
				title = "⚙ " + strings.ToUpper(tool)
			} else {
				title = m.terminalTitle(inst)
			}
		case pane.KindMarkdown:
			title = "PREVIEW " + baseName(inst.Preview().Path())
		case pane.KindDiff:
			l, rr := inst.Diff().Titles()
			title = "DIFF " + l + " ⇄ " + rr
		case pane.KindVCS:
			title = "VCS"
		case pane.KindDebug:
			title = "DEBUG"
		case pane.KindProblems:
			title = "PROBLEMS"
		case pane.KindStructure:
			title = "STRUCTURE"
		}
	}

	border := m.pal().Border
	if focused {
		border = m.pal().BorderFocus
	}
	if d := m.drag; d != nil && (d.kind == dragMove || d.kind == dragTab) && d.engaged() {
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

	if inst == nil {
		return paneBox(title, "", r.W, r.H, border)
	}
	// Cache the composed box keyed by a hash of the freshly-rendered content plus
	// the chrome (#612). The content is always recomputed, so the cache is never
	// stale; it only skips the expensive lipgloss box composition (border,
	// padding, per-line width measurement) when the pane's output is identical to
	// the last frame — the common case for the panes the user is not touching.
	content := inst.View()
	sig := pane.BoxSig{
		ContentHash: hashString(content),
		Title:       title,
		W:           r.W,
		H:           r.H,
		Border:      fmt.Sprintf("%v", border),
	}
	return inst.CachedBox(sig, func() string { return paneBox(title, content, r.W, r.H, border) })
}

// hashString is a fast non-cryptographic hash (FNV-1a) used to key the pane box
// cache on rendered content without storing the whole string for comparison.
func hashString(s string) uint64 {
	const (
		offset = 1469598103934665603
		prime  = 1099511628211
	)
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

// dockName labels an outer-edge dock zone for the ghost and status hint (#811).
func dockName(z layout.Zone) string {
	switch z {
	case layout.ZoneTop:
		return "top (full width)"
	case layout.ZoneBottom:
		return "bottom (full width)"
	case layout.ZoneLeft:
		return "left (full height)"
	default:
		return "right (full height)"
	}
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

// editorTitle returns an editor pane title: file basename with a dirty marker.
func (m Model) editorTitle(ed *editor.Model) string {
	if ed == nil || !ed.HasFile() {
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
		return m.activeWS().Panes.Get(key).Editor()
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

// todoPatterns reads [todo] patterns from the flattened config (#61): the
// comma-joined tag list, empty falling back to todoindex.DefaultPatterns.
func todoPatterns(cfg host.Config) []string {
	raw, _ := cfg.Get("todo.patterns")
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// displayPath renders a file path for the status line: relative to the project
// root (the working directory) when inside it, absolute when outside.
func displayPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	cwd, err := cachedGetwd()
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
	inst := m.activeWS().Panes.Get(key)
	if inst != nil && inst.Kind() == pane.KindEditor {
		return m.editorTitle(inst.Editor())
	}
	return strings.ToUpper(strings.SplitN(key, ":", 2)[0])
}
