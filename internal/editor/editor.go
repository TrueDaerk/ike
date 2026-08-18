// Package editor implements the text-editing pane: a vim-like modal editor built
// on the buffer/mode/motion/operator/textobject/register/history/viewport/search
// sub-packages. editor.go owns the Model and dispatches key input through the
// mode state machine; the per-mode handlers live in keys_*.go and the mutating
// actions in actions.go. commands.go bridges editor actions and ex-commands to
// the plugin registry; events.go is the LSP hook seam.
package editor

import (
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/complete/mru"
	"ike/internal/concealfilter"
	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
	"ike/internal/editor/motion"
	"ike/internal/editor/register"
	"ike/internal/editor/search"
	"ike/internal/editor/viewport"
	"ike/internal/editorconfig"
	"ike/internal/highlight"
	"ike/internal/histories"
	"ike/internal/host"
	"ike/internal/idcolor"
	"ike/internal/lang"
	"ike/internal/largefile"
	ilsp "ike/internal/lsp"
	"ike/internal/textenc"
	"ike/internal/theme"
	"ike/internal/vcs"
	"ike/internal/watch"
)

// Mode is re-exported from the mode package so callers (app, tests) keep using
// editor.Normal / editor.Insert without importing the sub-package.
type Mode = mode.Mode

const (
	Normal      = mode.Normal
	Insert      = mode.Insert
	Command     = mode.CommandLine
	Visual      = mode.Visual
	VisualLine  = mode.VisualLine
	VisualBlock = mode.VisualBlock
	Replace     = mode.Replace
)

// CloseMsg asks the root model to detach the editor (result of :q / :wq).
type CloseMsg struct {
	// Force skips the app's unsaved-changes guard (":q!", #259).
	Force bool
}

// SaveAsPromptMsg asks the root model to prompt for a path for an untitled
// buffer's first save (#730). CloseAfter carries the ":wq" intent through the
// prompt: accepting it saves and then closes the pane.
type SaveAsPromptMsg struct {
	CloseAfter bool
}

// awaiting enumerates the secondary-key states the normal-mode handler can be
// parked in: waiting for a second 'g', a find target char, a replace char, a
// register name, or a text-object selector after an operator.
type awaiting int

const (
	awaitNone awaiting = iota
	awaitG
	awaitZ    // fold commands za zc zo zM zR (#144) and scrolling zz zt zb (#1193)
	awaitZBig // after Z; awaiting ZZ (save and close) or ZQ (close without saving, #1193)
	awaitFind
	awaitReplace
	awaitObject    // after operator + i/a; awaiting the object char
	awaitRecordReg // after a bare q; awaiting the macro register name (#58)
	awaitPlayReg   // after @; awaiting the macro register name or a second @ (#58)
	awaitMark      // after m; awaiting the mark name (#1151)
	awaitMarkLine  // after '; awaiting the mark to jump to (line, first non-blank)
	awaitMarkExact // after a backtick; awaiting the mark to jump to (exact position)
	awaitBracketF  // after ]; awaiting c for next-change (#1170)
	awaitBracketB  // after [; awaiting c for previous-change (#1170)
	// Surround states (#1475): ys{motion}{pair}, ds{pair}, cs{old}{new}.
	awaitSurrMotion    // after ys; awaiting the motion, i/a, or s (yss)
	awaitSurrObject    // after ys + i/a; awaiting the object char
	awaitSurrAdd       // target resolved (or visual S); awaiting the pair char
	awaitSurrDelete    // after ds; awaiting the pair to remove
	awaitSurrChange    // after cs; awaiting the pair to replace
	awaitSurrChangeNew // after cs{old}; awaiting the replacement pair
	awaitLabel         // label-jump session (#787): target chars, then a label key (labeljump.go)
)

