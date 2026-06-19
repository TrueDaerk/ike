// Package editor implements the text-editing pane: a line buffer with a vim-like
// modal state machine (normal / insert / command) covering the motions and edits
// needed for the foundation slice, plus :w/:q/:wq command handling.
package editor

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mode is the editor's current modal state.
type Mode int

const (
	Normal Mode = iota
	Insert
	Command
)

// String renders the mode for the status line.
func (m Mode) String() string {
	switch m {
	case Insert:
		return "INSERT"
	case Command:
		return "COMMAND"
	default:
		return "NORMAL"
	}
}

// CloseMsg asks the root model to detach the editor (result of :q / :wq).
type CloseMsg struct{}

// Model is the editor pane.
type Model struct {
	path    string
	lines   []string // the buffer, one entry per line (never empty: at least "")
	row     int      // cursor line (0-based)
	col     int      // cursor column (0-based, may equal len(line))
	top     int      // first visible line (vertical scroll)
	mode    Mode
	cmd     string // pending ":" command text
	pending rune   // first key of a two-key normal command (g, d)
	dirty   bool
	width   int
	height  int // number of text rows available
	focused bool
}

// New returns an empty editor with no file loaded.
func New() Model {
	return Model{lines: []string{""}, mode: Normal}
}

// Load reads path into the buffer. On error the buffer is left empty and the
// error returned.
func (m *Model) Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	// A trailing newline yields a spurious empty final element; keep the buffer
	// as the logical lines without it, but never let the buffer be empty.
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	m.path = path
	m.lines = lines
	m.row, m.col, m.top = 0, 0, 0
	m.mode = Normal
	m.dirty = false
	m.pending = 0
	m.cmd = ""
	return nil
}

// Path returns the loaded file path ("" when no file is open).
func (m Model) Path() string { return m.path }

// Dirty reports whether the buffer has unsaved changes.
func (m Model) Dirty() bool { return m.dirty }

// Mode returns the current modal state.
func (m Model) ModeName() Mode { return m.mode }

// Cursor returns the 1-based line and column for the status line.
func (m Model) Cursor() (line, col int) { return m.row + 1, m.col + 1 }

// HasFile reports whether a file is currently open.
func (m Model) HasFile() bool { return m.path != "" }

// SetSize sets the available width and number of text rows.
func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.scroll()
}

// SetFocused toggles whether this pane receives key input.
func (m *Model) SetFocused(f bool) { m.focused = f }

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update routes a key to the handler for the current mode.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	var cmd tea.Cmd
	switch m.mode {
	case Insert:
		m.updateInsert(key)
	case Command:
		m, cmd = m.updateCommand(key)
	default:
		m = m.updateNormal(key)
	}
	m.scroll()
	return m, cmd
}

// --- normal mode -----------------------------------------------------------

func (m Model) updateNormal(key tea.KeyMsg) Model {
	s := key.String()

	// Resolve a pending two-key sequence first.
	if m.pending == 'g' {
		m.pending = 0
		if s == "g" {
			m.row, m.col = 0, 0
		}
		return m
	}
	if m.pending == 'd' {
		m.pending = 0
		if s == "d" {
			m.deleteLine()
		}
		return m
	}

	switch s {
	case "h", "left":
		m.moveLeft()
	case "l", "right":
		m.moveRight()
	case "j", "down":
		m.moveDown()
	case "k", "up":
		m.moveUp()
	case "0", "home":
		m.col = 0
	case "$", "end":
		m.col = m.lineLen(m.row)
		m.clampColNormal()
	case "g":
		m.pending = 'g'
	case "G":
		m.row = len(m.lines) - 1
		m.clampColNormal()
	case "w":
		m.wordForward()
	case "b":
		m.wordBackward()
	case "x":
		m.deleteRune()
	case "d":
		m.pending = 'd'
	case "i":
		m.mode = Insert
	case "a":
		if m.lineLen(m.row) > 0 {
			m.col++
		}
		m.mode = Insert
	case "o":
		m.openLineBelow()
		m.mode = Insert
	case "O":
		m.openLineAbove()
		m.mode = Insert
	case ":":
		m.mode = Command
		m.cmd = ""
	}
	return m
}

