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
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/archview"
	"ike/internal/backup"
	"ike/internal/bookmarks"
	"ike/internal/breakpanel"
	"ike/internal/callhier"
	"ike/internal/changefeed"
	"ike/internal/clipboard"
	"ike/internal/complete"
	"ike/internal/complete/emmet"
	"ike/internal/complete/mru"
	"ike/internal/complete/postfix"
	"ike/internal/complete/symbols"
	"ike/internal/complete/words"
	"ike/internal/config"
	"ike/internal/coverage"
	"ike/internal/dataview"
	"ike/internal/debug"
	"ike/internal/debugdoctor"
	"ike/internal/debugpanel"
	"ike/internal/diag"
	"ike/internal/diff"
	"ike/internal/domview"
	"ike/internal/editor"
	"ike/internal/editor/register"
	"ike/internal/espane"
	"ike/internal/esq"
	"ike/internal/explorer"
	"ike/internal/finder"
	"ike/internal/keydoctor"
	"ike/internal/forge"
	"ike/internal/format"
	"ike/internal/ghissues"
	"ike/internal/help"
	"ike/internal/highlight"
	"ike/internal/histories"
	"ike/internal/host"
	"ike/internal/httppane"
	"ike/internal/idcolor"
	"ike/internal/jqplay"
	"ike/internal/keymap"
	"ike/internal/lang"
	"ike/internal/largefile"
	"ike/internal/layout"
	"ike/internal/localhistory"
	ilsp "ike/internal/lsp"
	"ike/internal/market"
	"ike/internal/marks"
	"ike/internal/menu"
	"ike/internal/nav"
	"ike/internal/numhint"
	"ike/internal/openapi"
	"ike/internal/overlay"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/perfhud"
	"ike/internal/plugin"
	"ike/internal/preview"
	"ike/internal/problems"
	"ike/internal/project"
	"ike/internal/regextest"
	"ike/internal/registry"
	"ike/internal/remote"
	"ike/internal/search"
	"ike/internal/secret"
	"ike/internal/settings"
	"ike/internal/snippets"
	"ike/internal/structpanel"
	"ike/internal/terminal"
	"ike/internal/testresults"
	"ike/internal/textenc"
	"ike/internal/theme"
	"ike/internal/todoindex"
	"ike/internal/toolcatalog"
	"ike/internal/tour"
	"ike/internal/typehier"
	"ike/internal/ui"
	"ike/internal/undotree"
	"ike/internal/unidiff"
	"ike/internal/usages"
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
	// dbgTermKey is the debuggee terminal pane's key (#1370), "" while none
	// is open. It outlives the session (the output stays reviewable) and is
	// reused by the next launch when the pane is still open.
	dbgTermKey string
	navSkip    bool
	// ws manages the active workspace (Roadmap 0370, #776): the pane registry,
	// the split tree and the terminal return-focus live behind it so a project
	// switch can later swap the whole unit atomically. Focus is the registry's
	// focused key, which always names a layout leaf.
	ws *workspace.Manager
	// recentEditor is the key of the most-recently-focused editor, used as the
	// Replace open-target when the explorer (not an editor) holds focus.
	recentEditor string
	// viewerTabHost names the pane a handler-dispatched viewer open should land
	// in as a content tab (#1825) instead of splitting off viewerSplitTarget
	// (#1779). Set by the palette (focused pane) and the explorer's default
	// open (the last-focused editor, #1851) right before the file handler's
	// command is queued and consumed by the first viewer open that follows
	// (takeViewerTabHost); "" restores the split behaviour.
	viewerTabHost string
	// httpFlight tracks the .http requests currently in flight (#1272), keyed
	// by source file + request key: the duplicate-dispatch guard, the
	// statusline indicator and the cancel action all read it.
	httpFlight map[string]*httpFlightEntry
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
	// logsetToasted remembers which log paths already offered their rotated
	// set as a merged timeline (#1996), so re-opening the tab stays quiet.
	// A map for the same reason largeToasted is one.
	logsetToasted map[string]bool
	host          *host.Host
	reg           *registry.Registry
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
	// termDark is the terminal background's light/dark classification from
	// the OSC 11 reply (#1480); nil until (and unless) the terminal answers.
	// Feeds resolveTheme's [theme].auto pair selection.
	termDark *bool
	// Kitty graphics support (#1479): nil until the probe is acknowledged;
	// gfxQueried arms the probe once, when the first image pane opens.
	// liveImages tracks the image ids currently transmitted to the terminal
	// (shared by pointer across the value-model copies, like toolchainSeg)
	// so the per-pass reconcile can delete what no visible pane owns.
	kittyGfx   *bool
	gfxQueried bool
	liveImages map[int]bool
	// toolchainSeg caches the status line's toolchain label per language ID
	// (#101): resolving an interpreter stats the filesystem and scans PATH, too
	// costly per frame. Shared by pointer across the value-model copies (like
	// largeToasted); its keys are dropped on config reload.
	toolchainSeg map[string]string
	// vcs is the git status state (Roadmap 0320): the latest snapshot plus
	// refresh scheduling, shared by pointer across value-model copies. A nil
	// snapshot means "not a git repository".
	vcs *vcsState
	// forgePoll is the background forge poll service (#2085): one poller per
	// workspace root, shared by pointer across value-model copies like vcs.
	// Nil in bare test models — every accessor is nil-safe.
	forgePoll *forgePollState
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
	// shell is the base floating overlay (Roadmap 0035); floats is the
	// z-ordered stack of floating panes it lives in (#1237) — shell is its
	// persistent bottom layer, extra dialogs are pushed on top and the
	// topmost open layer owns key input and compositing order.
	shell  *ui.Floating
	floats *ui.Stack
	// popup is the popup terminal (#1398): a floating tab-host terminal
	// overlay outside the layout tree, toggled by terminal.popup.
	popup popupTerm
	// floatTerms are the torn-out floating terminal panels (#1793), z-ordered
	// bottom to top; floatFocus is the keyboard-owning panel (nil: the popup
	// box owns the layer's keys). Global panels ride across project switches;
	// project-owned ones park in wsExtras with the popup box (#1407).
	floatTerms []*floatTerm
	floatFocus *floatTerm
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

	// Follow mode (#1928, follow.go): one demand-armed tick drives the
	// watcher's poll fallback while at least one editor view follows its
	// file; it self-stops (no re-arm) once none does, so an idle session
	// never pays for it.
	followTickArmed bool

	// Performance HUD (#1999, perfhud.go): the sampling tick, armed only
	// while the HUD is on. The HUD's visibility itself lives in the
	// perfhud collector, not here — the model is rebuilt on a project
	// switch and the in-flight tick has to find the HUD still open.
	// perfBox caches the rendered box (perfBoxW: the width it was laid out
	// for): its content only changes once per sample, so composing it per
	// frame would be exactly the kind of waste the HUD exists to find.
	perfTickArmed bool
	perfBox       string
	perfBoxW      int

	// Idle autosave (#731): same debouncer shape as backup, but the tick
	// saves the quiet dirty buffers instead of snapshotting them.
	autosaveIdleDeb       *backup.Debouncer
	autosaveIdleTickArmed bool

	// hoverIdle tracks the pointer's resting cell for the mouse-idle hover
	// (#1129); hoverIdleTickArmed keeps at most one demand-armed tick in
	// flight (#1001). See hover_idle.go.
	hoverIdle          mouseHoverState
	hoverIdleTickArmed bool
	autosaveIdleIv     time.Duration
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

	// cloneOpen marks the clone-repository dialog (#1349) while the shell
	// shows it; cloneURL/cloneName are the two inputs with their cursors,
	// cloneField the focused one, cloneNameEdited stops the name from
	// following the URL once it was typed by hand, cloneRunning freezes the
	// fields while the git subprocess runs, and cloneErr is the validation or
	// git message shown under the fields.
	cloneOpen       bool
	cloneURL        string
	cloneURLPos     int
	cloneName       string
	cloneNamePos    int
	cloneField      int
	cloneNameEdited bool
	cloneRunning    bool
	cloneErr        string

	// newProj is the open new-project wizard (#1718); nil when it is closed.
	newProj *newProjState

	// tdGen is the open test-data wizard (#2134); nil when it is closed.
	tdGen *tdGenState

	// csvProfile is the open csv column profile (#1940); nil when it is
	// closed. The data viewer's profile lives in its own pane, not here.
	csvProfile *csvProfileContent

	// regexTester is the open regex tester (#1937); nil when it is closed.
	// regexHistory outlives the dialog so reopening still offers the
	// patterns tried earlier in this session.
	regexTester  *regexTesterState
	regexHistory regextest.History

	// play is the open jq playground (#1936), inline in its hosting pane
	// since #1970; nil when it is closed. playHistory outlives the mode for
	// the same reason regexHistory does, and is a *shared pointer* (#1977):
	// every open playground writes its programs straight into this one
	// session-wide list, so the history is the same whichever buffer or
	// response pane the mode was opened over — and no exit path can drop
	// entries by failing to copy them back.
	// playLastProgram remembers, per queried input (file path, unsaved buffer,
	// response pane), the last program that ran against it without an error
	// (#1982). The ordinary open prefills it instead of `.`, so reopening a
	// file resumes the look that was interrupted — something the one shared,
	// buffer-agnostic history cannot express. In memory for the session, like
	// the history itself.
	play            *playState
	playHistory     *jqplay.History
	playLastProgram map[string]string
	// playFilters is the palette mode listing the named saved filters of both
	// scopes (#1995), kept on the model so the insert and rename entry
	// commands can flip its action before opening it locked; playName is the
	// shell prompt that names a filter on save and on rename. The libraries
	// themselves are on disk, re-read per open — nothing to cache here.
	playFilters *playFiltersMode
	playName    playNamePrompt

	renamePos int
	// layoutSaveOpen marks the window.saveLayout name prompt (#1175) while the
	// shell shows it; input/pos are the typed name and cursor, err the
	// overwrite-confirmation hint. layoutsPicker is the palette mode listing
	// saved layouts, kept on the model so the two entry commands can flip its
	// apply/set-default action before opening it locked.
	layoutSaveOpen  bool
	layoutSaveInput string
	layoutSavePos   int
	layoutSaveErr   string
	layoutsPicker   *layoutsMode
	// httpRequests is the palette mode listing the .http requests that have
	// stored responses (#1829); the response pane's "r" fills and opens it.
	httpRequests *httpRequestsMode
	// httpEntries is the palette mode listing the stored responses of one
	// request (#1992); the response pane's "D" fills and opens it to pick the
	// second side of the diff.
	httpEntries *httpEntriesMode
	// httpEnvs is the palette mode listing the environments of an
	// http-client.env.json (#1867); httpEnv persists the chosen one per
	// directory.
	httpEnvs *httpEnvMode
	httpEnv  *httpEnvStore
	// runConfigs is the palette mode listing run/debug configurations
	// (#1914); run.select fills and opens it.
	runConfigs *runConfigsMode
	// tasks is the palette mode listing discovered build-tool tasks (#1915);
	// run.task / run.taskPromote fill and open it.
	tasks *tasksMode
	// ssh is the palette mode listing the ssh_config host aliases (#1938);
	// terminal.ssh fills and opens it.
	ssh *sshMode
	// remote is the SFTP browse host picker mode (#1997), the ssh list with a
	// browse pick.
	remote *remoteMode
	// watchExprs are the debugger watch expressions (#1914): in memory,
	// surviving debug sessions; re-evaluated on every stop.
	watchExprs []string
	// layoutSelect is the open pane-selection mini-map preceding the name
	// prompt (#1568); layoutSaveSel carries its confirmed selection into the
	// prompt's save (nil = full snapshot).
	layoutSelect  *layoutSelectState
	layoutSaveSel map[string]bool
	// movePending is the file whose move target the palette's directory picker
	// is currently asking for (file.move, #175); "" when no move is pending.
	movePending string
	// jbImportOpen marks the JetBrains keymap import prompt (#677) while the
	// shell shows it; jbImportInput/jbImportPos are the typed path and cursor.
	jbImportOpen  bool
	jbImportInput string
	jbImportPos   int
	// oapiImportOpen marks the OpenAPI import prompt (#1939) while the shell
	// shows it; oapiImportInput/oapiImportPos are the typed path and cursor.
	oapiImportOpen  bool
	oapiImportInput string
	oapiImportPos   int
	// oapiCheck* hold the URL validation of that prompt (#2009): the
	// sequence number of the newest check (older answers are stale),
	// whether one is in flight, the last error message, and the resolved
	// document a confirm imports from.
	oapiCheckSeq  int
	oapiChecking  bool
	oapiCheckErr  string
	oapiCheckDisc *openapi.Discovery
	// curlImportOpen marks the curl import prompt (#1994) while the shell
	// shows it; curlImportInput/curlImportPos are the typed command and
	// cursor.
	curlImportOpen  bool
	curlImportInput string
	curlImportPos   int
	// httpSaveOpen marks the response-body save prompt (#2059) while the
	// shell shows it; httpSaveInput/httpSavePos are the typed path and cursor.
	httpSaveOpen  bool
	httpSaveInput string
	httpSavePos   int
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
	// breakpointsReturnFocus is the same dance for the Breakpoints tool
	// window (#1377).
	breakpointsReturnFocus string
	// testsReturnFocus is the same dance for the Test Results tool window
	// (#1911); lastTestRun remembers the last captured test run for the
	// re-run actions and testRunSeq drops a stale run's completion.
	testsReturnFocus string
	// issuesReturnFocus is the same dance for the GitHub Issues tool window
	// (#1934).
	issuesReturnFocus string
	// The prominent forge event surface (#2086, forgenotify.go): forgeQueue
	// are the events the single open dialog shows (forgeCursor selects one),
	// forgeUnread are the events behind the status line's persistent badge —
	// badge-style ones plus the dialogs the typing guard held back.
	// forgeReveal is an issue number waiting for the next issues fetch to
	// jump to, and lastInputAt is the guard's "user is typing" stamp.
	forgeQueue  []forge.Event
	forgeUnread []forge.Event

	// Forge edit buffers (#2087, forgeedit.go): markdown scratch buffers
	// bound to a forge text, keyed by path; forgeEditKey names the buffer the
	// open push dialog belongs to, forgeEditConflict tells the stale-base
	// warning from the failed-push error, and forgeEditStale carries the
	// forge's current text the reload answer writes back.
	forgeEdits        map[string]*forgeEdit
	forgeEditKey      string
	forgeEditConflict bool
	forgeEditStale    string
	forgeCursor       int
	forgeDialog       bool
	forgeReveal       int
	lastInputAt       time.Time
	// doctorReturnFocus is the same dance for the Xdebug Doctor tool window
	// (#1991); doctorLog is its app-owned listener/connection trace, fed from
	// the bridge's ike.listenState / ike.debugConn events and surviving the
	// panel being closed.
	doctorReturnFocus string
	doctorLog         *debugdoctor.Log
	// domReturnFocus is the same dance for the DOM inspector tool window
	// (#1929). domReqPath/domReqVersion dedup the async buffer parses;
	// domHLPath/domHLRev remember which file's editors carry the selector
	// match highlights and at which pane match revision.
	domReturnFocus string
	domReqPath     string
	domReqVersion  int
	domHLPath      string
	domHLRev       int
	lastTestRun    *testRunState
	testRunSeq     int
	// coverage is the last coverage run's per-file line marks (#2081), fed by
	// finishTestRun through the language's ParseCover seam and pushed to
	// editors as gutter marks; coverageShown is the coverage.toggle display
	// state (data survives a hide, it just stops being pushed/rendered).
	coverage      *coverage.Store
	coverageShown bool
	// rawDiags caches each path's last published, unfiltered diagnostic set;
	// diagIgnore/diagIgnoreRaw are the compiled lsp.diagnostics_ignore rules
	// and their source strings (#1259). Publishes filter through the rules
	// before reaching probStore or an editor; a rule edit re-filters the cache
	// live (diag_ignore.go).
	rawDiags      map[string][]ilsp.Diagnostic
	diagIgnore    ilsp.IgnoreRules
	diagIgnoreRaw []string
	// diagSeverity/diagSeverityRaw are the compiled lsp.diagnostics_severity
	// remap rules (#1503), applied after the ignore filter on the same path.
	diagSeverity    ilsp.SeverityRules
	diagSeverityRaw []string
	// structReturnFocus is the same dance for the Structure tool window
	// (#1025); structReqPath is the last path a documentSymbol refresh was
	// issued for (the request dedup), and structForce marks a save-triggered
	// refresh that must re-request the unchanged path.
	structReturnFocus string
	structReqPath     string
	structForce       bool
	// docSymbols caches each file's hierarchical documentSymbol tree (#1153):
	// the breadcrumbs bar derives the cursor's enclosing chain from it at
	// render time (the Structure pane keeps its own flattened rows). Fed by
	// applyDocumentSymbols, evicted when the file's last view closes.
	// crumbSig is the last applied breadcrumb geometry signature; the settled
	// pass (syncBreadcrumbLayout) re-runs layout() when it changes.
	docSymbols map[string][]ilsp.SymbolNode
	crumbSig   string
	// usagesReturnFocus is the same dance for the Usages tool window (#1155).
	usagesReturnFocus string
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
	// projectClosePending is the busy close-current guard state (#1355): the
	// MRU background root to resume once the user confirms the close.
	projectClosePending *pendingProjectClose
	// peek marks the active workspace as a quick-peek (#2136): the origin
	// root project.peek.return goes back to, plus the state snapshot the
	// return's unchanged check compares against. Nil while not peeking; a
	// peek never survives the process (restart restores the origin normally).
	peek *peekState
	// peekReturnPending is the busy peek-return guard state (#2136): the
	// activity the drop would kill, awaiting the user's answer.
	peekReturnPending *pendingPeekReturn

	// closePending is the close request awaiting the unsaved-changes guard
	// (#259); nil when no guard is open.
	closePending *pendingClose
	// mergeClosePending is the merge-view key awaiting the unresolved-
	// conflicts close guard (#1478); "" when no guard is open.
	mergeClosePending string
	// termClosePending is true while the busy-terminal close guard (#986)
	// owns the keyboard.
	termClosePending bool
	// termClosePopup targets the open guard at the popup terminal's active
	// tab (#1398) instead of the focused pane.
	termClosePopup bool
	// termCloseSess pins the guard to the session it was raised for (#1786):
	// the busy shell can exit on its own while the prompt is open — its exit
	// closes the tab/pane and shifts what "active"/"focused" points at — so
	// the confirm re-resolves this key and closes nothing when it is gone.
	termCloseSess string

	// explorerRatio remembers the hidden explorer's split ratio so
	// explorer.toggle restores the tree at its prior width (#268); 0 means
	// "use the default width".
	explorerRatio float64
	// callhier is the call-hierarchy tree overlay (lsp.callHierarchy, #173).
	callhier *callhier.Model
	// typehier is the type-hierarchy tree overlay (lsp.typeHierarchy, #1454).
	typehier *typehier.Model
	// finder is the find-in-path overlay (Roadmap 0150); searcher is the
	// streaming scan service it drives.
	finder   *finder.Model
	searcher *search.Service
	// keyDoctor is the in-app keymap doctor overlay (#2080): the terminal
	// reality probe run inside the session, ahead of keymap resolution.
	keyDoctor *keydoctor.Model
	// todo is the TODO/FIXME index overlay (#61); todoSearch is its own scan
	// service — separate from searcher so the index and the finder never cancel
	// each other, with results wrapped in todoindex.ScanMsg so the finder can
	// never mistake them for its own generations.
	todo       *todoindex.Model
	todoSearch *search.Service
	// undoTree is the undo-tree overlay (#59): the focused editor's change
	// tree; jumps route back into that editor as HistoryJumpMsg.
	undoTree *undotree.Model
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
	fileUsage   *palette.Usage       // most-used file ranking in the ranked palettes (#1419)
	winSizes    *ui.WinSizes         // persisted floating-window resize deltas (#774)
	winSizesAll *ui.WinSizes         // user-scoped last-resize deltas, fallback for fresh projects (#1714)
	floatDrag   *floatResizeDrag     // live mouse resize of a floating window (#933)
	floatMove   *floatMoveDrag       // live titlebar move of a floating terminal (#1793)
	pins        *pinStore            // harpoon-style pinned file slots (#788)
	toolHide    *toolHideSnapshot    // hide-all-tool-windows snapshot (#791)
	termShiftAt time.Time            // last bare-shift tap in a terminal (#973)
	pinSel      int                  // pin-picker selection
	pinPicker   bool                 // pin picker owns the modal shell
	lhStore     *localhistory.Store  // local-history snapshot store (#1023)
	lhSel       int                  // local-history panel selection
	lhPicker    bool                 // local-history panel owns the modal shell
	lhPath      string               // file the open panel lists
	lhEntries   []localhistory.Entry // its snapshots, newest-first
	lhCur       string               // buffer text the open panel diffs against
	lhDiff      diff.Result          // selected snapshot vs lhCur, for the inline diff pane
	lhErr       string               // selection's snapshot load error, shown in place of the diff

	// feed is the session-scoped record of files changed by something other
	// than IKE (#2000) — a coding agent, a git checkout, a formatter run in a
	// tool pane. Recorded off every watcher file event (own saves already
	// suppressed), reviewed in the change-feed panel; the cf* fields are that
	// panel's open state, which is why the feed itself survives pane switches.
	feed      *changefeed.Feed
	cfEntries []changefeed.Entry // the open panel's snapshot of the feed
	cfSel     int                // change-feed panel selection
	cfPicker  bool               // change-feed panel owns the modal shell
	cfDiff    diff.Result        // selected entry's before vs now, for the mini-diff
	cfErr     string             // why the selection has no diff, shown in its place
	cfRevert  string             // file awaiting the revert confirmation

	tl       timelineState // per-file Timeline data (#1916)
	tlPicker bool          // the Timeline owns the modal shell

	histResult vcs.RangeLogMsg // range-history picker data (#1430)
	histLabel  string          // human range label ("lines 4–9")
	histSel    int             // selected commit row
	histPatch  bool            // patch view (vs commit list)
	histPicker bool            // range-history picker owns the modal shell
	paletteKey string
	// themePal is the resolved color scheme (Roadmap 0110): [theme].name mapped
	// to a theme.Palette. Chrome renders from its ui slots; panes get it threaded
	// at construction and on config reloads.
	themePal *theme.Palette
	// lastEscAt records when the previous key was an esc in a non-capturing
	// context, so a second esc within escEscTimeout opens the palette
	// (esc-esc toggle). A zero value means no esc is armed. Time-bounded
	// (#1750) so a forgotten esc from long ago doesn't arm a palette-open on
	// an unrelated later esc; the window distinguishes deliberate double-tap
	// from coincidence.
	lastEscAt time.Time
	// nowFn overrides the clock the esc-esc detector reads from; tests only.
	// Nil means the wall clock.
	nowFn func() time.Time
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
	// whichKeyGen counts pending-sequence changes (#1909): a delay timer only
	// opens the popup while its generation is still current.
	whichKeyGen int
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
	// bookmarks is the palette mode over the vim marks (#1151), same
	// pattern as refs; gmarks is the persistent global-mark store behind
	// the m{A-Z} marks, injected into every editor like bpts.
	bookmarks *bookmarksMode
	gmarks    *marks.Store
	// bmarks is the project bookmark store (#55): JetBrains-style line
	// bookmarks with an optional mnemonic and note, persisted per project
	// like bpts. bmPrompt is the open mnemonic/note prompt, nil when none.
	bmarks   *bookmarks.Store
	bmPrompt *bookmarkPrompt
	// qhist is the persistent query-history store (#1171): named recall
	// buckets for the editor's search/ex lines and find-in-path, shared by
	// every editor and the finder overlay.
	qhist *histories.Store
	// regs is the app-wide register store (#1540): one ring of yanks/deletes
	// and one set of named registers for every editor in every workspace,
	// vim style. Owned by the workspace manager so a project switch (which
	// rebuilds the model) keeps it.
	regs *register.Store
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

// escEscTimeout bounds the esc-esc palette toggle (#1750): the two presses
// must land within this window to count as a deliberate double-tap, not a
// forgotten first esc armed from an unrelated moment earlier.
const escEscTimeout = 350 * time.Millisecond

// clock returns the injectable "now" source for time-bounded key detectors
// (e.g. esc-esc), defaulting to the wall clock. Tests set m.nowFn to control
// it without time.Sleep.
func (m *Model) clock() time.Time {
	if m.nowFn != nil {
		return m.nowFn()
	}
	return time.Now()
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
	dragResize      dragKind = iota // dragging a divider to change a split ratio
	dragMove                        // dragging a pane title bar to relocate or spawn
	dragTab                         // dragging one tab label to move just that file (#305)
	dragTermSelect                  // dragging a text selection inside a terminal pane (#227)
	dragEditSelect                  // dragging a text selection inside an editor pane (#977)
	dragEditScroll                  // dragging the editor scrollbar thumb (#1022)
	dragExplScroll                  // dragging the explorer scrollbar thumb (#1036)
	dragScratchDiv                  // dragging the explorer's Scratches divider (#1963)
	dragDebugDiv                    // dragging a column separator inside the debug panel (#691)
	dragHTTPSelect                  // dragging a text selection in the HTTP response pane (#1266)
	dragHTTPScroll                  // dragging the HTTP response scrollbar thumb (#1367)
	dragTermScroll                  // dragging the terminal scrollback scrollbar thumb (#1368)
	dragDiffSelect                  // dragging a text selection in a diff pane (#2070)
	dragMergeSelect                 // dragging a text selection in a merge view's side column (#2070)
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
	// srcInst marks a popup-layer tab drag (#1793): the box the tab is torn
	// from — a popup split side or a floating panel host. Layout-pane tab
	// drags leave it nil and resolve srcPane against the registry as before.
	srcInst *pane.Instance
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
	terminal.SetDefaultScrollbackLines(cfg.Terminal.ScrollbackLines)
	// Arm the update-loop stall watchdog (#2163) as soon as the threshold is
	// known — a freeze during startup should leave evidence too.
	configureWatchdog(cfg)
	m := NewWith(registry.Global(), host.FromConfig(cfg))
	m.notifyConfigDiags(append(append(diags, associationDiags()...), unitMappingDiags()...))
	m.notifyKeymapDiags()
	return m
}

// unitMappingDiags reports every editor.number_hint_units entry the install
// skipped (#2008): a malformed line or an unknown unit word gates nothing, so
// the field keeps rendering in the built-in base — a `request_timeout` still
// read in milliseconds — while the mapping looks like it is in force. Same
// rule as associationDiags: an inert entry must be visible.
func unitMappingDiags() []config.Diagnostic {
	var out []config.Diagnostic
	for _, e := range numhint.InvalidEntries() {
		out = append(out, config.Diagnostic{
			Field:   "editor.number_hint_units",
			Message: strconv.Quote(e.Entry) + ": " + e.Reason + " — ignored",
		})
	}
	return out
}