// Model is the editor pane.
type Model struct {
	path string
	buf  *buffer.Buffer

	cursor     buffer.Position
	desiredCol int // remembered column for vertical motion across short lines

	// Multi-caret editing (#145): secondary carets fanning edits out around
	// the primary cursor, and the remembered add-next occurrence query.
	// Per-view state like the cursor — never shared between panes (#142).
	carets     []caret
	caretQuery search.Query

	mode    mode.Mode
	pending mode.Pending

	regs *register.Store
	hist *history.History
	// changes is the vim change list (#1174): the ring of recent edit
	// positions g; / g, walk. Per-view session state like the local marks;
	// reset wherever the undo history resets.
	changes changeList
	// changePending/changePos hand a freshly pushed change's CursorAfter to
	// emitChar's EventChange branch, which records it into the ring after
	// the same-event line-shift ran — recording at push time would let the
	// edit's own delta shift the brand-new entry (changelist.go).
	changePending bool
	changePos     buffer.Position
	view          viewport.Viewport

	// Secondary-key state machine.
	wait     awaiting
	findCmd  motion.FindKind // find variant parked while awaiting its char
	around   bool            // text object around (a) vs inner (i)
	lastFind motion.Find     // last f/t/F/T for ; and ,
	// Surround state parked between its await steps (#1475): the ys target
	// resolver and the pair cs is replacing.
	surrResolve surroundResolve
	surrOld     rune
	// leap is the live label-jump session (#787): non-nil from the gs /
	// editor.labelJump trigger until a label lands or the session cancels.
	leap *leapState

	// Command line / search input.
	cmdline    string
	cmdCur     int      // rune cursor within cmdline (#1110)
	cmdSuggest []string // path completion candidates on the ":" line (#543)
	// Query-history recall (#1171): histStore is the app-owned persistent
	// store (nil disables recall), cmdHistIdx the recall position on the
	// open command line (-1 = editing live, otherwise an index into the
	// bucket, newest first), cmdHistLive the stashed live line while
	// walking so down past the newest entry restores it.
	histStore   *histories.Store
	cmdHistIdx  int
	cmdHistLive string
	searching   bool
	searchDir   search.Direction
	query       search.Query
	// searchIgnoreCase mirrors editor.search_ignore_case (#1111): in-file
	// searches fold case by default; \C in the query forces exact matching.
	searchIgnoreCase bool

	// Incremental search (#255): the live-compiled preview query while the
	// "/" line is open, the cursor/viewport captured at search start (Esc
	// restores them exactly), and whether match highlights are shown
	// (cleared by a normal-mode Esc, vim's :noh; re-armed by / n N *).
	preview       search.Query
	searchOrigin  buffer.Position
	searchOrigTop int
	searchOrigLft int
	hlActive      bool
	cmdMsg        string           // transient ":"-line message (errors, reports); shown while idle
	lastSub       lastSubstitute   // last :substitute, for a bare ":s" repeat
	subConfirm    *subConfirmState // active ":s///c" confirmation, nil when idle
	replPanel     *replacePanel    // open find/replace panel (0240 phase 2, #283); nil when idle
	// panelFind/panelRepl remember the panel fields across opens (#292).
	panelFind, panelRepl string

	// Visual mode anchor (the fixed end of the selection).
	anchor buffer.Position

	// Multi-click state (#975): consecutive clicks on the same cell within
	// doubleClickWindow escalate click → word → line selection. clickVisual
	// marks a selection made by such a multi-click — a later plain click
	// collapses it back to a bare cursor. clickNow is the clock, overridable
	// in tests; nil means time.Now.
	lastClickAt  time.Time
	lastClickPos buffer.Position
	clickStreak  int
	clickVisual  bool
	clickNow     func() time.Time
	// Origin word of a double-click (#977): a word-wise drag keeps it fully
	// selected while extending in either direction.
	dragWord buffer.Range

	// True when the current visual selection was entered from insert/replace
	// mode (#979, mouse selection while editing): Backspace/Delete then
	// returns to insert mode after removing the selection.
	visualFromInsert bool

	// True while the active selection was started with Shift+arrows (#326):
	// such a selection is GUI-style — an unshifted navigation key drops it
	// instead of extending it (vim's keymodel=stopsel). Selections entered
	// with v/V/ctrl+v keep vim semantics.
	shiftSelect bool

	// Last visual selection line bounds (0-based) for the '< / '> ex addresses;
	// -1 when no selection has been made this session.
	visualStart int
	visualEnd   int

	// Last visual selection (anchor, cursor, variant) for "gv" (#1193);
	// refreshed on every visual-mode key so the state at exit survives.
	lastVis visualSnapshot

	// Position of the last insert-mode exit for "gi" (#1193).
	lastInsert struct {
		pos buffer.Position
		ok  bool
	}

	// Hard-wrap column for "gq" (editor.text_width, #1193).
	textWidth int

	// Insert-session recording for "." repeat.
	insert insertSession
	dot    *dotCommand

	// Macro recording & replay (#58). Macros are keystroke lists, not text, so
	// they live beside the register store rather than in it; like registers
	// they are per-view state (#142). recordReg is the register being recorded
	// into (0 when idle), recordKeys the keys captured so far, replayDepth the
	// live @-replay nesting (replayed keys are not re-recorded and the depth is
	// capped against runaway recursive macros), lastMacro the register @@ repeats.
	macros      map[rune][]tea.KeyPressMsg
	recordReg   rune
	recordKeys  []tea.KeyPressMsg
	replayDepth int
	lastMacro   rune

	dirty bool
	stale bool // file changed on disk while dirty (Roadmap 0140, #82)
	// pendingSave defers a manual write behind the LSP save chain (#1148:
	// organize imports, then format, then write); CompleteChainedSave performs
	// the write when the chain's SaveChainDoneMsg arrives. Per view, like the
	// save entry points themselves.
	pendingSave *pendingSave
	// Dependency-file edit guard (#565): depFile is set at Load when the path
	// lives under a dependency directory (.venv, node_modules, …); such a buffer
	// is read-only until the user confirms the first edit, which flips depOK for
	// the session. depPending holds the blocked edit so a confirm can replay it;
	// depSignal is set for one Update cycle when an edit was blocked, so Update
	// emits the DepEditBlockedMsg that opens the host's confirmation prompt.
	depFile    bool
	depOK      bool
	depPending func(*Model)
	depSignal  bool
	// readOnly locks the buffer permanently (#1762): a preview of content that
	// has no writable on-disk home — an archive entry — where the dependency
	// guard's "confirm and unlock" has nothing to unlock into. Every mutation
	// and every write is refused outright; see readonly.go.
	readOnly bool
	// eol/enc/mixedEOL describe how the open file is stored on disk (#66):
	// the buffer itself is always LF-joined UTF-8; save re-applies this flavor.
	// mixedEOL flags a load that saw both CRLF and LF (eol keeps the first
	// occurrence) — the next save normalizes to eol, which is surfaced as a
	// warning at load time. Document properties like dirty/stale: copied on
	// share, mirrored via SyncMsg. Changed explicitly by the
	// file.setLineEndings / file.setEncoding commands, which mark the buffer
	// dirty so the conversion persists on the next save.
	eol      textenc.LineEnding
	enc      textenc.Encoding
	mixedEOL bool
	// ec is the buffer path's resolved EditorConfig settings (#63), a
	// per-buffer override layer applied on top of the [editor] config each
	// applyConfig pass (see editorconfig.go). Re-resolved when the buffer's
	// identity changes and when a watched .editorconfig changes; nil when no
	// .editorconfig applies or the layer is disabled.
	ec editorconfig.Settings
	// diskHash is the content hash of the open file when buffer and disk last
	// agreed (Load, save, external reload) — the adoption key for persistent
	// undo (#148, see undopersist.go). A document property like dirty/stale:
	// copied on share, mirrored via SyncMsg. Empty for unsaved new files and
	// crash restores (nothing to key against).
	diskHash string
	// largeFile flags a document crossing the files.large_file_kb /
	// files.large_file_lines thresholds at Load/reload (#149): code insight
	// (highlighting, LSP sync, change-event text) degrades so typing stays
	// flat. A document property like dirty/stale — copied on share, mirrored
	// via SyncMsg. editor.forceCodeInsight overrides it per path (see
	// insightOff).
	largeFile bool
	focused   bool
	width     int
	height    int

	// sbGrab is the pointer's offset within the scrollbar thumb at press time
	// (#1022), so a thumb drag keeps the grab point under the pointer.
	sbGrab int

	cfg     host.Config
	emitter Emitter

	// Render-line cache (#614): renderEpoch bumps on every mutation that can
	// change a rendered line body; lineCache memoizes per-line bodies within an
	// epoch so a vertical scroll reuses them. See linecache.go.
	renderEpoch uint64
	lineCache   *lineCacheStore

	// Test-run gutter markers (#1150): the detected test declarations, cached
	// per document version (pointer, shared across value copies like
	// lineCache). See testmarks.go.
	testCache *testMarkStore

	// Merge-conflict blocks (#1149): detected conflict markers, cached per
	// document version like testCache; its epoch keys the scrollbar stripe
	// memo. See conflict.go.
	conflictCache *conflictStore

	// Syntax highlighting (Roadmap 0100). docVersion is a monotonic document
	// version bumped on every buffer change; it tags async parse results so stale
	// spans (a newer edit already landed) are dropped. hlIndex caches the spans
	// for the current version; hlTheme resolves capture names to colours.
	docVersion int
	hlVersion  int
	hlIndex    highlight.Index
	// Rich inline rendering. conceal holds the per-line concealed column
	// ranges split out of the same parse as hlIndex — markdown marker chrome
	// (@conceal captures, #881) and stand-in replacements like decoded
	// percent-encodings (#1585); mdRender is the editor.markdown_rendering
	// toggle gating all of it; mdTables caches the detected pipe tables per
	// document version (pointer, shared across the value copies like
	// lineCache).
	conceal map[int][]concealRange
	// concealExt holds the enclosing-span extents (@conceal.extent, #1599):
	// per-line column ranges of the inline spans (emphasis, code span, link)
	// whose marker chrome reveals while the caret is anywhere inside the span,
	// not only on a marker itself.
	concealExt map[int][]concealRange
	// decodes holds the decode-family stand-ins split out of the same parse,
	// keyed by capture: decoded epoch timestamps (#1618) and the #1620 escape
	// families (unicode escapes, HTML/XML entities, base64 Secret values).
	// Per-family conceal channels, so each toggle (tsDecode, uniDecode,
	// entDecode, b64Decode — see decodeOn) gates its family apart from the
	// markdown and log rendering layers and from the other families.
	decodes  map[string]map[int][]concealRange
	tsDecode bool
	// tsDecodeSet marks a per-view toggle override, like mdRenderSet.
	tsDecodeSet bool
	// The #1620 escape-family toggles, each with its own override flag:
	// unicode escapes, HTML/XML entities, base64 Secret values.
	uniDecode    bool
	uniDecodeSet bool
	entDecode    bool
	entDecodeSet bool
	b64Decode    bool
	b64DecodeSet bool
	// Cron hints (#1624) ride the same channel: the expression draws with
	// its English schedule appended.
	cronHints    bool
	cronHintsSet bool
	// The number-readability families (#1627), one toggle each: byte sizes,
	// durations, digit grouping and the radix hints.
	sizeHints     bool
	sizeHintsSet  bool
	durHints      bool
	durHintsSet   bool
	digitGroup    bool
	digitGroupSet bool
	radixHints    bool
	radixHintsSet bool
	// Permission hints (#1656): an octal file mode draws with its symbolic
	// rwx form.
	permHints    bool
	permHintsSet bool
	// The network-literal families (#1653): a CIDR prefix draws with its
	// range and host count, a punycode host with its decoded Unicode name.
	cidrHints    bool
	cidrHintsSet bool
	idnHints     bool
	idnHintsSet  bool
	// Secret masking (#1623): the dotenv value stand-ins, on by default, with
	// its own override flag like the decode families. See secrets.go.
	secretMask    bool
	secretMaskSet bool
	// hyperlinks wraps detected URLs in OSC 8 sequences so the terminal makes
	// them clickable (#1655, hyperlink.go); editor.hyperlinks, no per-view
	// toggle.
	hyperlinks bool
	// Per-file conceal gating (#1704, concealfile.go): concealRules is the
	// compiled editor.conceal_include / conceal_exclude / conceal_file_rules
	// filter, concealRaw the joined config values it was compiled from, so the
	// per-Update applyConfig pass recompiles only when the settings change
	// (the same memo discipline as rulersRaw).
	concealRules concealfilter.Rules
	concealRaw   [3]string
	mdRender     bool
	// mdRenderSet marks a per-view toggle override (#1599), like wrapSet: the
	// applyConfig refresh stops tracking editor.markdown_rendering once the
	// view toggled.
	mdRenderSet bool
	mdTables    *mdTableState
	// Separator-delimited table rendering (#1589, svtable.go). svRender is
	// the editor.csv_rendering toggle; svTable caches the visible-row column
	// layout (pointer, shared across the value copies like mdTables).
	svRender bool
	svTable  *svState
	// svWant is the table column vertical motion aims at (#1744) — per-view
	// cursor state like desiredCol, never shared between panes.
	svWant svWant
	// docPathCache caches the caret's JSON/YAML path (#1660, docpath.go) per
	// document version and caret position (pointer, shared like svTable).
	docPathCache *docPathState
	// Log rendering (#1621, logrender.go). logRender is the
	// editor.log_rendering toggle; logRenderSet marks a per-view
	// view.toggleLogRendering override, like mdRenderSet.
	logRender    bool
	logRenderSet bool
	// logRunCache caches the collapsed repeat runs (#1650, logfold.go) per
	// document version (pointer, shared across the value copies like svTable).
	logRunCache *logRunState
	// Follow mode (#1928, follow.go): follow streams content appended to the
	// open file into the buffer, tail -f style; followPaused parks the
	// auto-scroll while the user inspects earlier lines. followOffset is the
	// byte offset of the file consumed into the buffer and followTerm whether
	// that offset sits just past a line terminator (an unterminated tail line
	// is continued by the next append). followRotated remembers a remove
	// event, so the next change reloads wholesale instead of appending into a
	// replaced file; followPrevRO restores the read-only flag on toggle-off.
	follow        bool
	followPaused  bool
	followPrevRO  bool
	followRotated bool
	followOffset  int64
	followTerm    bool
	// logDeltaCache caches the inter-line elapsed times (#1651, logdelta.go)
	// per document version, the same way.
	logDeltaCache *logDeltaState
	// PEM summaries (#1652, pemsummary.go). pemSummary is the
	// editor.pem_summary toggle, pemSummarySet a per-view
	// view.togglePemSummary override (like mdRenderSet); pemCache holds the
	// blocks of one document version (pointer, shared like logRunCache).
	pemSummary    bool
	pemSummarySet bool
	pemCache      *pemState
	// colorPreview is the inline color-swatch toggle (#790,
	// editor.color_preview): color literals tint with their own color.
	// colorPreviewSet marks a per-view view.toggleColorPreview override
	// (#1622), like mdRenderSet.
	colorPreview    bool
	colorPreviewSet bool
	// Identifier color hashing (#1626, idcolors.go). idColors is the
	// editor.id_colors toggle, idColorMin the minimum hex-run length
	// (editor.id_color_min_length); idColorsSet marks a per-view
	// view.toggleIdentifierColors override, like mdRenderSet.
	idColors    bool
	idColorsSet bool
	idColorMin  int
	// scopes are the sticky-scroll scopes (#168) delivered by the same parse
	// as hlIndex: pre-ordered multi-line declarations whose header line pins
	// at the top of the view while the cursor is inside their body.
	scopes []highlight.Scope
	// Code folding (#144): folds are the foldable ranges delivered by the
	// same parse as hlIndex (pre-order); folded is this view's collapsed set,
	// keyed by header line with the fold's end line as value — per-view state
	// like the cursor, never shared between panes (#142). foldLines is the
	// buffer line count the collapsed set is anchored against, so edits can
	// shift/dissolve folds until the next parse reconciles them (fold.go).
	folds     []highlight.Fold
	folded    map[int]int
	foldLines int
	// lspFolds are the server-provided folding ranges (#1912), replaced by
	// each FoldingRangesMsg and merged over folds by foldRanges (lspfold.go);
	// LSP ranges win on a shared header and may carry a Kind. lspFolding is
	// the lsp.folding config gate. Server folds go stale between edits until
	// the next reply, so the merge clamps them to the buffer.
	lspFolds   []highlight.Fold
	lspFolding bool
	// selRange is the extend/shrink-selection ladder state (#1912,
	// selrange.go): the innermost-first range ladder of the last request plus
	// the applied depth; nil while idle. Pointer state like hover, shared
	// across the Model's value copies.
	selRange *selRangeState
	// semIndex is the LSP semantic-token overlay (#9), layered over hlIndex
	// in styleAt; kept until the next result replaces it (stale positions may
	// briefly lag an edit, like every semantic-token client).
	semIndex highlight.Index
	// occurrences are the LSP document-highlight marks (#172): every
	// occurrence of the symbol under the cursor, refreshed debounced by the
	// bridge on cursor moves; stale positions may briefly lag an edit like
	// semIndex.
	occurrences []ilsp.DocumentHighlight
	// inlayHints are the LSP inlay hints (#171): inline parameter-name/type
	// annotations refreshed by the bridge on every change, indexed per line
	// for rendering. Stale positions may briefly lag an edit like semIndex.
	inlayHints  []ilsp.InlayHint
	hintsByLine map[int][]ilsp.InlayHint
	// lensesByLine are the LSP code lenses (#1912) indexed per anchor line,
	// rendered as one trailing virtual-text annotation via lineLensHint and
	// executed through the lsp.codeLens command.
	lensesByLine map[int][]ilsp.CodeLens
	hlTheme      highlight.Theme
	pal          *theme.Palette // active theme (Roadmap 0110); nil = default

	// LSP UI state (Roadmap 0100): diagnostics indexed by line, the autocomplete
	// popup, and the hover popup. See lsp_state.go.
	diags      []ilsp.Diagnostic
	diagByLine map[int][]ilsp.Diagnostic
	// notes are the Go-computed lint notes of the language (#1623) — dotenv's
	// duplicate keys — indexed by line and produced by the highlight pass, not
	// by a server. They are a channel of their own so a later DiagnosticsMsg
	// cannot clobber them; the gutter tint and the inline underline read both
	// (see worstSeverityOnLine, diagSeverityAt).
	notes map[int][]lang.Note
	// diagsEpoch bumps on every diagnostics replacement, marksEpoch on every
	// git-marks replacement; sbcache memoizes the scrollbar stripes against
	// both (#1097, #1131).
	diagsEpoch int
	marksEpoch int
	sbcache    *sbCache
	// gitMarks are the gutter diff markers against HEAD (Roadmap 0320, #464),
	// keyed by 0-based line like diagByLine; recomputed by the app on save,
	// external change, and vcs refresh, so positions may briefly lag an edit.
	gitMarks map[int]vcs.LineMark
	// inheritMarks are the gutter inheritance arrows (#1453), keyed by 0-based
	// line: ↑ implements/overrides, ↓ has implementations. Pushed by the LSP
	// bridge per document, so positions may briefly lag an edit like gitMarks.
	inheritMarks map[int]int
	// Vim marks (#1151): marks are this view's local marks (m{a-z}),
	// per-session like the caret set; markLines is the last observed line
	// count for the edit-shift delta (the bpLines pattern). The gm* hooks
	// reach the app-owned persistent global-mark store (m{A-Z}), injected
	// like bpSource/bpAdjust; see marks.go.
	marks     map[rune]buffer.Position
	markLines int
	gmSet     func(r rune, path string, line, col int)
	gmLines   func(path string) []int
	gmAdjust  func(path string, cursorAfter, delta int)
	// Project bookmarks (#55) reach the editor the same way: bmSigns reports
	// a file's gutter glyphs by line (mnemonic digit or the anonymous flag),
	// bmAdjust shifts the store after an edit. Nil means no bookmarks.
	bmSigns  func(path string) map[int]string
	bmAdjust func(path string, cursorAfter, delta int)
	// bpSource reports the current breakpoint lines for a file (0350, #577):
	// injected by the app so the gutter always renders the live store without
	// per-view push bookkeeping. Nil means no breakpoints feature. bpAdjust
	// reports edit-driven line-count deltas back to the store; bpLines is the
	// last observed count (the folds' foldLines pattern).
	bpSource func(path string) []int
	// bpDisabledSource reports the disabled subset (#1377); those lines draw
	// a hollow ○ marker.
	bpDisabledSource func(path string) []int
	bpAdjust         func(path string, cursorAfter, delta int)
	bpLines          int
	// paused/pausedLine mark the debugger's current line (#579), set by the
	// app while a session is stopped in this buffer.
	paused     bool
	pausedLine int
	// blameOn shows the inline blame annotation on the cursor line (#468);
	// blame is the whole-file map behind it, refreshed by the app on save and
	// vcs refresh, so positions may briefly lag an edit like gitMarks.
	blameOn bool
	blame   map[int]vcs.BlameLine
	// httpFlight are the running .http dispatches of this buffer (#1746),
	// keyed by 0-based request line and holding the indicator text the app
	// refreshes on every flight tick; nil while nothing runs.
	httpFlight map[int]string
	// debugVals holds the debugger's inline local-variable values (#1914):
	// the app-pushed locals plus the per-line annotation map computed from
	// them, cached per document version (pointer, shared across value copies
	// like testCache; lazily allocated on the first push). See debugvalues.go.
	debugVals *debugValueStore
	comp      *completionState
	compMRU   *mru.Store // recently accepted completions (#854); nil-safe
	snippet   *snippetSession
	hover     *hoverState
	// mouseHover is the pending mouse-idle hover position (#1129): set when
	// the app fires the idle hover, matched against the LSP reply's position
	// so a stale answer never opens a popup at a cell the pointer has left.
	mouseHover *buffer.Position
	signature  *signatureState
	// peek is the peek-definition popup (#1154): a cursor-anchored excerpt of
	// the definition target; owns esc/enter/scroll keys while open (peek.go).
	peek      *peekState
	popupMaxW int // app-set popup content-width cap (#316); 0 = pane-derived

	// Editor settings, refreshed from cfg on each event so live config changes
	// take effect without a restart.
	tabWidth           int
	useSpaces          bool
	autoIndent         bool
	autoClosePairs     bool
	spaceAfterPunct    bool
	trimTrailing       bool
	insertFinalNewline bool
	showInlayHints     bool
	// showCodeLens gates the code-lens annotations (#1912); rendering-only,
	// like the inlay-hint toggle.
	showCodeLens bool
	// semanticTokens gates the LSP semantic-token overlay (#9) in styleAt
	// (#1912); the cached semIndex stays, so flipping the toggle back resumes
	// instantly — the same rendering-only gate the inlay hints use.
	semanticTokens bool
	stickyScroll   bool
	stickyDepth    int
	smartPaste     bool

	// View options (#64). softWrap/wsMode/indentGuides follow the [editor]
	// config until their palette toggle flips them; the *Set flags mark a
	// per-view override so the per-Update applyConfig refresh no longer
	// clobbers the toggled value. rulersRaw caches the last parsed
	// editor.rulers string so the list isn't re-split every Update.
	softWrap     bool
	wsMode       whitespaceMode
	indentGuides bool
	// rainbowGuides colors the guides by depth (#1628, guides.go); it follows
	// editor.rainbow_indent_guides and has no per-view toggle of its own —
	// the guides themselves are what a reader turns on and off.
	rainbowGuides bool
	rulers        []int
	wrapSet       bool
	wsSet         bool
	guidesSet     bool
	rulersRaw     string

	// Per-source, per-severity decoration toggles (#1259): sevShow[1..4] gates
	// LSP marks by severity, gitShow gates git change marks by kind, across the
	// scrollbar stripe, gutter colouring and inline underlines. The diagnostic
	// set itself stays complete — the details popup, diagnostic jump and the
	// Problems window keep seeing everything.
	sevShow [5]bool
	gitShow map[vcs.LineMark]bool
}

