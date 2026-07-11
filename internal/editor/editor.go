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

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
	"ike/internal/editor/motion"
	"ike/internal/editor/register"
	"ike/internal/editor/search"
	"ike/internal/editor/viewport"
	"ike/internal/highlight"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/theme"
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

// awaiting enumerates the secondary-key states the normal-mode handler can be
// parked in: waiting for a second 'g', a find target char, a replace char, a
// register name, or a text-object selector after an operator.
type awaiting int

const (
	awaitNone awaiting = iota
	awaitG
	awaitFind
	awaitReplace
	awaitObject // after operator + i/a; awaiting the object char
)

// Model is the editor pane.
type Model struct {
	path string
	buf  *buffer.Buffer

	cursor     buffer.Position
	desiredCol int // remembered column for vertical motion across short lines

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
	cmdline   string
	searching bool
	searchDir search.Direction
	query     search.Query

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

	dirty   bool
	stale   bool // file changed on disk while dirty (Roadmap 0140, #82)
	focused bool
	width   int
	height  int

	cfg     host.Config
	emitter Emitter

	// Syntax highlighting (Roadmap 0100). docVersion is a monotonic document
	// version bumped on every buffer change; it tags async parse results so stale
	// spans (a newer edit already landed) are dropped. hlIndex caches the spans
	// for the current version; hlTheme resolves capture names to colours.
	docVersion int
	hlVersion  int
	hlIndex    highlight.Index
	// semIndex is the LSP semantic-token overlay (#9), layered over hlIndex
	// in styleAt; kept until the next result replaces it (stale positions may
	// briefly lag an edit, like every semantic-token client).
	semIndex highlight.Index
	hlTheme  highlight.Theme
	pal      *theme.Palette // active theme (Roadmap 0110); nil = default

	// LSP UI state (Roadmap 0100): diagnostics indexed by line, the autocomplete
	// popup, and the hover popup. See lsp_state.go.
	diags      []ilsp.Diagnostic
	diagByLine map[int][]ilsp.Diagnostic
	comp       *completionState
	hover      *hoverState
	signature  *signatureState
	popupMaxW  int // app-set popup content-width cap (#316); 0 = pane-derived

	// Editor settings, refreshed from cfg on each event so live config changes
	// take effect without a restart.
	tabWidth           int
	useSpaces          bool
	autoIndent         bool
	trimTrailing       bool
	insertFinalNewline bool
}

// New returns an empty editor with no file loaded.
func New() Model {
	m := Model{
		buf:                buffer.New(nil),
		mode:               Normal,
		regs:               register.New(),
		hist:               history.New(),
		tabWidth:           4,
		insertFinalNewline: true,
		hlTheme:            highlight.NewTheme(nil, nil),
		visualStart:        -1,
		visualEnd:          -1,
	}
	m.view.LineNumbers = false
	return m
}

// Configure applies the [editor] configuration section and keeps a reference so
// later changes are re-read live. Unset keys keep their built-in defaults.
func (m *Model) Configure(cfg host.Config) {
	m.cfg = cfg
	m.rebuildTheme()
	m.applyConfig()
}

// SetPalette threads the active theme palette in (Roadmap 0110): its captures
// become the highlight defaults under any theme.captures.* overrides, and
// chrome (selection, LSP popups, diagnostics) reads its ui slots.
func (m *Model) SetPalette(p *theme.Palette) {
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
}

// applyConfig refreshes settings from the retained config reference.
func (m *Model) applyConfig() {
	if m.cfg == nil {
		return
	}
	if v, ok := m.cfg.Get("editor.tab_width"); ok {
		if n := atoi(v, m.tabWidth); n > 0 {
			m.tabWidth = n
		}
	}
	m.useSpaces = boolOr(m.cfg, "editor.use_spaces", m.useSpaces)
	m.autoIndent = boolOr(m.cfg, "editor.auto_indent", m.autoIndent)
	m.trimTrailing = boolOr(m.cfg, "editor.trim_trailing_whitespace", m.trimTrailing)
	m.insertFinalNewline = boolOr(m.cfg, "editor.insert_final_newline", m.insertFinalNewline)
	m.view.LineNumbers = boolOr(m.cfg, "editor.line_numbers", m.view.LineNumbers)
	m.view.RelativeNumbers = boolOr(m.cfg, "editor.relative_line_numbers", m.view.RelativeNumbers)
	if v, ok := m.cfg.Get("editor.scroll_off"); ok {
		m.view.ScrollOff = atoi(v, m.view.ScrollOff)
	}
}