// associationDiags reports every [files.associations] entry whose target
// names no registered language (#1365): the association is silently inert —
// the file falls back to built-in detection or plain text — so the broken
// entry must be visible like any other config problem.
func associationDiags() []config.Diagnostic {
	var out []config.Diagnostic
	for _, a := range lang.InvalidAssociations() {
		out = append(out, config.Diagnostic{
			Field:   "files.associations." + a.Pattern,
			Message: "unknown language id " + strconv.Quote(a.Lang) + " — file falls back to plain text",
		})
	}
	return out
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

// notifyKeymapDiags surfaces the binding table's build diagnostics — conflicts,
// cross-context shadows (#1875), unparseable override keys — as warning
// notifications, deduped per session like the config diagnostics above. The
// table used to compute these and nobody read them; a user binding silently
// swallowing a default (editor.cmd+e over the global recent-files chord) is
// exactly what must not pass without a word.
func (m *Model) notifyKeymapDiags() {
	if m.bindings == nil || m.bindings.Table() == nil {
		return
	}
	if m.cfgDiagSeen == nil {
		m.cfgDiagSeen = map[string]bool{}
	}
	for _, d := range m.bindings.Table().Diagnostics() {
		if m.cfgDiagSeen[d] {
			continue
		}
		m.cfgDiagSeen[d] = true
		m.host.Notify(host.Warn, d)
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
	themePal, themeWarning := resolveTheme(reg, cfg, nil)
	root, _ := os.Getwd()
	// Missing-formatter install hints (#1402, the #1067 pattern) surface as
	// warn toasts; re-wiring on a project switch keeps the live host.
	format.SetNotifier(func(text string) { h.Notify(host.Warn, text) })
	// The local completion engine (#851) listens to editor events next to the
	// LSP bridge; registration by name keeps a project switch idempotent. The
	// word (#852) and symbol (#853) indexes start their one-shot project
	// scans in the background.
	engine := complete.NewEngine(h.Send)
	engine.Register(words.New(root))
	engine.Register(symbols.New(root))
	engine.Register(emmet.New())
	// Live templates (#1152): user [[snippets]] + built-ins as popup items,
	// language-scoped per buffer. Reads config.Get() live, so reloads apply.
	engine.Register(snippets.NewSource())
	// Postfix completion (#1913): the only local source that answers after a
	// "." — `err.nil` rewrites the expression before the dot. The enabled
	// closure reads config.Get() per query, so the settings toggle applies on
	// a config reload without re-wiring.
	engine.Register(postfix.New(func() bool { return config.Get().Editor.PostfixCompletion }))
	// The ES console's query buffers (#1927): Query-DSL keys plus the index
	// mapping's field names, exclusive to <index>.es.json files so buffer-word
	// noise never mixes in.
	engine.Register(esq.NewCompletionSource())
	h.SetEditorEmitter("complete", engine)
	var resumed *workspace.Workspace
	if mgr != nil {
		resumed = mgr.Resume(root)
	}
	// One register store for every editor in every workspace (#1540): the
	// manager owns it so it survives the model rebuild a project switch does;
	// on first start the fresh store is adopted by the manager below.
	var regs *register.Store
	if mgr != nil {
		regs = mgr.Registers()
	} else {
		regs = register.New()
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
		panes = pane.NewRegistry(cfg, regs)
		panes.SetPalette(themePal)
		panes.AddExplorer()
		edKey = panes.AddEditor()
		panes.SetFocused(pane.ExplorerKey)
	}
	refs := &refsMode{}
	actions := &actionsMode{}
	symbols := &symbolMode{}
	pasteHist := &pasteHistMode{}
	bookmarksPicker := &bookmarksMode{}
	bindings := &keymap.LiveBindings{}
	recent := &recentFiles{}
	vcsSt := &vcsState{}                            // shared before the literal: the reverts picker mode reads it
	layoutsPicker := newLayoutsMode(layoutNames)    // saved window layouts picker (#1175)
	httpRequests := newHTTPRequestsMode()           // stored HTTP responses picker (#1829)
	httpEntries := newHTTPEntriesMode()             // stored-response diff picker (#1992)
	httpEnvs := newHTTPEnvMode()                    // http-client.env.json picker (#1867)
	runConfigs := newRunConfigsMode()               // run/debug configurations picker (#1914)
	tasksPicker := newTasksMode()                   // discovered-tasks picker (#1915)
	sshPicker := newSSHMode()                       // ssh_config host picker (#1938)
	remotePicker := newRemoteMode()                 // SFTP browse host picker (#1997)
	playFilters := newPlayFiltersMode()             // named saved jq filters (#1995)
	cmdUsage := palette.LoadUsage(usageFile())      // most-used ranking (#773)
	fileUsage := palette.LoadUsage(fileUsageFile()) // most-used file ranking (#1419)
	winSizes := ui.LoadWinSizes(winSizeFile())      // resizable floats (#774)
	winSizesAll := ui.LoadWinSizes(globalWinSizeFile())
	// Background forge polling (#2085) is anchored to the project root the
	// process just chdir'd into: a switch rebuilds the model, and with it a
	// poller that seeds the new project's snapshot silently.
	forgeSt := &forgePollState{poller: forge.NewPoller(root, forgePollInterval(cfg))}
	// The persistent listing cache's toggle (#2108) is pushed into the forge
	// package here and on every config reload (reconfigureForgePoll).
	forge.SetCacheEnabled(forgeCacheEnabled(cfg))
	wsMgr := wsManager(mgr, resumed, root, panes) // hoisted: the palette's recent-projects sources read it (#820)
	wsMgr.SetRegisters(regs)                      // first start: the manager adopts the store (#1540); a switch hands back its own
	// Clipboard-history ring size (#2061). Editors re-apply it on Configure;
	// setting it here means host-side copies are bounded correctly even before
	// the first editor is built.
	if v, ok := cfg.Get("editor.clipboard_history_size"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			regs.SetHistoryCap(n)
		}
	}
	// Per-terminal probe verdicts (#2080) install before the first
	// buildKeymap below: Defaults derives every binding's Fragile flag from
	// Classify at table-build time, so probed truth has to be in place first.
	keymap.SetProbeVerdicts(keymap.LoadProbeStore(keymap.ProbeStorePath()).Results(keymap.TerminalID(os.Getenv)))
	m := Model{
		cmdUsage:        cmdUsage,
		fileUsage:       fileUsage,
		winSizes:        winSizes,
		winSizesAll:     winSizesAll,
		pins:            loadPins(),                          // pinned file slots (#788)
		lhStore:         localhistory.New(localHistoryDir()), // local history (#1023)
		feed:            changefeed.New(),                    // external-change feed (#2000)
		completeEngine:  engine,
		ws:              wsMgr,
		recentEditor:    edKey,
		recent:          recent,
		largeToasted:    map[string]bool{},
		logsetToasted:   map[string]bool{},
		toolchainSeg:    map[string]string{},
		liveImages:      map[int]bool{},
		navHist:         &nav.History{},
		playHistory:     &jqplay.History{},   // one session-wide program list (#1977)
		playLastProgram: map[string]string{}, // per-file last valid program (#1982)
		compMRU:         mru.Load(mru.DefaultFile()),
		bpts:            debug.Load(),
		coverage:        coverage.NewStore(), // per-run test coverage (#2081)
		coverageShown:   true,
		doctorLog:       debugdoctor.NewLog(),
		host:            h,
		reg:             reg,
		themePal:        themePal,
		bindings:        bindings,
		help:            help.New(reg, bindings, helpMinCol(cfg)),
		shell:           ui.New(shellConfig(cfg)),
		vcs:             vcsSt,
		forgePoll:       forgeSt,
		palette:         buildPalette(reg, cfg, refs, actions, bindings, recent, symbols, pasteHist, bookmarksPicker, vcsSt, cmdUsage, fileUsage, wsMgr, layoutsPicker, httpRequests, httpEntries, httpEnvs, runConfigs, tasksPicker, sshPicker, remotePicker, playFilters),
		layoutsPicker:   layoutsPicker,
		httpRequests:    httpRequests,
		httpEntries:     httpEntries,
		httpEnvs:        httpEnvs,
		runConfigs:      runConfigs,
		tasks:           tasksPicker,
		ssh:             sshPicker,
		remote:          remotePicker,
		playFilters:     playFilters,
		httpEnv:         loadHTTPEnv(), // selected HTTP environments (#1867)
		refs:            refs,
		lspStatus:       map[string]string{},
		symbols:         symbols,
		actions:         actions,
		pasteHist:       pasteHist,
		bookmarks:       bookmarksPicker,
		gmarks:          &marks.Store{},
		bmarks:          bookmarks.Load(), // project bookmarks (#55)
		qhist:           &histories.Store{},
		regs:            regs,
		paletteKey:      paletteToggleKey(cfg),
		splitZone:       splitZone(cfg),
		focusKeys:       focusKeys(cfg),
		keys:            buildKeymap(cfg, bindings),
	}
	m.floats = ui.NewStack(m.shell)                 // z-ordered floating stack (#1237)
	m.floats.SetSizeStore(winSizes)                 // resizable modal shell (#774)
	m.palette.SetSizeStore(winSizes)                // resizable palette box (#774)
	m.floats.SetMaxWidth(popupMaxWidth())           // centered-popup width cap (#932)
	highlight.SetRainbow(rainbowConfigured())       // rainbow brackets (#789)
	unidiff.SetWordHighlight(diffWordsConfigured()) // word-level diff emphasis (#1630)
	applyIDColorConfig()                            // identifier colors (#1626)
	applyNumberHintUnits()                          // number-hint field units (#1685)
	applySecretMaskingKeys()                        // custom secret key patterns (#1712)
	m.palette.SetMaxWidth(popupMaxWidth())
	m.watcher = watch.New(m.host.Send)
	// The change feed hides exactly what the watcher would never have walked
	// into (#2000): dot-directories and vendored noise below the watch root.
	m.feed.Ignore = feedIgnore(m.watcher)
	m.backupSvc = backupService()
	m.backupIv = backupInterval(cfg)
	m.backupDeb = backup.NewDebouncer(m.backupIv)
	m.autosaveIdleIv = autosaveIdleInterval(cfg)
	m.autosaveIdleDeb = backup.NewDebouncer(m.autosaveIdleIv)
	m.searcher = search.New(m.host.Send)
	m.finder = finder.New(m.searcher)
	m.finder.SetPalette(themePal)
	m.keyDoctor = keydoctor.New()
	m.keyDoctor.SetPalette(themePal)
	m.finder.SetHistories(m.qhist) // persistent query recall (#1171)
	m.finder.SetDisplayPath(displayPath)
	m.probStore = problems.NewStore()
	m.rawDiags = map[string][]ilsp.Diagnostic{}
	m.compileDiagIgnore()   // seed the ignore rules (#1259)
	m.compileDiagSeverity() // seed the severity remap rules (#1503)
	m.todoSearch = search.New(func(msg tea.Msg) { h.Send(todoindex.ScanMsg{Inner: msg}) })
	m.todo = todoindex.New(m.todoSearch, ".", todoPatterns(cfg))
	m.todo.SetPalette(themePal)
	m.todo.SetDisplayPath(displayPath)
	m.callhier = callhier.New()
	m.callhier.SetPalette(themePal)
	m.typehier = typehier.New()
	m.typehier.SetPalette(themePal)
	m.undoTree = undotree.New()
	m.undoTree.SetPalette(themePal)
	m.callhier.SetDisplayPath(displayPath)
	m.typehier.SetDisplayPath(displayPath)
	m.menu = menu.New(menu.Defaults(), m.commandInfo(reg))
	m.ctxMenu = menu.NewContext(m.commandInfo(reg))
	m.ctxMenu.SetPalette(themePal)
	m.cfgOpts = config.Discover(".")
	pages := settings.BasePages(themeNames(reg), themeNamesByDark(reg, false), themeNamesByDark(reg, true), reg.Themes()...)
	// The [theme.captures] editor (#1238) belongs with the theme picker.
	pages = settings.InsertAfter(pages, "Appearance", settings.Page{
		Title:  "Syntax Colors",
		Custom: settings.NewColorsPage(m.cfgOpts),
	})
	// The conceal/hint control center (#2133) renders the page whose schema
	// entries stay the documented key list: the model replaces the generic
	// form, the entries keep feeding docgen and the no-dead-keys test.
	pages = settings.AttachCustom(pages, settings.ConcealPageTitle, settings.NewConcealPage(m.cfgOpts))
	// The [files.associations] editor (#1365) belongs with the file settings.
	pages = settings.InsertAfter(pages, "Files & Session", settings.Page{
		Title:  "File Associations",
		Custom: settings.NewAssocPage(m.cfgOpts),
	})
	keymapPage := settings.NewKeymapPage(m.cfgOpts, func(id string) bool {
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
	})
	// The keymap doctor launcher (#2080): the page's sub-panel dispatches
	// keymap.doctor; the handler closes the settings panel and opens the
	// full-screen probe overlay.
	keymapPage.SetDoctorLaunch(func() tea.Cmd {
		return func() tea.Msg { return KeymapDoctorMsg{} }
	})
	pages = append(pages, settings.Page{Section: "TOOLS", Title: "Keymap", Custom: keymapPage})
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
	// The recent-files MRU always reloads from the session file, on the
	// resumed path too (#1112): a resumed workspace skips restoreSession, and
	// before this the model kept the empty constructor list — which the next
	// saveSession (quit, hidden-toggle, switch-away) then persisted, wiping
	// the project's MRU history for good.
	if s, ok := loadSession(); ok {
		m.recent.Set(s.RecentFiles.toEntries())
	}
	if resumed == nil {
		m.restoreLayout(cfg)
		m.restoreSession()
	} else if extras, ok := resumed.Aux.(wsExtras); ok {
		// The debug session parked with the workspace re-attaches (#777).
		m.dbg = extras.dbg
		m.dbgLaunching = extras.dbgLaunching
		m.dbgLaunchGen = extras.dbgLaunchGen
		m.dbgTermKey = extras.dbgTermKey
		// The popup terminal comes back exactly as left (#1407) — tabs,
		// scrollback, running processes, open state — and so do the
		// project-owned floating panels (#1793). Palettes re-thread like the
		// pane registry's below. Global panels are not in Aux: performSwitch
		// carries them model-to-model.
		m.popup = extras.popup
		m.floatTerms = extras.floats
		for _, inst := range m.popupLayerInstances() {
			inst.SetPalette(themePal)
		}
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
	dbgTermKey   string
	popup        popupTerm    // popup terminal (#1398) is per-project state (#1407)
	floats       []*floatTerm // project-owned floating terminal panels (#1793); global ones never park
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
		// A buffer bound to a forge text pushes on save (#2087). The emitter
		// cannot know which paths are bound, so every save reports and the
		// handler drops the ones that are not.
		go e.host.Send(forgeEditSavedMsg{path: ev.Path})
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
		Key:          ev.Key,
		LangPath:     ev.LangPath,
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
		source, disabled, adjust := breakpointHooks(m.bpts)
		mkSet, mkLines, mkAdjust := markHooks(m.gmarks)
		bmSigns, bmAdjust := bookmarkHooks(m.bmarks)
		for _, ed := range inst.Editors() {
			ed.SetEmitter(editorEmitter{host: m.host, watcher: m.watcher, nav: m.navHist, key: key})
			ed.SetBreakpointSource(source)
			ed.SetBreakpointDisabledSource(disabled)
			ed.SetBreakpointAdjuster(adjust)
			ed.SetMarkHooks(mkSet, mkLines, mkAdjust)
			ed.SetBookmarkHooks(bmSigns, bmAdjust)
			ed.SetHistories(m.qhist) // search/ex query recall (#1171)
			ed.SetCompletionMRU(m.compMRU)
			ed.SetRegisters(m.regs) // app-wide registers (#1540); idempotent
		}
	}
}

// restoreLayout rebuilds the registry and tree from the saved layout store. A
// valid save with the explorer present exactly once replaces the default
// explorer+editor set: every non-explorer leaf becomes an editor whose file is
// reloaded best-effort (a missing file restores as an empty editor). Any
// structural breakage or a missing/duplicate explorer leaves the default intact.
// A project WITHOUT a persisted layout materializes the user's designated
// default layout instead (#1175), so a saved default shapes new projects; the
// built-in explorer+editor pair stays the last resort.
func (m *Model) restoreLayout(cfg host.Config) {
	tree, ids, ok := loadLayout()
	if !ok {
		tree, ids, ok = defaultLayoutSnapshot()
		if !ok {
			return
		}
	}
	m.restoreFromLayout(tree, ids, cfg)
}

// restoreFromLayout is restoreLayout's body over an already-decoded snapshot,
// shared with the default-layout path (#1175): kind-only identities restore
// as empty editors / fresh shells / restarted tools / empty panels.
func (m *Model) restoreFromLayout(tree layout.Node, ids map[string]paneIdentity, cfg host.Config) {
	// A selective layout's flexible placeholder (#1568) has no live panes to
	// graft at startup: it materializes as one scratch editor slot, keyed past
	// the layout's own editor keys so nothing collides.
	for _, key := range layout.Leaves(tree) {
		if ids[key].Kind != "flex" {
			continue
		}
		next := 1
		for _, k := range layout.Leaves(tree) {
			if k == "editor" && next < 2 {
				next = 2
			} else if n, err := strconv.Atoi(strings.TrimPrefix(k, "editor:")); err == nil && k != key && n >= next {
				next = n + 1
			}
		}
		fresh := "editor"
		if next > 1 {
			fresh = "editor:" + strconv.Itoa(next)
		}
		tree, _ = layout.Replace(tree, key, fresh)
		delete(ids, key)
		ids[fresh] = paneIdentity{Kind: "editor"}
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
		} else if ids[key].Kind == "image" {
			continue // restored below re-decoding the image file (#1479)
		} else if ids[key].Kind == "archive" {
			continue // restored below re-listing the archive file (#1762)
		} else if ids[key].Kind == "data" {
			continue // restored below re-opening the database file (#1764)
		} else if ids[key].Kind == "diff" {
			continue // restored below re-reading both files (#60; fix #490)
		} else if ids[key].Kind == "vcs" {
			continue // restored below as the empty singleton panel (0330)
		} else if ids[key].Kind == "debug" {
			continue // restored below as the empty singleton panel (#580)
		} else if ids[key].Kind == "debugTerm" {
			continue // dropped below: the debuggee terminal is session state (#1370)
		} else if ids[key].Kind == "runTool" {
			continue // dropped below: the Run tool's output is session state (#1905)
		} else if ids[key].Kind == "problems" {
			continue // restored below as the empty singleton panel (#1024; fix #1157)
		} else if ids[key].Kind == "tests" {
			continue // restored below as the empty singleton panel (#1911)
		} else if ids[key].Kind == "issues" {
			continue // restored below as the empty singleton panel (#1934)
		} else if ids[key].Kind == "structure" {
			continue // restored below as the empty singleton panel (#1025)
		} else if ids[key].Kind == "dom" {
			continue // restored below as the empty singleton panel (#1929)
		} else if ids[key].Kind == "scratch" {
			continue // dropped below: the #1932 pane became the explorer's Scratches section (#1963)
		} else if ids[key].Kind == "usages" {
			continue // restored below as the empty singleton panel (#1155)
		} else if ids[key].Kind == "http" {
			continue // restored below as the empty singleton viewer (#1250)
		} else if ids[key].Kind == "breakpoints" {
			continue // restored below seeded from the persisted store (#1377)
		} else if !isEditorKey(key) && !isTerminalKey(key) && !isContentHostKey(key) {
			// A terminal- or viewer-shaped key may carry an editor identity:
			// a converted tab host (#836, #1778) restores as an editor pane
			// below.
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
	panes := pane.NewRegistry(cfg, m.regs)
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
	// The debuggee terminal pane (#1370) never resurrects — its content is
	// session state, and restoring a shell in its place would be misleading.
	// Its leaf is pruned; the next debug session recreates the pane beside
	// the panel. The Run tool (#1905) prunes the same way: the next run
	// reopens it at its placement, and no program re-runs at startup. A
	// global tool leaf whose tool was explicitly closed in another project
	// prunes too (#1903): the manager, not this stale layout entry, decides
	// whether the tool is open.
	prunedTerm := map[string]bool{}
	for _, key := range leaves {
		// A persisted "scratch" pane (#1932) no longer exists: the list became
		// the explorer's Scratches section (#1963), so its leaf prunes.
		drop := ids[key].Kind == "debugTerm" || ids[key].Kind == "runTool" ||
			ids[key].Kind == "scratch"
		if !drop && ids[key].Kind == "tool" {
			if entry, ok := toolEntry(ids[key].Tool); ok && m.staleGlobalTool(entry) {
				drop = true
			}
		}
		if drop {
			if pruned, ok := layout.Close(tree, key); ok {
				tree = pruned
				prunedTerm[key] = true
			}
		}
	}
	for _, key := range leaves {
		if key == pane.ExplorerKey {
			continue
		}
		if id := ids[key]; id.Kind == "debugTerm" || id.Kind == "runTool" || id.Kind == "scratch" {
			if prunedTerm[key] {
				continue // leaf pruned above (#1370, #1905)
			}
			// The sole leaf cannot be pruned; restore an empty editor so the
			// layout stays consistent.
			ids[key] = paneIdentity{Kind: "editor"}
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
			if prunedTerm[key] {
				continue // stale global tool leaf pruned above (#1903)
			}
			if entry, ok := toolEntry(id.Tool); ok && m.staleGlobalTool(entry) {
				// The sole leaf cannot be pruned; restore an empty editor so
				// the layout stays consistent (the debugTerm precedent).
				ids[key] = paneIdentity{Kind: "editor"}
			}
		}
		if id := ids[key]; id.Kind == "tool" {
			// A tool pane restores by restarting its configured program
			// (#741); a tool no longer configured degrades to a fresh shell
			// in the saved position rather than breaking the layout. A global
			// tool (#1890) with a live session parked on the manager
			// re-attaches that session in the saved slot instead of spawning
			// a duplicate process.
			if entry, ok := toolEntry(id.Tool); ok {
				if entry.Global && m.ws != nil {
					if term, taken := m.ws.TakeGlobalTool(entry.Name); taken {
						term.SetParked(false)
						panes.AdoptToolKey(key, term)
						continue
					}
				}
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
			// status snapshot re-feeds it (0330, #482).
			panes.AddVCS()
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
		if id := ids[key]; id.Kind == "usages" {
			// The Usages panel restores empty in its saved slot (#1155):
			// find-references results are session state; the next
			// lsp.referencesPanel run re-fills it.
			panes.Get(panes.AddUsages()).Usages().SetDisplayPath(displayPath)
			continue
		}
		if id := ids[key]; id.Kind == "tests" {
			// The Test Results panel restores empty in its saved slot
			// (#1911): the next captured test run re-fills it.
			panes.AddTests()
			continue
		}
		if id := ids[key]; id.Kind == "issues" {
			// The GitHub Issues panel restores empty in its saved slot
			// (#1934) with the same factories openIssuesPanel injects —
			// refresh, timeline (#2084), mutations (#2088) and the metadata
			// probe the edit gating reads (#2087). Without them a restored
			// pane would come back read-only; 'r' re-fetches the listing and
			// runs the probe.
			p := panes.Get(panes.AddIssues()).Issues()
			p.SetRefresh(forge.RefreshFactory("."))
			p.SetTimeline(forge.TimelineFactory("."))
			p.SetMutate(forge.MutateFactory("."))
			p.SetMeta(forge.MetaFactory("."))
			p.SetPRDetailFetch(forge.PRDetailFactory("."))
			p.SetPRAction(forge.PRActionFactory("."))
			continue
		}
		if id := ids[key]; id.Kind == "http" {
			// The HTTP response viewer restores empty in its saved slot
			// (#1250): the next http.run dispatch re-fills it.
			panes.AddHTTP()
			continue
		}
		if id := ids[key]; id.Kind == "breakpoints" {
			// The Breakpoints panel restores in its saved slot (#1377),
			// seeded from the persisted store loaded at start.
			m.wireBreakpointsPanel(panes.Get(panes.AddBreakpoints()).Breakpoints())
			continue
		}
		if id := ids[key]; id.Kind == "xdoctor" {
			// The Xdebug Doctor restores in its saved slot (#1991), sharing
			// the app-owned trace log (empty at start; connection attempts
			// are session state).
			m.wireDoctorPanel(panes.Get(panes.AddDoctor()).Doctor())
			continue
		}
		if id := ids[key]; id.Kind == "structure" {
			// The Structure panel restores empty (#1025); the first
			// buffer-change sync re-requests the symbols.
			panes.AddStructure()
			continue
		}
		if id := ids[key]; id.Kind == "dom" {
			// The DOM inspector restores empty (#1929); the first
			// buffer-change sync reparses the focused HTML buffer.
			panes.AddDOM()
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
		if id := ids[key]; id.Kind == "image" {
			// An image preview restores by re-decoding the file (#1479); a
			// vanished file restores as the pane's own decode-error fallback.
			panes.AddImageKey(key, id.Path)
			continue
		}
		if id := ids[key]; id.Kind == "archive" {
			// An archive viewer restores by re-listing the file (#1762); a
			// vanished file restores as the pane's own error notice.
			panes.AddArchiveKey(key, id.Path)
			continue
		}
		if id := ids[key]; id.Kind == "data" {
			// A data viewer restores by re-opening the database (#1764); a
			// vanished file restores as the pane's own error notice.
			panes.AddDataKey(key, id.Path)
			continue
		}
		if id := ids[key]; id.Kind == "es" {
			// An ES console restores by reconnecting to its endpoint (#1927);
			// a dead or unconfigured endpoint restores as the pane's own
			// error notice.
			panes.AddESKey(key, id.Path)
			continue
		}
		if id := ids[key]; id.Kind == "remote" {
			// A remote browser restores by re-dialing its host in the
			// background (#1997); an unreachable host restores as the pane's
			// own error notice.
			panes.AddRemoteKey(key, id.Path)
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
		pinned := make(map[int]bool, len(id.Pinned))
		for _, n := range id.Pinned {
			pinned[n] = true
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
			if pinned[i] {
				// Pins round-trip restarts (#1172): the index convention is
				// the persisted Tabs list, same as Active.
				inst.SetTabPinned(inst.ActiveTab(), true)
			}
		}
		// Tool sessions hosted as tabs (#836) restart their configured
		// program in place, like dedicated tool panes (#741); a tool no
		// longer configured restores as nothing. A pane that held only
		// tool tabs drops its placeholder empty editor tab again.
		wasEmpty := inst.IsEmptyEditor()
		toolTabs, activeTool := 0, -1
		for n, tool := range id.Tools {
			entry, ok := toolEntry(tool)
			if !ok || m.staleGlobalTool(entry) {
				// Unconfigured restores as nothing; so does a global tool
				// explicitly closed elsewhere since the save (#1903).
				continue
			}
			// A parked live global instance re-attaches instead of
			// spawning a duplicate (#1890); restoredToolSession decides.
			inst.AddTerminalTab(m.restoredToolSession(panes, entry))
			toolTabs++
			if id.ActiveTool == n+1 {
				activeTool = inst.ActiveTab()
			}
		}
		if wasEmpty && toolTabs > 0 {
			inst.CloseTab(0)
			active = inst.ActiveTab()
			if activeTool > 0 {
				activeTool-- // the dropped placeholder shifted every tool tab
			}
		}
		if activeTool >= 0 {
			// The tool tab selected when the layout was saved wins (#1906);
			// a tool that no longer restores leaves active as it was — the
			// last restored tab — instead of pointing at nothing.
			active = activeTool
		}
		// Content tabs (#1778) rebuild from their viewer identities at their
		// remembered positions — the same per-kind restore the dedicated
		// panes get. A pane that held only content tabs still starts with
		// the placeholder scratch tab; it is dropped once the content is in.
		actPtr := inst.Tab(active)
		placeholder := wasEmpty && toolTabs == 0 && len(id.CTabs) > 0
		var ctabPos []int
		for _, ct := range id.CTabs {
			kind, ok := contentKindFromString(ct.Kind)
			if !ok {
				continue
			}
			nested := panes.NewContentPane(kind, ct.Path, ct.Path2, ct.Rev, ct.Rev2)
			if nested == nil {
				continue
			}
			switch kind {
			case pane.KindMarkdown:
				if data, err := os.ReadFile(ct.Path); err == nil {
					nested.Preview().SetSourceImmediate(string(data))
				}
			case pane.KindDiff:
				if ct.Rev != "" || ct.Rev2 != "" {
					nested.Diff().SetContents(revContentOrFile(ct.Rev, ct.Path, ct.Path2), revContentOrFile(ct.Rev2, ct.Path2, ct.Path2))
				} else {
					nested.Diff().SetContents(readFileOrEmpty(ct.Path), readFileOrEmpty(ct.Path2))
				}
			}
			if !inst.AddContentTab(nested) {
				continue
			}
			pos := ct.Index
			if placeholder {
				pos++ // the scratch placeholder still occupies slot 0
			}
			if pos < 0 {
				pos = 0
			}
			if pos >= inst.TabCount() {
				pos = inst.TabCount() - 1
			}
			inst.MoveTab(inst.ActiveTab(), pos)
			inst.SetTabPinned(pos, ct.Pinned)
			ctabPos = append(ctabPos, pos)
		}
		if placeholder && len(ctabPos) > 0 {
			inst.CloseTab(0)
			for i := range ctabPos {
				ctabPos[i]--
			}
		}
		if id.ActiveCTab > 0 && id.ActiveCTab <= len(ctabPos) {
			inst.ActivateTab(ctabPos[id.ActiveCTab-1])
		} else {
			// Content inserts may have shifted the active file/tool tab's
			// index; re-resolve it by slot identity.
			for idx := 0; idx < inst.TabCount(); idx++ {
				if inst.Tab(idx) == actPtr {
					active = idx
					break
				}
			}
			inst.ActivateTab(active)
		}
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
	// s.RecentFiles is loaded in buildModel (#1112): the MRU must survive the
	// resumed-workspace path too, which never reaches this restore.
	m.explorer().Restore(explorer.State{
		Expanded:         s.Explorer.Expanded,
		ShowHidden:       s.Explorer.ShowHidden,
		Cursor:           s.Explorer.Cursor,
		ScratchCollapsed: s.Explorer.ScratchCollapsed,
		ScratchHeight:    s.Explorer.ScratchHeight,
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
		RecentFiles: recentListFromEntries(m.recent.Entries()),
		Explorer: explorerSession{
			Expanded:         st.Expanded,
			ShowHidden:       st.ShowHidden,
			Cursor:           st.Cursor,
			ScratchCollapsed: st.ScratchCollapsed,
			ScratchHeight:    st.ScratchHeight,
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
	for _, inst := range m.popupLayerInstances() {
		// Popup terminal sessions (#1398) — the box's and every floating
		// panel's, global ones included (#1793: a global session ends with
		// the app) — are session state only; end them tidily instead of
		// leaving the shells to die with the process.
		inst.CloseTerminalTabs()
	}
	m.backupCleanShutdown()
	// End everything that would otherwise outlive the process (#1546). The
	// active workspace's pane and tab terminal sessions close like a parked
	// workspace's would; the active debug session gets Disconnect — the only
	// call carrying terminateDebuggee — before Close, because adapters start
	// detached (setsid) and would survive IKE as orphans otherwise, debuggee
	// included. Parked workspaces run the full teardown (terminals, popup
	// tabs, debug session, DBGp listener).
	for _, key := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(key)
		if inst == nil {
			continue
		}
		switch inst.Kind() {
		case pane.KindTerminal:
			inst.Terminal().Close()
		case pane.KindEditor:
			inst.CloseTerminalTabs()
		}
	}
	if m.dbg != nil && m.dbg.sess != nil {
		sess := m.dbg.sess
		_ = sess.Disconnect()
		sess.Close()
	}
	// teardownWorkspace also ends the parked popup terminals riding in Aux
	// (#1407) and disconnects a parked debug session.
	for _, root := range m.ws.Background() {
		teardownWorkspace(m.ws.Peek(root))
	}
	// Detached global tool sessions (#1890) belong to no workspace registry —
	// end them here so no tool process outlives IKE; an attached one closed
	// with the active workspace's terminals above.
	m.closeParkedGlobalTools()
	// Language servers shut down through the spec's handshake via the quit
	// hooks (#1546). Their cmds run synchronously here — blocking the Update
	// goroutine is fine while quitting, and it guarantees the process does
	// not vanish under the shutdown handshake.
	for _, cmd := range m.fireHooks(plugin.EventAppQuit, nil) {
		if cmd != nil {
			cmd()
		}
	}
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

// whichKeyDelayMsg fires when a held prefix has pended for the configured
// which-key delay (#1909). gen is the pending-sequence generation the timer was
// armed for: a stale timer — the sequence resolved, was cancelled or restarted
// meanwhile — carries an older generation and is dropped, so a chord completed
// before the delay never flashes a popup.
type whichKeyDelayMsg struct{ gen int }

// showWhichKey fills the hint rows from the resolver's held prefix. Called once
// per pending-prefix change (delay expiry, or a narrowing key while the popup
// is already up), never per render.
func (m *Model) showWhichKey() {
	prefix, conts := m.keys.PendingContinuations(m.keyContext())
	if prefix == "" {
		m.whichKey = nil
		return
	}
	m.whichKey = append([]string{prefix + " —"}, keymap.FormatContinuations(conts, 12)...)
}

// clearWhichKey hides the popup and invalidates any armed delay timer, so a
// sequence that ended (resolved, cancelled, clicked away) stays silent.
func (m *Model) clearWhichKey() {
	m.whichKey = nil
	m.whichKeyGen++
}

// resolveKeymap feeds one key to the keybinding resolver in the focused context.
// It returns (cmd, true) when the key is consumed by the keymap layer — either a
// resolved command to run or a partial chord to wait on — and (nil, false) when
// the key should fall through to the existing dispatch (no match, or an inert
// binding whose command id is not registered).
func (m *Model) resolveKeymap(k keymap.Key) (tea.Cmd, bool) {
	// Esc abandons a sequence in progress (#1909): the pending chord and its
	// which-key popup go away and the key is consumed, so cancelling a chord
	// never doubles as an esc for the focused pane (leaving insert mode,
	// closing a popup). Unmodified esc only — cmd+k esc could be bound.
	if m.keys.Pending() && k.Base == "esc" && k.Mods == 0 {
		if !m.keys.Continues(k, m.keyContext()) {
			m.keys.Reset()
			m.clearWhichKey()
			return nil, true
		}
	}
	res := m.keys.Feed(k, m.keyContext())
	switch res.Status {
	case keymap.Pending:
		// Hold the partial chord and arm the resolver timeout; swallow the key
		// meanwhile. The which-key hints (0081/40) wait for the configured
		// delay (#1909) so a sequence typed at speed never flashes a popup —
		// but once the popup is up, a narrowing key updates it at once.
		m.whichKeyGen++
		cmds := []tea.Cmd{tea.Tick(keymap.TimeoutDuration, func(time.Time) tea.Msg {
			return keymapTimeoutMsg{}
		})}
		switch on, delay := whichKeyConfig(); {
		case !on:
			m.whichKey = nil
		case len(m.whichKey) > 0 || delay <= 0:
			m.showWhichKey()
		default:
			gen := m.whichKeyGen
			cmds = append(cmds, tea.Tick(delay, func(time.Time) tea.Msg {
				return whichKeyDelayMsg{gen: gen}
			}))
		}
		return tea.Batch(cmds...), true
	case keymap.Resolved:
		m.clearWhichKey()
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
		m.clearWhichKey()
	}
	return nil, false
}

// whichKeyConfig reads the which-key switch and delay (#1909) off the live
// config, so a settings edit applies to the next pending prefix.
func whichKeyConfig() (bool, time.Duration) {
	c := config.Get()
	if c == nil {
		return true, 300 * time.Millisecond
	}
	return c.Keymap.WhichKey, time.Duration(c.Keymap.WhichKeyDelayMs) * time.Millisecond
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
func buildPalette(reg *registry.Registry, cfg host.Config, refs *refsMode, actions *actionsMode, bindings *keymap.LiveBindings, recent *recentFiles, symbols *symbolMode, pasteHist *pasteHistMode, bookmarks *bookmarksMode, vcsSt *vcsState, usage, fileUsage *palette.Usage, wsMgr *workspace.Manager, layouts *layoutsMode, httpRequests *httpRequestsMode, httpEntries *httpEntriesMode, httpEnvs *httpEnvMode, runConfigs *runConfigsMode, tasks *tasksMode, ssh *sshMode, remoteHosts *remoteMode, playFilters *playFiltersMode) *palette.Palette {
	pcfg := palette.Config{
		MaxResults:    paletteMaxResults(cfg),
		DefaultPrefix: paletteDefaultPrefix(cfg),
	}
	cmd := palette.NewCommandMode(reg, bindings, paletteHideOff(cfg))
	cmd.SetUsage(usage)
	file := palette.NewFileMode()
	file.SetUsage(fileUsage)
	file.SetScratchList(scratchList)
	dir := palette.NewDirMode()
	proj := project.NewPickerMode(nil)
	mru := palette.NewRecentMode(func() []palette.RecentEntry {
		entries := recent.Entries()
		out := make([]palette.RecentEntry, len(entries))
		for i, e := range entries {
			out[i] = palette.RecentEntry{Path: e.Path, LastOpened: e.LastOpened}
		}
		return out
	})
	// The Recent Files dialog grows a Recent Projects column (#778): entries
	// from project.history (current project excluded), whose activation goes
	// through the normal validated seamless-switch path (project.PickedMsg).
	// Background workspaces still open in memory (#777) are marked with "●"
	// and closable in place via the aux action (#820).
	openInMemory := func(path string) bool { return wsMgr != nil && wsMgr.Peek(path) != nil }
	proj.SetOpen(openInMemory)
	// The peek flavour behind project.peek (#2136): same list, activation
	// peeks; alt+enter still switches for real.
	projPeek := project.NewPeekPickerMode(nil)
	projPeek.SetOpen(openInMemory)
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
				// Right-aligned last-opened column (#842, #1114).
				Time: project.RelTime(e.LastOpened, time.Now()),
			}
			if openInMemory(e.Path) {
				it.Badge = "●"
				it.Aux = project.CloseWorkspaceMsg{Path: e.Path}
				it.AuxGlyph = project.CloseAuxGlyph
			} else {
				// Unloaded entries prune from the history (#842), like in
				// the project picker.
				it.Aux = project.RemoveFromHistoryMsg{Path: e.Path}
			}
			items = append(items, it)
		}
		return items
	})
	scr := palette.NewScratchMode(scratchEntries)
	scrNew := scratchNewMode{}
	bufLang := bufferLangMode{} // "Treat Buffer as …" language picker (#2033)
	// Classes are their own search-everywhere category (#1849), ranked right
	// after the commands: a class is what users most often search for by name,
	// and its own per-kind cap keeps it out of the workspace-symbol crowd. Both
	// views read the one symbol cache; the symbol seat drops the class-like
	// kinds so no symbol is listed twice.
	classes := newClassMode(symbols)
	all := palette.NewSearchAllMode(cmd, classes, file, newNonClassSymbolMode(symbols))
	all.SetRecents(mru)
	reverts := newRevertsMode(func() (string, []vcs.RevertSnapshot) { return vcsSt.revertsPath, vcsSt.reverts })
	openPath := palette.NewOpenPathMode()
	return palette.New(pcfg, cmd, file, dir, proj, projPeek, refs, actions, mru, all, symbols, classes, scr, scrNew, pasteHist, bookmarks, reverts, openPath, layouts, httpRequests, httpEntries, httpEnvs, runConfigs, tasks, ssh, remoteHosts, playFilters, bufLang)
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

// focusedDeadTerminal returns the focused pane's terminal when its session
// has finished (#1951) — the exited-run view terminalFocused() deliberately
// excludes, since no key may reach a child that is gone. nil whenever the
// focused pane hosts no terminal or the session still runs.
func (m Model) focusedDeadTerminal() *terminal.Model {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return nil
	}
	if inst.Kind() != pane.KindTerminal && inst.Kind() != pane.KindEditor {
		return nil
	}
	t := inst.ActiveTerminal()
	if t == nil || t.Running() {
		return nil
	}
	return t
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
// cmd+c over an active mouse selection, cmd+v (system-clipboard paste), the
// global navigation allowlist (terminalGlobalCommands + the configured
// palette.toggle_key, #805), and terminal-context bindings (#1794 —
// terminalContextChord: ctrl+t → new terminal tab and any user
// keymap.bindings.terminal.* chord) are reserved in the caller
// (#228, #227, #727). Everything else, including tab, ctrl+c, esc and the
// F-keys, belongs to the shell. shift+pgup/pgdn page the scrollback inside
// the pane itself.
func (m Model) terminalReservedKey(keys string) (bool, tea.Model, tea.Cmd) {
	// Canonicalize the chord (#981): bubbletea encodes the Command key as
	// super+/meta+ tokens, which ParseKey folds onto the logical cmd form the
	// cases below use. Deliberately NOT platform-folded to ctrl — inside a
	// terminal ctrl+d belongs to the shell on every platform (and ctrl+t is a
	// terminal-context *binding*, not this hardcoded set, so it stays
	// rebindable, #1794).
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
	case "cmd+f":
		// cmd+f opens the scrollback search (#1504) — the muscle-memory
		// entry point to the same inline search `/` starts from scrollback
		// (#1169), working from the live view too. Under an alt-screen or
		// mouse-reporting child (vim, lazygit) the chord stays with the
		// child, which owns its own find; outside terminals cmd+f keeps
		// its global binding (editor.find).
		if term := m.activeWS().Panes.FocusedInstance().ActiveTerminal(); term != nil && term.StartSearch() {
			return true, m, nil
		}
		return false, m, nil
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
// The translation itself lives in keymap so the terminal probe exercises the
// same mapping.
func mouseChordKey(b tea.MouseButton) (keymap.Key, bool) {
	return keymap.FromMouseButton(b)
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
	"project.close":            true,
	// #2136: the one-key way back from a peek must work with a terminal
	// focused too — a peek often ends while looking at a shell.
	"project.peek.return": true,
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
	"tests.toggle":          true,
	"issues.toggle":         true,
	"structure.toggle":      true,
	"dom.toggle":            true,
	"debug.doctor":          true,
	"scratch.panel":         true,
	"notifications.history": true,
	// #997: tab switching stays reachable from a focused terminal/tool pane
	// (the shell never meaningfully sees ctrl+cmd+arrows). The secondary
	// ctrl+alt+arrow bindings stay with the shell — see terminalShellChords.
	"editor.tab.next": true,
	"editor.tab.prev": true,
	// #934: zen must toggle (and untoggle) with a terminal or tool pane
	// focused; the shell never meaningfully sees the zen chord.
	"view.zenMode": true,
	// #1398: the popup terminal must open from a focused pane terminal too.
	"terminal.popup": true,
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
		if b, found := table.Lookup(chord, m.keyContext()); found && terminalGlobalCommands[b.Command] {
			if c, okc := m.reg.Command(b.Command); okc {
				return true, m.dispatchCommand(b.Command, c)
			}
		}
	}
	return false, nil
}

// terminalShellEssential are chords a terminal-context binding may never take
// from the shell (#1794): the POSIX interrupt/EOF/suspend strokes every CLI
// leans on. They forward to the PTY even when a terminal-scoped binding names
// them; everything else a terminal-context binding claims is intercepted.
var terminalShellEssential = map[string]bool{
	"ctrl+c": true,
	"ctrl+d": true,
	"ctrl+z": true,
}

// terminalContextChord resolves a single-step chord against the live binding
// table looking for a terminal-scoped binding (#1794): per-context defaults
// (ctrl+t → terminal.newTab) and user overrides under
// keymap.bindings.terminal.* run BEFORE the raw PTY forwarding, so the same
// chord can do IDE work in a terminal and something else elsewhere. Guard
// rails keep the shell usable: only bindings explicitly scoped to the
// terminal context are eligible (Global chords stay with the shell unless
// allowlisted in terminalGlobalCommands), unmodified keys are never
// intercepted (they are typing), and terminalShellEssential /
// terminalShellChords always forward. Taking ctrl+t from the shell
// (readline's transpose-chars) is the documented trade — unbinding
// `keymap.bindings."terminal.ctrl+t"` restores the forwarding.
func (m *Model) terminalContextChord(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k, ok := keymap.FromKeyMsg(msg)
	if !ok || k.Mods == 0 {
		return false, nil
	}
	table := m.bindings.Table()
	if table == nil {
		return false, nil
	}
	chord := keymap.Chord{Steps: []keymap.Key{k}}
	cs := chord.String()
	if terminalShellEssential[cs] || terminalShellChords[cs] {
		return false, nil
	}
	b, found := table.Lookup(chord, keymap.Terminal)
	if !found || b.Context != keymap.Terminal {
		return false, nil
	}
	if c, okc := m.reg.Command(b.Command); okc {
		return true, m.dispatchCommand(b.Command, c)
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

// newEditorTab appends a fresh empty editor tab to the focused editor pane
// (#1794, the editor half of the per-context ctrl+t pair), falling back to
// the active editor pane, else spawning one. A truly empty active tab (no
// file, no text — the shared predicate, #641) is reused instead of stacking
// blank tabs.
func (m *Model) newEditorTab() {
	key := ""
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor {
		key = inst.Key()
	}
	if key == "" {
		key = m.activeEditorKey()
	}
	if key == "" {
		key = m.spawnEditor()
	}
	inst := m.activeWS().Panes.Get(key)
	if inst == nil {
		return
	}
	if ed := inst.Editor(); ed == nil || !ed.IsEmpty() {
		inst.AddTab()
		m.installEmitter(key)
	}
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
// (the project root): at the slot assigned to "terminal" when one is active
// (#1946 — further terminals join the slot pane as tabs), otherwise split
// off the active editor — below by default, to the right on wide landscape
// hosts (auxZone, #1588) — falling back to the focused leaf when no editor
// exists.
func (m *Model) openTerminal() {
	if m.activeWS().Tree != nil {
		if tpl, slot := assignedSlot(terminalToolID); slot != "" && m.openShellAtSlot(tpl, slot) {
			return
		}
	}
	target := m.activeEditorKey()
	if target == "" {
		target = m.activeWS().Panes.Focused()
	}
	if target == "" || m.activeWS().Tree == nil {
		return
	}
	key := m.activeWS().Panes.AddTerminal(terminal.Shell(m.configuredShell()), ".", terminalEnv(), m.host.Send)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, key, m.auxZone(target))
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
	inst.AddTerminalTab(m.newShellTab())
	m.setFocus(target)
	saveLayout(m.activeWS().Tree, m.activeWS().Panes)
}

// configuredShell reads the terminal.shell setting; "" follows $SHELL.
func (m *Model) configuredShell() string {
	if v, ok := m.host.Config().Get("terminal.shell"); ok {
		return v
	}
	return ""
}

// newShellTab spawns a fresh shell session ready to host as a terminal tab.
func (m *Model) newShellTab() terminal.Model {
	key := m.activeWS().Panes.MintTerminalKey()
	return terminal.New(key, terminal.Shell(m.configuredShell()), ".", 80, 24, terminalEnv(), m.host.Send)
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
	if hostKey, tabIdx, _, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindMarkdown && c.Preview().Path() == path
	}); ok {
		m.focusContentAt(hostKey, tabIdx) // may live in a tab (#1778)
		return
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
	if inst, hostKey, tabIdx, ok := m.findDiffPane(leftPath, rightPath, "", ""); ok {
		inst.Diff().SetContents(readFileOrEmpty(leftPath), readFileOrEmpty(rightPath))
		m.focusContentAt(hostKey, tabIdx)
		return
	}
	// Single diff window (#513): retarget the existing pane instead of
	// splitting another one.
	if inst, hostKey, tabIdx, ok := m.diffSlot(); ok {
		inst.StopDiffEdit()
		inst.Diff().Retarget(baseName(leftPath), baseName(rightPath), leftPath, rightPath, "", "", true)
		inst.Diff().SetContents(readFileOrEmpty(leftPath), readFileOrEmpty(rightPath))
		m.focusContentAt(hostKey, tabIdx)
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

// diffSlot returns the diff viewer to reuse in single-window mode (#513):
// the first open diff — dedicated pane or content tab (#1778) — unless
// config diff.windows = "multi" restores the split-per-open behavior. The
// host key and tab index (-1 for a pane) locate it for focusContentAt.
func (m Model) diffSlot() (*pane.Instance, string, int, bool) {
	if v, ok := m.host.Config().Get("diff.windows"); ok && v == "multi" {
		return nil, "", -1, false
	}
	hostKey, tabIdx, inst, ok := m.findContent(func(c *pane.Instance) bool {
		return c.Kind() == pane.KindDiff
	})
	return inst, hostKey, tabIdx, ok
}

// findDiffPane locates an open diff viewer matching the identity: the file
// pair plus the per-side revisions ("" = working tree). Re-opening the same
// diff focuses it instead of splitting a duplicate (#509) — wherever it
// lives, dedicated pane or content tab (#1778).
func (m Model) findDiffPane(leftPath, rightPath, leftRev, rightRev string) (*pane.Instance, string, int, bool) {
	hostKey, tabIdx, inst, ok := m.findContent(func(c *pane.Instance) bool {
		if c.Kind() != pane.KindDiff {
			return false
		}
		d := c.Diff()
		lr, rr := d.Revs()
		return d.LeftPath() == leftPath && d.RightPath() == rightPath && lr == leftRev && rr == rightRev
	})
	return inst, hostKey, tabIdx, ok
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

// previewsForPath returns every markdown preview instance bound to path —
// dedicated panes and content tabs (#1778) alike.
func (m Model) previewsForPath(path string) []*pane.Instance {
	var out []*pane.Instance
	m.contentInstances(func(_ string, _ int, c *pane.Instance) bool {
		if c.Kind() == pane.KindMarkdown && c.Preview().Path() == path {
			out = append(out, c)
		}
		return true
	})
	return out
}

// explorer returns the singleton explorer model.
func (m Model) explorer() *explorer.Model {
	return m.activeWS().Panes.Get(pane.ExplorerKey).Explorer()
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.explorer().Init()}
	// Query the terminal background (OSC 11, #1480) so [theme].auto can pick
	// the light/dark pair; harmless where unsupported (no reply, no wait).
	cmds = append(cmds, tea.RequestBackgroundColor)
	// The TODO index's initial full scan (#61): Init runs after main.go wires
	// the sender (and again after a project switch), so the streamed results
	// land and the status-line count is live without opening the overlay.
	m.todo.Rescan()
	// Every restored data viewer opens its database in the background (#1795),
	// so a workspace holding a huge database still comes up instantly.
	cmds = append(cmds, m.initDataPanes()...)
	// Restored ES consoles reconnect to their clusters the same way (#1927).
	cmds = append(cmds, m.initESPanes()...)
	// A restored issues pane shows the persisted listing snapshot instantly
	// (#2108), marked stale until the first background poll replaces it. The
	// command reads a file (and one git call for the remote key) and resolves
	// to nothing when there is no usable cache — cheap enough for Init, whose
	// commands the test helpers drain synchronously.
	if p := m.issuesPanel(); p != nil && !p.Loaded() {
		cmds = append(cmds, forge.LoadCacheCmd("."))
	}
	cmds = append(cmds, m.initRemotePanes()...)
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
	// The performance HUD's message counter (#1999). Dispatch is the natural
	// counting point — every wake of the program passes here exactly once —
	// and with the HUD hidden the whole hook is one atomic load.
	if perfhud.Enabled() {
		perfhud.Count(msg)
	}
	// The stall watchdog's heartbeat (#2163): a pass that never reaches
	// LoopExit is a frozen loop, and the watchdog goroutine dumps stacks.
	diag.LoopEnter(msg)
	defer diag.LoopExit()
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
	// The DOM inspector rides the same settled pass (#1929): cursor follow is
	// a cheap in-place highlight, a buffer switch or edit spawns the async
	// reparse, and moved selector matches re-route to the editors.
	if sync := mm.domSyncCmd(); sync != nil {
		cmd = tea.Batch(cmd, sync)
	}
	// Image panes reconcile their Kitty graphics placements here, once the
	// pass settled (#1479): any message may have opened, closed or resized
	// one, and the raw transmit/delete sequences must follow the layout.
	if gfx := mm.imageSyncCmd(); gfx != nil {
		cmd = tea.Batch(cmd, gfx)
	}
	// The breadcrumbs bar (#1153) claims or releases its editor row here,
	// once the pass settled: symbol data arriving, tab/zen switches and the
	// config toggle all change the row's visibility without a layout event.
	mm.syncBreadcrumbLayout()
	// An armed explorer reveal (#1042) drains here once the pass settled:
	// SetActive's call sites (focus changes, tab switches, the CLI open flow)
	// cannot dispatch Cmds, so auto-reveal / Reveal() only mark the model and
	// the expansion scans start now.
	if reveal := mm.explorer().PendingRevealCmd(); reveal != nil {
		cmd = tea.Batch(cmd, reveal)
	}
	// Background forge polling (#2085) reopens its chain here when a config
	// reload turned it back on — the one edge the self-sustaining chain cannot
	// cover itself, since reloadConfig has no command to return. On every
	// other pass this is a flag check and nothing else: arming unconditionally
	// would mean no Update pass ever settles without a pending tick.
	if poll := mm.armForgePoll(); poll != nil {
		cmd = tea.Batch(cmd, poll)
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

	case tea.BackgroundColorMsg:
		// The OSC 11 reply (#1480): classify the terminal background and,
		// with [theme].auto on, re-resolve the light/dark pair. A terminal
		// that never answers simply never sends this — no timeout needed,
		// [theme].name stays in effect.
		dark := theme.IsDarkColor(msg.Color)
		m.termDark = &dark
		if m.autoThemeEnabled() {
			pal, warning := resolveTheme(m.reg, m.host.Config(), m.termDark)
			m.applyTheme(pal)
			if warning != "" {
				m.host.Notify(host.Warn, warning)
			}
		}
		return m, nil

	case SyncThemeMsg:
		// Re-query the background on demand (themes.syncTerminal); the reply
		// re-enters above.
		return m, tea.RequestBackgroundColor

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.applyPopupSize() // the popup lives outside the tree; layout() never sizes it
		m.floats.SetSize(m.width, m.height)
		m.palette.SetSize(m.width, m.height)
		m.finder.SetSize(m.width, m.height)
		m.keyDoctor.SetSize(m.width, m.height)
		m.todo.SetSize(m.width, m.height)
		m.callhier.SetSize(m.width, m.height)
		m.typehier.SetSize(m.width, m.height)
		m.undoTree.SetSize(m.width, m.height)
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
		// A click while a chord is pending interrupts the sequence (#1482):
		// the surviving which-key popup closes and the click acts normally.
		if m.keys.Pending() {
			m.keys.Reset()
			m.clearWhichKey()
		}
		// The keymap doctor (#2080) probes the mouse nav buttons like chords;
		// every other press is swallowed — the panes below are hidden.
		if m.keyDoctor.IsOpen() {
			if k, isNav := keymap.FromMouseButton(msg.Button); isNav {
				m.keyDoctor.RecordKey(k)
			}
			return m, nil
		}
		// The dedicated back/forward buttons (#816) resolve through the
		// keymap as synthetic chords — rebindable like keys, default
		// nav.back / nav.forward — regardless of the hovered pane. Unbound
		// presses are swallowed: no pane expects button 4/5.
		if k, isNav := mouseChordKey(msg.Button); isNav {
			// Leave a trace that the terminal DID deliver the button (#816):
			// a silent no-op is otherwise indistinguishable from a terminal
			// that never reports buttons 4/5. `cmd/keyprobe` answers the same
			// question interactively.
			logMouseNavButton(k.Base)
			// A modal overlay owns the input; navigating the editor hidden
			// underneath it would be invisible and would strand the overlay.
			if m.overlayCapturesKeyboard() {
				return m, nil
			}
			if cmd, handled := m.resolveKeymap(k); handled {
				return m, cmd
			}
			return m, nil
		}
		return m.handleMouse(mouseEvent{Mouse: msg.Mouse(), action: mousePress})
	case tea.MouseReleaseMsg:
		if m.keyDoctor.IsOpen() {
			return m, nil // the press was probe evidence; nothing below may react
		}
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
		// The explorer's default open (enter / l / double-click) always lands in
		// the last-focused editor pane — a plain file as an editor tab, a viewer
		// file as a content tab beside it (#1851). Only the explicit "open in
		// split" action (o) still splits.
		if msg.NewPane {
			return m.openPath(msg.Path, true)
		}
		return m.openPathInEditor(msg.Path)

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
		_ = m.bmarks.Save() // edit-shifted bookmarks persist the same way (#55)
		// The Structure pane re-requests the saved buffer's symbols (#1025);
		// the Update wrapper's sync issues the actual request. A cached
		// breadcrumbs tree (#1153) forces the refresh the same way, so the
		// bar tracks the saved file's new symbol spans.
		_, crumbs := m.docSymbols[msg.path]
		if sp := m.structPanel(); (sp != nil && sp.Path() == msg.path) || crumbs {
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

	case testresults.OpenLocationMsg:
		// A Test Results activation (#1911): jump to the failure location
		// (already 0-based, resolved against the run's directory).
		return m.openPathAt(msg.Path, msg.Line, 0)

	case testresults.LocateTestMsg:
		// A passed test has no failure location: scan the run's test files
		// for the declaration, like the gutter markers do.
		return m.locateTest(msg.RerunID)

	case problems.CopyMsg:
		// "y"/cmd+c on a Problems row (#2071): the marked line goes to the
		// clipboard through the shared copy path, with a toast.
		return m.copyPanelRow(msg.Text, msg.What)

	case usages.CopyMsg:
		return m.copyPanelRow(msg.Text, msg.What)

	case breakpanel.CopyMsg:
		return m.copyPanelRow(msg.Text, msg.What)

	case testresults.CopyMsg:
		return m.copyPanelRow(msg.Text, msg.What)

	case testresults.RerunMsg:
		// The panel's re-run actions (#1911): all, failed only, or one test.
		return m, m.rerunTests(msg)

	case TestRunDoneMsg:
		// A captured test run finished off-loop: parse and fill the pane; a
		// coverage run (#2081) also pushes fresh gutter marks.
		return m, m.finishTestRun(msg)

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

	case OpenInBrowserMsg:
		// #1429: open the focused file (explorer selection or editor file)
		// in the platform default browser; non-viewable types toast instead.
		return m, m.openInBrowser()

	case CopyPathMsg:
		// #1173: resolve the subject — explorer selection when the explorer
		// is focused, else the focused editor's file — and copy the wanted
		// form with a toast (#252 pattern).
		return m, m.copyPath(msg.Kind)

	case explorer.FileDeletedMsg:
		// The explorer removed a path; close any editor still showing it so a
		// deleted file does not linger in an open pane — and drop its stale
		// findings from the Problems store (#1102).
		m.closeEditorsForPath(msg.Path, msg.IsDir)
		m.probStore.Drop(msg.Path, msg.IsDir)
		m.dropRawDiags(msg.Path, msg.IsDir)
		m.refreshProblemsPanel()
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

	case ImportOpenAPIMsg:
		// http.importOpenAPI (palette, #1939): prompt for an OpenAPI 3.x
		// document, then generate the .http file beside it.
		m.startOpenAPIImport()
		return m, nil

	case openAPICheckDoneMsg:
		// A finished URL discovery (#2009): arm the confirm or turn the
		// prompt red with the reason.
		return m.finishOpenAPICheck(msg)

	case openAPIImportDoneMsg:
		// The finished import: toast the summary and open the generated file.
		return m.finishOpenAPIImport(msg)

	case ImportCurlMsg:
		// http.importCurl (palette, #1994): prompt for a curl command, then
		// append the equivalent request block to the focused .http file.
		m.startCurlImport()
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
		case explorer.NewFileMsg, explorer.NewDirMsg, explorer.DeleteMsg, explorer.RenameMsg,
			explorer.SearchMsg:
			// The speed search (#1087) equally captures keys only while the
			// tree holds focus, so a palette invocation moves focus first.
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
	case TabCloseOthersMsg:
		// editor.tab.closeOthers (tab context menu / palette, #1128): keep
		// only the active tab; dirty tabs stay open.
		m.closeOtherTabs()
		return m, nil
	case TabTogglePinMsg:
		// editor.tab.togglePin (tab context menu / palette, #1172): flip the
		// active tab's pin, exempting it from LRU eviction and Close Others.
		m.togglePinTab()
		return m, nil

	case ClosePaneMsg:
		// pane.close (pane-title context menu / palette, #1128): close the
		// focused pane whole, behind the unsaved-changes guard.
		m.guardedClosePane()
		return m, nil

	case ForceCodeInsightMsg:
		// editor.forceCodeInsight (palette): override the large-file
		// degradation (#149) for the focused document.
		return m.forceCodeInsight()

	case ShowKeymapHelpMsg:
		// palette.keymapHelp (f1, cmd+k cmd+s / palette): the cheatsheet overlay.
		m.openHelp()
		return m, nil

	case KeymapDoctorMsg:
		// keymap.doctor (palette / settings): the in-app probe overlay
		// (#2080). The settings panel closes first — the doctor is
		// full-screen and must own every raw key.
		if m.settings.IsOpen() {
			m.settings.Close()
		}
		m.keyDoctor.SetSize(m.width, m.height)
		m.keyDoctor.Open(keymap.TerminalID(os.Getenv))
		return m, nil

	case keydoctor.ResultMsg:
		// A finished doctor run (#2080): a saved run becomes this terminal's
		// stored override set and installs immediately; the config reload
		// rebuilds the binding table so every Fragile flag re-derives from
		// the probed truth. A discarded run changes nothing.
		if !msg.Save {
			return m, nil
		}
		store := keymap.LoadProbeStore(keymap.ProbeStorePath())
		store.Set(msg.Terminal, time.Now().UTC().Format(time.RFC3339), msg.Results)
		if err := store.Save(keymap.ProbeStorePath()); err != nil {
			m.host.Notify(host.Warn, "keymap doctor: saving probe results: "+err.Error())
			return m, nil
		}
		keymap.SetProbeVerdicts(store.Results(keymap.TerminalID(os.Getenv)))
		m.host.Notify(host.Info, "keymap doctor: saved probe results for "+msg.Terminal)
		opts := m.cfgOpts
		return m, func() tea.Msg {
			cfg, diags := config.Load(opts)
			return config.ConfigReloadedMsg{Config: cfg, Diags: diags}
		}

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

	case settings.PreviewMsg:
		// A staged settings value shown without being written (#1296). Only
		// keys whose whole point is their appearance preview; discarding the
		// batch sends the previous value back through here.
		if msg.Key == "theme.name" {
			m.previewTheme(msg.Value)
		}
		return m, nil

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

	case SaveLayoutPromptMsg:
		// window.saveLayout (palette, #1175): pick the panes the layout pins
		// (#1568), then name the snapshot.
		m.startLayoutSelect()
		return m, nil

	case ShowLayoutsMsg:
		// window.layouts / window.setDefaultLayout (palette, #1175).
		m.openLayoutPicker(msg.SetDefault)
		return m, nil

	case ApplyLayoutMsg:
		// A picker row activated (#1175): re-shape the active workspace.
		m.applyLayoutByName(msg.Name)
		return m, nil

	case SetDefaultLayoutMsg:
		m.setDefaultLayout(msg.Name)
		return m, nil

	case DeleteLayoutMsg:
		// The picker's shift+delete aux action (#1175), in place like #1113.
		m.deleteLayout(msg.Name)
		return m, nil

	case RestoreDefaultLayoutMsg:
		// window.restoreLayout (shift+f12, #1175): JetBrains' Restore Default
		// Layout — the designated default, or the built-in pair.
		m.applyDefaultLayout()
		return m, nil

	case ZenModeMsg:
		// view.zenMode (ctrl+alt+f / View menu, #359): maximize + no chrome.
		m.toggleZen()
		return m, nil

	case SplitViewMsg:
		// editor.splitViewRight / editor.splitViewDown (#147): second shared
		// view of the focused editor's document.
		return m.splitView(msg.Zone)

	case MemoryStatsMsg:
		// diag.memoryStats (#1537): scavenge, then toast the heap summary.
		return m.showMemoryStats()

	case HeapDumpMsg:
		// diag.heapDump (#1537): write goroutine + heap profiles.
		return m.writeHeapDump()

	case TogglePerfHUDMsg:
		// perf.hud (ctrl+alt+p / View menu, #1999): show or hide the HUD and
		// start or stop the measurement hooks with it.
		return m, m.togglePerfHUD()

	case perfTickMsg:
		// One HUD measurement window closed (#1999); re-arms while open.
		return m, m.perfTick()

	case PerfSnapshotMsg:
		// perf.snapshot (#1999): the numbers as a plain-text block, ready to
		// paste into a bug report.
		return m.copyPerfSnapshot()

	case NewScratchMsg:
		// scratch.new.<lang> (#351): create under the scratch store, open
		// through the standard funnel.
		return m.newScratch(msg.Ext)

	case ShowBufferLangMsg:
		// editor.setBufferLanguage (alt+enter intention / palette, #2033):
		// pick the language a file-less buffer is treated as, locked to the
		// picker mode.
		m.openBufferLangPicker()
		return m, nil

	case SetBufferLangMsg:
		// One picked language installed on the focused file-less buffer
		// (#2033); the returned command reparses under the new type.
		return m, m.setBufferLang(msg)

	case MaterializeBufferMsg:
		// editor.materializeBuffer (alt+enter intention / palette / View
		// menu, #2056): write the typed file-less buffer to a scratch file of
		// its extension and bind it there, so LSP and friends apply.
		return m, m.materializeBuffer()

	case ShowNewScratchMsg:
		// scratch.new (cmd+shift+n / File menu, #1223): pick the language
		// first, locked to the picker mode; the chosen row runs the matching
		// scratch.new.<id> command.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{
			ContextID: m.focusContext(),
			Root:      ".",
		}, scratchNewPrefix)
		return m, nil

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
		// terminal.newTab (palette / menu, #573; terminal-context ctrl+t,
		// #1794): with the popup terminal open the new tab joins the popup;
		// with a terminal focused the new session opens as its sibling tab
		// (iTerm's cmd+t, #729); otherwise a shell tab joins the active
		// editor pane, next to the file tabs.
		switch {
		case m.popupLayerOpen():
			m.newPopupTerminalTab()
		case m.focusContext() == string(keymap.Terminal):
			m.newTerminalSibling()
		default:
			m.openTerminalTab()
		}
		return m, nil

	case NewEditorTabMsg:
		// editor.tab.new (editor-context ctrl+t, #1794): a fresh empty
		// editor tab in the focused (else the active) editor pane.
		m.newEditorTab()
		return m, nil

	case RunFileMsg:
		// run.file (shift+f10 / Run menu / palette, #576): run the active
		// file through its run configuration.
		return m, m.runCurrentFile()

	case RunRerunMsg:
		// run.rerun (Run menu / palette, #576): repeat the last run.
		return m, m.rerunLast()

	case RunTestAtCursorMsg:
		// run.testAtCursor (Run menu / palette / editor context menu /
		// ctrl-or-cmd+click on the gutter run marker, #1150).
		return m, m.runTestAtCursor()

	case RunTestsInFileMsg:
		// run.testsInFile (Run menu / palette, #1150): the file's package tests.
		return m, m.runTestsInFile()

	case RunTestsWithCoverageMsg:
		// run.testsWithCoverage (palette, #2081): the file's package tests
		// with per-line coverage collection.
		return m, m.runTestsWithCoverage()

	case CoverageToggleMsg:
		// coverage.toggle (palette, #2081): hide/show the coverage gutter
		// marks; the data survives a hide.
		return m, m.toggleCoverageMarks()

	case HTTPRunMsg:
		// http.run (Run menu / palette / cmd+enter, #1250): dispatch the
		// .http request under the cursor off-loop.
		return m, m.runHTTPRequestAtCursor()

	case HTTPResponseMsg:
		// One dispatch finished (#1250): open/reuse the response viewer, and
		// report what the request's capture directives took out of the
		// response (#1993).
		return m, m.fillHTTPPanel(msg)

	case HTTPStreamStartMsg:
		// A streaming response's headers arrived (#1776): show them live and
		// keep pumping the dispatch's event channel.
		m.beginHTTPStream(msg)
		return m, nextHTTPEvent(msg.events)

	case HTTPStreamChunkMsg:
		// One live chunk of a running stream (#1776).
		m.appendHTTPStream(msg)
		return m, nextHTTPEvent(msg.events)

	case httpTickMsg:
		// Keep the in-flight indicator moving while dispatches run (#1272) —
		// the statusline segment reads the flight set directly, the inline
		// markers in the .http file are refreshed here (#1746).
		m.refreshHTTPFlightMarks()
		if len(m.httpFlight) == 0 {
			return m, nil
		}
		return m, tea.Tick(httpFlightTick, func(time.Time) tea.Msg { return httpTickMsg{} })

	case HTTPCancelMsg:
		// http.cancel (palette, x in the response pane, #1272).
		m.cancelHTTPRequests()
		return m, nil

	case HTTPResponseHistoryMsg:
		// http.responseHistory (palette, #1267): focus the viewer, say how
		// many stored responses are browsable.
		m.showHTTPHistory()
		return m, nil

	case HTTPShowResponseMsg:
		// http.showResponse (palette, #1492): show the stored responses of
		// the request under the cursor without dispatching it.
		m.showStoredHTTPResponse()
		return m, nil

	case HTTPResendMsg:
		// http.resend (palette, #1832): the shown response's stored request
		// goes out again, verbatim.
		return m, m.resendHTTPRequest()

	case httppane.ResendMsg:
		// ctrl+r or the header affordance in the response pane (#1832): the
		// pane holds the snapshot, the host dispatches it.
		return m, m.resendHTTPRequest()

	case HTTPCopyBodyMsg:
		// http.copyBody (palette, #1266): the shown body to the clipboard.
		return m, m.copyHTTPResponse(false)

	case HTTPCopyHeadersMsg:
		// http.copyHeaders (palette, #1266): status line plus headers.
		return m, m.copyHTTPResponse(true)

	case HTTPCopyShownAsCurlMsg:
		// http.copyShownAsCurl (palette, #2059): the shown response's as-sent
		// request as a runnable curl command.
		return m, m.copyShownHTTPRequestAsCurl()

	case httppane.CopyCurlMsg:
		// "C" in the response pane (#2059): the pane holds the snapshot, the
		// host renders and copies it.
		return m, m.copyShownHTTPRequestAsCurl()

	case HTTPSaveResponseMsg:
		// http.saveResponse (palette, #2059): prompt for a path, then write
		// the raw body there.
		m.startHTTPSaveResponse()
		return m, nil

	case httppane.SaveBodyMsg:
		// "S" in the response pane (#2059): the same prompt, pane-local entry.
		m.startHTTPSaveResponse()
		return m, nil

	case HTTPCopyAsCurlMsg:
		// http.copyAsCurl (palette, #1994): the request under the caret, with
		// its variables substituted, as a runnable curl command.
		return m, m.copyHTTPRequestAsCurl()

	case InsertCurlAsRequestMsg:
		// http.insertCurlAsRequest (intention popup, #2020): the caret
		// line's curl command becomes an .http block — in place, or in a
		// fresh scratch file.
		return m.insertCurlAsRequest()

	case HTTPCopyFoldMsg:
		// http.copyFold (palette, #1787): the target fold, hidden rows and all.
		return m, m.copyHTTPFold()

	case HTTPDiffResponsesMsg:
		// http.diffResponses (palette, #1992): pick a second stored response
		// and open the pair side by side.
		m.openHTTPResponseDiff()
		return m, nil

	case httppane.DiffHistoryMsg:
		// "D" in the response pane (#1992): same picker, reached without
		// knowing the command.
		m.openHTTPResponseDiff()
		return m, nil

	case HTTPDiffPreviousRunMsg:
		// http.diffPreviousRun (palette, #2060): skip the picker and diff the
		// shown response directly against the run before it.
		m.openHTTPPreviousRunDiff()
		return m, nil

	case httppane.DiffPreviousRunMsg:
		// "P" in the response pane (#2060): same direct diff, reached without
		// knowing the command.
		m.openHTTPPreviousRunDiff()
		return m, nil

	case DiffHTTPEntriesMsg:
		// A row of that picker was chosen (#1992): the two stored responses
		// open in the reusable diff pane, JSON-normalized.
		m.diffHTTPEntries(msg)
		return m, nil

	case httppane.PickRequestMsg:
		// "r" in the response pane (#1829): list the .http file's requests
		// that have stored responses and switch the pane to the chosen one.
		m.openHTTPRequestPicker()
		return m, nil

	case HTTPSelectEnvMsg:
		// http.selectEnvironment (palette, #1867): choose which
		// http-client.env.json environment the file's {{name}} placeholders
		// resolve against.
		m.openHTTPEnvPicker()
		return m, nil

	case SelectHTTPEnvMsg:
		// A row of that picker was chosen (#1867): persist it for the .http
		// file's directory; the next dispatch resolves against it.
		m.selectHTTPEnv(msg)
		return m, nil

	case ShowStoredHTTPResponseMsg:
		// A picker row was chosen (#1829): same loading path as
		// http.showResponse, just named by the picker instead of the cursor.
		m.loadStoredHTTPResponse(msg.Source, msg.Request)
		return m, nil

	case httppane.CancelMsg:
		// "x" in the response pane (#1272): the pane cannot reach the
		// dispatch context, so the host aborts on its behalf.
		m.cancelHTTPRequests()
		return m, nil

	case httppane.CopyMsg:
		// The response viewer asks the host for the clipboard (#1266) — the
		// pane never touches globals itself.
		m.copyToClipboard(msg.Text)
		m.host.Notify(host.Info, "copied "+msg.What)
		return m, nil

	case diff.CopyMsg:
		// The diff and merge views ask the host for the clipboard the same
		// way (#2070): a mouse selection or the current hunk as a patch.
		m.copyToClipboard(msg.Text)
		m.host.Notify(host.Info, "copied "+msg.What)
		return m, nil

	case DebugToggleBreakpointMsg:
		// debug.toggleBreakpoint (ctrl+f8 / Run menu / palette, #577).
		m.toggleBreakpointAtCursor()
		return m, nil

	case DebugStartMsg:
		// debug.start (shift+f9 / Run menu / palette, #579).
		m.startDebug()
		return m, nil

	case DebugTestAtCursorMsg:
		// debug.testAtCursor (Run menu / palette, #1914).
		m.debugTestAtCursor()
		return m, nil

	case RunSelectMsg:
		// run.select (Run menu / palette, #1914): the run-configuration picker.
		m.openRunConfigPicker()
		return m, nil

	case RunConfigPickedMsg:
		// A picker row was activated (#1914): run or debug the configuration.
		return m, m.runPickedConfig(msg)

	case TaskSelectMsg:
		// run.task (Run menu / palette, #1915): the Run Task picker.
		m.openTaskPicker(false)
		return m, nil

	case TaskPromoteMsg:
		// run.taskPromote (#1915): store a task as a run configuration.
		m.openTaskPicker(true)
		return m, nil

	case SSHPickerMsg:
		// terminal.ssh (palette / Terminal menu, #1938): the ssh_config host
		// picker.
		m.openSSHPicker()
		return m, nil

	case SSHHostPickedMsg:
		// A host row was activated (#1938): a terminal running `ssh <host>`.
		m.openSSHTerminal(msg.Host)
		return m, nil

	case RemoteBrowseMsg:
		// remote.browse (palette / Tools menu, #1997): the same host list,
		// picked for SFTP browsing instead of a terminal.
		m.openRemotePicker()
		return m, nil

	case RemoteHostPickedMsg:
		// A host row was activated (#1997): the host's SFTP browser pane.
		return m, m.openRemotePane(msg.Host)

	case remote.ResultMsg:
		// A browser's background dial or directory scan landed (#1997).
		return m, m.remoteResult(msg)

	case remote.OpenFileMsg:
		// A remote file was activated (#1997): download it into the cache.
		return m, m.openRemoteFile(msg)

	case remoteFetchedMsg:
		// The download landed (#1997): viewer dispatch or read-only buffer.
		return m, m.remoteFetched(msg)

	case TaskPickedMsg:
		// A task row was activated (#1915): run it or promote it.
		return m, m.runPickedTask(msg)

	case TaskProblemsMsg:
		// A task run's matchers found problems (#1915): the source replaces
		// wholesale, so the panel always mirrors the run's current output.
		m.probStore.SetTaskSource(msg.Source, msg.ByPath)
		m.refreshProblemsPanel()
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
		// Raw adapter events (initialized, stopped, output, terminated, …),
		// routed by owning session (#1523): a parked workspace's events never
		// touch the active session's state.
		m.handleDebugEvent(msg.sess, msg.ev)
		return m, nil

	case debugEventBatchMsg:
		// A parked session's coalesced output window (#1557): all events apply
		// in one Update pass instead of one pass each.
		for _, ev := range msg.evs {
			m.handleDebugEvent(msg.sess, ev)
		}
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
			mm.fetchScopes(top.ID, top.Source.Path)
			mm.refreshWatches()
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
		// — navigate the editor to its location and re-scope the variables;
		// watches and inline values follow the selected frame (#1914).
		if m.dbg != nil {
			m.dbg.curFrameID = msg.Frame.ID
		}
		m.fetchScopes(msg.Frame.ID, msg.Frame.Source.Path)
		m.refreshWatches()
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

	case debugpanel.AddWatchMsg, debugpanel.EditWatchMsg, debugpanel.RemoveWatchMsg:
		// A watch mutation from the panel (#1914): the model owns the list.
		m.handleWatchMsg(msg)
		return m, nil

	case debugWatchesMsg:
		// Evaluated watch results; a stale session's are dropped (#1523).
		if m.dbg != nil && msg.sess == m.dbg.sess {
			if p := m.debugPanel(); p != nil {
				p.SetWatches(msg.results)
			}
		}
		return m, nil

	case debugLocalsMsg:
		// The selected frame's Locals render as inline values (#1914).
		m.applyInlineValues(msg)
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

	case CompareClipboardMsg:
		// diff.compareWithClipboard (palette, #1477): the active buffer — or
		// the visual selection, when one is active — against the clipboard.
		m.compareWithClipboard()
		return m, nil

	case diff.JumpMsg:
		// enter on a hunk: open the diff's right-hand file with the cursor on
		// the hunk's first line (JumpMsg lines are 1-based, openPathAt 0-based).
		return m.openPathAt(msg.Path, msg.Line-1, 0)

	case preview.RenderTickMsg:
		// A preview's debounce timer fired: route it to the owning viewer —
		// dedicated pane or content tab (#1778), matched by the model's own
		// key so a re-keyed pane still receives it — which renders only when
		// the tick is still the newest one.
		if _, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
			return c.Kind() == pane.KindMarkdown && c.Preview().Key() == msg.Key
		}); ok {
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

	case TestsToggleMsg:
		// tests.toggle (#1911): same state machine for the Test Results pane.
		m.toggleTestsPanel()
		return m, nil

	case IssuesToggleMsg:
		// issues.toggle (#1934): same state machine for the GitHub Issues
		// pane; opening returns the first fetch command.
		return m, m.toggleIssuesPanel()

	case forge.IssuesMsg:
		// A finished issue/PR fetch (#1934) lands in the pane; a background
		// poll's result (#2085) also folds into the poll service, which turns
		// it into snapshot events, backoff and the degrade/recover toasts. A
		// reveal the forge event dialog asked for (#2086) then runs on the
		// fresh listing and may need the revealed issue's timeline (#2084).
		listing := m.applyForgeListing(msg)
		return m, tea.Batch(listing, m.applyForgeReveal())

	case forge.CachedListingMsg:
		// The persisted listing snapshot (#2108): seeds a not-yet-loaded
		// issues pane so it renders instantly, marked stale, while the real
		// fetch runs. A pane that already holds a fetched listing ignores it.
		if p := m.issuesPanel(); p != nil {
			p.SetCached(msg.Issues, msg.PRs)
		}
		return m, nil

	case forge.PollTickMsg:
		// One background poll deadline (#2085). The handler only dispatches
		// the fetch command — the Update loop never waits on the forge.
		return m, m.forgePollTick(msg)

	case forge.TimelineMsg:
		// One fetched issue-timeline page (#2084) lands in the open detail,
		// which may owe another page while 'r' restores the loaded depth
		// (#2113).
		return m, m.fillIssuesTimeline(msg)

	case forge.RepoMetaMsg:
		// Capabilities plus the repository's labels and assignable users
		// (#2088): the gate in front of the mutation actions.
		m.fillIssuesMeta(msg)
		return m, nil

	case forge.MutationMsg:
		// One finished label/assignee/state write (#2088): the pane rolls
		// back on a rejection and refetches on success.
		return m, m.finishIssueMutation(msg)

	case forge.PRDetailMsg:
		// One fetched full pull request (#2089) lands in the open PR detail.
		m.fillPRDetail(msg)
		return m, nil

	case forge.PRActionMsg:
		// One finished PR merge/close (#2089): the pane surfaces the forge's
		// reason on a rejection and refetches on success.
		return m, m.finishPRAction(msg)

	case ghissues.CleanupRequestMsg:
		// The accepted post-merge cleanup offer (#2089): delete the issue
		// branch locally and on origin, back to an up-to-date default branch.
		return m, forge.CleanupBranchCmd(".", msg.Branch)

	case forge.CleanupDoneMsg:
		return m, m.finishBranchCleanup(msg)

	case forge.EventsMsg:
		// Snapshot-diff events from the forge poller (#2085) reach their
		// surface: dialog, status-line badge, toast, or history only (#2086).
		return m, m.handleForgeEvents(msg)

	case ghissues.StartWorkRequestMsg:
		// The pane's 's' action (#1934): branch issue/<n>-<slug> off an
		// up-to-date default branch.
		return m, forge.StartWorkCmd(".", msg.Number, msg.Title)

	case forge.StartWorkDoneMsg:
		return m, m.finishStartWork(msg)

	case ghissues.OpenURLMsg:
		// The pane's 'o' action (#1934): the issue page in the browser.
		return m, m.openIssueURL(msg.URL)

	case ghissues.EditTextRequestMsg:
		// The pane's 'e'/'c' actions (#2087): open a markdown buffer bound to
		// the forge text, prefilled with what the pane knows it holds.
		return m.openForgeEdit(msg)

	case forgeEditSavedMsg:
		// A saved buffer bound to a forge text pushes it (#2087); an unbound
		// path is the ordinary case and resolves to nil.
		return m, m.pushForgeEdit(msg.path, false)

	case forge.SaveTextMsg:
		// One finished push (#2087): closed and refreshed on success, kept
		// with a dialog on a stale base or an error.
		return m, m.finishForgeEdit(msg)

	case BreakpointsToggleMsg:
		// debug.breakpoints (#1377): same state machine for the Breakpoints
		// tool window.
		m.toggleBreakpointsPanel()
		return m, nil

	case breakpanel.OpenLocationMsg, breakpanel.ToggleEnabledMsg,
		breakpanel.RemoveMsg, breakpanel.RemoveAllMsg:
		// Breakpoints-list actions (#1377): jump, enable/disable, delete,
		// delete-all — the root model mutates the store so the gutter, the
		// persistence file and a live session stay in sync.
		model, cmd, _ := m.handleBreakpanelMsg(msg)
		return model, cmd

	case UsagesToggleMsg:
		// usages.toggle (#1155): same state machine for the Usages pane.
		m.toggleUsagesPanel()
		return m, nil
	case OpenInFindPanelMsg:
		// find.openInPanel (#2055): tip the open overlay's hits into the
		// persistent panel — "Open in Find Window".
		m.openInFindPanel()
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

	case DOMToggleMsg:
		// dom.toggle (#1929): same state machine for the DOM inspector; the
		// Update wrapper's sync parses the buffer and routes the highlights.
		m.toggleDOMPanel()
		return m, nil

	case DebugDoctorMsg:
		// debug.doctor (#1991): same state machine for the Xdebug Doctor.
		m.toggleDoctorPanel()
		return m, nil

	case debugdoctor.ClearMsg:
		// 'c' in the doctor pane (#1991): drop the connection trace; the
		// listener status stays.
		m.doctorLog.Clear()
		return m, nil

	case domParsedMsg:
		// An async DOM parse finished (#1929) — fill the open panel.
		m.applyDOMParsed(msg)
		return m, nil

	case domview.NavigateMsg:
		// Enter / double-click on a DOM node row (#1929): jump the editor to
		// the node's source position through the standard open funnel.
		return m.openPathAt(msg.Path, msg.Line, msg.Col)

	case ScratchSectionFocusMsg:
		// scratch.panel, re-pointed (#1963): the pane became the explorer's
		// Scratches section, so the command focuses the explorer and puts the
		// cursor on the section's first entry.
		m.focusScratchSection()
		return m, nil

	case explorer.ScratchNewMsg:
		// The explorer's new-file affordance on the Scratches section (#1963)
		// delegates to scratch.new: the language picker, then the store.
		return m.Update(ShowNewScratchMsg{})

	case domview.CopyMsg:
		// Copy actions on a DOM node (#1929): selector path or outer HTML.
		m.copyToClipboard(msg.Text)
		m.host.Notify(host.Info, "copied "+msg.What)
		return m, nil

	case diff.EditRequestMsg:
		// 'e' in a diff pane (0340, #496): mount a live editor as the right
		// column. Revision-only diffs (the log's parent-vs-commit view) stay
		// read-only with a hint. The diff may live in a tab (#1778), so it is
		// matched by the model's own key.
		_, _, inst, ok := m.findContent(func(c *pane.Instance) bool {
			return c.Kind() == pane.KindDiff && c.Diff().Key() == msg.Key
		})
		if !ok || inst.DiffEditor() != nil {
			return m, nil
		}
		if !inst.Diff().Editable() || msg.Path == "" {
			m.host.Notify(host.Info, "this diff compares revisions — read-only")
			return m, nil
		}
		ed := editor.New()
		ed.SetRegisters(m.regs) // app-wide registers (#1540)
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
		// the focused diff's hunk — dedicated pane or active content tab
		// (#1778); a non-diff focus is a quiet no-op (the bindings are
		// diff-scoped, so this is belt and braces).
		if inst := m.focusedContent(); inst != nil && inst.Kind() == pane.KindDiff {
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

	case TerminalPopupMsg:
		// terminal.popup: show/hide the floating popup terminal (#1398).
		m.togglePopupTerminal()
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

	case terminal.AutoScrollMsg:
		// A selection drag rests past a pane edge (#1821): one more scroll
		// step, the selection following. The terminal drops the tick once the
		// drag ends or the history runs out, which stops the repeat.
		if m.drag == nil || m.drag.kind != dragTermSelect {
			return m, nil
		}
		if term := m.dragTerminal(m.drag.srcPane); term != nil {
			return m, term.AutoScroll(msg)
		}
		return m, nil

	case terminal.ExitedMsg:
		// The shell ended: close its pane like ctrl+w would; when the layout
		// refuses (last leaf), the pane stays showing [process exited]. A
		// command session (#576) stays open instead — its output is the point
		// of the run; terminal tabs (#573) stay open the same way.
		if inst, idx, t := m.popupTabForSession(msg.Key); t != nil {
			// A popup terminal shell ended (#1398): its tab closes; the last
			// tab drops the whole popup, and the next toggle spawns fresh.
			m.closePopupTab(inst, idx)
			return m, nil
		}
		// A global tool that exited while detached (#1890) has no pane; the
		// dead session stays parked with its exit status (#1903), so the next
		// switch-in materializes the #810 exited overlay instead of the tool
		// silently vanishing.
		if m.parkedGlobalToolExited(msg.Key) {
			return m, nil
		}
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
		// accepted directory as the query, so enter descends like tab. The
		// msg names the mode to return to — the '@' finder's path queries
		// descend within '@' (#1433), the ';' picker within ';'.
		prefix := msg.Prefix
		if prefix == 0 {
			prefix = palette.OpenPathPrefix
		}
		// A descend out of the editor's anchored '@' finder stays anchored
		// (#1775): re-derive the anchor from the still-focused pane.
		anchored := m.palette.Anchored()
		m.palette.SetSize(m.width, m.height)
		cx := palette.Context{ContextID: m.focusContext(), Root: "."}
		if x, y, w, ok := m.paneAnchor(); anchored && ok {
			m.palette.OpenAnchoredWith(cx, prefix, msg.Query, x, y, w)
			return m, nil
		}
		m.palette.OpenLockedWith(cx, prefix, msg.Query)
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

	case palette.RemoveRecentFileMsg:
		// Prune-from-list (#1113, mirroring the project picker's #842): the
		// aux action of a recent-files row drops the entry from the MRU. The
		// session persists immediately so the removal survives a kill/crash,
		// and the still-open palette re-lists without the entry.
		m.recent.Remove(msg.Path)
		saveSession(m.snapshotSession())
		m.palette.Refresh()
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

	case ShowBookmarksMsg:
		// nav.bookmarks (palette, #1151): the focused editor's local marks
		// plus the persistent global marks, locked to the bookmarks mode.
		m.bookmarks.Set(m.focusedEditor(), m.gmarks, m.bmarks)
		if len(m.bookmarks.items) == 0 {
			m.host.Notify(host.Info, "no bookmarks yet — toggle one with Toggle Bookmark, or set a vim mark with m + a letter (A-Z survives restarts)")
			return m, nil
		}
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, bookmarksPrefix)
		return m, nil

	case BookmarkToggleMsg:
		// bookmark.toggle (#55): flip the anonymous bookmark on the focused
		// editor's cursor line, persisting immediately.
		m.toggleBookmarkAtCursor()
		return m, nil

	case BookmarkMnemonicMsg:
		// bookmark.toggleMnemonic / bookmark.jumpMnemonic (#55): the digit
		// prompt, assigning or jumping.
		kind := bmPromptMnemonic
		if msg.Jump {
			kind = bmPromptJump
		}
		m.startBookmarkPrompt(kind)
		return m, nil

	case BookmarkNoteMsg:
		// bookmark.annotate (#55): the note prompt on the cursor line.
		m.startBookmarkPrompt(bmPromptNote)
		return m, nil

	case BookmarkStepMsg:
		// bookmark.next / bookmark.previous (#55): step through the project's
		// bookmarks in (path, line) order, wrapping at both ends.
		return m.stepBookmark(msg.Delta)

	case BookmarkJumpMsg:
		// A picker row was chosen: local marks jump within the focused
		// editor, global marks route through the standard open funnel so
		// the navigation history records the departure.
		if msg.Local {
			if ed := m.focusedEditor(); ed != nil {
				ed.JumpToLocalMark(msg.Letter)
			}
			return m, nil
		}
		return m.openPathAt(msg.Path, msg.Line, msg.Col)

	case BookmarkRemoveMsg:
		// The aux action (#1151, the #842/#1113 prune pattern): drop the
		// mark, keep the palette open, re-list without the entry.
		switch {
		case msg.Local:
			if ed := m.focusedEditor(); ed != nil {
				ed.RemoveLocalMark(msg.Letter)
			}
		case msg.Project:
			m.bmarks.Remove(msg.Path, msg.Line)
			m.saveBookmarks()
		default:
			m.gmarks.Remove(msg.Letter)
		}
		m.bookmarks.Set(m.focusedEditor(), m.gmarks, m.bmarks)
		m.palette.Refresh()
		return m, nil

	case editor.OpenPathMsg:
		// "gf" (#1193): resolve the name under the cursor against the
		// requesting file's directory, then the working root, and open the
		// first existing file through the standard funnel.
		var candidates []string
		if filepath.IsAbs(msg.Path) {
			candidates = []string{msg.Path}
		} else {
			if msg.From != "" {
				candidates = append(candidates, filepath.Join(filepath.Dir(msg.From), msg.Path))
			}
			if cwd, err := cachedGetwd(); err == nil {
				candidates = append(candidates, filepath.Join(cwd, msg.Path))
			}
		}
		for _, p := range candidates {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return m.openPathAt(p, 0, 0)
			}
		}
		m.host.Notify(host.Info, "gf: no file "+msg.Path)
		return m, nil

	case editor.GlobalMarkJumpMsg:
		// '{A-Z} / `{A-Z} in an editor (#1151): resolve against the store
		// and open through the standard funnel — cross-file jumps open the
		// file, same-file jumps record in the nav history. The quote form
		// then settles on the line's first non-blank.
		mk, ok := m.gmarks.Get(msg.Letter)
		if !ok {
			m.host.Notify(host.Info, "mark "+string(msg.Letter)+" not set")
			return m, nil
		}
		col := 0
		if msg.Exact {
			col = mk.Col
		}
		model, cmd := m.openPathAt(mk.Path, mk.Line, col)
		if !msg.Exact {
			if mm, ok := model.(Model); ok {
				if ed := mm.editorForPath(canonicalPath(mk.Path)); ed != nil {
					ed.MoveToFirstNonBlank()
				}
			}
		}
		return model, cmd

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

	case project.OpenPeekPickerMsg:
		// project.peek (#2136): the same recent-projects list, locked to the
		// peek flavour — plain activation peeks, alt+enter switches for real.
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, project.PeekPickerPrefix)
		return m, nil

	case project.PeekPickedMsg:
		// Peek activation (#2136): validate off the Update loop; the result
		// comes back as PeekProjectMsg or SwitchFailedMsg.
		return m, project.PeekTo(msg.Path)

	case project.PeekProjectMsg:
		return m.handlePeekProject(msg)

	case project.PeekReturnMsg:
		return m.handlePeekReturn()

	case project.PeekKeepMsg:
		return m.handlePeekKeep()

	case project.OpenCloneMsg:
		// project.clone (palette / File menu): the two-field clone dialog.
		m.startClonePrompt()
		return m, nil

	case vcs.CloneDoneMsg:
		// The clone finished: switch to the fresh checkout or show the error.
		return m.finishClone(msg)

	case regexEvalDoneMsg:
		// An off-loop regex evaluation came back (#1937); a stale generation
		// is dropped by finishRegexEval.
		m.finishRegexEval(msg)
		return m, nil

	case OpenRegexTesterMsg:
		// tools.regexTester (palette / Tools menu): the floating regex tester,
		// prefilled from the editor's visual selection when there is one.
		return m, m.startRegexTester()

	case OpenPlaygroundMsg:
		// json.jqPlayground / yaml.yqPlayground (palette / Tools menu, #1936,
		// #2039): the query line, mounted inline in the buffer or HTTP
		// response pane at hand (#1970), on `.` or this input's last valid
		// program (#1982).
		return m, m.startPlayground(msg.Dialect, false)

	case OpenPlaygroundAtPathMsg:
		// json.jqPlaygroundAtPath / yaml.yqPlaygroundAtPath (palette / Tools
		// menu, #1982): the same mode, prefilled with the caret's document
		// path in the dialect's own spelling (#1660).
		return m, m.startPlayground(msg.Dialect, true)

	case SaveFilterPromptMsg:
		// json.jqSaveFilter (ctrl+s in the query line, palette / Tools menu,
		// #1995): name the program on the query line and store it in the
		// project or the global filter library of the open playground's
		// dialect (#2039).
		m.startPlaySavePrompt()
		return m, nil

	case ShowFiltersMsg:
		// json.jqFilters / json.jqRenameFilter and their yq twins (ctrl+l in
		// the query line, palette / Tools menu, #1995): the saved-filter
		// picker over one dialect's two libraries, in its insert or its
		// rename spelling.
		m.openPlayFilterPicker(msg.Dialect, msg.Rename)
		return m, nil

	case TogglePlaygroundQueryViewMsg:
		// json.jqQueryView (ctrl+alt+e, palette / Tools menu, #2032): show the
		// whole program over several wrapped rows, or fold it back to the one
		// windowed line.
		m.togglePlayQueryView()
		return m, nil

	case InsertFilterMsg:
		// A picked filter goes on the query line and runs (#1995), opening
		// the playground first when none is up.
		return m, m.insertPlayFilter(msg)

	case RenameFilterPromptMsg:
		// The picker's rename spelling reached an entry (#1995).
		m.startPlayRenamePrompt(msg)
		return m, nil

	case DeleteFilterMsg:
		// The picker's aux action (shift+delete) on a saved filter (#1995);
		// the picker stays open and refreshes in place.
		m.deletePlayFilter(msg)
		return m, nil

	case playParseDoneMsg:
		// The input snapshot finished parsing off the event loop (#1936).
		return m, m.finishPlayParse(msg)

	case playDebounceMsg:
		// The query line went quiet long enough to run the program (#1936);
		// a stale generation means the user kept typing.
		return m, m.firePlayDebounce(msg)

	case playEvalDoneMsg:
		// An off-loop jq evaluation came back (#1936); a stale generation is
		// dropped by finishPlayEval, a current one refreshes the result buffer.
		return m, m.finishPlayEval(msg)

	case project.OpenNewProjectMsg:
		// project.new (palette / File menu, #1718): the new-project wizard.
		m.startNewProjectPrompt()
		return m, nil

	case newProjectDoneMsg:
		// The scaffold finished: open the project or show the error.
		return m.finishNewProject(msg)

	case GenerateScratchMsg:
		// scratch.generate (#2134): the wizard, or — for the per-format
		// commands — a straight generation from that format's preset.
		if msg.Format != "" {
			return m.startPresetGenerate(msg.Format)
		}
		m.startGenerateScratch()
		return m, nil

	case scratchGenDoneMsg:
		// The generation finished: open the scratch or show the error.
		return m.finishGenerateScratch(msg)

	case project.PickedMsg:
		// Picker selection: validate off the Update loop; the result comes
		// back as SwitchProjectMsg or SwitchFailedMsg.
		return m, project.SwitchTo(msg.Path)

	case project.CloseProjectMsg:
		// project.close (#1355): close the current project and resume the MRU
		// background workspace; quit (guarded) when none is open.
		return m.handleCloseProject()

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
		// A peek-enter announces itself with the way back (#2136); the marker
		// is already set — the fresh model carried it out of the transaction.
		if m.peek != nil && m.activeWS().Root == msg.Root {
			m.host.Notify(host.Info, "peeking "+msg.Root+" — "+peekReturnChord(m)+" returns")
			return m, nil
		}
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

	case OpenImageMsg:
		m.openImagePreview(msg.Path)
		return m, nil

	case OpenArchiveMsg:
		// archives.view (#1762): a tar (plain or compressed) opens as an
		// entry list, never as a raw text buffer.
		m.openArchivePane(msg.Path)
		return m, nil

	case OpenGzipMsg:
		// gzip.view (#1763): a plain .gz opens transparently decompressed in a
		// read-only buffer, with the inner file's language and highlighting.
		return m, m.openGzipFile(msg.Path)

	case archview.OpenEntryMsg:
		// Enter on a file row: extract that entry into a read-only buffer.
		model, cmd, _ := m.handleArchviewMsg(msg)
		return model, cmd

	case OpenDataMsg:
		// data.view (#1764): a database file opens as a table browser,
		// never as a raw text buffer. The database itself opens in the
		// background (#1795).
		return m, m.openDataPane(msg.Path)

	case dataview.ResultMsg:
		// A data viewer's background open or row count landed (#1795).
		return m, m.dataResult(msg)

	case dataview.CopyMsg:
		// y in the data viewer's column profile popup (#1940).
		m.copyToClipboard(msg.Text)
		m.host.Notify(host.Info, "copied "+msg.What)
		return m, nil

	case DataColumnProfileMsg:
		// data.columnProfile (#1940): the focused grid column's aggregates.
		return m, m.profileColumn()

	case CSVColumnProfileMsg:
		// csv.columnProfile (#1940): the caret's column in a table-rendered
		// csv/tsv/psv buffer, scanned in the background.
		return m, m.profileCSVColumn()

	case csvProfileMsg:
		return m, m.showCSVProfile(msg)

	case dataview.ShowSchemaMsg:
		// Schema key in the data pane: the table's DDL, read-only.
		return m, m.showTableSchema(msg)

	case OpenESConsoleMsg:
		// es.console.<name> (#1927): the cluster console. The connect runs in
		// the background, like a data viewer's open.
		return m, m.openESPane(msg.Endpoint)

	case espane.ResultMsg:
		// A console's background connect, page or mapping landed (#1927).
		return m, m.esResult(msg)

	case espane.ShowJSONMsg:
		// Mapping/hit/aggregations key in the console: the document, read-only.
		return m, m.showESJSON(msg)

	case espane.OpenQueryMsg:
		// Query key in the console: the index's query buffer, a real file.
		return m.openESQuery(msg)

	case ESRunMsg:
		// es.run in a query buffer: the buffer text becomes the index's
		// active query and the console fetches its first page on it.
		return m, m.runESQuery()

	case uv.KittyGraphicsEvent:
		m.handleKittyGraphics(msg)
		return m, nil

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
		// Number-hint field units (#1685) and custom secret key patterns
		// (#1712) install before the diagnostics are worded: the entries the
		// mapping had to skip (#2008) are part of what this reload reports,
		// and both must run — no short-circuit — so one changing never skips
		// installing the other. The re-parse they need happens below.
		changedUnits := applyNumberHintUnits()
		changedSecrets := applySecretMaskingKeys()
		diags := append(msg.Diags, associationDiags()...)
		diags = append(diags, unitMappingDiags()...)
		m.notifyConfigDiags(diags)
		m.notifyKeymapDiags()             // the reload above rebuilt the binding table
		m.settings.NoteReloadDiags(diags) // inline in the panel too (#891)
		m.palette.Refresh()
		// Diagnostic ignore (#1259) and severity remap (#1503) rules apply
		// live: a rule change re-filters every cached raw set into the
		// Problems store and the open editors. Both compiles must run — no
		// short-circuit — so one changing never skips recompiling the other.
		changedIgnore := m.compileDiagIgnore()
		if m.compileDiagSeverity() || changedIgnore {
			cmds := m.refilterDiagnostics()
			// The conceal stores installed above are already committed, so
			// their re-parse has to ride this batch: leaving on the branch
			// below would drop it, and the next reload reports no change.
			if changedUnits || changedSecrets {
				cmds = append(cmds, m.reparseOpenEditors()...)
			}
			if len(cmds) > 0 {
				return m, tea.Batch(cmds...)
			}
		}
		applyIDColorConfig() // identifier colors (#1626): no re-parse needed
		// The mapping and the key patterns installed above decide which family
		// a literal gets, so a change only lands once the spans are produced
		// again — re-parse every open editor when either moved.
		if changedSecrets || changedUnits {
			return m, tea.Batch(m.reparseOpenEditors()...)
		}
		// Rainbow brackets (#789): a toggle flip re-parses every open editor
		// so the change lands without waiting for the next edit.
		if before := highlight.RainbowEnabled(); before != rainbowConfigured() {
			highlight.SetRainbow(!before)
			return m, tea.Batch(m.reparseOpenEditors()...)
		}
		// Word-level diff emphasis (#1630): same deal — a toggle flip
		// re-parses so open .diff buffers update without waiting for an edit.
		if before := unidiff.WordHighlightEnabled(); before != diffWordsConfigured() {
			unidiff.SetWordHighlight(!before)
			return m, tea.Batch(m.reparseOpenEditors()...)
		}
		return m, nil

	case palette.RunCommandMsg:
		// A palette-window selection — never a keybind invocation — bumps the
		// most-used counter (#773).
		m.cmdUsage.Bump(msg.ID)
		return m, m.RunCommand(msg.ID)

	case palette.OpenFileMsg:
		// A file selection confirmed from Run a Command / Search Everywhere —
		// never the explorer, go-to-file or the anchored "@" finder — bumps
		// the file-usage counter (#1419), mirroring the command bump above.
		if msg.CountUsage {
			m.fileUsage.Bump(msg.Path)
		}
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
		// A palette pick opens where the user is working: a file lands in the
		// focused pane's tab list, and so does a viewer file — the '@' finder
		// on a .duckdb opens the data viewer as a tab in the focused pane
		// rather than splitting one off beside it (#1825).
		return m.openPathFocused(msg.Path)

	case host.OpenModalRequest:
		m.shell.SetContent(ui.ModelContent{Heading: msg.Title, Body: msg.View})
		m.shell.SetSize(m.width, m.height)
		m.shell.Open()
		return m, nil

	case editor.ActionMsg:
		// A registry command drives the focused editor through this message
		// path. The inline playground's result buffer takes it while its
		// pane is focused (#1980) — the Edit menu's copy must act on the
		// visible selection, not the pane's hidden document; mutations bounce
		// off the read-only flag as usual. A focused merge view routes it into
		// its result editor (#1478), so the merge accepts / write work from
		// the palette.
		if s := m.play; s != nil && s.resultEd != nil && m.playFocused() {
			var cmd tea.Cmd
			*s.resultEd, cmd = s.resultEd.Update(msg)
			return m, cmd
		}
		if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindMerge {
			return m, inst.Update(msg)
		}
		if key := m.activeEditorKey(); key != "" {
			cmd := m.activeWS().Panes.Get(key).Update(msg)
			return m, cmd
		}
		return m, nil

	case highlight.SpansMsg:
		// The inline playground's result buffer (#1970) lives outside every
		// pane; parse results for its virtual path route to it directly, and
		// its lint notes never reach the Problems store — a throwaway result
		// is not a project diagnostic.
		if s := m.play; s != nil && s.resultEd != nil && msg.Path == s.dialect.ResultPath() {
			var cmd tea.Cmd
			*s.resultEd, cmd = s.resultEd.Update(msg)
			return m, cmd
		}
		// Async Tree-sitter parse results route to every editor leaf owning the
		// path (background panes and shared-document views included); each pane
		// filters by its own document version. The pass's Go-computed lint
		// notes (#1623/#1654) also feed the Problems store — its own channel,
		// so server publishes and lint findings replace independently — and an
		// empty set legitimately clears the path.
		// A file-less buffer's parse (#2033) travels under its view's key
		// rather than a path; the Problems store stays path-keyed, so such a
		// result feeds it under the empty path exactly as before.
		notesPath := msg.Path
		if editor.IsBufferKey(notesPath) {
			notesPath = ""
		}
		m.probStore.SetNotes(notesPath, editor.NoteDiagnostics(msg.Notes))
		m.refreshProblemsPanel()
		return m, m.routeParse(msg)

	case editor.SyncMsg:
		// Any buffer change also outdates the file's stored coverage (#2081):
		// the flag makes later opens of the file show the marks as stale
		// (each live view detects its own edits by document version).
		m.coverage.MarkStale(msg.Path)
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
		// editor or not — so the tool window aggregates project-wide. Sets pass
		// the ignore filter first (#1259, diag_ignore.go).
		cmd := m.applyDiagnostics(msg.Path, msg.Diagnostics)
		m.refreshProblemsPanel()
		return m, cmd

	case ilsp.DiagnosticsBatchMsg:
		// Coalesced diagnostics (#597): route each document's set to its editor
		// leaf in one Update pass, so a workspace publish storm re-renders once
		// instead of once per file. Unopened paths route to nothing (cheap) but
		// still land in the Problems store (#1024). Sets pass the ignore filter
		// first (#1259, diag_ignore.go).
		var cmds []tea.Cmd
		for _, d := range msg.Items {
			if cmd := m.applyDiagnostics(d.Path, d.Diagnostics); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		m.refreshProblemsPanel()
		return m, tea.Batch(cmds...)

	case ilsp.IgnoreDiagnosticMsg:
		// The editor's "ignore diagnostic under caret" action (#1259).
		return m, m.ignoreDiagnostic(msg.Diagnostic)
	case editor.ConcealRuleMsg:
		// A rule made in the conceal explain popover (#1998): persisted into
		// the store the heuristics already read, so the reload re-parses the
		// open editors and the Settings UI lists it like a hand-written entry.
		return m, m.writeConcealRule(msg)
	case ilsp.CompletionMsg:
		// Routed by key, not path: a buffer with no file is reachable only by
		// its view's ParseKey, and that is what the local engine stamps on a
		// batch (#2048). For a file the key *is* the path, so every view of a
		// shared document is still served.
		return m, m.routeToEditorKey(msg.RouteKey(), msg)
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
	case ilsp.InheritanceMarksMsg:
		return m, m.routeToEditor(msg.Path, msg)
	case ilsp.DefinitionMsg:
		// Navigate to a definition target and place the cursor there. Also the
		// activation msg of a references-list entry (references.go) and of
		// Enter inside the peek popup (#1154).
		return m.openPathAt(msg.Path, msg.Line, msg.Col)

	case ilsp.PeekDefinitionMsg:
		// lsp.peekDefinition (#1154): show the target's surrounding lines in
		// a popup on the focused editor instead of jumping.
		m.openPeek(msg)
		return m, nil

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
	case ChangeFeedMsg:
		// watch.changeFeed (#2000): the session's external file changes with
		// a mini-diff of the selected one.
		m.openChangeFeed()
		return m, nil
	case ExportScreenshotMsg:
		// view.exportScreenshot / view.exportWindowScreenshot (#2001): paint
		// the composed frame — the focused pane, or the whole window — into a
		// PNG off the update loop.
		return m, m.exportScreenshot(msg.Whole)
	case screenshotDoneMsg:
		m.screenshotDone(msg)
		return m, nil

	case TimelineMsg:
		// file.timeline (#1916): snapshots and commits on one axis; the git
		// half loads incrementally behind the already-shown snapshots. The
		// command is taken into a local first — openTimeline mutates m, and
		// the order of a return statement's operands is unspecified.
		cmd := m.openTimeline()
		return m, cmd
	case vcs.FileLogMsg:
		m.timelineFileLog(msg)
		return m, nil
	case timelineDiffMsg:
		m.openTimelineDiff(msg)
		return m, nil
	case timelineRestoreMsg:
		cmd := m.applyTimelineRestore(msg)
		return m, cmd
	case localHistorySnapshotMsg:
		// A buffer save: record the written bytes into the snapshot store —
		// and fire the plugin hook (#1161): EventBufferSaved was never
		// emitted, leaving the lsp plugin's didSave/file-event hook dead in
		// native builds. This funnel sees every save flow (#1023).
		m.recordLocalHistory(msg.path)
		return m, tea.Batch(m.fireHooks(plugin.EventBufferSaved, msg.path)...)

	case NavBackMsg:
		// nav.back (Roadmap 0220): return to the previous recorded position.
		return m.navigateHistory(m.navHist.BackWhere, "no earlier position in the navigation history")
	case NavForwardMsg:
		return m.navigateHistory(m.navHist.ForwardWhere, "no later position in the navigation history")

	case ilsp.CodeActionsMsg:
		// lsp.codeAction (#2020): the offer merges with the built-in
		// intention providers and opens anchored at the caret; picking an
		// entry dispatches actionPickedMsg below. The code-lens picker
		// (Intentions false, #1912) keeps the plain centered list.
		if msg.Intentions {
			m.openIntentions(msg)
			return m, nil
		}
		m.actions.Set(msg)
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, actionsPrefix)
		return m, nil

	case actionPickedMsg:
		// A built-in intention entry names a registered command; an LSP
		// entry runs the bridge-built continuation.
		if id := m.actions.CommandFor(msg); id != "" {
			return m, m.RunCommand(id)
		}
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

	case format.FileRequestMsg:
		// Reformat File (#1401): resolve the formatter registry's provider
		// chain for the active buffer and run the winner (reformat.go).
		return m, m.handleReformat(false)

	case format.RangeRequestMsg:
		return m, m.handleReformat(true)

	case ilsp.FormatEditsMsg:
		// lsp.format / rename / code actions: applied as one undo unit
		// (editor/textedit.go) through exactly ONE view. Views of the same
		// file alias one document (#142), so routing to every view — like
		// diagnostics or highlight spans — applied the edits once per view
		// (#366: rename z -> match1 became match1atch1 with a second view
		// open). The first view applies, the change-sync broadcast converges
		// the others, mirroring replace.go's single-view rule.
		var reparse tea.Cmd
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
			// The applying view bypasses its own Update loop, so its stale
			// highlight/conceal caches must be dropped and a parse scheduled
			// here (#1683); the change-sync broadcast already does the same
			// for the other views.
			reparse = views[0].ReparseEdits()
		}
		if msg.Applied != nil {
			// The save chain's edit-applied signal (#1148): fires after the
			// buffer holds the edits — or immediately when no view owns the
			// path anymore, so a chain never stalls on a closed buffer.
			msg.Applied()
		}
		return m, reparse

	case ilsp.WillRenameDoneMsg:
		// The willRenameFiles round trip for an explorer rename/move finished
		// (#1912): hand the message to the explorer, which performs the
		// deferred FS operation.
		exp := m.explorer()
		var cmd tea.Cmd
		*exp, cmd = exp.Update(msg)
		return m, cmd

	case ilsp.SaveChainDoneMsg:
		// Format/organize-imports on save finished (#1148): every view that
		// parked a manual save behind the chain performs its write now.
		var cmds []tea.Cmd
		for _, ed := range m.editorViewsForPath(msg.Path) {
			if c := ed.CompleteChainedSave(); c != nil {
				cmds = append(cmds, c)
			}
		}
		return m, tea.Batch(cmds...)

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
	case ilsp.UsagesMsg:
		// lsp.referencesPanel (#1155): the persistent counterpart to the
		// palette list above — open the Usages pane and fill it, empty
		// result included (the pane shows its found-nothing state).
		m.fillUsagesPanel(msg)
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
	case ilsp.TypeHierarchyMsg:
		// lsp.typeHierarchy (#1454): the prepared roots open the tree overlay;
		// nothing prepared never reaches here (the bridge toasts instead).
		m.typehier.SetSize(m.width, m.height)
		return m, m.typehier.Open(msg)
	case ilsp.TypeHierarchyItemsMsg:
		// One lazy node expansion; stale replies are dropped inside.
		m.typehier.Apply(msg)
		return m, nil
	case ilsp.DefinitionCandidatesMsg:
		// lsp.definition with several targets (#279): pick, don't guess. The
		// list reuses the references rows; Enter navigates via DefinitionMsg —
		// or peeks the chosen target when the request was a peek (#1154).
		if msg.Peek {
			m.refs.SetPeek(msg.Refs)
		} else {
			m.refs.Set(msg.Refs)
		}
		m.refs.SetPlaceholder("Definitions — pick a target…")
		m.palette.SetSize(m.width, m.height)
		m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, refsPrefix)
		return m, nil
	case ilsp.ImplementationsMsg:
		// lsp.implementations / lsp.goToSuper (#1452): one target navigates
		// straight there, several open the picker with a wording that names
		// the direction. Empty never arrives — the bridge toasts instead.
		if len(msg.Refs) == 0 {
			return m, nil
		}
		if len(msg.Refs) == 1 {
			r := msg.Refs[0]
			return m.openPathAt(r.Path, r.Line, r.Col)
		}
		m.refs.Set(msg.Refs)
		if msg.Super {
			m.refs.SetPlaceholder("Super declarations — pick a target…")
		} else {
			m.refs.SetPlaceholder("Implementations — pick a target…")
		}
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
			}
		}
		// Record it in the change feed (#2000) *before* anything routes the
		// event onward: the pre-change content is read off the open buffer,
		// which the auto-reload below is about to overwrite.
		m.recordChangeFeed(msg)
		// Announce the (kind-fixed) file event to hook subscribers (#1144):
		// the LSP bridge forwards it to the servers as
		// workspace/didChangeWatchedFiles, so Intelephense re-indexes
		// externally created/changed/deleted files.
		hookCmds := m.fireHooks(plugin.EventExternalFileChange, plugin.FileChange{
			Path: msg.Path,
			Kind: fileChangeKind(msg.Kind),
		})
		if msg.Kind == watch.FileRemoved {
			if ed := m.editorForPath(msg.Path); ed != nil && ed.Following() {
				// A followed file disappeared (#1928): rotation in progress,
				// not a close — keep the pane, re-stamp the poll tracker so
				// the replacement file is picked up (Poll dropped the entry
				// when it reported the removal), and let the editor mark the
				// pending rotation.
				if m.watcher != nil {
					m.watcher.Track(msg.Path)
				}
			} else if ed != nil && !ed.Dirty() {
				// Externally deleted, nothing unsaved: same as the
				// explorer's delete flow — close the pane (#83). A dirty
				// buffer instead stays open, marked stale by the editor.
				m.closeEditorsForPath(msg.Path, false)
				return m, tea.Batch(append(hookCmds, vcsCmd)...)
			}
		}
		if msg.Kind == watch.FileChanged || msg.Kind == watch.FileCreated {
			// A gz preview's buffer path names content *inside* the archive
			// (#1763), so the editor's own reload never matches the file that
			// changed: re-decompress it here. The command it returns re-runs
			// the parse the fresh content needs (#1853).
			hookCmds = append(hookCmds, m.refreshGzipBuffers(msg.Path))
		}
		// A merged rotation set's buffer path names no file either (#1996): its
		// followers tail the set's newest member, so the event is routed by
		// follow source rather than by path.
		hookCmds = append(hookCmds, m.routeMergedLogFollow(msg))
		return m, tea.Batch(append(hookCmds, m.routeToEditor(msg.Path, msg), vcsCmd)...)

	case vcsInvalidateMsg:
		// Something changed the working tree (a buffer save, a mutating VCS
		// command): refresh the git status snapshot after the debounce.
		return m, m.scheduleVCSRefresh()

	case vcsTickMsg:
		// The git status debounce expired (Roadmap 0320): run the refresh.
		m.vcs.tickArmed = false
		return m, m.startVCSRefresh()

	case workspaceIdleMsg:
		// A parked workspace may have sat past the background LSP timeout
		// (#1521): stop its servers if it is in fact still parked and idle.
		return m.handleWorkspaceIdle(msg)

	case vcs.SnapshotMsg:
		return m, m.applyVCSSnapshot(msg)

	case vcs.MarksMsg:
		// Recomputed gutter diff markers (#464): to every view of the path.
		return m, m.routeToEditor(msg.Path, msg)

	case coverage.MarksMsg:
		// Coverage gutter marks (#2081): to every view of the path.
		return m, m.routeToEditor(msg.Path, msg)

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
		// A conflicted row opens the three-way merge view instead (#1478).
		if snap.Status(abs) == vcs.StatusConflicted {
			return m, vcs.MergeStagesCmd(snap.Root, abs)
		}
		return m, vcs.HeadDiffCmd(snap.Root, abs)

	case DiffHeadMsg:
		return m.diffAgainstHead()

	case MergeFileMsg:
		return m.mergeFocusedFile()

	case MergeApplyMsg:
		return m.mergeApply()

	case vcs.MergeStagesMsg:
		if msg.Err != nil {
			m.host.Notify(host.Error, "merge: "+msg.Err.Error())
			return m, nil
		}
		m.openMergePane(msg)
		return m, nil

	case vcs.HeadDiffMsg:
		if msg.Err != nil {
			m.host.Notify(host.Error, "diff: "+msg.Err.Error())
			return m, nil
		}
		m.openDiffHeadPane(msg.Path, msg.Head)
		return m, nil

	case HistoryForSelectionMsg:
		// #1430: git log -L over the visual selection (caret line fallback).
		return m.historyForSelection()

	case vcs.RangeLogMsg:
		m.openHistoryPicker(msg)
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

	case editor.FollowMsg:
		// A view entered follow mode (#1928): refresh the poll stamp for its
		// file (so the first poll compares against right now) and arm the
		// follow tick. Leaving needs nothing — the tick self-stops.
		if msg.On && m.watcher != nil {
			m.watcher.Track(msg.Path)
		}
		if msg.On {
			return m, m.armFollowTick()
		}
		return m, nil

	case followTickMsg:
		// The follow poll deadline elapsed (#1928): poll the tracked files
		// and re-arm while a view still follows.
		return m, m.followTick()

	case OpenMergedLogMsg:
		// log.openRotatedSet (#1996): merge the focused buffer's rotation set
		// into one timeline, off the loop.
		return m, m.openMergedLogSet()

	case editor.MergeLogSetMsg:
		// A followed merged timeline saw its newest member rotated (#1996):
		// read the whole set again, the replacement's lines belonging after
		// the ones the buffer holds.
		return m, m.remergeLogSet(msg.Path)

	case mergedLogMsg:
		// An assembled timeline came back (#1996): install it read-only, or
		// replace the content of the views already showing that set.
		return m, m.installMergedLog(msg)

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

	case mouseHoverTickMsg:
		// The mouse-idle hover deadline elapsed (#1129): fire the hover when
		// the pointer still rests on the tracked cell, else re-arm/no-op.
		return m, m.mouseHoverTick(time.Now())

	case toastExpireMsg:
		m.expireToast(msg.id)
		return m, nil

	case keymapTimeoutMsg:
		// A held partial chord timed out: resolve it as an exact binding if
		// one exists. A bare prefix (cmd+k alone, prefix of the pane-split
		// sequence family) survives (#1482): the resolver keeps the chord and
		// the which-key overlay stays until a continuation, a non-matching
		// key, or a mouse click ends it.
		switch res := m.keys.Timeout(m.keyContext()); res.Status {
		case keymap.Resolved:
			m.clearWhichKey()
			if c, ok := m.reg.Command(res.Command); ok {
				return m, m.dispatchCommand(res.Command, c)
			}
		case keymap.Pending:
			return m, nil // popup stays; no timer re-arm needed
		default:
			m.clearWhichKey()
		}
		return m, nil

	case whichKeyDelayMsg:
		// The which-key delay elapsed (#1909): open the popup if the very
		// sequence the timer was armed for is still pending.
		on, _ := whichKeyConfig()
		if on && msg.gen == m.whichKeyGen && m.keys.Pending() {
			m.showWhichKey()
		}
		return m, nil

	case tea.KeyPressMsg:
		// Any key cancels a pending or open mouse-idle hover (#1129) — also
		// for keys the app consumes before the editor's own dismissHover
		// would see them.
		m.cancelMouseHover()
		// Keys landing in an editor or terminal stamp the do-not-interrupt
		// guard (#2086): a forge event dialog never lands mid-word.
		m.noteTypingInput()
		// The keymap doctor (#2080) outranks everything: probing only means
		// anything if the overlay sees the raw key before any other consumer
		// — toast dismissal, overlay paste, keymap resolution — touches it.
		if m.keyDoctor.IsOpen() {
			return m, m.keyDoctor.Update(msg)
		}
		// Esc dismisses persistent error toasts but keeps its normal meaning
		// (pass-through) so it never costs an extra press elsewhere.
		if msg.Code == tea.KeyEscape {
			m.dismissErrorToasts()
			// Also dismisses the large-file banner (#1124), equally additive.
			m.dismissLargeBanner()
		}
		// Cmd+V into an overlay's text input (#1273). The chord maps to
		// editor.paste, which no overlay handles, and overlays own the
		// keyboard before the keymap layer runs — so without this the only
		// paste route into the palette or a prompt would be the terminal's
		// bracketed paste. Same shape as the terminal pane's Cmd+V (#727):
		// read the system clipboard and hand the block to the focused input.
		if m.overlayCapturesKeyboard() {
			if k, ok := keymap.FromKeyMsg(msg); ok && k.Mods == keymap.ModMeta && k.Base == "v" {
				if text := clipboardRead(); text != "" {
					cmd, _ := m.routeOverlayPaste(text)
					return m, cmd
				}
				return m, nil
			}
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
		if m.callhier.IsOpen() {
			// The call-hierarchy overlay owns the keyboard the same way (#173).
			return m, m.callhier.Update(msg)
		}
		if m.typehier.IsOpen() {
			// The type-hierarchy overlay owns the keyboard the same way (#1454).
			return m, m.typehier.Update(msg)
		}
		if m.palette.IsOpen() {
			// Palette-context bindings (#2055) resolve here: the overlay
			// owns the keyboard, so the keymap layer further down never
			// sees the key.
			if cmd, handled := m.paletteBindingCmd(msg); handled {
				return m, cmd
			}
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
		// The forge event dialog (#2086) owns the keyboard the same way:
		// enter opens the issue, d/esc dismisses, a dismisses all.
		if m.forgeDialogOpen() {
			return m.updateForgeDialog(msg)
		}
		// The forge edit dialogs (#2087) — failed push, stale base — own the
		// keyboard the same way: r retries, o overwrites, l loads, esc keeps
		// the buffer.
		if m.forgeEditDialogOpen() {
			return m.updateForgeEditDialog(msg)
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
		// The per-file Timeline (#1916) owns the keyboard the same way.
		if m.timelineOpen() {
			return m.updateTimeline(msg)
		}
		// The external-change feed (#2000) owns the keyboard the same way.
		if m.changeFeedOpen() {
			return m.updateChangeFeed(msg)
		}
		// Its revert confirmation (#2000): enter / esc answer it.
		if m.changeFeedRevertOpen() {
			return m.updateChangeFeedRevert(msg)
		}
		// The range-history picker (#1430) owns the keyboard the same way.
		if m.historyPickerOpen() {
			return m.updateHistoryPicker(msg)
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
		// The busy close-current-project guard (#1355): s / d / esc answer it.
		if m.projectClosePromptOpen() {
			return m.updateProjectClosePrompt(msg)
		}
		// The busy peek-return guard (#2136): s / d / esc answer it.
		if m.peekReturnPromptOpen() {
			return m.updatePeekReturnPrompt(msg)
		}
		// The PHP path-mapping suggestion (#832): m / esc answer it.
		if m.debugMapPromptOpen() {
			return m.updateDebugMapPrompt(msg)
		}
		// The unsaved-changes guard on a close (#259): s / d / esc answer it.
		if m.closePromptOpen() {
			return m.updateClosePrompt(msg)
		}
		// The merge-view close guard (#1478): d / esc answer it.
		if m.mergeClosePromptOpen() {
			return m.updateMergeClosePrompt(msg)
		}
		// The busy-terminal close guard (#986): enter / esc answer it.
		if m.termClosePromptOpen() {
			return m.updateTermClosePrompt(msg)
		}
		// The open popup terminal layer (#1398, floating panels #1793) owns
		// the keyboard like a focused terminal pane: its focused shell takes
		// every key raw except the reserved popup set. The overlays and
		// prompts above still win — they can be opened from inside the popup
		// and must get their keys back.
		if m.popupLayerOpen() {
			if handled, tm, cmd := m.popupReservedKey(msg.String()); handled {
				return tm, cmd
			}
			// cmd+c copies an active mouse selection, cmd+v pastes — the same
			// pair the pane-terminal block below reserves (#227, #727). Both
			// act on the focused split side; a broadcast paste (#1427) goes
			// to every side's active shell like typed keys do.
			if k, ok := keymap.FromKeyMsg(msg); ok && k.Mods == keymap.ModMeta {
				if term := m.popupFocused().ActiveTerminal(); term != nil {
					switch {
					case k.Base == "c" && term.HasSelection():
						m.copyTerminalSelection(term)
						return m, nil
					case k.Base == "v":
						if text := clipboardRead(); text != "" {
							for _, t := range m.popupInputTerminals() {
								t.PasteText(text)
							}
						}
						return m, nil
					}
				}
			}
			// Global navigation chords (palette, settings, …) stay with the
			// IDE (#805); everything else belongs to the popup's shell —
			// under broadcast (#1427) to both split sides at once.
			if handled, cmd := m.terminalGlobalChord(msg); handled {
				return m, cmd
			}
			// Broadcast (#1427) is a box affair: a focused floating panel
			// (#1793) always receives alone.
			if m.floatFocused() == nil && m.popup.broadcast && m.popup.split != nil {
				return m, tea.Batch(m.popup.inst.Update(msg), m.popup.split.Update(msg))
			}
			return m, m.popupFocused().Update(msg)
		}
		// A focused terminal whose run has finished (#1951) is a read-only
		// view of the output: the copy chord copies the mouse selection, the
		// same way the live terminal does below. Everything else keeps its
		// route — r restarts and ctrl+w closes as before.
		if term := m.focusedDeadTerminal(); term != nil && term.HasSelection() {
			if k, ok := keymap.FromKeyMsg(msg); ok && k.Mods == keymap.ModMeta && k.Base == "c" {
				m.copyTerminalSelection(term)
				return m, nil
			}
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
			// IDE (#805), and terminal-context bindings (ctrl+t → new terminal
			// tab, #1794) resolve before PTY forwarding; everything else
			// belongs to the shell.
			if handled, cmd := m.terminalGlobalChord(msg); handled {
				return m, cmd
			}
			if handled, cmd := m.terminalContextChord(msg); handled {
				return m, cmd
			}
			return m.routeKey(msg)
		}
		// The rename prompt (#175) owns the keyboard the same way: typed
		// characters build the new name, enter applies, esc cancels.
		if m.renameOpen() {
			return m.updateRenamePrompt(msg)
		}
		// The clone-repository dialog (#1349) mirrors it, with tab moving
		// between the URL and the directory-name field.
		if m.clonePromptOpen() {
			return m.updateClonePrompt(msg)
		}
		// The regex tester (#1937) owns the keyboard the same way: the
		// pattern line and the test-text area, with tab between them.
		if m.regexTesterOpen() {
			return m.updateRegexTester(msg)
		}
		// The jq playground (#1936, inline #1970) owns the keyboard the same
		// way while its hosting pane is focused: the query line by default,
		// the read-only result buffer after tab. With the focus on another
		// pane the mode stays mounted but keys route normally (#1980), so
		// editing elsewhere works while the filtered result stays visible.
		// The saved-filter name prompt (#1995) is checked first: it is a
		// modal shell prompt opened *from* the playground, and the mode's
		// pane still holds the focus while it is up.
		if m.playNamePromptOpen() {
			return m.updatePlayNamePrompt(msg)
		}
		if m.playFocused() {
			return m.updatePlayground(msg)
		}
		// The new-project wizard (#1718) mirrors it, with three steps walked
		// by enter/esc.
		if m.newProjectPromptOpen() {
			return m.updateNewProjectPrompt(msg)
		}
		// The test-data wizard (#2134) mirrors it, with four steps walked by
		// enter/esc.
		if m.generateScratchOpen() {
			return m.updateGenerateScratch(msg)
		}
		// The untitled save-as prompt (#730) mirrors it.
		if m.saveAsOpen() {
			return m.updateSaveAsPrompt(msg)
		}
		// The save-layout pane-selection mini-map (#1568) owns the keyboard
		// the same way: hjkl/arrows move, space toggles, enter continues.
		if m.layoutSelectOpen() {
			return m.updateLayoutSelect(msg)
		}
		// The bookmark mnemonic/note prompts (#55) mirror it: a digit or a
		// typed note, enter/esc.
		if m.bookmarkPromptOpen() {
			return m.updateBookmarkPrompt(msg)
		}
		// The save-layout name prompt (#1175) mirrors it.
		if m.layoutSavePromptOpen() {
			return m.updateLayoutSavePrompt(msg)
		}
		// The JetBrains keymap import prompt (#677) mirrors it, plus tab
		// path completion.
		if m.jbImportPromptOpen() {
			return m.updateJBImportPrompt(msg)
		}
		// The OpenAPI import prompt (#1939) mirrors it exactly.
		if m.openAPIImportPromptOpen() {
			return m.updateOpenAPIImportPrompt(msg)
		}
		// The curl import prompt (#1994) likewise — one line, enter/esc.
		if m.curlImportPromptOpen() {
			return m.updateCurlImportPrompt(msg)
		}
		// The response-body save prompt (#2059) mirrors the JetBrains import:
		// one path line with tab completion.
		if m.httpSavePromptOpen() {
			return m.updateHTTPSavePrompt(msg)
		}
		// The symbol-rename prompt (0100, #6) mirrors it.
		if m.lspRenameOpen() {
			return m.updateLSPRenamePrompt(msg)
		}
		if m.floats.IsOpen() && !m.tourOpen() {
			// The tour never reaches this branch: its keys are handled (or
			// deliberately passed through, #680) above, and the shell scroller
			// must not swallow a try-it chord. The stack routes to the topmost
			// open layer only (#1237).
			m.floats.Update(msg)
			return m, nil
		}
		// A focused explorer with an open prompt (new-file name entry, delete/undo
		// confirmation) or speed search (#1087) captures every key, ahead of the
		// keymap and global layers, so typed names, y/n answers and search
		// queries reach it intact — the tree's single-letter file-op bindings
		// must not fire mid-word.
		if m.explorerCapturing() {
			return m.routeKey(msg)
		}
		// While the debug panel edits a variable value it captures every key
		// (incl. enter/esc/plain letters), like an editor in insert mode (#627).
		// Routed ahead of the esc-esc detector: an esc the editor consumes to
		// cancel must not arm the double-esc palette (#640).
		if m.debugPanelEditing() {
			m.lastEscAt = time.Time{}
			return m.routeKey(msg)
		}
		keys := msg.String()
		if m.paletteKey != "" && keys == m.paletteKey {
			m.lastEscAt = time.Time{}
			m.openPalette()
			return m, nil
		}
		// esc-esc opens the palette from a non-capturing context; the first esc is
		// still forwarded (clears selection, etc.). The second esc only counts if
		// it lands within escEscTimeout of the first (#1750): otherwise a
		// long-forgotten armed esc would open the palette on an unrelated esc.
		if keys == "esc" && !m.editorCapturing() {
			now := m.clock()
			if !m.lastEscAt.IsZero() && now.Sub(m.lastEscAt) <= escEscTimeout {
				m.lastEscAt = time.Time{}
				m.openPalette()
				return m, nil
			}
			m.lastEscAt = now
			return m.routeKey(msg)
		}
		m.lastEscAt = time.Time{}
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
			// A live selection in the focused pane outranks the quit chord
			// (#2062). The response pane advertises ctrl+c as its copy key
			// (#1266), and on a platform without a Cmd key that is the only
			// copy chord it has — quitting the IDE instead is the worst
			// possible reading of the key. Without a selection ctrl+c keeps
			// its global meaning.
			if m.paneSelectionCopy() {
				return m.routeKey(msg)
			}
			// Quit routes through the unsaved-changes guard (#287) so a
			// dirty buffer prompts instead of being dropped.
			return m.guardedQuit()
		case "q":
			if m.quitKey() {
				return m.guardedQuit()
			}
		case "tab":
			// A focused data viewer owns tab (#1788): it toggles the pane's
			// own sidebar/grid regions, the way an editor keeps its plain
			// keys. Pane focus still cycles with ctrl+tab (the pane switcher)
			// and the focus keys.
			if m.dataPaneFocused() {
				return m.routeKey(msg)
			}
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
// EventFileOpened hooks fire either way. A viewer file (data, image, archive)
// splits off viewerSplitTarget (#1779); openPathFocused and openPathInEditor
// are the variants that nest it as a tab instead.
func (m Model) openPath(path string, newPane bool) (tea.Model, tea.Cmd) {
	m.viewerTabHost = "" // no pending tab request: viewers split (#1779)
	return m.openPathWith(path, newPane)
}

// openPathFocused opens path like openPath, except that a file claimed by a
// viewer file handler opens as a content tab in the focused pane (#1825) —
// the palette's open target, matching where a plain file lands. It falls back
// to the #1779 split when the focused pane cannot host tabs (the explorer, a
// tool window), so opening from a tool window still lands beside the pane the
// user last worked in.
func (m Model) openPathFocused(path string) (tea.Model, tea.Cmd) {
	m.viewerTabHost = m.focusedTabHost()
	return m.openPathWith(path, false)
}

// openPathInEditor opens path like openPath, except that a file claimed by a
// viewer file handler opens as a content tab in the very pane a plain file
// would land in — the editor fileEditorKey resolves (#1851). It is the
// explorer's default open: whatever the file's kind, it becomes a tab in the
// last-focused editor rather than a split beside it. With no editor pane to
// host the tab, one is spawned first, matching the plain-file fallback.
func (m Model) openPathInEditor(path string) (tea.Model, tea.Cmd) {
	m.viewerTabHost = m.fileEditorKey()
	if m.viewerTabHost == "" {
		m.viewerTabHost = m.spawnEditor()
	}
	return m.openPathWith(path, false)
}

// openPathWith is the shared body of openPath / openPathFocused; the caller
// has already decided the viewer open target in m.viewerTabHost.
func (m Model) openPathWith(path string, newPane bool) (tea.Model, tea.Cmd) {
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
		m.viewerTabHost = "" // no handler, so no viewer open to honour it (#1825)
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
			// A log file with rotated siblings offers its merged timeline
			// (#1996) — nothing else says that the file next to this one holds
			// the hour before it.
			m.notifyRotatedSet(m.activeWS().Panes.Get(key).Editor())
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
			// Stored coverage marks for the fresh buffer (#2081).
			if cmd := m.coverageMarksCmd(path); cmd != nil {
				cmds = append(cmds, cmd)
			}
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
// Dirty, scratch, terminal and pinned (#1172) tabs are exempt — when nothing
// is eligible (e.g. every other tab is pinned) the limit is exceeded rather
// than data risked or a pin overridden. 0 (or negative) disables.
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
			// The LRU eviction is a tab close like any other (#1550): the
			// undo history persists (the tab is non-dirty by selection, so
			// PersistUndo writes) and the crash snapshot drops — the manual
			// close path (closeTab) does both, and skipping them here
			// silently lost the evicted tab's persistable undo.
			ed.PersistUndo()
			m.backupDropOnCloseTab(ed, inst.Key())
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

// explorerLocalY translates an absolute mouse row into the explorer pane's
// content-local row, so the wheel knows whether it sits over the tree or the
// Scratches section (#1965). A pane without a rect keeps the absolute row —
// it can only be above the section anyway.
func (m Model) explorerLocalY(key string, y int) int {
	r, ok := m.lay.Panes[key]
	if !ok {
		return y
	}
	return y - (r.Y + m.contentYOff(key))
}

// syncExplorerOpen refreshes the explorer's set of open files (every editor
// pane holding a file), so their rows render underlined + italic, and pushes
// the MRU store's last-opened times in for the Scratches age column (#1965).
// Called after anything that opens or closes an editor.
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
	m.explorer().SetScratchOpened(m.lastOpenedTimes())
}

// lastOpenedTimes is the MRU store as a path → last-opened lookup, the data
// behind the Scratches section's age column (#1965). Entries without a
// timestamp (pre-#1113 sessions) are dropped so the section falls back to the
// file's mtime instead of rendering a zero time.
func (m Model) lastOpenedTimes() map[string]time.Time {
	entries := m.recent.Entries()
	out := make(map[string]time.Time, len(entries))
	for _, e := range entries {
		if e.LastOpened.IsZero() {
			continue
		}
		out[e.Path] = e.LastOpened
	}
	return out
}

// fileChangeKind maps a watcher file-event kind onto the hook payload kind
// (#1144). Only the three file kinds reach it — the dir/git/config kinds
// return early in the watch.EventMsg case.
func fileChangeKind(k watch.Kind) plugin.FileChangeKind {
	switch k {
	case watch.FileCreated:
		return plugin.FileCreated
	case watch.FileRemoved:
		return plugin.FileDeleted
	}
	return plugin.FileModified
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
		// The last view is gone: drop the breadcrumbs symbol cache (#1153)
		// with it, so a re-open starts from a fresh documentSymbol request,
		// and stop poll-watching the file — otherwise every file ever opened
		// keeps its stamp (mtime, size, content hash) and gets re-stat'ed on
		// every poll for the rest of the session (#1537).
		delete(m.docSymbols, path)
		delete(m.largeToasted, path)
		if m.watcher != nil {
			m.watcher.Untrack(path)
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
	m.help.SetExtra(m.blockedHelpGroup(), m.paneKeysHelpGroup())
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
	x, y, w, ok := m.paneAnchor()
	if !ok {
		m.openPalette()
		return
	}
	cx := palette.Context{ContextID: m.focusContext(), Root: "."}
	m.palette.OpenAnchored(cx, '@', x, y, w)
}

// paneAnchor is the anchored palette's placement: the focused pane's top-left
// interior and its inner width. It reports false while the pane has no
// computed rectangle yet — shared by the initial open and the anchored
// re-open a directory descend performs (#1775).
func (m *Model) paneAnchor() (x, y, w int, ok bool) {
	r, ok := m.lay.Panes[m.activeWS().Panes.Focused()]
	if !ok {
		return 0, 0, 0, false
	}
	return r.X + 1, r.Y + 1, r.W - 2, true
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
	if m.popupLayerOpen() {
		// The open popup terminal layer (#1398, #1793) owns the keyboard, so
		// bindings and the mode indicator resolve under its focused host's
		// context, not the pane below.
		return m.popupFocused().ContextID()
	}
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil {
		return inst.ContextID()
	}
	return ctxExplorer
}

// keyContext is focusContext for the keymap layer (#1876): the focused pane's
// context, narrowed to the buffer's language id when the focus is an editor
// showing a classified buffer — so language-scoped bindings (editor[http])
// resolve and shadow only there. Every other consumer of the context id
// (palette scoping, registry, help snapshot) keeps the plain focusContext.
func (m Model) keyContext() keymap.Context {
	ctx := keymap.Context(m.focusContext())
	if ctx != keymap.Editor || m.popupLayerOpen() {
		return ctx
	}
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil {
		if ed := inst.Editor(); ed != nil {
			ctx = keymap.WithLang(ctx, ed.LangID())
		}
	}
	return ctx
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
	inst := m.focusedContent() // a diff may live in a tab (#1778)
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
// open modal prompt or speed search (#1087), in which case keys go straight
// to it (see Update).
func (m Model) explorerCapturing() bool {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindExplorer {
		return false
	}
	return inst.Explorer().Prompting() || inst.Explorer().Searching()
}

// dataPaneFocused reports whether the focused pane shows a data viewer
// (#1764) or an ES console (#1927) — dedicated pane or active content tab
// (#1778) — which claims tab for its own region toggle before the global
// focus cycle.
func (m Model) dataPaneFocused() bool {
	inst := m.focusedContent()
	return inst != nil && (inst.Kind() == pane.KindData || inst.Kind() == pane.KindES)
}

// paneSelectionCopy reports whether the focused pane holds a live text
// selection its own copy key would put on the clipboard (#2062), so a chord
// the shell would otherwise claim has to be routed into the pane instead.
// The HTTP response pane (#1266), the diff viewer and the merge view's side
// columns (#2070) qualify: the editor reaches editor.copy through the keymap
// layer above, and a focused terminal — the other selectable surface — is
// handled before the shell dispatch ever runs.
func (m Model) paneSelectionCopy() bool {
	inst := m.focusedContent()
	if inst == nil {
		return false
	}
	switch inst.Kind() {
	case pane.KindHTTP:
		p := inst.HTTP()
		return p != nil && p.HasSelection()
	case pane.KindDiff:
		d := inst.Diff()
		return d != nil && d.HasSelection()
	case pane.KindMerge:
		mg := inst.Merge()
		return mg != nil && mg.HasSelection()
	}
	return false
}

// explorerPromptOpen reports whether the focused explorer has a modal prompt
// open — the mouse routing needs the narrower check: prompt clicks go to
// PromptMouseClick, while an open speed search keeps normal row clicks.
func (m Model) explorerPromptOpen() bool {
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

// viewerSplitTarget returns the leaf a viewer pane — data (#1764), image
// (#1479), archive (#1762) — splits off from (#1779). Opening one from the
// explorer must not split the explorer, so the choice follows fileEditorKey:
// the focused pane when it hosts content, else the most-recent editor, else
// the first content leaf in tree order. Content means anything but the
// explorer and the singleton tool windows, so a viewer opened next to another
// viewer or a terminal still splits where the user was working. The focused
// leaf stays the last resort: a workspace made only of tool windows still
// gets its pane rather than none.
func (m Model) viewerSplitTarget() string {
	hostsContent := func(inst *pane.Instance) bool {
		if inst == nil {
			return false
		}
		switch inst.Kind() {
		case pane.KindExplorer, pane.KindVCS, pane.KindDebug, pane.KindProblems,
			pane.KindStructure, pane.KindUsages, pane.KindHTTP, pane.KindBreakpoints,
			pane.KindTests, pane.KindIssues, pane.KindDOM, pane.KindDoctor:
			return false
		}
		return true
	}
	if hostsContent(m.activeWS().Panes.FocusedInstance()) {
		return m.activeWS().Panes.Focused()
	}
	if m.recentEditor != "" && hostsContent(m.activeWS().Panes.Get(m.recentEditor)) {
		return m.recentEditor
	}
	for _, key := range m.leafOrder() {
		if hostsContent(m.activeWS().Panes.Get(key)) {
			return key
		}
	}
	return m.activeWS().Panes.Focused()
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
	// The inline playground survives focus leaving its pane (#1980); the
	// substitute editor's cursor cell tracks whether the pane holds the
	// keyboard, so an unfocused playground draws no caret.
	if s := m.play; s != nil && s.resultEd != nil {
		s.resultEd.SetFocused(key == s.paneKey && s.bufFocus)
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

// spawnEditor splits a fresh editor pane into the tree, returning its key.
// Used by open-in-new-pane and Replace-with-no-editor. The split target is
// the pane a file open would land in (so opening from the explorer lands the
// new pane in the editor area, not beside the explorer — and never at a
// tool-tab host, #1989); with no such pane the designated layout's editor
// slot anchors the split, and only as a last resort the focused leaf does.
func (m *Model) spawnEditor() string {
	target := m.fileEditorKey()
	zone, ratio := m.splitZone, 0.0
	if target == "" {
		// No live pane edits files: a tool-tab host must not attract the
		// split (#1989) — the designated layout's editor slot decides the
		// position instead, so the recreated editor lands in its original
		// layout area.
		target, zone, ratio = m.editorSlotAnchor()
	}
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
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, zone)
	if !ok {
		// Target not in the tree (e.g. focused leaf already gone): drop the spare.
		m.activeWS().Panes.Close(newKey)
		return m.activeWS().Panes.Focused()
	}
	if ratio > 0 && ratio < 1 {
		// The saved layout's split share carries over, so the recreated
		// editor slot keeps its original proportions instead of an even split.
		if sp := parentSplit(tree, newKey); sp != nil {
			sp.Ratio = ratio
		}
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
	m.closePane(m.activeWS().Panes.Focused())
}

// closePane closes the leaf named key outright — every tab at once — and
// repairs focus, layout and persistence. Callers guard unsaved changes first
// (guardedClosePane); shared by closeFocused, the pane.close command (#1128)
// and the dead tool pane's ✕ action.
func (m *Model) closePane(key string) {
	if m.closeKey(key) {
		// Focus the leaf that now occupies the closed pane's position: the first
		// leaf in walk order is a safe, always-present choice (explorer at minimum).
		m.setFocus(m.focusAfterClose())
		m.syncExplorerOpen()
		m.layout()
		saveLayout(m.activeWS().Tree, m.activeWS().Panes)
	}
}

// guardedClosePane closes the focused pane whole — all its tabs — behind the
// unsaved-changes guard (#1128, pane.close): dirty documents not visible in
// another pane open the prompt with a whole-pane pending close first.
func (m *Model) guardedClosePane() {
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil {
		return
	}
	// A merge view with unresolved conflicts or an unsaved result warns
	// before it closes (#1478).
	if m.guardMergeClose(inst) {
		return
	}
	if inst.Kind() == pane.KindEditor {
		if dirty := m.dirtyOnClose(inst, -1); len(dirty) > 0 {
			m.openClosePrompt(inst.Key(), -1, dirty)
			return
		}
	}
	m.closePane(inst.Key())
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
	// A global tool session still hosted here ends with the pane — an
	// explicit close ends the tool everywhere (#1890), and the recorded close
	// keeps stale layout entries in other projects from resurrecting it on
	// the next switch (#1903).
	m.noteGlobalToolCloses(inst)
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
	// The inline playground dies with its hosting pane (#1980): the mode
	// survives focus changes, so the pane can be closed from elsewhere while
	// the playground is still mounted in it.
	if s := m.play; s != nil && s.paneKey == key {
		m.closePlayground()
	}
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
	// session. A global tool tab closing here is an explicit close-everywhere
	// (#1903): record it so no stale layout resurrects the tool.
	m.noteGlobalToolTabClose(inst.TabTerminal(idx))
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

// routeParse delivers a finished parse to the view that scheduled it, keyed by
// editor.ParseKey: the file path for a saved buffer — every leaf showing it,
// background panes and shared-document views included — or the view's own tag
// for a buffer with no file, which a path route could never find (#2033).
func (m *Model) routeParse(msg highlight.SpansMsg) tea.Cmd {
	return m.routeToEditorKey(msg.Path, msg)
}

// routeToEditorKey delivers a message to every editor tab answering to an
// editor.ParseKey — the file path, or a file-less view's own tag. It is the
// route for everything a path-keyed one would drop on a buffer with no file:
// async parses (#2033) and local completion batches (#2048).
func (m *Model) routeToEditorKey(key string, msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for _, paneKey := range m.activeWS().Panes.Keys() {
		inst := m.activeWS().Panes.Get(paneKey)
		if inst == nil || inst.Kind() != pane.KindEditor {
			continue
		}
		if cmd := inst.UpdateForParseKey(key, msg); cmd != nil {
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

// reparseOpenEditors schedules a fresh parse of every open editor, so a
// highlight-affecting toggle flip lands without waiting for the next edit.
func (m *Model) reparseOpenEditors() []tea.Cmd {
	var cmds []tea.Cmd
	for _, key := range m.activeWS().Panes.Keys() {
		if inst := m.activeWS().Panes.Get(key); inst != nil && inst.Kind() == pane.KindEditor {
			for _, ed := range inst.Editors() {
				cmds = append(cmds, ed.Reparse())
			}
		}
	}
	return cmds
}

// diffWordsConfigured reads editor.diff_word_highlight (#1630, default on).
func diffWordsConfigured() bool {
	if c := config.Get(); c != nil {
		return c.Editor.DiffWordHighlight
	}
	return true
}

// rainbowConfigured reads editor.rainbow_brackets (#789, default on).
func rainbowConfigured() bool {
	if c := config.Get(); c != nil {
		return c.Editor.RainbowBrackets
	}
	return true
}

// applyIDColorConfig pushes editor.id_colors / editor.id_color_min_length
// (#1626) into the idcolor package globals. The editor keeps its own per-view
// toggle; the globals serve the consumers without config plumbing of their
// own — today the .http response pane.
func applyIDColorConfig() {
	c := config.Get()
	if c == nil {
		return
	}
	idcolor.SetEnabled(c.Editor.IDColors)
	idcolor.SetMinLength(c.Editor.IDColorMinLength)
}

// applyNumberHintUnits pushes editor.number_hint_units (#1685) into the
// numhint package global, where the lang.Language.Spans hooks read it — those
// have no config plumbing of their own. It reports whether the mapping
// changed, so the caller can re-parse the open editors.
func applyNumberHintUnits() bool {
	c := config.Get()
	if c == nil {
		return false
	}
	return numhint.SetFieldUnits(c.Editor.NumberHintUnits)
}

// applySecretMaskingKeys pushes editor.secret_masking_keys (#1712) into the
// secret package global, where the dotenv producer reads it — same deal as
// applyNumberHintUnits, and it reports a change for the same reason: which
// values carry a mask is decided when the spans are produced.
func applySecretMaskingKeys() bool {
	c := config.Get()
	if c == nil {
		return false
	}
	return secret.SetKeyPatterns(c.Editor.SecretMaskingKeys)
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
		// The breadcrumbs bar (#1153) is one extra vertical chrome row for
		// the editor panes showing it; breadcrumbRows is the shared predicate
		// renderPane and the mouse translation (contentYOff) key off too.
		inst.SetSize(paneInterior(r.W, paneChromeW), paneInterior(r.H, paneChromeH+m.breadcrumbRows(inst)))
		if inst.Kind() == pane.KindEditor && m.pendingScroll != nil && m.pendingScroll.key == key {
			if ed := inst.Editor(); ed != nil {
				ed.SetScroll(m.pendingScroll.top, m.pendingScroll.left)
			}
			m.pendingScroll = nil
		}
	}
	// The inline playground's result buffer (#1970) tracks its hosting
	// pane's interior minus the query header rows.
	m.sizePlayResult()
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

// auxSplitRightMinW is the host width (outer cells) above which an
// auto-placed auxiliary pane opens to the right instead of below (#1588).
const auxSplitRightMinW = 120

// auxZone picks the split direction for an auto-placed auxiliary pane
// (terminal, debug view, tool panes, http response, bottom panels): a host
// wider than auxSplitRightMinW cells that is also wider than it is tall
// splits to the right; everything else keeps the conventional bottom split
// (#1588). Cells are roughly twice as tall as wide, so width > height is
// already biased toward landscape hosts — matching the intent.
func (m *Model) auxZone(target string) layout.Zone {
	if r, ok := m.lay.Panes[target]; ok && r.W > auxSplitRightMinW && r.W > r.H {
		return layout.ZoneRight
	}
	return layout.ZoneBottom
}

// handleMouse runs the drag state machine: press hit-tests the layout to start a
// resize (divider) or move (title bar), motion updates the in-flight gesture, and
// release commits and persists. A title drag onto another pane relocates it
// (0036); a drag to the source pane's own edge spawns a fresh split there (0037).
// floatResizeDrag tracks a live mouse resize of a floating window (#933):
// press on the window's border ring grabs an edge (sx or sy set) or corner
// (both), motion applies pointer deltas as size deltas, release persists.
type floatResizeDrag struct {
	kind         string     // which float: "settings", "palette", "shell", "popupterm", "floatterm"
	target       *floatTerm // kind "floatterm" (#1793): the panel being resized
	sx, sy       int        // grow direction of the grabbed edge/corner (−1/0/+1)
	lastX, lastY int        // last applied pointer cell
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
		// The drag grabbed the topmost open layer of the floating stack
		// (#1237); with a single layer that is the shell, as before.
		if top := m.floats.Top(); top != nil {
			top.AdjustSize(ddw, ddh)
		}
	case "popupterm":
		// The popup terminal (#1398) re-clamps in popupSize; the PTYs follow.
		m.popupTermResize(ddw, ddh, false)
	}
}

func (m Model) handleMouse(msg mouseEvent) (tea.Model, tea.Cmd) {
	// Any press, release, or wheel cancels a pending or open mouse-idle hover
	// (#1129); motion is handled cell-wise by trackMouseHover below.
	if msg.action != mouseMotion {
		m.cancelMouseHover()
	}
	// An active float resize drag (#933) owns the mouse until release: each
	// motion step applies the pointer delta along the grabbed edge/corner as a
	// size delta (motion events are already folded by the input coalescer, so
	// this runs at most once per rendered frame), release persists the store.
	// Floats are centered, so a size delta grows both sides — one pointer cell
	// maps to two size cells so the grabbed edge tracks the pointer exactly
	// (#1243).
	if m.floatDrag != nil {
		switch msg.action {
		case mouseMotion:
			d := m.floatDrag
			if d.kind == "floatterm" && d.target != nil {
				// A floating terminal panel (#1793) is corner-anchored, not
				// centered: the grabbed edge tracks the pointer 1:1.
				m.applyFloatTermResize(d, msg.X, msg.Y)
				return m, nil
			}
			ddw, ddh := (msg.X-d.lastX)*d.sx*2, (msg.Y-d.lastY)*d.sy*2
			if ddw != 0 || ddh != 0 {
				d.lastX, d.lastY = msg.X, msg.Y
				m.applyFloatResize(d.kind, ddw, ddh)
			}
			return m, nil
		case mouseRelease:
			kind := m.floatDrag.kind
			m.floatDrag = nil
			switch kind {
			case "popupterm":
				// The popup delta also becomes the user-scoped fallback (#1714).
				m.popupTermPersist()
			case "floatterm":
				// Panel geometry is runtime state (#1793): nothing persists.
			default:
				m.winSizes.Flush()
			}
			return m, nil
		case mousePress:
			m.floatDrag = nil // stray press: drop the drag, fall through
		}
	}
	// An active titlebar move drag (#1793) owns the mouse until release: each
	// motion step moves the popup box (offset through the winSizes stores,
	// clamped in popupTermRect) or the grabbed floating panel; the box's
	// release persists the offset — panel geometry is runtime state.
	if m.floatMove != nil {
		switch msg.action {
		case mouseMotion:
			d := m.floatMove
			dx, dy := msg.X-d.lastX, msg.Y-d.lastY
			if dx != 0 || dy != 0 {
				d.lastX, d.lastY = msg.X, msg.Y
				if d.target != nil {
					d.target.x = ui.ClampDelta(d.target.x, dx, 0, max(m.width-d.target.w, 0))
					d.target.y = ui.ClampDelta(d.target.y, dy, 0, max(m.height-d.target.h, 0))
				} else {
					m.popupTermMoveBy(dx, dy, false)
				}
			}
			return m, nil
		case mouseRelease:
			moved := m.floatMove.target == nil
			m.floatMove = nil
			if moved {
				m.popupTermPersistPos()
			}
			return m, nil
		case mousePress:
			m.floatMove = nil // stray press: drop the drag, fall through
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
	// The large-file banner (#1124) overlays the focused pane's first content
	// row: ✕ dismisses, any other press on it runs Force Code Insight.
	if text, bx, by, bw, ok := m.largeFileBanner(); ok && msg.action == mousePress && msg.Button == tea.MouseLeft &&
		msg.Y == by && msg.X >= bx && msg.X < bx+lipgloss.Width(text) {
		if msg.X >= bx+bw-2 {
			m.dismissLargeBanner()
			return m, nil
		}
		return m, func() tea.Msg { return ForceCodeInsightMsg{} }
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
		// The panel's geometry comes from Size(), never from measuring View()
		// (#1396): mouse motion arrives at very high frequency, and a full
		// panel render per event saturated the Update loop.
		w, h := m.settings.Size()
		bx, by := (m.width-w)/2, (m.height-h)/2
		if msg.action == mousePress && w > 0 && !inRect(msg.X, msg.Y, bx, by, w, h) {
			m.settings.Close()
			return m, nil
		}
		switch {
		case msg.action == mousePress && msg.Button == tea.MouseLeft:
			// The border ring starts a mouse resize (#933); anything inside
			// is a content click (panel-local coordinates, the box is centered).
			if sx, sy, ok := ui.ResizeZone(msg.X-bx, msg.Y-by, w, h); ok {
				m.floatDrag = &floatResizeDrag{kind: "settings", sx: sx, sy: sy, lastX: msg.X, lastY: msg.Y}
				return m, nil
			}
			return m, m.settings.Click(msg.X-bx, msg.Y-by)
		case msg.action == mouseMotion:
			// Hover affordance (#885), menu-bar parity.
			m.settings.Hover(msg.X-bx, msg.Y-by)
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelUp:
			m.settings.Wheel(msg.X-bx, msg.Y-by, -wheelLines*msg.ticks())
		case msg.action == mouseWheel && msg.Button == tea.MouseWheelDown:
			m.settings.Wheel(msg.X-bx, msg.Y-by, wheelLines*msg.ticks())
		}
		return m, nil
	}
	if top := m.floats.Top(); top != nil {
		// Mouse routes to the topmost open layer (#1237): a press outside it
		// pops only that layer, a border press resizes it.
		if clickOutside(msg, top.View(), m.width, m.height) {
			m.floats.Pop()
			return m, nil
		}
		if msg.action == mousePress && msg.Button == tea.MouseLeft {
			v := top.View()
			w, h := lipgloss.Width(v), lipgloss.Height(v)
			bx, by := (m.width-w)/2, (m.height-h)/2
			if sx, sy, ok := ui.ResizeZone(msg.X-bx, msg.Y-by, w, h); ok {
				m.floatDrag = &floatResizeDrag{kind: "shell", sx: sx, sy: sy, lastX: msg.X, lastY: msg.Y}
			} else if top == m.shell && m.layoutSelectOpen() {
				// The save-layout mini-map (#1570): a click on a cell focuses
				// and toggles that pane.
				ox, oy := m.shell.ContentOrigin()
				m.layoutSelectClick(msg.X-bx-ox, msg.Y-by-oy+m.shell.ScrollOffset())
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
		// The wheel scrolls the column under the cursor (#2041): the recent
		// dialog's projects column has its own window, so a notch over it must
		// move that column and not the file list.
		if msg.action == mouseWheel && inRect(msg.X, msg.Y, bx, by, lipgloss.Width(v), lipgloss.Height(v)) {
			switch msg.Button {
			case tea.MouseWheelUp:
				m.palette.Wheel(msg.X-bx, msg.Y-by, -wheelLines*msg.ticks())
			case tea.MouseWheelDown:
				m.palette.Wheel(msg.X-bx, msg.Y-by, wheelLines*msg.ticks())
			}
		}
		return m, nil
	}
	// The popup terminal layer (#1398, floating panels #1793) hit-tests after
	// the overlays that render above it. An active drag (selection, scrollbar,
	// tab tear-out) skips the branch — the generic drag machinery below
	// handles motion and release popup-aware.
	if m.popupLayerOpen() && m.drag == nil {
		if tm, cmd, done := m.popupLayerMouse(msg); done {
			return tm, cmd
		}
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
		// The wheel scrolls the inline playground result buffer (#1970) while the
		// mode owns the pane; horizontal wheel and shift+wheel sideways,
		// like the editor (#230).
		if s := m.play; s != nil && s.paneKey == key {
			switch {
			case msg.Button == tea.MouseWheelLeft:
				s.resultEd.ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelRight:
				s.resultEd.ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp && shift:
				s.resultEd.ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelDown && shift:
				s.resultEd.ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp:
				s.resultEd.ScrollBy(-lines)
			case msg.Button == tea.MouseWheelDown:
				s.resultEd.ScrollBy(lines)
			}
			return m, nil
		}
		if c := inst.ActiveContent(); c != nil {
			// A tab host's body scrolls like the equivalent dedicated pane
			// (#1778); the tab-bar row keeps its tab-cycling wheel below.
			if r, ok := m.lay.Panes[key]; !ok || msg.Y != r.Y+1 {
				inst = c
			}
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
				m.explorer().ScrollAt(m.explorerLocalY(key, msg.Y), -lines)
			case msg.Button == tea.MouseWheelDown:
				m.explorer().ScrollAt(m.explorerLocalY(key, msg.Y), lines)
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
			// The wheel scrolls the diff by visual rows (#60); the horizontal
			// wheel and shift+wheel shift both sides in lockstep (#1700).
			switch {
			case msg.Button == tea.MouseWheelLeft:
				inst.Diff().ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelRight:
				inst.Diff().ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp && shift:
				inst.Diff().ScrollXBy(-lines)
			case msg.Button == tea.MouseWheelDown && shift:
				inst.Diff().ScrollXBy(lines)
			case msg.Button == tea.MouseWheelUp:
				inst.Diff().ScrollBy(-lines)
			case msg.Button == tea.MouseWheelDown:
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
		case pane.KindTests:
			// The wheel scrolls the Test Results tree or detail (#1911).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Tests().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Tests().Wheel(lines)
			}
		case pane.KindIssues:
			// The wheel scrolls the issue list or detail view (#1934);
			// scrolling to the end of the detail pulls the next timeline
			// page (#2113).
			switch msg.Button {
			case tea.MouseWheelUp:
				return m, inst.Issues().Wheel(-lines)
			case tea.MouseWheelDown:
				return m, inst.Issues().Wheel(lines)
			}
		case pane.KindData:
			// The wheel scrolls the data viewer's focused region (#1788) —
			// the table list or the grid's rows; the horizontal wheel and
			// shift+wheel pan the grid's columns, like the diff pane.
			switch {
			case msg.Button == tea.MouseWheelLeft:
				inst.Data().WheelX(-lines)
			case msg.Button == tea.MouseWheelRight:
				inst.Data().WheelX(lines)
			case msg.Button == tea.MouseWheelUp && shift:
				inst.Data().WheelX(-lines)
			case msg.Button == tea.MouseWheelDown && shift:
				inst.Data().WheelX(lines)
			case msg.Button == tea.MouseWheelUp:
				inst.Data().Wheel(-lines)
			case msg.Button == tea.MouseWheelDown:
				inst.Data().Wheel(lines)
			}
		case pane.KindArchive:
			// The wheel scrolls the archive entry list (#1852); the cursor is
			// dragged along so it stays inside the visible window.
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Archive().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Archive().Wheel(lines)
			}
		case pane.KindBreakpoints:
			// The wheel scrolls the breakpoints list (#1377).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Breakpoints().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Breakpoints().Wheel(lines)
			}
		case pane.KindStructure:
			// The wheel scrolls the symbol list (#1025).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Structure().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Structure().Wheel(lines)
			}
		case pane.KindDOM:
			// The wheel scrolls the DOM tree (#1929).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.DOM().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.DOM().Wheel(lines)
			}
		case pane.KindDoctor:
			// The wheel scrolls the connection trace (#1991).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Doctor().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Doctor().Wheel(lines)
			}
		case pane.KindUsages:
			// The wheel scrolls the usages list (#1155).
			switch msg.Button {
			case tea.MouseWheelUp:
				inst.Usages().Wheel(-lines)
			case tea.MouseWheelDown:
				inst.Usages().Wheel(lines)
			}
		case pane.KindHTTP:
			// The wheel scrolls the response viewer (#1250); horizontal wheel
			// and shift+wheel pan wide bodies sideways (#1290), like the
			// editor (#230).
			switch {
			case msg.Button == tea.MouseWheelLeft:
				inst.HTTP().ScrollX(-lines)
			case msg.Button == tea.MouseWheelRight:
				inst.HTTP().ScrollX(lines)
			case msg.Button == tea.MouseWheelUp && shift:
				inst.HTTP().ScrollX(-lines)
			case msg.Button == tea.MouseWheelDown && shift:
				inst.HTTP().ScrollX(lines)
			case msg.Button == tea.MouseWheelUp:
				inst.HTTP().Scroll(-lines)
			case msg.Button == tea.MouseWheelDown:
				inst.HTTP().Scroll(lines)
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
		if m.explorerPromptOpen() {
			// The prompt floats centered within the explorer pane's own
			// content area, so it reads the same content-local coordinates
			// as a normal row click.
			if r, ok := m.lay.Panes[pane.ExplorerKey]; ok {
				m.explorer().PromptMouseClick(msg.X-(r.X+paneContentX), msg.Y-(r.Y+paneContentY))
			}
			return m, nil
		}
		// The status line (#1128) sits below the layout tree; a left press on
		// one of its clickable segments — TODO count, notifications counter,
		// LSP state — dispatches that segment's command, any other press on
		// the row is swallowed.
		if !m.zen && msg.Y == m.height-1 {
			if msg.Button == tea.MouseLeft {
				if id, ok := statusSegmentCommands[m.statusSegmentAt(msg.X)]; ok {
					if c, found := m.reg.Command(id); found {
						return m, m.dispatchCommand(id, c)
					}
				}
			}
			return m, nil
		}
		hit := m.lay.Hit(msg.X, msg.Y)
		switch hit.Kind {
		case layout.HitDivider:
			m.drag = &dragState{kind: dragResize, divider: *hit.Divider, curX: msg.X, curY: msg.Y}
		case layout.HitTitle:
			// A right press on the title row opens a chrome context menu
			// (#1128): on a tab segment the clicked tab is selected first —
			// like the left-click focus path — so the menu's actions target
			// it; elsewhere on the band the pane menu opens.
			if msg.Button == tea.MouseRight {
				if key, idx, _, ok := m.tabBarHit(msg.X, msg.Y); ok {
					inst := m.activeWS().Panes.Get(key)
					m.setFocus(key)
					m.switchTab(inst, idx)
					m.ctxMenu.Open(tabContextItems(inst.TabPinned(idx)), msg.X, msg.Y, m.width, m.height)
					return m, nil
				}
				m.setFocus(hit.Pane)
				m.ctxMenu.Open(paneContextItems(), msg.X, msg.Y, m.width, m.height)
				return m, nil
			}
			// Clicks on a tab-bar segment act on that tab (#159): left-click
			// focuses it, middle-click closes it. The active tab's own
			// segment — and the row outside the segments — still starts a
			// pane move, keeping the title row as the drag handle.
			if key, idx, onClose, ok := m.tabBarHit(msg.X, msg.Y); ok {
				inst := m.activeWS().Panes.Get(key)
				if msg.Button == tea.MouseMiddle {
					m.closeBarTab(key, idx)
					return m, nil
				}
				if msg.Button == tea.MouseLeft {
					if onClose {
						// The segment's ✕ zone (#1128) closes the clicked
						// tab; a dirty one is selected first so the
						// unsaved-changes guard targets it.
						if len(m.dirtyOnClose(inst, idx)) > 0 {
							m.setFocus(key)
							m.switchTab(inst, idx)
							m.guardedCloseFocused()
							return m, nil
						}
						m.closeBarTab(key, idx)
						return m, nil
					}
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
			// Mouse-idle hover (#1129): overlay branches returned above, so
			// this motion is over plain panes — track the resting cell.
			return m, m.trackMouseHover(msg)
		}
		// A drag owns the mouse: no idle hover while it runs.
		m.cancelMouseHover()
		m.drag.curX, m.drag.curY = msg.X, msg.Y
		switch m.drag.kind {
		case dragResize:
			m.drag.divider.ResizeTo(msg.X, msg.Y)
			m.layout()
		case dragTermSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if term := m.dragTerminal(m.drag.srcPane); term != nil {
					// Dragging past a pane edge auto-scrolls the terminal
					// (#1821); the returned tick keeps it scrolling while the
					// pointer rests there.
					return m, term.MouseDrag(lx, ly)
				}
			}
		case dragEditSelect:
			// dragEditor: the drag may target the inline playground result buffer
			// (#1970) instead of the pane's own document editor.
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if ed := m.dragEditor(m.drag.srcPane); ed != nil {
					ed.MouseDrag(lx, ly)
				}
			}
		case dragEditScroll:
			if _, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if ed := m.dragEditor(m.drag.srcPane); ed != nil {
					ed.ScrollbarDrag(ly)
				}
			}
		case dragExplScroll:
			// The explorer thumb follows the pointer (#1036).
			if _, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindExplorer {
					inst.Explorer().ScrollbarDrag(ly)
				}
			}
		case dragScratchDiv:
			// The Scratches divider follows the pointer (#1963), resizing the
			// section against the tree.
			if _, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindExplorer {
					inst.Explorer().ScratchDividerDrag(ly)
				}
			}
		case dragDebugDiv:
			if lx, _, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindDebug {
					inst.Debug().ResizeSeparator(m.drag.sep, lx)
				}
			}
		case dragHTTPSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.bodyContent(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindHTTP {
					inst.HTTP().MouseDrag(lx, ly)
				}
			}
		case dragHTTPScroll:
			// The response-viewer thumb follows the pointer (#1367).
			if _, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.bodyContent(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindHTTP {
					inst.HTTP().ScrollbarDrag(ly)
				}
			}
		case dragTermScroll:
			// The scrollback thumb follows the pointer (#1368).
			if _, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if term := m.dragTerminal(m.drag.srcPane); term != nil {
					term.ScrollbarDrag(ly)
				}
			}
		case dragDiffSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.bodyContent(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindDiff {
					inst.Diff().MouseDrag(lx, ly)
				}
			}
		case dragMergeSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindMerge {
					inst.Merge().MouseDrag(lx, ly)
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
			if m.drag.kind == dragTab && m.drag.srcInst != nil {
				// A popup-layer tab drag (#1793) never touches the layout:
				// it tears the tab out into a floating panel or moves it
				// into another layer box, session live.
				d := m.drag
				m.drag = nil
				m.commitPopupTabTear(d, msg.X, msg.Y)
				return m, nil
			}
			if m.drag.kind == dragMove {
				m.commitMove(msg.X, msg.Y)
			} else {
				m.commitTabMove(msg.X, msg.Y)
			}
		case dragTermSelect:
			if lx, ly, ok := m.termLocal(m.drag.srcPane, msg); ok {
				if term := m.dragTerminal(m.drag.srcPane); term != nil {
					term.MouseRelease(lx, ly)
				}
			}
			m.drag = nil
			return m, nil // a selection drag never moved the layout
		case dragEditSelect:
			m.drag = nil
			return m, nil // the editor selection is already in place; nothing to commit
		case dragHTTPSelect:
			if inst := m.bodyContent(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindHTTP {
				inst.HTTP().MouseRelease()
			}
			m.drag = nil
			return m, nil // a selection drag never moved the layout
		case dragDiffSelect:
			if inst := m.bodyContent(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindDiff {
				inst.Diff().MouseRelease()
			}
			m.drag = nil
			return m, nil // a selection drag never moved the layout
		case dragMergeSelect:
			if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindMerge {
				inst.Merge().MouseRelease()
			}
			m.drag = nil
			return m, nil // a selection drag never moved the layout
		case dragEditScroll:
			m.drag = nil
			return m, nil // the viewport already followed the thumb; nothing to commit
		case dragExplScroll:
			m.drag = nil
			return m, nil // the tree already followed the thumb; nothing to commit
		case dragScratchDiv:
			// An unmoved press-release toggles the section collapse (#1963);
			// either way the new collapse/height state persists immediately,
			// like the show-hidden toggle (#629).
			if inst := m.activeWS().Panes.Get(m.drag.srcPane); inst != nil && inst.Kind() == pane.KindExplorer {
				inst.Explorer().ScratchDividerRelease()
			}
			m.drag = nil
			saveSession(m.snapshotSession())
			return m, nil
		case dragHTTPScroll:
			m.drag = nil
			return m, nil // the viewport already followed the thumb; nothing to commit
		case dragTermScroll:
			m.drag = nil
			return m, nil // the scrollback view already followed the thumb; nothing to commit
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
		if inst := m.activeWS().Panes.Get(target); canHostTabs(inst) && m.dragCarriesTab(m.drag) {
			zone = layout.DropZoneWithCenter(r, x, y)
		}
		if zone == layout.ZoneCenter {
			// Center drop on a tab host merges the source pane's files into
			// the target's tab list instead of relocating the pane (#318); a
			// terminal pane moves its live session there as a terminal tab
			// (#708), a viewer pane its live content as a content tab (#1778).
			// A terminal/tool or viewer target converts into a tab host first
			// (#836), its running content becoming the first tab.
			if !m.ensureTabHost(target) {
				return
			}
			if m.dragCarriesTerminal(m.drag) {
				m.adoptTerminalPane(m.drag.srcPane, target)
				return
			}
			if m.dragCarriesContent(m.drag) {
				m.adoptContentPane(m.drag.srcPane, target)
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
	if tab := inst.Tab(m.drag.srcTab); tab != nil && tab.Content() != nil {
		m.commitContentTabMove(x, y, inst, r)
		return
	}
	ed := inst.TabEditor(m.drag.srcTab)
	if ed == nil || !ed.HasFile() {
		return
	}
	path := ed.Path()
	// A dragged-out pinned tab keeps its pin in the destination pane (#1172).
	srcPinned := inst.TabPinned(m.drag.srcTab)
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
				m.splitTabTo(target, zone, path, ed, srcPinned)
			}
			return
		}
		// A tab-hosting target shows five zones (#318): the center merges
		// the file into its tab list (a terminal/tool target converts
		// first, #836), the edges split next to it like #317.
		if zone := layout.DropZoneWithCenter(m.lay.Panes[target], x, y); zone != layout.ZoneCenter {
			m.splitTabTo(target, zone, path, ed, srcPinned)
			return
		}
		if !m.ensureTabHost(target) {
			return
		}
		if m.openInTab(target, path) && srcPinned {
			tinst.SetTabPinned(tinst.ActiveTab(), true)
		}
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
		m.splitTabTo(src, zone, path, ed, srcPinned)
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

// commitContentTabMove applies a content tab's drag release (#1778),
// mirroring commitTerminalTabMove: another tab host's center zone moves the
// live content into that pane's tab list (the target converts first when
// needed, #836); any edge zone — a host's, a non-host pane's (#317
// semantics) or the source pane's own — splits the content off as its own
// viewer pane again. The content never reloads; a pinned tab keeps its pin
// (#1172).
func (m *Model) commitContentTabMove(x, y int, inst *pane.Instance, r layout.Rect) {
	src := m.drag.srcPane
	target, ok := m.lay.PaneAt(x, y)
	if !ok || (target == src && y < r.Y+layout.TitleBarRows) {
		return // dropped outside any pane, or a plain click (#304 semantics)
	}
	if target == src {
		if zone, near := edgeZone(r, x, y); near {
			m.splitContentTabTo(src, zone)
		}
		return
	}
	tinst := m.activeWS().Panes.Get(target)
	if tinst == nil {
		return
	}
	if !canHostTabs(tinst) {
		if zone, near := edgeZone(m.lay.Panes[target], x, y); near {
			m.splitContentTabTo(target, zone)
		}
		return
	}
	if zone := layout.DropZoneWithCenter(m.lay.Panes[target], x, y); zone != layout.ZoneCenter {
		m.splitContentTabTo(target, zone)
		return
	}
	if !m.ensureTabHost(target) {
		return
	}
	srcPinned := inst.TabPinned(m.drag.srcTab)
	nested, ok := inst.DetachContentTab(m.drag.srcTab)
	if !ok {
		return
	}
	tinst.AddContentTab(nested)
	if srcPinned {
		tinst.SetTabPinned(tinst.ActiveTab(), true)
	}
	m.setFocus(target)
	m.layout()
}

// splitContentTabTo finishes a content tab's drag by splitting pane target at
// zone into a fresh viewer pane hosting the dragged tab's live content
// (#1778). When the split — or the re-registration — is refused the tab is
// re-adopted, never dropped.
func (m *Model) splitContentTabTo(target string, zone layout.Zone) {
	inst := m.activeWS().Panes.Get(m.drag.srcPane)
	if inst == nil {
		return
	}
	nested, ok := inst.DetachContentTab(m.drag.srcTab)
	if !ok {
		return
	}
	newKey, ok := m.activeWS().Panes.AddContentPaneFrom(nested)
	if !ok {
		inst.AddContentTab(nested)
		return
	}
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, zone)
	if !ok {
		if c, detached := m.activeWS().Panes.Get(newKey).DetachContent(); detached {
			inst.AddContentTab(c)
		}
		m.activeWS().Panes.Close(newKey) // content-less after the detach: harmless
		return
	}
	m.activeWS().Tree = tree
	m.setFocus(newKey)
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
// A pinned source tab keeps its pin on the new pane (#1172).
func (m *Model) splitTabTo(target string, zone layout.Zone, path string, ed *editor.Model, pinned bool) {
	newKey := m.activeWS().Panes.AddEditor()
	m.installEmitter(newKey)
	tree, ok := layout.SplitLeaf(m.activeWS().Tree, target, newKey, zone)
	if !ok {
		m.activeWS().Panes.Close(newKey)
		return
	}
	m.activeWS().Tree = tree
	m.layout()
	if inst := m.activeWS().Panes.Get(newKey); m.openInTab(newKey, path) && pinned && inst != nil {
		inst.SetTabPinned(inst.ActiveTab(), true)
	}
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
	localY := msg.Y - (r.Y + m.contentYOff(key))
	// The inline playground (#1970) owns its pane's mouse: body clicks
	// move the caret and select in the result buffer, header clicks return
	// the focus to the query line. A click into any other pane just moves
	// the focus (#1980) — the playground stays mounted with its query and
	// result intact, and its key routing is scoped to its pane, so the
	// clicked pane takes typing normally.
	if s := m.play; s != nil && key == s.paneKey {
		return m.playPaneClick(key, msg, localX, localY)
	}
	// The breadcrumbs row (#1153) sits between the title row and the content
	// (content-local y = -1): a left press on a symbol segment jumps there, any
	// other press on the row is swallowed so it can't hit the content below.
	if inst.Kind() == pane.KindEditor && localY == -1 && m.breadcrumbRows(inst) == 1 {
		if msg.Button == tea.MouseLeft {
			return m.breadcrumbClick(key, inst, localX)
		}
		return m, nil
	}
	if c := inst.ActiveContent(); c != nil {
		// A tab host's body takes clicks like the equivalent dedicated pane
		// (#1778): a data-viewer tab pages, an HTTP tab selects, and so on.
		inst = c
	}
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
		// A left press on the Scratches divider (#1963) arms a resize drag;
		// an unmoved release toggles the collapse instead (the click).
		if msg.Button == tea.MouseLeft && exp.ScratchDividerHit(localX, localY) {
			exp.ScratchDividerPress()
			m.drag = &dragState{kind: dragScratchDiv, srcPane: key, curX: msg.X, curY: msg.Y}
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
				// cmd+click on a file:line reference opens it (#1168),
				// mirroring the editor's cmd+click; it never anchors a
				// selection, so plain-click selection stays untouched.
				// The scrollback scrollbar (#1368) outranks the link and
				// selection routing, like in a dedicated terminal pane.
				if term.ScrollbarHit(localX, localY) {
					if term.ScrollbarPress(localY) {
						m.drag = &dragState{kind: dragTermScroll, srcPane: key, curX: msg.X, curY: msg.Y}
					}
					return m, nil
				}
				if msg.Mod&(tea.ModSuper|tea.ModMeta) != 0 {
					if p, line, col, ok := term.LinkAt(localX, localY); ok {
						return m.openPathAt(p, line, col)
					}
					return m, nil
				}
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
				// The caret moved first, so the merge entries (#1149) reflect
				// the clicked position: they appear only when it sits inside a
				// conflict block.
				m.ctxMenu.Open(editorContextItems(ed.ConflictAtCursor()), msg.X, msg.Y, m.width, m.height)
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
		// (0350, #577), JetBrains-style — everywhere, including test-marker
		// lines, so breakpoints on test functions keep working. Running the
		// test from its ▶ gutter marker (#1150) is the modified click:
		// ctrl+click or cmd+click on the marker's line (on lines without a
		// marker the modified click still toggles the breakpoint).
		if ed := inst.Editor(); ed != nil && ed.HasFile() && msg.Button == tea.MouseLeft && msg.Mod&tea.ModAlt == 0 {
			if line, ok := ed.GutterHit(localX, localY); ok {
				if msg.Mod&(tea.ModCtrl|tea.ModSuper|tea.ModMeta) != 0 {
					if t, isTest := ed.TestMarkAt(line); isTest {
						return m, m.runTest(ed.Path(), &t)
					}
				}
				m.toggleBreakpoint(ed.Path(), line)
				return m, nil
			}
		}
		// A plain left click on the ⧉ affordance of a collapsed fold header
		// (#1787) copies the hidden range instead of moving the caret; the
		// rest of the header row keeps its click meaning.
		if ed := inst.Editor(); ed != nil && msg.Button == tea.MouseLeft &&
			msg.Mod&(tea.ModAlt|tea.ModCtrl|tea.ModSuper|tea.ModMeta) == 0 {
			if line, ok := ed.FoldCopyHit(localX, localY); ok {
				return m, ed.CopyFoldAt(line)
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
				m.closePane(key)
				return m, nil
			}
			// A press on the scrollback scrollbar (#1368) outranks the link
			// and selection routing there; the hit is false for a
			// mouse-reporting child, whose clicks keep passing through.
			if inst.Terminal().ScrollbarHit(localX, localY) {
				if inst.Terminal().ScrollbarPress(localY) {
					m.drag = &dragState{kind: dragTermScroll, srcPane: key, curX: msg.X, curY: msg.Y}
				}
				return m, nil
			}
			// cmd+click on a file:line reference opens it (#1168); no link
			// under the pointer keeps the press inert so it cannot steal a
			// selection anchor.
			if msg.Mod&(tea.ModSuper|tea.ModMeta) != 0 {
				if p, line, col, ok := inst.Terminal().LinkAt(localX, localY); ok {
					return m.openPathAt(p, line, col)
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
	case pane.KindTests:
		// Test-tree clicks (#1911): a click selects (a detail-column click
		// moves the scroll focus), a double-click jumps to the test.
		if msg.Button == tea.MouseLeft {
			return m, inst.Tests().Click(localX, localY)
		}
	case pane.KindIssues:
		// Issues-window clicks (#1934, #2090): a click on the tab bar
		// switches the view, a body click selects, a double-click opens the
		// issue's detail (or the pull request's page).
		if msg.Button == tea.MouseLeft {
			return m, inst.Issues().Click(localX, localY)
		}
	case pane.KindBreakpoints:
		// Breakpoints-list clicks (#1377): a click selects, the glyph cell
		// flips enabled, a double-click jumps to the breakpoint.
		if msg.Button == tea.MouseLeft {
			return m, inst.Breakpoints().Click(localX, localY)
		}
	case pane.KindStructure:
		// Structure-pane clicks (#1025): a row click selects, a double-click
		// navigates; the emitted message routes like the key-driven enter.
		if msg.Button == tea.MouseLeft {
			return m, inst.Structure().Click(localX, localY)
		}
	case pane.KindDOM:
		// DOM-inspector clicks (#1929): the selector line starts editing, a
		// fold glyph toggles, a row click selects, a double-click navigates.
		if msg.Button == tea.MouseLeft {
			return m, inst.DOM().Click(localX, localY)
		}
	case pane.KindDoctor:
		// Doctor-trace clicks (#1991): a row click selects.
		if msg.Button == tea.MouseLeft {
			return m, inst.Doctor().Click(localX, localY)
		}
	case pane.KindUsages:
		// Usages-list clicks (#1155): a click selects, a double-click opens
		// the reference's location, mirroring the Problems panel.
		if msg.Button == tea.MouseLeft {
			return m, inst.Usages().Click(localX, localY)
		}
	case pane.KindArchive:
		// Archive-pane clicks (#1852): a click selects the row, a press on a
		// directory's fold glyph toggles it, and a double-click activates —
		// opening a file read-only, exactly like enter.
		if msg.Button == tea.MouseLeft {
			return m, inst.Archive().Click(localX, localY)
		}
	case pane.KindData:
		// Data-viewer clicks (#1788): the clicked half takes the region
		// focus, a sidebar click selects the object and a double-click loads
		// it (like enter), a grid click moves the row cursor.
		if msg.Button == tea.MouseLeft {
			return m, inst.Data().Click(localX, localY)
		}
	case pane.KindHTTP:
		// Response-viewer clicks (#1266): a left press anchors a text
		// selection and tracks the drag, exactly like a terminal pane —
		// double click selects a word, triple click the line.
		if msg.Button == tea.MouseLeft {
			// A press on the scrollbar column (#1367) outranks the selection
			// there: thumb presses start a drag, track presses jump.
			if inst.HTTP().ScrollbarHit(localX, localY) {
				if inst.HTTP().ScrollbarPress(localY) {
					m.drag = &dragState{kind: dragHTTPScroll, srcPane: key, curX: msg.X, curY: msg.Y}
				}
				return m, nil
			}
			// The header's ⟳ re-send affordance (#1832) sits on the title row,
			// which selection would otherwise claim: clicking it sends the
			// shown response's stored request again.
			if inst.HTTP().ResendHit(localX, localY) {
				return m, m.resendHTTPRequest()
			}
			// The ⧉ affordance of a collapsed fold (#1787) is one cell and
			// outranks both: it copies the hidden range instead of selecting
			// or toggling the fold.
			if row, ok := inst.HTTP().FoldCopyHit(localX, localY); ok {
				return m, inst.HTTP().CopyFoldAt(row)
			}
			inst.HTTP().MousePress(localX, localY)
			m.drag = &dragState{kind: dragHTTPSelect, srcPane: key, curX: msg.X, curY: msg.Y}
		}
	case pane.KindDiff:
		// Diff-viewer clicks (#2070): a left press anchors a text selection
		// over the rendered rows, like the HTTP response pane. Edit mode's
		// embedded editor owns the pane then (#496) and the model ignores
		// presses, so no drag is armed.
		if msg.Button == tea.MouseLeft && !inst.Diff().EditMode() {
			inst.Diff().MousePress(localX, localY)
			m.drag = &dragState{kind: dragDiffSelect, srcPane: key, curX: msg.X, curY: msg.Y}
		}
	case pane.KindMerge:
		// Merge-view clicks (#2070): a press in a read-only side column
		// anchors a text selection; the middle result column stays the
		// embedded editor's ground.
		if msg.Button == tea.MouseLeft && inst.Merge().MousePress(localX, localY) {
			m.drag = &dragState{kind: dragMergeSelect, srcPane: key, curX: msg.X, curY: msg.Y}
		}
	case pane.KindDebug:
		// Debug-panel clicks (#626): select a frame/variable, double-click to
		// activate (frame select / variable expand); messages route like keys.
		if msg.Button == tea.MouseLeft {
			// A press on the column separator starts a resize drag (#691),
			// mirroring the layout divider gesture; it never selects a row.
			if sep := inst.Debug().SeparatorHit(localX); sep >= 0 {
				m.drag = &dragState{kind: dragDebugDiv, srcPane: key, sep: sep, curX: msg.X, curY: msg.Y}
				return m, nil
			}
			return m, inst.Debug().Click(localX, localY)
		}
	}
	return m, nil
}

// editorContextItems is the editor pane's right-click menu (#1020): the
// JetBrains staples, each referencing a registered command so availability
// and shortcuts resolve through the same InfoFunc as the menu bar (LSP
// entries render disabled while no server backs them). When the caret sits
// inside a merge-conflict block (#1149) the per-block accept entries are
// appended contextually.
func editorContextItems(conflict bool) []menu.Item {
	items := []menu.Item{
		{Title: "Cut", Command: "editor.cut"},
		{Title: "Copy", Command: "editor.copy"},
		{Title: "Paste", Command: "editor.paste"},
		{Title: "Go to Definition", Command: "lsp.definition"},
		{Title: "Peek Definition", Command: "lsp.peekDefinition"},
		{Title: "Find Usages", Command: "lsp.references"},
		{Title: "Find Usages (Panel)", Command: "lsp.referencesPanel"},
		{Title: "Go to Super", Command: "lsp.goToSuper"},
		{Title: "Go to Implementations", Command: "lsp.implementations"},
		{Title: "Copy Reference", Command: "file.copyReference"},
		{Title: "Open in Browser", Command: "file.openInBrowser"},
		{Title: "Show History for Selection", Command: "vcs.historyForSelection"},
		{Title: "Reformat File", Command: "lsp.format"},
		{Title: "Run Test at Cursor", Command: "run.testAtCursor"},
	}
	if conflict {
		items = append(items,
			menu.Item{Title: "Accept Ours", Command: "merge.acceptOurs"},
			menu.Item{Title: "Accept Theirs", Command: "merge.acceptTheirs"},
			menu.Item{Title: "Accept Both", Command: "merge.acceptBoth"},
		)
	}
	return items
}

// tabContextItems is the tab segment's right-click menu (#1128). The clicked
// tab was selected on open, so Close, Close Others and Pin target it; entries
// resolve through the same InfoFunc as the menu bar. The menu is built per
// open, so the pin entry's label reflects the clicked tab's state (#1172).
func tabContextItems(pinned bool) []menu.Item {
	pinTitle := "Pin Tab"
	if pinned {
		pinTitle = "Unpin Tab"
	}
	return []menu.Item{
		{Title: "Close", Command: "editor.closeTab"},
		{Title: "Close Others", Command: "editor.tab.closeOthers"},
		{Title: pinTitle, Command: "editor.tab.togglePin"},
		{Title: "Reopen Closed", Command: "editor.tab.reopenClosed"},
	}
}

// paneContextItems is the pane title band's right-click menu (#1128): layout
// operations on the pane whose band was clicked (it was focused on open).
func paneContextItems() []menu.Item {
	return []menu.Item{
		{Title: "Split Right", Command: "pane.splitRight"},
		{Title: "Split Down", Command: "pane.splitDown"},
		{Title: "Maximize", Command: "pane.maximize"},
		{Title: "Close Pane", Command: "pane.close"},
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
		{Title: "Copy Path", Command: "file.copyPath"},
		{Title: "Copy Relative Path", Command: "file.copyRelPath"},
		{Title: "Open in Browser", Command: "file.openInBrowser"},
		{Title: "Refresh", Command: "explorer.refresh"},
		{Title: "Expand All", Command: "explorer.expandAll"},
		{Title: "Reveal Open File", Command: "explorer.reveal"},
	}
}

// termLocal translates a screen-cell mouse event into pane-content-local
// coordinates for the given terminal pane key.
func (m Model) termLocal(key string, msg mouseEvent) (x, y int, ok bool) {
	if key == popupPaneKey {
		// The popup terminal layer (#1398) is not a layout leaf: content-local
		// coordinates derive from the focused host's box rectangle — a
		// floating panel's own rect (#1793), or the popup box offset to the
		// focused split side while split (#1427).
		if !m.popupLayerOpen() {
			return 0, 0, false
		}
		if f := m.floatFocused(); f != nil {
			return msg.X - (f.x + paneContentX), msg.Y - (f.y + paneContentY), true
		}
		px, py, _, _ := m.popupTermRect()
		if m.popup.focusRight && m.popup.split != nil {
			wl, _ := m.popupSplitWidths()
			px += wl
		}
		return msg.X - (px + paneContentX), msg.Y - (py + paneContentY), true
	}
	r, found := m.lay.Panes[key]
	if !found || m.activeWS().Panes.Get(key) == nil {
		return 0, 0, false
	}
	// contentYOff (not paneContentY): an editor pane showing the breadcrumbs
	// row (#1153) starts its content one row lower.
	return msg.X - (r.X + paneContentX), msg.Y - (r.Y + m.contentYOff(key)), true
}

// copyPath copies the focused file's path (#1173): absolute, relative to the
// project root, or relpath:line at the cursor. Explorer focus targets its
// selection (line form falls back to the bare relpath there).
func (m *Model) copyPath(kind int) tea.Cmd {
	path, line := "", 0
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil {
		switch inst.Kind() {
		case pane.KindExplorer:
			if p, _, ok := m.explorer().Selected(); ok {
				path = p
			}
		case pane.KindEditor:
			if ed := inst.Editor(); ed != nil && ed.HasFile() {
				path = ed.Path()
				line, _ = ed.Cursor() // 1-based
			}
		}
	}
	if path == "" {
		m.host.Notify(host.Info, "nothing to copy a path from")
		return nil
	}
	out := path
	if kind != copyAbs {
		if cwd, err := cachedGetwd(); err == nil {
			if rel, rerr := filepath.Rel(cwd, path); rerr == nil && !strings.HasPrefix(rel, "..") {
				out = rel
			}
		}
		if kind == copyRef && line > 0 {
			out += ":" + strconv.Itoa(line)
		}
	}
	m.copyToClipboard(out)
	m.host.Notify(host.Info, "copied "+out)
	return nil
}

// clipboardWrite is a seam over the system clipboard so tests don't clobber
// the user's real clipboard.
var clipboardWrite = func(text string) {
	if c := clipboard.System(); c != nil {
		_ = c.Write(text)
	}
}

// copyPanelRow is the shared handler behind the list panels' CopyMsg (#2071):
// the marked row's text goes through the same clipboard seam every pane copy
// uses, confirmed by a toast naming what was copied.
func (m Model) copyPanelRow(text, what string) (tea.Model, tea.Cmd) {
	m.copyToClipboard(text)
	m.host.Notify(host.Info, "copied "+what)
	return m, nil
}

// copyToClipboard is the host-side copy path (#2061): every pane copy action
// — the response viewer, the DOM tree, the data viewer, path/hash/curl copies
// — goes to the system clipboard *and* onto the app-wide clipboard history,
// so cmd+shift+v offers it next to the editor's yanks and deletes.
func (m Model) copyToClipboard(text string) {
	clipboardWrite(text)
	recordClipboardHistory(m.regs, text)
}

// recordClipboardHistory pushes a copy onto the shared history ring. A
// trailing newline marks the entry linewise, matching how the register store
// classifies system-clipboard reads, so pasting a copied line opens a line.
func recordClipboardHistory(regs *register.Store, text string) {
	if regs == nil {
		return
	}
	regs.PushHistory(register.Entry{Text: text, Linewise: strings.HasSuffix(text, "\n")})
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
	m.copyToClipboard(term.SelectionText())
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
	// A frame that never finishes composing freezes the loop as surely as a
	// stuck Update; the watchdog covers both (#2163).
	diag.LoopEnter("view/render")
	defer diag.LoopExit()
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
// largeFileBanner resolves the persistent large-file notice (#1124) for the
// focused editor: the banner text and its screen rect, ok=false when no
// flagged, undismissed document is focused. It renders over the pane's first
// content row (an overlay — pane geometry and mouse math stay untouched).
func (m Model) largeFileBanner() (text string, x, y, w int, ok bool) {
	key := m.activeWS().Panes.Focused()
	inst := m.activeWS().Panes.Get(key)
	if inst == nil || inst.Kind() != pane.KindEditor {
		return "", 0, 0, 0, false
	}
	ed := inst.Editor()
	if ed == nil || !ed.HasFile() || !ed.InsightOff() || largefile.NoticeDismissed(ed.Path()) {
		return "", 0, 0, 0, false
	}
	r, rok := m.lay.Panes[key]
	if !rok {
		return "", 0, 0, 0, false
	}
	w = r.W - paneContentX - 2
	if w < 10 {
		return "", 0, 0, 0, false
	}
	pal := m.pal()
	body := " Large file — highlighting and language features are disabled. Click to Force Code Insight, or raise files.large_file_kb. "
	closeZone := "✕ "
	avail := w - lipgloss.Width(closeZone)
	body = ansi.Truncate(body, avail, "…")
	if pad := avail - lipgloss.Width(body); pad > 0 {
		body += strings.Repeat(" ", pad)
	}
	text = lipgloss.NewStyle().Background(pal.Panel).Foreground(pal.Warning).Render(body) +
		lipgloss.NewStyle().Background(pal.Panel).Foreground(pal.Foreground).Bold(true).Render(closeZone)
	return text, r.X + paneContentX, r.Y + m.contentYOff(key), w, true
}

// dismissLargeBanner marks the focused flagged document's banner dismissed.
func (m Model) dismissLargeBanner() {
	if inst := m.activeWS().Panes.FocusedInstance(); inst != nil && inst.Kind() == pane.KindEditor {
		if ed := inst.Editor(); ed != nil && ed.HasFile() {
			largefile.DismissNotice(ed.Path())
		}
	}
}

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
	contentY := r.Y + m.contentYOff(key)
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
	if ed.PeekOpen() {
		// The peek-definition popup (#1154): explicitly invoked, so it wins
		// over the transient popups below.
		col, line := ed.PeekAnchor()
		return place(ed.PeekView(), col, line)
	}
	if ed.ExplainOpen() {
		// The conceal explain popover (#1998) is explicitly invoked too, and
		// owns its keys while open.
		col, line := ed.ExplainAnchor()
		return place(ed.ExplainView(), col, line)
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
	defer func() {
		took := time.Since(start)
		renderNanos.Store(int64(took))
		if perfhud.Enabled() {
			perfhud.RecordFrame(took)
		}
	}()
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
	base = m.compositePlayCompletion(base)
	base = m.compositeWhichKey(base)
	if text, bx, by, _, ok := m.largeFileBanner(); ok {
		// Persistent large-file notice (#1124): an overlay over the focused
		// pane's first content row — geometry stays untouched.
		base = overlay.Place(base, text, bx, by, m.width, m.height)
	}
	if m.ctxMenu.IsOpen() {
		// The right-click context menu (#1020) floats at its clamped anchor.
		x, y := m.ctxMenu.Pos()
		base = overlay.Place(base, m.ctxMenu.View(), x, y, m.width, m.height)
	}
	if m.popupLayerOpen() && !m.settings.IsOpen() {
		// The popup terminal layer (#1398) floats above the workspace but
		// below the exclusive overlays: a palette or the settings panel opened
		// from inside it must draw on top (settings composites earlier, so it
		// suppresses the popup for its modal lifetime instead). The layer
		// draws bottom-to-top so the topmost surface is drawn last (#1237):
		// the panels below the box, the box at its moved-and-clamped rect
		// (#1793) in its own z-slot (#1806), then the panels above it.
		below, above := m.floatTermsSplit()
		for _, f := range below {
			base = overlay.Place(base, m.renderFloatTerm(f), f.x, f.y, m.width, m.height)
		}
		if m.popup.inst != nil {
			px, py, _, _ := m.popupTermRect()
			base = overlay.Place(base, m.renderPopupTerm(), px, py, m.width, m.height)
		}
		for _, f := range above {
			base = overlay.Place(base, m.renderFloatTerm(f), f.x, f.y, m.width, m.height)
		}
	}
	result := base
	switch {
	case m.keyDoctor.IsOpen():
		result = overlay.Center(base, m.keyDoctor.View(), m.width, m.height)
	case m.finder.IsOpen():
		result = overlay.Center(base, m.finder.View(), m.width, m.height)
	case m.todo.IsOpen():
		result = overlay.Center(base, m.todo.View(), m.width, m.height)
	case m.undoTree.IsOpen():
		result = overlay.Center(base, m.undoTree.View(), m.width, m.height)
	case m.callhier.IsOpen():
		result = overlay.Center(base, m.callhier.View(), m.width, m.height)
	case m.typehier.IsOpen():
		result = overlay.Center(base, m.typehier.View(), m.width, m.height)
	case m.palette.IsOpen():
		v := m.palette.View()
		if m.palette.Anchored() {
			x, y := m.palette.AnchorPos()
			result = overlay.Place(base, v, x, y, m.width, m.height)
		} else {
			result = overlay.Center(base, v, m.width, m.height)
		}
	case m.floats.IsOpen():
		// The floating stack composites bottom-to-top (#1237): the topmost
		// layer is drawn last and fully readable over the lower ones.
		result = m.floats.Composite(base, m.width, m.height)
	}
	if perfhud.Enabled() {
		// The performance HUD (#1999) floats in the top-right corner above
		// every overlay but the toasts.
		result = m.compositePerfHUD(result)
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
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.pal().Border).
		BorderBackground(m.pal().Panel).
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
	if d.kind == dragTab && d.srcInst != nil {
		// A popup-layer tab drag (#1793) previews the floating panel the
		// release would spawn: a ghost at the pointer, sized like the source
		// box, clamped on screen.
		gw, gh := m.popupSize()
		if _, _, bw, bh, found := m.popupBoxRectFor(d.srcInst); found {
			gw, gh = bw, bh
		}
		gw, gh = min(gw, m.width), min(gh, m.height)
		label := "⌨ terminal"
		if t := d.srcInst.Tab(d.srcTab); t != nil {
			label = "⌨ " + t.Title()
		}
		gx := ui.ClampDelta(d.curX-gw/2, 0, 0, max(m.width-gw, 0))
		gy := ui.ClampDelta(d.curY, 0, 0, max(m.height-gh, 0))
		return ghostBox(gw, gh, label, m.pal().Ghost), gx, gy, true
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
	if isHost && m.dragCarriesTab(d) {
		return layout.DropZoneWithCenter(r, d.curX, d.curY), true
	}
	return layout.DropZone(r, d.curX, d.curY), true
}

// canHostTabs reports whether the pane can take a merged tab (#836, #1778):
// an editor pane natively, every other tabbable kind (terminal/tool, viewer)
// after in-place conversion. The explorer and the singleton tool windows —
// the HTTP response viewer included (#2042) — stay edge-only targets.
func canHostTabs(inst *pane.Instance) bool {
	return inst != nil && pane.KindTabbable(inst.Kind())
}

// ensureTabHost makes the target pane tab-hosting in place (#836): editors
// already are; a terminal/tool or viewer pane (#1778) converts, its live
// content becoming the first tab. Reports whether the pane can now take tabs.
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
// (#708): a tab-host target then shows the center merge zone that adopts the
// live session as a terminal tab.
func (m Model) dragCarriesTerminal(d *dragState) bool {
	if d.kind != dragMove {
		return false
	}
	inst := m.activeWS().Panes.Get(d.srcPane)
	return inst != nil && inst.Kind() == pane.KindTerminal
}

// dragCarriesContent reports whether the drag moves a whole viewer pane
// (#1778) — markdown, image, diff, archive, data — whose content a tab-host
// target could adopt as a content tab. The HTTP response viewer is a tool
// window (#2042): its drag keeps the edge-only relocate zones.
func (m Model) dragCarriesContent(d *dragState) bool {
	if d.kind != dragMove {
		return false
	}
	inst := m.activeWS().Panes.Get(d.srcPane)
	return inst != nil && inst.Kind() != pane.KindEditor && inst.Kind() != pane.KindTerminal &&
		pane.KindTabbable(inst.Kind())
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
	if inst.TabCount() > 0 && len(inst.Editors()) == 0 {
		// A tab host holding only terminal/content tabs (#1778) still merges:
		// every tab moves over, none of them is a file.
		return true
	}
	return false
}

// dragCarriesTab is the kind-agnostic "this drag could land as a tab" check
// (#1778): any tab drag, or a whole-pane move whose source content is
// tabbable.
func (m Model) dragCarriesTab(d *dragState) bool {
	return m.dragCarriesFiles(d) || m.dragCarriesTerminal(d) || m.dragCarriesContent(d)
}

// mergePaneTabs finishes a whole-pane center drop (#318, #1778): every tab of
// the source host — documents, terminal sessions, nested content — moves into
// the target's tab list. A file the target already shows stays behind as a
// duplicate and closes with the source pane, the dedupe openInTab used to do.
func (m *Model) mergePaneTabs(src, target string) {
	inst, tinst := m.activeWS().Panes.Get(src), m.activeWS().Panes.Get(target)
	if inst == nil || tinst == nil {
		return
	}
	tinst.AdoptTabsFrom(inst)
	m.installEmitter(target) // moved editors emit under the target's key now
	m.closeKey(src)
	m.setFocus(target)
	m.syncExplorerOpen()
	m.layout()
}

// adoptContentPane finishes a viewer pane's center drop on a tab host
// (#1778): the live content moves into the target's tab list as a content
// tab (no reload), then the vacated pane closes.
func (m *Model) adoptContentPane(src, target string) {
	sinst, tinst := m.activeWS().Panes.Get(src), m.activeWS().Panes.Get(target)
	if sinst == nil || tinst == nil || tinst.Kind() != pane.KindEditor {
		return
	}
	nested, ok := sinst.DetachContent()
	if !ok {
		return
	}
	tinst.AddContentTab(nested)
	m.closeKey(src)
	m.setFocus(target)
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
		if tab := inst.Tab(d.srcTab); tab != nil && (tab.IsTerminal() || tab.Content() != nil) {
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
	// Popup terminal tabs (#1398) live outside every registry — check them
	// first so their output/exit messages resolve while the popup is hidden.
	if _, _, t := m.popupTabForSession(sess); t != nil {
		return t
	}
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

// paneEditorMode reports the input mode of the editor a pane is showing, and
// whether it is showing one at all: a non-editor pane, or an editor pane whose
// active tab hosts a terminal (#573), has no mode to signal.
func paneEditorMode(inst *pane.Instance) (editor.Mode, bool) {
	if inst == nil || inst.Kind() != pane.KindEditor || inst.ActiveTerminal() != nil {
		return editor.Normal, false
	}
	ed := inst.Editor()
	if ed == nil {
		return editor.Normal, false
	}
	return ed.ModeName(), true
}

// renderPane renders a single leaf at its outer rectangle. It is the
// performance HUD's per-pane attribution point (#1999): with the HUD on, the
// wall-clock cost of this leaf's chrome and content is booked against its
// registry key, which is what answers "which pane is burning CPU". With the
// HUD off the wrapper is one atomic load — no clock read, no defer closure.
func (m Model) renderPane(key string, r layout.Rect) string {
	if perfhud.Enabled() {
		start := time.Now()
		out := m.renderPaneBox(key, r)
		perfhud.RecordPane(key, time.Since(start))
		return out
	}
	return m.renderPaneBox(key, r)
}

// renderPaneBox resolves the leaf's key to an instance for title, content, and
// focus state. During a move drag the source pane and the hovered drop target
// are recolored. An unknown key (no instance) renders an empty titled box
// rather than crashing.
func (m Model) renderPaneBox(key string, r layout.Rect) string {
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
					title = toolPaneTitle(term)
				} else {
					title = "TERMINAL — " + inst.Tab(inst.ActiveTab()).Title()
				}
			}
			if c := inst.ActiveContent(); c != nil {
				// The active tab hosts viewer content (#1778): title it like
				// the equivalent dedicated pane.
				title = contentPaneTitle(c)
			}
			// The tab bar takes over the title row once the pane holds
			// multiple tabs (#157); paneBox draws it like any title.
			if bar, ok := m.tabBar(inst, r.W-paneChromeW); ok {
				title = bar
			}
		case pane.KindTerminal:
			// A tool pane (#741) is chromed as the tool, not as a terminal:
			// no shell, no directory, no OSC title, no interpreter mappings.
			if inst.Terminal().Tool() != "" {
				title = toolPaneTitle(inst.Terminal())
			} else {
				title = m.terminalTitle(inst)
			}
		case pane.KindMarkdown, pane.KindImage, pane.KindArchive, pane.KindData, pane.KindES, pane.KindDiff, pane.KindRemote:
			title = contentPaneTitle(inst)
		case pane.KindVCS:
			title = "VCS"
		case pane.KindDebug:
			title = "DEBUG"
		case pane.KindProblems:
			title = "PROBLEMS"
		case pane.KindTests:
			title = "TESTS"
		case pane.KindIssues:
			title = "ISSUES"
		case pane.KindBreakpoints:
			title = "BREAKPOINTS"
		case pane.KindStructure:
			title = "STRUCTURE"
		case pane.KindDOM:
			title = "DOM"
		case pane.KindDoctor:
			title = "XDEBUG DOCTOR"
		case pane.KindUsages:
			title = "USAGES"
		case pane.KindHTTP:
			title = contentPaneTitle(inst)
		}
	}

	// The inline playground (#1970) takes over the pane's chrome with its
	// content: the title names the mode — jq or yq (#2039) — and the queried
	// snapshot.
	if m.playInlineActive(key) {
		title = strings.ToUpper(m.play.dialect.Name()) + " — " + m.play.source
	}

	border := m.pal().Border
	if focused {
		border = m.pal().BorderFocus
		// The focused editor's input mode paints the whole border (#1353): the
		// caret alone is too small a signal to catch at a glance. Normal mode
		// keeps BorderFocus, so the resting look is unchanged and a coloured
		// border always means "this pane is doing something other than
		// navigating" — green insert, yellow visual, red replace, blue command.
		// The playground result buffer signals its mode the same way while it holds
		// the keyboard (#1970).
		if s := m.play; s != nil && s.paneKey == key {
			if md := s.resultEd.ModeName(); s.bufFocus && md != editor.Normal {
				border = editor.ModeColor(md, m.pal())
			}
		} else if md, ok := paneEditorMode(inst); ok && md != editor.Normal {
			border = editor.ModeColor(md, m.pal())
		}
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
	var content string
	if m.playInlineActive(key) {
		// The inline playground (#1970): the query header plus the
		// read-only result buffer replace the pane's own content; the pane's
		// component keeps its state untouched underneath.
		content = m.playInlineBody(r.W - paneChromeW)
	} else {
		content = inst.View()
		// The breadcrumbs bar (#1153) is the first content row of an editor
		// pane showing it; layout() shrank the instance's interior by the same
		// row, so the composed box still fills the rect exactly.
		if row, ok := m.breadcrumbRowFor(inst, r.W-paneChromeW); ok {
			content = row + "\n" + content
		}
	}
	br, bg, bb, ba := border.RGBA()
	sig := pane.BoxSig{
		ContentHash: hashString(content),
		Title:       title,
		W:           r.W,
		H:           r.H,
		Border:      [4]uint32{br, bg, bb, ba},
	}
	return inst.CachedBox(sig, func() string { return paneBox(title, content, r.W, r.H, border) })
}

// contentPaneTitle is the title-band label of a viewer instance — the same
// chrome whether it is a dedicated pane or the active content tab of a tab
// host (#1778).
func contentPaneTitle(inst *pane.Instance) string {
	switch inst.Kind() {
	case pane.KindMarkdown:
		return "PREVIEW " + baseName(inst.Preview().Path())
	case pane.KindImage:
		return "IMAGE " + baseName(inst.Image().Path())
	case pane.KindArchive:
		return "ARCHIVE " + baseName(inst.Archive().Path())
	case pane.KindData:
		return "DATA " + baseName(inst.Data().Path())
	case pane.KindES:
		return "ES " + inst.ES().Endpoint()
	case pane.KindRemote:
		return "SFTP " + inst.Remote().Alias()
	case pane.KindDiff:
		l, r := inst.Diff().Titles()
		return "DIFF " + l + " ⇄ " + r
	case pane.KindHTTP:
		return strings.ToUpper(inst.HTTP().Title())
	}
	return ""
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
	if ed.ReadOnly() {
		// A read-only preview names what it previews (#1762): an archive
		// entry shows "main.go (src.tar)", so the pane says where the
		// unwritable content came from. A merged rotation set (#1996) names
		// the set instead: "app.log (merged)".
		if t, ok := mergedLogTitle(ed.Path()); ok {
			name = t
		} else if t, ok := remoteEntryTitle(ed.Path()); ok {
			// A remote preview names its host (#1997): "app.log (web01)".
			name = t
		} else if t, ok := archiveEntryTitle(ed.Path()); ok {
			name = t
		}
		name += " [RO]"
	}
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
		if c := inst.ActiveContent(); c != nil {
			return c.ContentTitle() // the active tab shows viewer content (#1778)
		}
		return m.editorTitle(inst.Editor())
	}
	return strings.ToUpper(strings.SplitN(key, ":", 2)[0])
}