// whitespaceMode selects which whitespace runs render visibly (#64).
type whitespaceMode int

const (
	wsNone     whitespaceMode = iota
	wsTrailing                // only the line-end whitespace run
	wsAll                     // every space and tab
)

// parseWhitespaceMode maps the editor.show_whitespace config value; config
// validation already normalised it to none|trailing|all.
func parseWhitespaceMode(v string) whitespaceMode {
	switch v {
	case "trailing":
		return wsTrailing
	case "all":
		return wsAll
	}
	return wsNone
}

// New returns an empty editor with no file loaded.
func New() Model {
	m := Model{
		buf:                buffer.New(nil),
		sbcache:            &sbCache{},
		mode:               Normal,
		regs:               register.New(),
		hist:               history.New(),
		tabWidth:           4,
		textWidth:          80,
		insertFinalNewline: true,
		spaceAfterPunct:    true,
		showInlayHints:     false,
		showCodeLens:       true,
		semanticTokens:     true,
		lspFolding:         true,
		stickyScroll:       true,
		stickyDepth:        4,
		smartPaste:         true,
		hlTheme:            highlight.NewTheme(nil, nil),
		cmdHistIdx:         -1,
		visualStart:        -1,
		visualEnd:          -1,
		eol:                textenc.LF,
		enc:                textenc.UTF8,
		lineCache:          newLineCache(),
		testCache:          newTestMarkStore(),
		conflictCache:      newConflictStore(),
		mdRender:           true,
		mdTables:           &mdTableState{},
		rainbowGuides:      true,
		svRender:           true,
		svTable:            &svState{},
		docPathCache:       &docPathState{},
		logRender:          true,
		logRunCache:        &logRunState{},
		logDeltaCache:      &logDeltaState{},
		pemSummary:         true,
		pemCache:           &pemState{},
		tsDecode:           true,
		uniDecode:          true,
		entDecode:          true,
		b64Decode:          true,
		cronHints:          true,
		sizeHints:          true,
		durHints:           true,
		digitGroup:         true,
		radixHints:         true,
		permHints:          true,
		cidrHints:          true,
		idnHints:           true,
		secretMask:         true,
		hyperlinks:         true,
		colorPreview:       true,
		idColors:           true,
		idColorMin:         idcolor.DefaultMinLength,
		sevShow:            [5]bool{false, true, true, true, true},
		gitShow: map[vcs.LineMark]bool{
			vcs.LineAdded:   true,
			vcs.LineChanged: true,
			vcs.LineDeleted: true,
		},
	}
	m.view.LineNumbers = false
	return m
}

