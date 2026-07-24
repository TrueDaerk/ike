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
	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
	"ike/internal/editor/motion"
	"ike/internal/editor/register"
	"ike/internal/editor/search"
	"ike/internal/editor/viewport"
	"ike/internal/editorconfig"
	"ike/internal/highlight"
	"ike/internal/host"
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
	awaitZ // fold commands: za zc zo zM zR (#144)
	awaitFind
	awaitReplace
	awaitObject    // after operator + i/a; awaiting the object char
	awaitRecordReg // after a bare q; awaiting the macro register name (#58)
	awaitPlayReg   // after @; awaiting the macro register name or a second @ (#58)
	awaitMark      // after m; awaiting the mark name (#1151)
	awaitMarkLine  // after '; awaiting the mark to jump to (line, first non-blank)
	awaitMarkExact // after a backtick; awaiting the mark to jump to (exact position)
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
	view viewport.Viewport

	// Secondary-key state machine.
	wait     awaiting
	findCmd  motion.FindKind // find variant parked while awaiting its char
	around   bool            // text object around (a) vs inner (i)
	lastFind motion.Find     // last f/t/F/T for ; and ,

	// Command line / search input.
	cmdline    string
	cmdCur     int      // rune cursor within cmdline (#1110)
	cmdSuggest []string // path completion candidates on the ":" line (#543)
	searching  bool
	searchDir  search.Direction
	query      search.Query
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
	// Markdown rich rendering (#881). conceal holds the per-line marker-chrome
	// column ranges split out of the same parse as hlIndex (@conceal captures);
	// mdRender is the editor.markdown_rendering toggle; mdTables caches the
	// detected pipe tables per document version (pointer, shared across the
	// value copies like lineCache).
	conceal  map[int][][2]int
	mdRender bool
	mdTables *mdTableState
	// colorPreview is the inline color-swatch toggle (#790,
	// editor.color_preview): color literals tint with their own color.
	colorPreview bool
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
	hlTheme     highlight.Theme
	pal         *theme.Palette // active theme (Roadmap 0110); nil = default

	// LSP UI state (Roadmap 0100): diagnostics indexed by line, the autocomplete
	// popup, and the hover popup. See lsp_state.go.
	diags      []ilsp.Diagnostic
	diagByLine map[int][]ilsp.Diagnostic
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
	// bpSource reports the current breakpoint lines for a file (0350, #577):
	// injected by the app so the gutter always renders the live store without
	// per-view push bookkeeping. Nil means no breakpoints feature. bpAdjust
	// reports edit-driven line-count deltas back to the store; bpLines is the
	// last observed count (the folds' foldLines pattern).
	bpSource func(path string) []int
	bpAdjust func(path string, cursorAfter, delta int)
	bpLines  int
	// paused/pausedLine mark the debugger's current line (#579), set by the
	// app while a session is stopped in this buffer.
	paused     bool
	pausedLine int
	// blameOn shows the inline blame annotation on the cursor line (#468);
	// blame is the whole-file map behind it, refreshed by the app on save and
	// vcs refresh, so positions may briefly lag an edit like gitMarks.
	blameOn bool
	blame   map[int]vcs.BlameLine
	comp    *completionState
	compMRU *mru.Store // recently accepted completions (#854); nil-safe
	snippet *snippetSession
	hover   *hoverState
	// mouseHover is the pending mouse-idle hover position (#1129): set when
	// the app fires the idle hover, matched against the LSP reply's position
	// so a stale answer never opens a popup at a cell the pointer has left.
	mouseHover *buffer.Position
	signature  *signatureState
	// peek is the peek-definition popup (#1154): a cursor-anchored excerpt of
	// the definition target; owns esc/enter/scroll keys while open (peek.go).
	peek *peekState
	popupMaxW  int // app-set popup content-width cap (#316); 0 = pane-derived

	// Editor settings, refreshed from cfg on each event so live config changes
	// take effect without a restart.
	tabWidth           int
	useSpaces          bool
	autoIndent         bool
	autoClosePairs     bool
	trimTrailing       bool
	insertFinalNewline bool
	showInlayHints     bool
	stickyScroll       bool
	stickyDepth        int

	// View options (#64). softWrap/wsMode/indentGuides follow the [editor]
	// config until their palette toggle flips them; the *Set flags mark a
	// per-view override so the per-Update applyConfig refresh no longer
	// clobbers the toggled value. rulersRaw caches the last parsed
	// editor.rulers string so the list isn't re-split every Update.
	softWrap     bool
	wsMode       whitespaceMode
	indentGuides bool
	rulers       []int
	wrapSet      bool
	wsSet        bool
	guidesSet    bool
	rulersRaw    string
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
		insertFinalNewline: true,
		showInlayHints:     false,
		stickyScroll:       true,
		stickyDepth:        4,
		hlTheme:            highlight.NewTheme(nil, nil),
		visualStart:        -1,
		visualEnd:          -1,
		eol:                textenc.LF,
		enc:                textenc.UTF8,
		lineCache:          newLineCache(),
		testCache:          newTestMarkStore(),
		conflictCache:      newConflictStore(),
		mdRender:           true,
		mdTables:           &mdTableState{},
		colorPreview:       true,
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
	if m.cfg != nil {
		get = m.cfg.Get
	}
	m.hlTheme = highlight.NewTheme(captures, get)
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
	m.autoIndent = boolOr(m.cfg, "editor.auto_indent", m.autoIndent)
	m.autoClosePairs = boolOr(m.cfg, "editor.auto_close_pairs", m.autoClosePairs)
	m.trimTrailing = boolOr(m.cfg, "editor.trim_trailing_whitespace", m.trimTrailing)
	m.showInlayHints = boolOr(m.cfg, "lsp.inlay_hints", m.showInlayHints)
	m.insertFinalNewline = boolOr(m.cfg, "editor.insert_final_newline", m.insertFinalNewline)
	m.view.LineNumbers = boolOr(m.cfg, "editor.line_numbers", m.view.LineNumbers)
	m.view.RelativeNumbers = boolOr(m.cfg, "editor.relative_line_numbers", m.view.RelativeNumbers)
	if v, ok := m.cfg.Get("editor.scroll_off"); ok {
		m.view.ScrollOff = atoi(v, m.view.ScrollOff)
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
	if v, ok := m.cfg.Get("editor.rulers"); ok && v != m.rulersRaw {
		m.rulersRaw = v
		m.rulers = parseRulers(v)
	}
	m.stickyScroll = boolOr(m.cfg, "editor.sticky_scroll", m.stickyScroll)
	m.searchIgnoreCase = boolOr(m.cfg, "editor.search_ignore_case", m.searchIgnoreCase)
	m.mdRender = boolOr(m.cfg, "editor.markdown_rendering", m.mdRender)
	m.colorPreview = boolOr(m.cfg, "editor.color_preview", m.colorPreview)
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
	m.restoreUndo(data)
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
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
	m.clearLocalMarks() // local marks belong to the previous content (#1151)
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
	m.diskHash = "" // nothing on disk yet; the first :w stamps it
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
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
	m.hist.MarkNeverSaved() // recovered text is dirty even after undoing back to it
	m.diskHash = ""         // recovered content matches no on-disk state
	m.dirty = true
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.conceal = nil
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
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
	m.scopes = nil
	m.resetFolds()
	m.semIndex = highlight.Index{}
	m.occurrences = nil
	m.inlayHints, m.hintsByLine = nil, nil
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
// their inputs.
func (m Model) Capturing() bool {
	return m.mode.Capturing() || m.replPanel != nil || m.subConfirm != nil
}

// Cursor returns the 1-based line and column for the status line.
func (m Model) Cursor() (line, col int) { return m.cursor.Line + 1, m.cursor.Col + 1 }

// CursorPos returns the 0-based line and column, for session persistence.
func (m Model) CursorPos() (line, col int) { return m.cursor.Line, m.cursor.Col }

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

// JumpTo places the cursor like SetCursor and frames the landing for a
// navigation jump (#996): the target line sits jumpTopMargin rows below the
// viewport's top edge (small context margin, JetBrains-like) instead of being
// scrolled minimally into view. Already-visible targets reframe too —
// consistent landings beat the occasional saved scroll (documented decision).
// SetScroll clamps, so a target near the end of the buffer never over-scrolls.
func (m *Model) JumpTo(line, col int) {
	m.SetCursor(line, col)
	top := m.cursor.Line - jumpTopMargin
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

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update routes a message to the handler for the current mode.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
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
			style, conceal := concealSplit(msg.Spans)
			m.hlIndex = highlight.NewIndex(style)
			m.conceal = conceal
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
		m.view.Scroll(m.cursor.Line, m.cursor.Col, m.buf.LineCount())
		m.foldScrollFix()
	}
	m.unhideCursor()
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

// SetScroll restores the viewport framing saved by ScrollOffset, clamping into
// the current buffer. Unlike a cursor move it does not re-derive Top from the
// cursor, so the file reopens scrolled exactly as it was left. Apply it after the
// editor has been sized.
func (m *Model) SetScroll(top, left int) {
	// No overscroll past the end: the last line stops at the bottom of the
	// viewport instead of scrolling up to an almost-empty screen. Soft wrap
	// and collapsed folds keep the looser lineCount-1 clamp — wrap renders
	// more rows than lines (the tight clamp could hide a wrapped tail) and
	// folds render fewer (reaching the end can need a deeper Top).
	max := m.buf.LineCount() - 1
	if h := m.view.Height(); !m.softWrap && !m.hasFolds() && h > 0 {
		if max = m.buf.LineCount() - h; max < 0 {
			max = 0
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