func (m *Model) lineLen(row int) int { return len([]rune(m.lines[row])) }

// clampColNormal keeps the cursor on the last rune in normal mode (where the
// cursor sits on a character, not past the end).
func (m *Model) clampColNormal() {
	max := m.lineLen(m.row) - 1
	if max < 0 {
		max = 0
	}
	if m.col > max {
		m.col = max
	}
	if m.col < 0 {
		m.col = 0
	}
}

func (m *Model) moveLeft() {
	if m.col > 0 {
		m.col--
	}
}

func (m *Model) moveRight() {
	if m.col < m.lineLen(m.row)-1 {
		m.col++
	}
}

func (m *Model) moveUp() {
	if m.row > 0 {
		m.row--
		m.clampColNormal()
	}
}

func (m *Model) moveDown() {
	if m.row < len(m.lines)-1 {
		m.row++
		m.clampColNormal()
	}
}

// wordForward moves to the start of the next word (whitespace-delimited).
func (m *Model) wordForward() {
	r := []rune(m.lines[m.row])
	i := m.col
	// skip current word
	for i < len(r) && !isSpace(r[i]) {
		i++
	}
	// skip spaces
	for i < len(r) && isSpace(r[i]) {
		i++
	}
	if i >= len(r) {
		// move to next line's first word if possible
		if m.row < len(m.lines)-1 {
			m.row++
			m.col = 0
			r2 := []rune(m.lines[m.row])
			j := 0
			for j < len(r2) && isSpace(r2[j]) {
				j++
			}
			if j < len(r2) {
				m.col = j
			}
			return
		}
		m.col = len(r) - 1
		if m.col < 0 {
			m.col = 0
		}
		return
	}
	m.col = i
}

// wordBackward moves to the start of the previous word.
func (m *Model) wordBackward() {
	r := []rune(m.lines[m.row])
	i := m.col
	if i > 0 {
		i--
	}
	for i > 0 && isSpace(r[i]) {
		i--
	}
	for i > 0 && !isSpace(r[i-1]) {
		i--
	}
	if m.col == 0 && m.row > 0 {
		m.row--
		m.col = m.lineLen(m.row)
		m.clampColNormal()
		return
	}
	m.col = i
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

// deleteRune removes the rune under the cursor (x).
func (m *Model) deleteRune() {
	r := []rune(m.lines[m.row])
	if len(r) == 0 {
		return
	}
	if m.col >= len(r) {
		m.col = len(r) - 1
	}
	r = append(r[:m.col], r[m.col+1:]...)
	m.lines[m.row] = string(r)
	m.dirty = true
	m.clampColNormal()
}

// deleteLine removes the current line (dd).
func (m *Model) deleteLine() {
	if len(m.lines) == 1 {
		m.lines[0] = ""
		m.col = 0
		m.dirty = true
		return
	}
	m.lines = append(m.lines[:m.row], m.lines[m.row+1:]...)
	if m.row >= len(m.lines) {
		m.row = len(m.lines) - 1
	}
	m.clampColNormal()
	m.dirty = true
}

func (m *Model) openLineBelow() {
	idx := m.row + 1
	m.lines = append(m.lines, "")
	copy(m.lines[idx+1:], m.lines[idx:])
	m.lines[idx] = ""
	m.row = idx
	m.col = 0
	m.dirty = true
}

func (m *Model) openLineAbove() {
	idx := m.row
	m.lines = append(m.lines, "")
	copy(m.lines[idx+1:], m.lines[idx:])
	m.lines[idx] = ""
	m.col = 0
	m.dirty = true
}

// --- insert mode -----------------------------------------------------------

func (m *Model) updateInsert(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyEsc:
		m.mode = Normal
		if m.col > 0 {
			m.col--
		}
		m.clampColNormal()
	case tea.KeyEnter:
		m.splitLine()
	case tea.KeyBackspace:
		m.backspace()
	case tea.KeySpace:
		m.insertRune(' ')
	case tea.KeyTab:
		m.insertRune('\t')
	case tea.KeyRunes:
		for _, r := range key.Runes {
			m.insertRune(r)
		}
	}
}