// Configure applies the [editor] configuration section and keeps a reference so
// later changes are re-read live. Unset keys keep their built-in defaults.
func (m *Model) Configure(cfg host.Config) {
	m.bumpRender() // a live config reload can change wrap/whitespace/gutter/colors (#614)
	m.cfg = cfg
	m.rebuildTheme()
	m.applyConfig()
}

// SetPalette threads the active theme palette in (Roadmap 0110): its captures
// become the highlight defaults under any theme.captures.* overrides, and
// chrome (selection, LSP popups, diagnostics) reads its ui slots.
func (m *Model) SetPalette(p *theme.Palette) {
	m.bumpRender() // theme colors change every rendered line (#614)
	m.pal = p
	m.rebuildTheme()
}

// theme returns the active palette, defaulting when none was threaded in
// (tests, zero values), so chrome renderers never nil-check.
func (m Model) theme() *theme.Palette {
	if m.pal != nil {
		return m.pal
	}
	return theme.DefaultPalette()
}

// rebuildTheme re-derives the capture→style table from the palette defaults
// layered under the retained config, so per-key config wins over the theme.
func (m *Model) rebuildTheme() {
	var captures map[string]string
	if m.pal != nil {
		captures = m.pal.Captures
	}
	var get func(string) (string, bool)
	var keys func() []string
	if m.cfg != nil {
		get, keys = m.cfg.Get, m.cfg.Keys
	}
	// Enumeration (#1318) so an override may name a capture the theme itself
	// does not define, e.g. a grammar-specific "function.builtin".
	m.hlTheme = highlight.NewThemeKeys(captures, get, keys)
	// Pre-rendered markdown table rows bake in theme styles (#945): drop them
	// so the next render re-resolves against the new theme.
	if m.mdTables != nil {
		m.mdTables.valid = false
	}
}

