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

	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
	"ike/internal/editor/mode"
	"ike/internal/editor/motion"
	"ike/internal/editor/register"
	"ike/internal/editor/search"
	"ike/internal/editor/viewport"
	"ike/internal/host"
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
type CloseMsg struct{}

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

	// Visual mode anchor (the fixed end of the selection).
	anchor buffer.Position

	// Insert-session recording for "." repeat.
	insert insertSession
	dot    *dotCommand

	dirty   bool
	focused bool
	width   int
	height  int

	cfg     host.Config
	emitter Emitter

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
	}
	m.view.LineNumbers = false
	return m
}

// Configure applies the [editor] configuration section and keeps a reference so
// later changes are re-read live. Unset keys keep their built-in defaults.
func (m *Model) Configure(cfg host.Config) {
	m.cfg = cfg
	m.applyConfig()
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
	m.hist = history.New()
	m.scroll()
	return nil
}

// Path returns the loaded file path ("" when no file is open).
func (m Model) Path() string { return m.path }

// Dirty reports whether the buffer has unsaved changes.
func (m Model) Dirty() bool { return m.dirty }

// ModeName returns the current modal state.
func (m Model) ModeName() Mode { return m.mode }

// Capturing reports whether the editor is consuming raw text (insert / replace /
// command line), so the host must not intercept single-letter global keys.
func (m Model) Capturing() bool { return m.mode.Capturing() }

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
	case ActionMsg:
		return m.runAction(msg.Action)
	case tea.KeyMsg:
		var cmd tea.Cmd
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
		return m, cmd
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
	line := m.buf.Line(i)
	j := 0
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	return line[:j]
}

// tabText is the string a Tab key inserts, honouring expandtab.
func (m *Model) tabText() string {
	if m.useSpaces {
		return strings.Repeat(" ", m.tabWidth)
	}
	return "\t"
}

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