func (m *Model) insertRune(r rune) {
	line := []rune(m.lines[m.row])
	if m.col > len(line) {
		m.col = len(line)
	}
	line = append(line, 0)
	copy(line[m.col+1:], line[m.col:])
	line[m.col] = r
	m.lines[m.row] = string(line)
	m.col++
	m.dirty = true
}

func (m *Model) splitLine() {
	line := []rune(m.lines[m.row])
	if m.col > len(line) {
		m.col = len(line)
	}
	left, right := string(line[:m.col]), string(line[m.col:])
	m.lines[m.row] = left
	idx := m.row + 1
	m.lines = append(m.lines, "")
	copy(m.lines[idx+1:], m.lines[idx:])
	m.lines[idx] = right
	m.row = idx
	m.col = 0
	m.dirty = true
}

func (m *Model) backspace() {
	if m.col > 0 {
		line := []rune(m.lines[m.row])
		line = append(line[:m.col-1], line[m.col:]...)
		m.lines[m.row] = string(line)
		m.col--
		m.dirty = true
		return
	}
	if m.row == 0 {
		return
	}
	// join with previous line
	prev := m.lines[m.row-1]
	m.col = len([]rune(prev))
	m.lines[m.row-1] = prev + m.lines[m.row]
	m.lines = append(m.lines[:m.row], m.lines[m.row+1:]...)
	m.row--
	m.dirty = true
}

// --- command mode ----------------------------------------------------------

func (m Model) updateCommand(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.Type {
	case tea.KeyEsc:
		m.mode = Normal
		m.cmd = ""
	case tea.KeyEnter:
		return m.runCommand()
	case tea.KeyBackspace:
		if r := []rune(m.cmd); len(r) > 0 {
			m.cmd = string(r[:len(r)-1])
		} else {
			m.mode = Normal
		}
	case tea.KeySpace:
		m.cmd += " "
	case tea.KeyRunes:
		m.cmd += string(key.Runes)
	}
	return m, nil
}

// runCommand interprets the typed ":" command.
func (m Model) runCommand() (Model, tea.Cmd) {
	cmd := strings.TrimSpace(m.cmd)
	m.mode = Normal
	m.cmd = ""
	switch cmd {
	case "w":
		_ = m.save()
		return m, nil
	case "q":
		return m, func() tea.Msg { return CloseMsg{} }
	case "wq", "x":
		_ = m.save()
		return m, func() tea.Msg { return CloseMsg{} }
	}
	return m, nil
}

// save writes the buffer to disk with a trailing newline and clears the dirty
// flag. No-op when no file is open.
func (m *Model) save() error {
	if m.path == "" {
		return nil
	}
	data := strings.Join(m.lines, "\n") + "\n"
	if err := os.WriteFile(m.path, []byte(data), 0o644); err != nil {
		return err
	}
	m.dirty = false
	return nil
}

// --- rendering -------------------------------------------------------------

// scroll keeps the cursor within the visible window.
func (m *Model) scroll() {
	if m.height <= 0 {
		return
	}
	if m.row < m.top {
		m.top = m.row
	}
	if m.row >= m.top+m.height {
		m.top = m.row - m.height + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// CommandLine returns the text shown on the command line ("" when not in
// command mode).
func (m Model) CommandLine() string {
	if m.mode == Command {
		return ":" + m.cmd
	}
	return ""
}

// View renders the buffer with the cursor highlighted.
func (m Model) View() string {
	if m.path == "" {
		return lipgloss.NewStyle().Faint(true).Render("(no file open)")
	}
	end := m.top + m.height
	if m.height <= 0 || end > len(m.lines) {
		end = len(m.lines)
	}
	cursorStyle := lipgloss.NewStyle().Reverse(true)
	var out []string
	for i := m.top; i < end; i++ {
		line := []rune(m.lines[i])
		if i == m.row && m.focused {
			out = append(out, renderCursorLine(line, m.col, cursorStyle))
		} else {
			out = append(out, string(line))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, out...)
}

// renderCursorLine renders one line with the cursor cell reverse-highlighted.
func renderCursorLine(line []rune, col int, style lipgloss.Style) string {
	if col >= len(line) {
		return string(line) + style.Render(" ")
	}
	before := string(line[:col])
	at := style.Render(string(line[col]))
	after := string(line[col+1:])
	return before + at + after
}