// applyConfig refreshes settings from the retained config reference, then
// overlays the buffer language's indent-style default (#1137) and the
// buffer's resolved EditorConfig settings (#63) — their precedence is
// built-in defaults < IKE config < language default < .editorconfig.
func (m *Model) applyConfig() {
	if m.cfg == nil {
		m.applyLangIndent()
		m.applyEditorconfig()
		return
	}
	if v, ok := m.cfg.Get("editor.tab_width"); ok {
		if n := atoi(v, m.tabWidth); n > 0 {
			m.tabWidth = n
		}
	}
	m.useSpaces = boolOr(m.cfg, "editor.use_spaces", m.useSpaces)
	// Yank → system clipboard mirroring (#1256), vim's clipboard=unnamed.
	m.regs.SetClipboardSync(boolOr(m.cfg, "editor.clipboard_sync", m.regs.ClipboardSync()))
	m.autoIndent = boolOr(m.cfg, "editor.auto_indent", m.autoIndent)
	m.autoClosePairs = boolOr(m.cfg, "editor.auto_close_pairs", m.autoClosePairs)
	m.spaceAfterPunct = boolOr(m.cfg, "editor.typing.space_after_punctuation", m.spaceAfterPunct)
	m.trimTrailing = boolOr(m.cfg, "editor.trim_trailing_whitespace", m.trimTrailing)
	m.showInlayHints = boolOr(m.cfg, "lsp.inlay_hints", m.showInlayHints)
	m.showCodeLens = boolOr(m.cfg, "lsp.code_lens", m.showCodeLens)
	m.semanticTokens = boolOr(m.cfg, "lsp.semantic_tokens", m.semanticTokens)
	m.lspFolding = boolOr(m.cfg, "lsp.folding", m.lspFolding)
	m.insertFinalNewline = boolOr(m.cfg, "editor.insert_final_newline", m.insertFinalNewline)
	m.view.LineNumbers = boolOr(m.cfg, "editor.line_numbers", m.view.LineNumbers)
	m.view.RelativeNumbers = boolOr(m.cfg, "editor.relative_line_numbers", m.view.RelativeNumbers)
	if v, ok := m.cfg.Get("editor.scroll_off"); ok {
		m.view.ScrollOff = atoi(v, m.view.ScrollOff)
	}
	if v, ok := m.cfg.Get("editor.text_width"); ok {
		if n := atoi(v, m.textWidth); n >= 0 {
			m.textWidth = n
		}
	}
	// View options (#64): a palette toggle overrides the config value for
	// this view until the next toggle; rulers have no toggle and always track
	// the config.
	if !m.wrapSet {
		m.softWrap = boolOr(m.cfg, "editor.wrap", m.softWrap)
	}
	if !m.wsSet {
		if v, ok := m.cfg.Get("editor.show_whitespace"); ok {
			m.wsMode = parseWhitespaceMode(v)
		}
	}
	if !m.guidesSet {
		m.indentGuides = boolOr(m.cfg, "editor.indent_guides", m.indentGuides)
	}
	m.rainbowGuides = boolOr(m.cfg, "editor.rainbow_indent_guides", m.rainbowGuides)
	if v, ok := m.cfg.Get("editor.rulers"); ok && v != m.rulersRaw {
		m.rulersRaw = v
		m.rulers = parseRulers(v)
	}
	m.applyMarkToggles()
	m.refreshConcealRules() // the per-file conceal gate (#1704, concealfile.go)
	m.stickyScroll = boolOr(m.cfg, "editor.sticky_scroll", m.stickyScroll)
	m.smartPaste = boolOr(m.cfg, "editor.smart_paste", m.smartPaste)
	m.searchIgnoreCase = boolOr(m.cfg, "editor.search_ignore_case", m.searchIgnoreCase)
	if !m.mdRenderSet {
		m.mdRender = boolOr(m.cfg, "editor.markdown_rendering", m.mdRender)
	}
	m.svRender = boolOr(m.cfg, "editor.csv_rendering", m.svRender)
	if !m.logRenderSet {
		m.logRender = boolOr(m.cfg, "editor.log_rendering", m.logRender)
	}
	if !m.tsDecodeSet {
		m.tsDecode = boolOr(m.cfg, "editor.timestamp_decoding", m.tsDecode)
	}
	if !m.uniDecodeSet {
		m.uniDecode = boolOr(m.cfg, "editor.unicode_escape_decoding", m.uniDecode)
	}
	if !m.entDecodeSet {
		m.entDecode = boolOr(m.cfg, "editor.entity_decoding", m.entDecode)
	}
	if !m.b64DecodeSet {
		m.b64Decode = boolOr(m.cfg, "editor.base64_decoding", m.b64Decode)
	}
	if !m.cronHintsSet {
		m.cronHints = boolOr(m.cfg, "editor.cron_hints", m.cronHints)
	}
	if !m.sizeHintsSet {
		m.sizeHints = boolOr(m.cfg, "editor.byte_size_hints", m.sizeHints)
	}
	if !m.durHintsSet {
		m.durHints = boolOr(m.cfg, "editor.duration_hints", m.durHints)
	}
	if !m.digitGroupSet {
		m.digitGroup = boolOr(m.cfg, "editor.digit_grouping", m.digitGroup)
	}
	if !m.radixHintsSet {
		m.radixHints = boolOr(m.cfg, "editor.radix_hints", m.radixHints)
	}
	if !m.permHintsSet {
		m.permHints = boolOr(m.cfg, "editor.permission_hints", m.permHints)
	}
	if !m.cidrHintsSet {
		m.cidrHints = boolOr(m.cfg, "editor.cidr_hints", m.cidrHints)
	}
	if !m.idnHintsSet {
		m.idnHints = boolOr(m.cfg, "editor.idn_hints", m.idnHints)
	}
	if !m.secretMaskSet {
		m.secretMask = boolOr(m.cfg, "editor.secret_masking", m.secretMask)
	}
	m.hyperlinks = boolOr(m.cfg, "editor.hyperlinks", m.hyperlinks)
	if !m.pemSummarySet {
		m.pemSummary = boolOr(m.cfg, "editor.pem_summary", m.pemSummary)
	}
	if !m.colorPreviewSet {
		m.colorPreview = boolOr(m.cfg, "editor.color_preview", m.colorPreview)
	}
	if !m.idColorsSet {
		m.idColors = boolOr(m.cfg, "editor.id_colors", m.idColors)
	}
	if v, ok := m.cfg.Get("editor.id_color_min_length"); ok {
		m.idColorMin = idcolor.Clamp(atoi(v, m.idColorMin))
	}
	if v, ok := m.cfg.Get("editor.sticky_scroll_depth"); ok {
		if n := atoi(v, m.stickyDepth); n > 0 {
			m.stickyDepth = n
		}
	}
	m.applyLangIndent()
	m.applyEditorconfig()
}

// applyLangIndent overlays the buffer language's indent-style default (#1137)
// onto the global editor.use_spaces value: make recipes require a literal tab
// and gofmt output is tab-indented, so those languages declare UseTabs and
// win over the global preference. Runs before applyEditorconfig, so an
// explicit .editorconfig indent_style keeps the last word.
func (m *Model) applyLangIndent() {
	if m.path == "" {
		return
	}
	if l, ok := lang.ByPath(m.path); ok && l.UseTabs != nil {
		m.useSpaces = !*l.UseTabs
	}
}

// Load reads path into the buffer, resetting cursor, mode, and history. The
// bytes are decoded (#66): a BOM or the files.encoding fallback picks the
// character encoding, the line-ending flavor is detected and remembered for
// save, and mixed line endings surface as a warning on the ex line.
func (m *Model) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Resolve .editorconfig before decoding: its charset is the decode
	// fallback (#63). Restore the previous identity if the decode fails, so a
	// failed :e leaves the open buffer untouched.
	prevPath, prevEC := m.path, m.ec
	m.path = path
	m.resolveEditorconfig()
	text, info, err := textenc.Decode(data, m.fallbackEncoding())
	if err != nil {
		m.path, m.ec = prevPath, prevEC
		return err
	}
	m.buf = buffer.FromString(text)
	m.sniffLanguage()
	m.seedBreakpointLines()
	m.seedMarkLines()
	m.clearLocalMarks() // local marks belong to the previous content (#1151)
	m.eol, m.enc, m.mixedEOL = info.EOL, info.Encoding, info.MixedEOL
	if eol, ok := m.editorconfigEOL(); ok {
		// end_of_line applies on save, like every EditorConfig client: the
		// stored flavor flips so the next write converts.
		m.eol = eol
	}
	m.cmdMsg = ""
	if info.MixedEOL {
		m.cmdMsg = "W: mixed line endings, first is " + string(info.EOL) +
			" — saving normalizes; file.setLineEndings converts explicitly"
	}
	m.largeFile = m.limits().Exceeded(int64(len(data)), m.buf.LineCount())
	m.readOnly = false // a real file replaced any read-only preview (#1762)
	// A different file in the same view stops following (#1928); the app's
	// follow tick self-stops once no view follows.
	m.follow, m.followPaused, m.followRotated = false, false, false
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.cmdline = ""
	m.searching = false
	m.dirty = false
	m.stale = false
	// Dependency-file guard (#565): lock a vendored file on open. A reload of the
	// same path keeps a prior confirmation; loading a different file re-locks it.
	m.depFile = dependencyDir(path)
	if path != prevPath {
		m.depOK = false
	}
	m.hist = history.New()
	m.changes = changeList{} // the change list follows the history (#1174)
	m.restoreUndo(data)
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.decodes = nil
	m.notes = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.lensesByLine = nil
	m.applyConfig() // pick the .editorconfig overrides up before the next Update
	m.scroll()
	return nil
}

// NewFile points the editor at a not-yet-existing path (CLI open of a missing
// file, Roadmap 0270; `:e` on a new path — vim-style): an unmodified buffer
// whose first :w creates the file on disk. The buffer is seeded with the
// path's language template when one is registered (#170) but stays clean —
// discarding it by quitting loses nothing user-authored. Everything else
// resets exactly like Load.
func (m *Model) NewFile(path string) {
	m.path = path
	m.resolveEditorconfig()
	m.buf = buffer.FromString(lang.TemplateFor(path))
	m.seedBreakpointLines()
	m.seedMarkLines()
	m.clearLocalMarks()                                        // local marks belong to the previous content (#1151)
	m.eol, m.enc, m.mixedEOL = textenc.LF, textenc.UTF8, false // nothing on disk to preserve (#66)
	// A new file has no on-disk flavor to preserve, so .editorconfig picks
	// the initial line endings and charset outright (#63).
	if eol, ok := m.editorconfigEOL(); ok {
		m.eol = eol
	}
	if enc, ok := m.editorconfigCharset(); ok {
		m.enc = enc
	}
	m.largeFile = false // a template seed is never large
	m.readOnly = false  // a new file is writable from its first :w (#1762)
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.cmdline = ""
	m.searching = false
	m.dirty = false
	m.stale = false
	// A newly created file is authored by the user even under a dependency dir,
	// so it is never guarded (#565).
	m.depFile = false
	m.depOK = false
	m.hist = history.New()
	m.changes = changeList{} // the change list follows the history (#1174)
	m.diskHash = ""          // nothing on disk yet; the first :w stamps it
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.decodes = nil
	m.notes = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.lensesByLine = nil
	m.applyConfig() // pick the .editorconfig overrides up before the next Update
	m.scroll()
}

// RestoreText installs crash-recovered text into the buffer and marks it dirty
// (Roadmap 0210). Undo history resets to the recovered content — recovery is a
// fresh starting point, not a continuation of the dead session's history. The
// path is left as-is, so the caller can Load the base file first (titled restore)
// or leave it empty (untitled restore).
func (m *Model) RestoreText(text string) {
	m.buf = buffer.FromString(text)
	m.seedBreakpointLines()
	m.seedMarkLines()
	m.clearLocalMarks() // recovered text is a fresh starting point (#1151)
	m.largeFile = m.limits().Exceeded(int64(len(text)), m.buf.LineCount())
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.hist = history.New()
	m.changes = changeList{} // the change list follows the history (#1174)
	m.hist.MarkNeverSaved()  // recovered text is dirty even after undoing back to it
	m.diskHash = ""          // recovered content matches no on-disk state
	m.dirty = true
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.decodes = nil
	m.notes = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.lensesByLine = nil
	m.scroll()
}