// Load reads path into the buffer, resetting cursor, mode, and history.
func (m *Model) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	m.path = path
	m.buf = buffer.FromString(string(data))
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.cmdline = ""
	m.searching = false
	m.dirty = false
	m.stale = false
	m.hist = history.New()
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.semIndex = highlight.Index{}
	m.scroll()
	return nil
}

// RestoreText installs crash-recovered text into the buffer and marks it dirty
// (Roadmap 0210). Undo history resets to the recovered content — recovery is a
// fresh starting point, not a continuation of the dead session's history. The
// path is left as-is, so the caller can Load the base file first (titled restore)
// or leave it empty (untitled restore).
func (m *Model) RestoreText(text string) {
	m.buf = buffer.FromString(text)
	m.cursor = buffer.Position{}
	m.desiredCol = 0
	m.mode = Normal
	m.pending.Reset()
	m.wait = awaitNone
	m.hist = history.New()
	m.hist.MarkNeverSaved() // recovered text is dirty even after undoing back to it
	m.dirty = true
	m.docVersion++
	m.hlIndex = highlight.Index{}
	m.semIndex = highlight.Index{}
	m.scroll()
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
	m.hlIndex = highlight.Index{}
	m.semIndex = highlight.Index{}
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
// normal-mode position and scrolling it into view. Used to restore a saved
// session; out-of-range coordinates land on the nearest valid cell.
func (m *Model) SetCursor(line, col int) {
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: line, Col: col})
	m.desiredCol = m.cursor.Col
	m.scroll()
}

// HasFile reports whether a file is currently open.
func (m Model) HasFile() bool { return m.path != "" }

// SetSize sets the available width and number of text rows.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.view.SetSize(width, height)
	m.scroll()
}

// SetFocused toggles whether this pane receives key input.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetClipboard wires the system-clipboard implementation for the "+ register.
func (m *Model) SetClipboard(c register.Clipboard) { m.regs.SetClipboard(c) }

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update routes a message to the handler for the current mode.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	m.applyConfig()
	switch msg := msg.(type) {
	case highlight.SpansMsg:
		// Accept a parse result only if it matches the current document and
		// version; a newer edit since the parse was scheduled drops it.
		if msg.Path == m.path && msg.Version == m.docVersion {
			m.hlIndex = highlight.NewIndex(msg.Spans)
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
	case ilsp.HoverMsg:
		if msg.Path == m.path && msg.Contents != "" {
			m.hover = &hoverState{lines: strings.Split(msg.Contents, "\n")}
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
	case ilsp.FormatEditsMsg:
		// Formatting edits (Roadmap 0100, #7): applied as one undo unit.
		if msg.Path == m.path {
			edits := make([]TextEdit, len(msg.Edits))
			for i, e := range msg.Edits {
				edits[i] = TextEdit{
					StartLine: e.StartLine, StartCol: e.StartCol,
					EndLine: e.EndLine, EndCol: e.EndCol,
					Text: e.Text,
				}
			}
			m.ApplyTextEdits(edits)
		}
		return m, nil
	case watch.EventMsg:
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
	case tea.KeyPressMsg:
		m.dismissHover() // any key dismisses a hover popup
		if msg.Code == tea.KeyEscape {
			m.dismissSignature() // esc also drops the signature popup
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
		m.scroll()
		return m.maybeReparse(before, cmd)
	}
	return m, nil
}

// scroll keeps the cursor within the visible window.
func (m *Model) scroll() { m.view.Scroll(m.cursor.Line, m.cursor.Col, m.buf.LineCount()) }

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
	if max := m.buf.LineCount() - 1; top > max {
		top = max
	}
	if top < 0 {
		top = 0
	}
	if left < 0 {
		left = 0
	}
	m.view.Top = top
	m.view.Left = left
}