// sniffLanguage wires the content/context sniff layer on open. Context
// sniffers (#897) run first and may override the extension's verdict (a
// role-tree .yml is ansible, not yaml); the shebang fallback (#893) runs only
// when neither the static lookups nor a sniffer resolve the path. A hit is
// recorded in the lang registry via AssociatePath, so every path-keyed
// consumer — highlighting, LSP didOpen, the statusline — resolves the file
// through the ordinary ByPath from here on.
func (m *Model) sniffLanguage() {
	if m.path == "" || m.buf.LineCount() == 0 {
		return
	}
	// A user-configured association (#1365) is explicit intent: it beats the
	// sniffers, so an associated file never gets re-classified by content.
	if _, ok := lang.ByAssociation(m.path); ok {
		return
	}
	if l, ok := lang.Sniff(m.path); ok {
		lang.AssociatePath(m.path, l.ID)
		return
	}
	if _, ok := lang.ByPath(m.path); ok {
		return
	}
	if l, ok := lang.ForShebang(m.buf.Line(0)); ok {
		lang.AssociatePath(m.path, l.ID)
	}
}

// Path returns the loaded file path ("" when no file is open).
func (m Model) Path() string { return m.path }

// SetPath re-points the editor at a new location of the same file after a
// rename or move (#175): buffer, cursor, mode and — crucially — undo history
// stay exactly as they are; only the path changes. Highlighting restarts (a
// new extension can mean a new grammar); the returned command runs the
// reparse. The emitted change event carries the new path, so the LSP bridge
// syncs the document under it.
func (m *Model) SetPath(path string) tea.Cmd {
	if path == m.path || m.path == "" {
		return nil
	}
	m.path = path
	m.resolveEditorconfig()
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.decodes = nil
	m.notes = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
	m.lensesByLine = nil
	m.emit(EventChange)
	return m.parseCmd()
}

// Text returns the full buffer content (host-side consumers: tests, the
// upcoming diff viewer #60).
func (m Model) Text() string { return m.buf.String() }

// Dirty reports whether the buffer has unsaved changes.
func (m Model) Dirty() bool { return m.dirty }

// Stale reports whether the file changed on disk while the buffer holds
// unsaved edits (Roadmap 0140): the tab and status line show an indicator and
// the next save opens the conflict prompt.
func (m Model) Stale() bool { return m.stale }

// LargeFile reports whether the document crossed the large-file thresholds at
// its last load/reload (#149).
func (m Model) LargeFile() bool { return m.largeFile }

// InsightOff reports whether code insight is degraded for this document
// (#149): flagged large and not overridden per path via ForceCodeInsight. The
// status line renders its indicator off this, and parseCmd/emit gate on it.
func (m Model) InsightOff() bool { return m.largeFile && !largefile.Forced(m.path) }

// ForceCodeInsight punches through the large-file degradation for this
// document's path (editor.forceCodeInsight, #149): highlighting and change
// text resume, and the returned command runs the first full reparse. The app
// layer re-fires the file-opened hook alongside so the LSP bridge didOpens.
// Nil when the document is not flagged.
func (m *Model) ForceCodeInsight() tea.Cmd {
	if !m.largeFile || !m.HasFile() {
		return nil
	}
	largefile.Force(m.path)
	return m.parseCmd()
}

// limits evaluates the configured large-file thresholds; no config means the
// built-in defaults.
func (m Model) limits() largefile.Limits {
	if m.cfg == nil {
		return largefile.LimitsFrom(nil)
	}
	return largefile.LimitsFrom(m.cfg.Get)
}

// ModeName returns the current modal state.
func (m Model) ModeName() Mode { return m.mode }

// Capturing reports whether the editor is consuming raw text (insert / replace /
// command line), so the host must not intercept single-letter global keys.
// Capturing also covers the modal editor prompts that consume keys ahead of
// the mode machine: the find/replace panel (#283) and the ":s///c" confirm —
// without this the app layer would steal plain keys (tab = pane cycle) from
// their inputs. A label-jump session (#787) captures the same way: its target
// and label characters include keys the app claims in plain normal mode
// (q, tab, @).
func (m Model) Capturing() bool {
	return m.mode.Capturing() || m.replPanel != nil || m.subConfirm != nil || m.leap != nil
}

// Cursor returns the 1-based line and column for the status line.
func (m Model) Cursor() (line, col int) { return m.cursor.Line + 1, m.cursor.Col + 1 }

// CursorPos returns the 0-based line and column, for session persistence.
func (m Model) CursorPos() (line, col int) { return m.cursor.Line, m.cursor.Col }

// SelectionLines returns the 1-based, inclusive line range of the active
// visual selection; ok is false outside visual mode. Used by range-scoped
// actions like Show History for Selection (#1430).
func (m Model) SelectionLines() (start, end int, ok bool) {
	if !m.mode.IsVisual() {
		return 0, 0, false
	}
	start, end = m.anchor.Line, m.cursor.Line
	if start > end {
		start, end = end, start
	}
	return start + 1, end + 1, true
}

// SelectionText returns the text of the active visual selection — whole
// lines in visual-line mode, the inclusive charwise span otherwise; ok is
// false outside visual mode. Used by selection-scoped consumers like
// Compare with Clipboard (#1477).
func (m *Model) SelectionText() (text string, ok bool) {
	if !m.mode.IsVisual() {
		return "", false
	}
	t := m.visualSelection()
	if t.Linewise {
		return strings.Join(m.buf.Lines()[t.Range.Start.Line:t.Range.End.Line+1], "\n"), true
	}
	return m.buf.Slice(t.Range), true
}

// SetCursor moves the cursor to a 0-based line/column, clamping to a valid
// normal-mode position and scrolling it into view. Used for programmatic
// placement (session restore, go-to-definition, usages picks, nav history);
// out-of-range coordinates land on the nearest valid cell. It emits an
// EventCursorMove so the LSP bridge tracks programmatic jumps the same as
// interactive motions — otherwise position-based actions (rename, references)
// right after a jump would query the pre-jump location (#371).
func (m *Model) SetCursor(line, col int) {
	m.bumpRender() // the cursor cell + current-line styling move (#614)
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: line, Col: col})
	m.desiredCol = m.cursor.Col
	m.scroll()
	m.emit(EventCursorMove)
}

// jumpTopMargin is how many context rows stay above a navigation landing
// (#996): the jumped-to line sits this far below the pane's top edge.
const jumpTopMargin = 3

// jumpEdgeMargin is the scrolloff-style comfort zone for on-screen jump
// targets (#1373): a visible target within this many rows of the viewport's
// top or bottom edge gets a minimal scroll onto the margin line instead of
// the full near-top reframe. editor.scroll_off widens it when set larger.
const jumpEdgeMargin = 5

// JumpTo places the cursor like SetCursor and frames the landing for a
// navigation jump. Off-screen targets sit jumpTopMargin rows below the
// viewport's top edge (small context margin, JetBrains-like, #996). Targets
// already comfortably visible move only the cursor, and targets inside the
// jumpEdgeMargin comfort zone scroll minimally onto the margin line (#1373) —
// no viewport yank when the destination is in sight. SetScroll clamps, so a
// target near the end of the buffer never over-scrolls.
func (m *Model) JumpTo(line, col int) {
	oldTop := m.view.Top
	m.SetCursor(line, col)
	line = m.cursor.Line
	margin := jumpEdgeMargin
	if m.view.ScrollOff > margin {
		margin = m.view.ScrollOff
	}
	h := m.view.Height()
	top := line - jumpTopMargin // off-screen (or margin-swallowed pane): #996 near-top framing
	if h > 2*margin {
		switch {
		case line >= oldTop+margin && line < oldTop+h-margin:
			top = oldTop // comfortably visible: cursor only, viewport untouched
		case line >= oldTop && line < oldTop+margin:
			top = line - margin // near the top edge: settle on the margin line
		case line >= oldTop+h-margin && line < oldTop+h:
			top = line - (h - 1 - margin) // near the bottom edge: settle on the margin line
		}
	}
	if top < 0 {
		top = 0
	}
	m.SetScroll(top, m.view.Left)
}

// HasFile reports whether a file is currently open.
func (m Model) HasFile() bool { return m.path != "" }

// IsEmpty reports whether this tab is a reusable blank: no file and no text.
// It is the single emptiness predicate shared by the file-open and diff-open
// paths (#628, #641) — a pathless tab that already holds typed scratch text is
// not empty, so opens must not fill it in place and lose the content.
func (m Model) IsEmpty() bool { return !m.HasFile() && m.buf.String() == "" }

// SetSize sets the available width and number of text rows.
func (m *Model) SetSize(width, height int) {
	if width != m.width {
		m.bumpRender() // the text width changes every line body (#614)
	}
	m.width = width
	m.height = height
	m.view.SetSize(width, height)
	m.scroll()
}

// SetFocused toggles whether this pane receives key input.
func (m *Model) SetFocused(f bool) {
	if f != m.focused {
		m.bumpRender() // focus toggles the cursor cell / current-line styling (#614)
	}
	m.focused = f
}

// ScrollTop returns the first visible buffer line (0-based) — the diff
// pane's edit mode aligns its left column to it (0340, #496).
func (m Model) ScrollTop() int { return m.view.Top }

// SetClipboard wires the system-clipboard implementation for the "+ register.
func (m *Model) SetClipboard(c register.Clipboard) { m.regs.SetClipboard(c) }

// SetRegisters replaces the editor's register store with a shared one (#1540):
// the app threads one store into every editor so named registers, the delete
// ring and the paste-from-history ring span panes, tabs and workspaces, vim
// style. Call it before Configure/SetClipboard so those apply to the shared
// store. nil keeps the private store from New (standalone editors, tests).
func (m *Model) SetRegisters(s *register.Store) {
	if s != nil {
		m.regs = s
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update routes a message to the handler for the current mode, then re-derives
// the follow-mode pause flag (#1928) for the user-driven message kinds — key
// input and palette actions move the cursor/viewport, and whether the view
// still sits at the buffer's end decides paused vs. resumed. Wheel and
// scrollbar scrolls bypass Update (ScrollBy, ScrollbarDrag) and re-derive on
// their own.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	nm, cmd := m.updateMsg(msg)
	switch msg.(type) {
	case tea.KeyPressMsg, ActionMsg:
		nm.refreshFollowPause()
	}
	return nm, cmd
}

// updateMsg is the message dispatch behind Update.
func (m Model) updateMsg(msg tea.Msg) (Model, tea.Cmd) {
	// Every routed message (a key, or an async decoration update — syntax,
	// semantic, diagnostics, git marks, occurrences, inlay hints, sync) may change
	// a rendered line, so invalidate the line cache (#614). Vertical scroll does
	// not come through here, so the cache stays warm across a scroll.
	m.renderEpoch++
	m.applyConfig()
	switch msg := msg.(type) {
	case highlight.SpansMsg:
		// Accept a parse result only if it matches the current document and
		// version; a newer edit since the parse was scheduled drops it.
		if msg.Path == m.path && msg.Version == m.docVersion {
			// Conceal spans (#881) feed the markdown rendering layer, not the
			// style index — a marker cell styles raw on the cursor line but
			// disappears elsewhere.
			style, conceal, extents, decodes := concealSplit(msg.Spans)
			m.hlIndex = highlight.NewIndex(style)
			m.conceal = conceal
			m.concealExt = extents
			m.decodes = decodes
			m.setNotes(msg.Notes)
			m.scopes = msg.Scopes
			m.folds = msg.Folds
			m.reconcileFolds()
			m.hlVersion = msg.Version
		}
		return m, nil
	case ilsp.DiagnosticsMsg:
		if msg.Path == m.path {
			m.setDiagnostics(msg.Diagnostics)
		}
		return m, nil
	case ilsp.CompletionMsg:
		if msg.Path == m.path {
			m.openCompletion(msg)
		}
		return m, nil
	case ilsp.CompletionResolveMsg:
		if msg.Path == m.path {
			m.applyCompletionResolve(msg)
		}
		return m, nil
	case ilsp.HoverMsg:
		if msg.Path == m.path && msg.Contents != "" {
			if msg.Mouse {
				// Mouse-idle hover (#1129): anchored at the hovered cell,
				// validated against the pending request, diagnostics on top.
				m.applyMouseHover(msg)
			} else {
				m.hover = m.newHover(msg.Contents)
			}
		}
		return m, nil
	case ilsp.SignatureHelpMsg:
		if msg.Path == m.path {
			m.applySignature(msg)
		}
		return m, nil
	case ilsp.SemanticSpansMsg:
		if msg.Path == m.path {
			m.semIndex = highlight.NewIndex(msg.Spans)
		}
		return m, nil
	case ilsp.FoldingRangesMsg:
		// Server folding ranges (#1912): stored next to the Tree-sitter folds
		// and merged by foldRanges (lspfold.go); reconcile re-anchors this
		// view's collapsed set against the new merged set.
		if msg.Path == m.path {
			m.lspFolds = msg.Folds
			m.reconcileFolds()
		}
		return m, nil
	case ilsp.SelectionRangesMsg:
		// Extend-selection ladder (#1912, selrange.go). The returned command
		// is the Tree-sitter fallback when the server ladder came back empty.
		return m, m.handleSelectionRanges(msg)
	case ilsp.DocumentHighlightsMsg:
		if msg.Path == m.path {
			m.applyDocumentHighlights(msg)
		}
		return m, nil
	case ilsp.InlayHintsMsg:
		if msg.Path == m.path {
			m.setInlayHints(msg.Hints)
		}
		return m, nil
	case ilsp.CodeLensesMsg:
		// Code lenses (#1912): indexed per line, rendered as trailing
		// virtual text next to the inlay hints (lineLensHint).
		if msg.Path == m.path {
			m.setCodeLenses(msg.Lenses)
		}
		return m, nil
	case ilsp.InheritanceMarksMsg:
		// Gutter inheritance arrows (#1453); the bridge already drops replies
		// that raced an edit, so a path match is the whole staleness check.
		if msg.Path == m.path {
			m.setInheritanceMarks(msg.Marks)
		}
		return m, nil
	case vcs.MarksMsg:
		// Recomputed gutter diff markers against HEAD (Roadmap 0320, #464);
		// nil clears them (clean file, untracked, not a repo).
		if msg.Path == m.path {
			m.gitMarks = msg.Marks
			m.marksEpoch++ // invalidates the scrollbar git-mark memo (#1131)
		}
		return m, nil
	case vcs.BlameMsg:
		// A refreshed inline-blame map (#468); errors clear it so a stale
		// annotation never outlives its file.
		if msg.Path == m.path {
			m.blame = msg.Lines
		}
		return m, nil
	// ilsp.FormatEditsMsg is deliberately NOT handled here: views of a shared
	// document (#142) all receive path-routed messages, and applying edits in
	// each view hit the shared buffer once per view (#366). The app applies
	// them through exactly one view (app.go) via ApplyTextEdits.
	case watch.EventMsg:
		// A changed .editorconfig re-resolves this buffer's override layer
		// (#63) before the usual external-change handling.
		if m.handleEditorconfigChange(msg.Path) {
			m.applyConfig()
		}
		// External change of the open file (Roadmap 0140): reload.go decides
		// whether to reload in place (clean buffer) or leave it alone.
		return m.handleExternalChange(msg)
	case ReconcileMsg:
		// Watcherless catch-up after a parked workspace resumes (#1515).
		return m.handleReconcile()
	case SyncMsg:
		// Another view of this shared document changed it (#142).
		return m.applySync(msg)
	case ActionMsg:
		before := m.docVersion
		m, cmd := m.runAction(msg.Action)
		return m.maybeReparse(before, cmd)
	case HistoryJumpMsg:
		// The undo-tree overlay picked a state (#59): restore the buffer to it.
		before := m.docVersion
		m.jumpHistory(msg.Seq)
		m.scroll()
		return m.maybeReparse(before, nil)
	case ConfirmDepEditMsg:
		// The host's dependency-file prompt was accepted (#565): unlock and
		// replay the blocked edit, reparsing as a normal change would.
		before := m.docVersion
		m.ConfirmDepEdit()
		m.scroll()
		return m.maybeReparse(before, nil)
	case tea.KeyPressMsg:
		if m.peek != nil {
			// The peek popup (#1154) owns esc/enter/up/down/ctrl+d/ctrl+u;
			// any other key closes it and falls through to normal dispatch.
			if handled, cmd := m.peekKey(msg); handled {
				return m, cmd
			}
		}
		m.dismissHover() // any key dismisses a hover popup
		if msg.Code == tea.KeyEscape {
			m.dismissSignature() // esc also drops the signature popup
		}
		// Macro recording (#58) taps every keypress here, before dispatch, so
		// inserts, visual selections and ex commands are captured alike. Keys
		// fed back by an @-replay are not re-recorded — a macro replayed while
		// recording stores the literal `@x`, vim-style. The stopping `q` is
		// popped again by stopRecording.
		if m.recordReg != 0 && m.replayDepth == 0 {
			m.recordKeys = append(m.recordKeys, msg)
		}
		before := m.docVersion
		var cmd tea.Cmd
		if m.subConfirm != nil {
			// An open ":s///c" confirmation consumes keys before the mode machine.
			m = m.updateSubConfirm(msg)
			m.scroll()
			return m.maybeReparse(before, cmd)
		}
		if m.replPanel != nil {
			// The find/replace panel (#283) owns the keyboard the same way.
			m, cmd = m.updateReplacePanel(msg)
			m.scroll()
			return m.maybeReparse(before, cmd)
		}
		switch m.mode {
		case Insert, Replace:
			m.updateInsert(msg)
		case Command:
			m, cmd = m.updateCommandLine(msg)
		default:
			if m.mode.IsVisual() {
				m, cmd = m.updateVisual(msg)
			} else {
				m, cmd = m.updateNormal(msg)
			}
		}
		// A blocked edit on a dependency file asks the host to confirm (#565).
		if dep := m.takeDepSignal(); dep != nil {
			cmd = tea.Batch(cmd, dep)
		}
		// A system-clipboard write that failed reports here (#1255), covering
		// every key-driven path at once: `"+y`, the Cmd+C/Cmd+X actions when
		// they arrive as keys, and the synced yanks of #1256.
		if clip := m.takeClipboardSignal(); clip != nil {
			cmd = tea.Batch(cmd, clip)
		}
		m.scroll()
		return m.maybeReparse(before, cmd)
	}
	return m, nil
}

// scroll keeps the cursor within the visible window, including the rows
// covered by pinned sticky-scroll headers (#168). It first opens any
// collapsed fold the cursor jumped into (#144) — every cursor-moving path
// funnels through here — then corrects the viewport for folds rendered as
// one row.
func (m *Model) scroll() {
	m.unfoldAtCursor()
	if m.softWrap {
		// Soft wrap (#64): follow the cursor in visual rows through the wrap
		// map; the rows callback already counts folds (header = 1, hidden =
		// 0), so no fold fix-up pass is needed.
		segs := m.wrapSegs(m.cursor.Line)
		m.view.ScrollWrapped(m.cursor.Line, viewport.SegmentIndex(segs, m.cursor.Col), m.buf.LineCount(), m.wrapRows)
	} else {
		col := m.cursor.Col
		if m.svActive() {
			// Table-rendered sv buffers scroll horizontally in display space
			// (#1724): track the caret by its expanded column so view.Left —
			// a display-cell offset there — keeps it inside the window.
			col = m.svDisplayCol(m.cursor.Line, col)
		}
		left := m.view.Left
		m.view.ScrollWidth(m.cursor.Line, col, m.buf.LineCount(), m.scrollTextWidth())
		if !m.svActive() {
			// Conceal stand-ins render at a width of their own (#1585/#1623),
			// so a rune column is not a display column: redo the offset
			// view.Scroll just derived from the raw column (#1752).
			m.concealScrollFix(left)
		}
		m.foldScrollFix()
	}
	m.unhideCursor()
}

// scrollTextWidth is the window horizontal cursor-following must keep the
// caret's display cell inside: the text width, minus the column the overlaid
// vertical scrollbar claims (#1827). Without the reservation the follow logic
// happily parks the caret in the pane's rightmost column, where the bar covers
// it — the same overlay problem right-aligned annotations solve in
// annotColumnWidth (#1728). Rendering keeps the full TextWidth: content still
// draws under the bar, only the caret must stay left of it.
func (m Model) scrollTextWidth() int {
	w := m.view.TextWidth(m.buf.LineCount())
	if _, _, _, _, ok := m.scrollbarGeometry(); ok && w > 1 {
		w--
	}
	return w
}

// concealScrollFix re-derives the horizontal offset on a cursor line carrying
// conceal ranges (#1752). view.Left stays a buffer rune column there — that is
// what renderSpan slices from — but the window it opens is measured in display
// cells, and a stand-in (secret mask #1623, decoded timestamp #1618, …) rarely
// has its source's width. Comparing raw columns therefore holds the caret
// visible while it has already run off the right edge (mask wider than the
// value) or scrolls too eagerly (mask narrower). Both sides are measured
// through the conceal expansion instead, so the smallest offset that keeps the
// caret's cell inside the text width wins. Lines without conceal ranges keep
// view.Scroll's raw-column result untouched; on the others the offset restarts
// from prev — what it was before view.Scroll ran — so the raw comparison
// cannot leave a scroll of its own behind.
func (m *Model) concealScrollFix(prev int) {
	prefix := m.concealPrefix(m.cursor.Line)
	if prefix == nil {
		return
	}
	m.view.Left = prev
	tw := m.scrollTextWidth()
	cur := concealDisplayColAt(prefix, m.cursor.Col)
	if m.view.Left > m.cursor.Col || cur < concealDisplayColAt(prefix, m.view.Left) {
		m.view.Left = m.cursor.Col
	}
	for m.view.Left < m.cursor.Col && cur-concealDisplayColAt(prefix, m.view.Left) > tw-1 {
		m.view.Left++
	}
	if m.view.Left < 0 {
		m.view.Left = 0
	}
	// An offset landing inside a stand-in renders none of it — renderSpan
	// emits a replacement only at its range start — so snap past the range to
	// keep the offset and what is drawn in agreement. The caret never sits in
	// a live range (lineConcealRanges drops those), so the range ends at or
	// before it and the caret stays visible.
	if cr, ok := rangeAt(m.lineConcealRanges(m.cursor.Line), m.view.Left); ok && m.view.Left > cr.start {
		m.view.Left = cr.end
	}
}

// moveTo places the cursor at p (clamped to a real character) and remembers the
// column for vertical motion. It emits a cursor-move event.
func (m *Model) moveTo(p buffer.Position) {
	m.cursor = m.buf.ClampCursor(p)
	m.desiredCol = m.cursor.Col
	m.emit(EventCursorMove)
}

// jumpTo is moveTo for in-file jumps (search landings): it first emits the
// departure position as an EventJump — the navigation-history seam (Roadmap
// 0220) — then moves.
func (m *Model) jumpTo(p buffer.Position) {
	m.emit(EventJump)
	m.moveTo(p)
}

// atoi parses s as an int, returning def on failure.
func atoi(s string, def int) int {
	n, sign, seen := 0, 1, false
	for i, r := range s {
		if i == 0 && r == '-' {
			sign = -1
			continue
		}
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		seen = true
	}
	if !seen {
		return def
	}
	return n * sign
}

// boolOr reads a "true"/"false" config key, returning def when absent.
func boolOr(cfg host.Config, key string, def bool) bool {
	if v, ok := cfg.Get(key); ok {
		return v == "true"
	}
	return def
}

// indentOf returns the leading whitespace run of line i (for auto-indent).
func (m *Model) indentOf(i int) string {
	return leadingWhitespace(m.buf.Line(i))
}

// tabText is the string a Tab key inserts, honouring expandtab.
func (m *Model) tabText() string {
	if m.useSpaces {
		return strings.Repeat(" ", m.tabWidth)
	}
	return "\t"
}

// GutterWidth returns the current gutter width in cells, so the app can place a
// cursor-anchored popup (completion/hover) at the right screen column.
func (m Model) GutterWidth() int { return m.view.GutterWidth(m.buf.LineCount()) }

// ScrollOffset returns the 0-based first visible line and column, so a session
// can restore the exact viewport framing (not just the cursor — Top is sticky
// and not derivable from the cursor alone).
func (m Model) ScrollOffset() (top, left int) { return m.view.Top, m.view.Left }

// bottomOverscroll is how many empty rows may show below the last line when
// scrolling past the end (#1535): a small comfort zone so the last line is
// not pinned flush to the pane's bottom edge, sized to match jumpEdgeMargin.
const bottomOverscroll = 5

// SetScroll restores the viewport framing saved by ScrollOffset, clamping into
// the current buffer. Unlike a cursor move it does not re-derive Top from the
// cursor, so the file reopens scrolled exactly as it was left. Apply it after the
// editor has been sized.
func (m *Model) SetScroll(top, left int) {
	// Bounded overscroll past the end (#1134, #1535): scrolling stops with
	// bottomOverscroll empty rows below the last line instead of an
	// almost-empty screen, and never past the point where only the last line
	// remains. Soft wrap and collapsed folds keep the looser lineCount-1
	// clamp — wrap renders more rows than lines (the tight clamp could hide
	// a wrapped tail) and folds render fewer (reaching the end can need a
	// deeper Top).
	max := m.buf.LineCount() - 1
	if h := m.view.Height(); !m.softWrap && !m.hasFolds() && h > 0 {
		if max = m.buf.LineCount() - h; max <= 0 {
			max = 0 // the buffer fits the pane: no scrolling at all
		} else if max += bottomOverscroll; max > m.buf.LineCount()-1 {
			max = m.buf.LineCount() - 1
		}
	}
	if top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}
	if left != m.view.Left {
		// Horizontal scroll shifts the rendered column window of every line, so
		// it invalidates the cache; a pure vertical move (Top only) does not (#614).
		m.bumpRender()
	}
	m.view.Top = top
	m.view.Left = left
}
