package explorer

// fileops.go implements the explorer's file operations: create a file/folder,
// rename, delete, and undo/redo. Deleting is gated behind a modal confirmation;
// creating and renaming prompt for a name. Deletes (and the undo of a create)
// move the entry into a per-session trash directory rather than removing it, so
// every operation is reversible without data loss — which is why undo and redo
// apply instantly, with no confirmation prompt. The undo/redo stacks are linear
// (create/delete/rename) and independent of the editor's text undo.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/lang"
)

// promptKind selects how a prompt reads input: a free-text line, or a yes/no
// confirmation.
type promptKind int

const (
	promptInput   promptKind = iota // type a name, Enter accepts, Esc cancels
	promptConfirm                   // y/Enter accepts, anything else cancels
)

// prompt is the explorer's modal overlay. accept runs on acceptance with the
// trimmed input (empty for confirmations) and returns any Cmd the action needs
// (typically a re-scan of the affected directory). pos is the cursor's rune
// index into input (promptInput only); it starts at the end of any prefilled
// text and can be moved with the arrow keys or a click.
type prompt struct {
	kind   promptKind
	title  string
	input  string
	pos    int
	accept func(m *Model, input string) tea.Cmd
}

// opKind distinguishes the reversible file operations.
type opKind int

const (
	opCreate opKind = iota // created path; undo moves it to the trash
	opDelete               // moved path to trashPath; undo moves it back
	opRename               // renamed path to newPath; undo renames it back
)

// fileOp is one entry on the undo/redo stacks.
type fileOp struct {
	kind      opKind
	path      string // the entry's location in the tree
	newPath   string // the entry's location after a rename (opRename only)
	trashPath string // where a trashed entry lives (opDelete; opCreate after undo)
	isDir     bool
}

// Prompting reports whether a modal prompt is open, so the root model can route
// raw keys straight to the explorer instead of the keymap/global layer.
func (m Model) Prompting() bool { return m.prompt != nil }

// handlePromptKey feeds one key to the open prompt and returns any Cmd produced
// by accepting it. It clears the prompt on accept or cancel.
func (m *Model) handlePromptKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.prompt
	if p.kind == promptConfirm {
		switch msg.String() {
		case "y", "Y", "enter":
			m.prompt = nil
			return p.accept(m, "")
		default: // n, esc, or any other key cancels
			m.prompt = nil
			return nil
		}
	}
	// promptInput
	switch {
	case msg.Code == tea.KeyEnter:
		name := trimSpace(p.input)
		m.prompt = nil
		if name == "" {
			return nil
		}
		return p.accept(m, name)
	case msg.Code == tea.KeyEscape:
		m.prompt = nil
	case msg.Code == tea.KeyLeft:
		if p.pos > 0 {
			p.pos--
		}
	case msg.Code == tea.KeyRight:
		if r := []rune(p.input); p.pos < len(r) {
			p.pos++
		}
	case msg.Code == tea.KeyHome:
		p.pos = 0
	case msg.Code == tea.KeyEnd:
		p.pos = len([]rune(p.input))
	case msg.Code == tea.KeyBackspace:
		if r := []rune(p.input); p.pos > 0 {
			p.input = string(append(r[:p.pos-1:p.pos-1], r[p.pos:]...))
			p.pos--
		}
	case msg.Code == tea.KeyDelete:
		if r := []rune(p.input); p.pos < len(r) {
			p.input = string(append(r[:p.pos:p.pos], r[p.pos+1:]...))
		}
	case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		// Printable input, including a bare space (Text == " "), inserted at pos.
		r := []rune(p.input)
		ins := []rune(msg.Text)
		p.input = string(append(append(append([]rune{}, r[:p.pos]...), ins...), r[p.pos:]...))
		p.pos += len(ins)
	}
	return nil
}

// targetDir is the directory a new entry is created in: the selected directory
// itself, or the parent of the selected file (the root when nothing is
// selected).
func (m *Model) targetDir() string {
	n := m.current()
	if n == nil {
		return m.root.path
	}
	if n.isDir {
		return n.path
	}
	if p := m.parentOf(n); p != nil {
		return p.path
	}
	return m.root.path
}

// promptNewEntry opens the name prompt for a new file (isDir false) or folder.
func (m *Model) promptNewEntry(isDir bool) {
	dir := m.targetDir()
	what := "file"
	if isDir {
		what = "folder"
	}
	m.prompt = &prompt{
		kind:  promptInput,
		title: fmt.Sprintf("New %s in %s/", what, filepath.Base(dir)),
		accept: func(mm *Model, name string) tea.Cmd {
			return mm.createEntry(dir, name, isDir)
		},
	}
}

// createEntry creates a file or a directory under dir, records the create
// for undo, and re-scans dir so the new entry appears (and is selected). A new
// file starts with its language's template when one is registered — `package
// <dir>` for Go, `<?php` for PHP (#170) — and empty otherwise.
func (m *Model) createEntry(dir, name string, isDir bool) tea.Cmd {
	path := filepath.Join(dir, name)
	if _, err := os.Lstat(path); err == nil {
		m.err = fmt.Errorf("already exists: %s", name)
		return nil
	}
	var err error
	if isDir {
		err = os.MkdirAll(path, 0o755)
	} else {
		if err = os.MkdirAll(dir, 0o755); err == nil {
			err = os.WriteFile(path, []byte(lang.TemplateFor(path)), 0o644)
		}
	}
	if err != nil {
		m.err = err
		return nil
	}
	m.pushOp(fileOp{kind: opCreate, path: path, isDir: isDir})
	m.pendingSel = path
	return m.refreshDir(dir)
}

// promptDelete opens a confirmation for deleting the selected entry. The root is
// never deletable.
func (m *Model) promptDelete() {
	n := m.current()
	if n == nil || n == m.root {
		return
	}
	what := "file"
	if n.isDir {
		what = "folder"
	}
	path, isDir := n.path, n.isDir
	m.prompt = &prompt{
		kind:  promptConfirm,
		title: fmt.Sprintf("Delete %s %q?", what, n.name),
		accept: func(mm *Model, _ string) tea.Cmd {
			return mm.deleteEntry(path, isDir)
		},
	}
}

// deleteEntry moves path into the session trash, records the delete for undo,
// and re-scans the parent directory.
func (m *Model) deleteEntry(path string, isDir bool) tea.Cmd {
	tp, err := m.toTrash(path)
	if err != nil {
		m.err = err
		return nil
	}
	m.pushOp(fileOp{kind: opDelete, path: path, trashPath: tp, isDir: isDir})
	return tea.Batch(m.refreshDir(filepath.Dir(path)), deletedCmd(path, isDir))
}

// deletedCmd announces a removed path so the app can close editors on it.
func deletedCmd(path string, isDir bool) tea.Cmd {
	return func() tea.Msg { return FileDeletedMsg{Path: path, IsDir: isDir} }
}

// toTrash moves path into a hidden, same-filesystem trash directory under the
// project root (so the rename never crosses devices) and returns its new
// location. A sequence number keeps entries with equal basenames distinct.
func (m *Model) toTrash(path string) (string, error) {
	if m.trashDir == "" {
		d := filepath.Join(m.root.path, ".ike-trash")
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
		m.trashDir = d
	}
	m.trashSeq++
	tp := filepath.Join(m.trashDir, fmt.Sprintf("%d-%s", m.trashSeq, filepath.Base(path)))
	if err := os.Rename(path, tp); err != nil {
		return "", err
	}
	return tp, nil
}

// promptRename opens the name prompt for renaming the selected entry, prefilled
// with its current name. The root is never renameable.
func (m *Model) promptRename() {
	n := m.current()
	if n == nil || n == m.root {
		return
	}
	what := "file"
	if n.isDir {
		what = "folder"
	}
	path, isDir := n.path, n.isDir
	m.prompt = &prompt{
		kind:  promptInput,
		title: fmt.Sprintf("Rename %s %q to:", what, n.name),
		input: n.name,
		pos:   len([]rune(n.name)),
		accept: func(mm *Model, name string) tea.Cmd {
			return mm.renameEntry(path, name, isDir)
		},
	}
}

// renameEntry moves path to a new name within the same directory and re-scans
// it. Editors open on the path follow the change via FileMovedMsg (#175).
func (m *Model) renameEntry(path, name string, isDir bool) tea.Cmd {
	return m.relocateEntry(path, filepath.Join(filepath.Dir(path), name), isDir)
}

// moveEntry moves path into targetDir, keeping its base name (file.move, #175).
func (m *Model) moveEntry(path, targetDir string, isDir bool) tea.Cmd {
	return m.relocateEntry(path, filepath.Join(targetDir, filepath.Base(path)), isDir)
}

// relocateEntry is the shared core of rename and move: one os.Rename from path
// to newPath, recorded on the undo stack, with both affected directories
// re-scanned and a FileMovedMsg so open editors re-point instead of closing.
func (m *Model) relocateEntry(path, newPath string, isDir bool) tea.Cmd {
	if newPath == path {
		return nil
	}
	if isDir && strings.HasPrefix(newPath, path+string(filepath.Separator)) {
		m.err = fmt.Errorf("cannot move a folder into itself")
		return nil
	}
	if _, err := os.Lstat(newPath); err == nil {
		m.err = fmt.Errorf("already exists: %s", filepath.Base(newPath))
		return nil
	}
	if err := os.Rename(path, newPath); err != nil {
		m.err = err
		return nil
	}
	m.pushOp(fileOp{kind: opRename, path: path, newPath: newPath, isDir: isDir})
	m.pendingSel = newPath
	return tea.Batch(m.refreshDirs(path, newPath), movedCmd(path, newPath, isDir))
}

// refreshDirs re-scans the parent directories of both ends of a relocation,
// once when they coincide (a plain rename).
func (m *Model) refreshDirs(path, newPath string) tea.Cmd {
	from, to := filepath.Dir(path), filepath.Dir(newPath)
	if from == to {
		return m.refreshDir(from)
	}
	return tea.Batch(m.refreshDir(from), m.refreshDir(to))
}

// movedCmd announces a relocated path so the app can re-point editors on it.
func movedCmd(old, new string, isDir bool) tea.Cmd {
	return func() tea.Msg { return FileMovedMsg{Old: old, New: new, IsDir: isDir} }
}

// pushOp records a freshly completed operation on the undo stack. A new
// operation invalidates the redo history, exactly like a text editor's undo.
func (m *Model) pushOp(op fileOp) {
	m.ops = append(m.ops, op)
	m.redoOps = nil
}

// undo reverses the last file operation instantly (no confirmation — every
// operation is recoverable via redo): a create moves the entry to the trash, a
// delete restores it, a rename renames back. A no-op on an empty stack; on
// failure (e.g. the entry changed externally) the op stays put and the error is
// shown.
func (m *Model) undo() tea.Cmd {
	if len(m.ops) == 0 {
		m.err = nil
		return nil
	}
	op := m.ops[len(m.ops)-1]
	var cmd tea.Cmd
	switch op.kind {
	case opCreate:
		tp, err := m.toTrash(op.path)
		if err != nil {
			m.err = err
			return nil
		}
		op.trashPath = tp
		cmd = tea.Batch(m.refreshDir(filepath.Dir(op.path)), deletedCmd(op.path, op.isDir))
	case opDelete:
		if err := os.Rename(op.trashPath, op.path); err != nil {
			m.err = err
			return nil
		}
		m.pendingSel = op.path
		cmd = m.refreshDir(filepath.Dir(op.path))
	case opRename:
		if err := os.Rename(op.newPath, op.path); err != nil {
			m.err = err
			return nil
		}
		m.pendingSel = op.path
		cmd = tea.Batch(m.refreshDirs(op.path, op.newPath), movedCmd(op.newPath, op.path, op.isDir))
	}
	m.ops = m.ops[:len(m.ops)-1]
	m.redoOps = append(m.redoOps, op)
	m.err = nil
	return cmd
}

// redo re-applies the most recently undone operation and pushes it back onto
// the undo stack. A no-op on an empty redo stack.
func (m *Model) redo() tea.Cmd {
	if len(m.redoOps) == 0 {
		m.err = nil
		return nil
	}
	op := m.redoOps[len(m.redoOps)-1]
	var cmd tea.Cmd
	switch op.kind {
	case opCreate:
		if err := os.Rename(op.trashPath, op.path); err != nil {
			m.err = err
			return nil
		}
		op.trashPath = ""
		m.pendingSel = op.path
		cmd = m.refreshDir(filepath.Dir(op.path))
	case opDelete:
		if err := os.Rename(op.path, op.trashPath); err != nil {
			m.err = err
			return nil
		}
		cmd = tea.Batch(m.refreshDir(filepath.Dir(op.path)), deletedCmd(op.path, op.isDir))
	case opRename:
		if err := os.Rename(op.path, op.newPath); err != nil {
			m.err = err
			return nil
		}
		m.pendingSel = op.newPath
		cmd = tea.Batch(m.refreshDirs(op.path, op.newPath), movedCmd(op.path, op.newPath, op.isDir))
	}
	m.redoOps = m.redoOps[:len(m.redoOps)-1]
	m.ops = append(m.ops, op)
	m.err = nil
	return cmd
}

// refreshDir expands and re-scans dir so a just-changed directory reloads in
// place. Existing children stay put until the scan result merges over them
// (setChildren reuses matching nodes), so expanded subdirectories survive. It
// falls back to the standard refresh when dir is not a node in the current
// tree (e.g. a collapsed ancestor).
func (m *Model) refreshDir(dir string) tea.Cmd {
	n := nodeByPath(m.root, dir)
	if n == nil {
		return m.refresh()
	}
	n.expanded = true
	n.loading = true
	m.rebuild()
	return scanCmd(dir)
}

// promptInputPrefix precedes the typed text on a promptInput's input row; its
// width locates the text column for both rendering and click hit-testing.
const promptInputPrefix = "> "

// promptCursorStyle highlights the rune the input cursor sits on (reverse
// video) rather than inserting a separate caret glyph, so the cursor overlays
// a cell instead of pushing the text sideways as it moves.
var promptCursorStyle = lipgloss.NewStyle().Reverse(true)

// promptInnerWidth is the horizontal space a prompt line may occupy inside
// the box's border and padding (2 cells each side), floored at 1 so even a
// degenerate pane renders something.
func (m Model) promptInnerWidth() int {
	if w := m.width - 4; w > 1 {
		return w
	}
	return 1
}

// promptInputWindow returns the rune offset of the visible slice of the
// prompt's input text and the cell width available for it on the input row.
// The window slides right just far enough to keep the cursor cell visible,
// so long prefilled names (rename) still show where typing lands.
func (m Model) promptInputWindow() (off, avail int) {
	avail = m.promptInnerWidth() - len(promptInputPrefix)
	if avail < 1 {
		avail = 1
	}
	if m.prompt.pos+1 > avail {
		off = m.prompt.pos + 1 - avail
	}
	return off, avail
}

// promptBox renders the active prompt as a bordered box for View to overlay.
// The input row's cursor is drawn at p.pos by reverse-video-ing that cell,
// keeping the line the same length as the text (no inserted caret rune).
// Every line is truncated (title) or windowed (input) to the pane's width so
// the box always fits horizontally — a prompt that captures keys must never
// be wider than the pane it renders in (#373).
func (m Model) promptBox() string {
	p := m.prompt
	inner := m.promptInnerWidth()
	body := ansi.Truncate(p.title, inner, "…")
	switch p.kind {
	case promptInput:
		r := []rune(p.input)
		off, avail := m.promptInputWindow()
		before, after := string(r[off:p.pos]), ""
		cur := " " // past the last rune: a blank cursor cell
		if p.pos < len(r) {
			cur = string(r[p.pos])
			if end := off + avail; p.pos+1 < end {
				after = string(r[p.pos+1 : min(end, len(r))])
			}
		}
		body += "\n" + promptInputPrefix + before + promptCursorStyle.Render(cur) + after
	default:
		body += "\n" + ansi.Truncate("[y]es  [n]o", inner, "…")
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme().BorderFocus).
		Padding(0, 1).
		Render(body)
}

// promptBoxOrigin returns the content-local cell of the prompt box's
// upper-left corner (border included), mirroring the placement math View uses
// to overlay it via overlay.Place — within the explorer's own m.width/m.height,
// i.e. the pane's content area, not the full terminal. promptBox truncates
// itself to the pane width and a too-tall box is clipped by Place rather than
// dropped, so the origin is clamped at 0 and ok is false only when there is no
// box at all: an active prompt is always rendered (#373).
func (m Model) promptBoxOrigin() (x, y, w, h int, ok bool) {
	box := m.promptBox()
	if box == "" {
		return 0, 0, 0, 0, false
	}
	lines := strings.Split(box, "\n")
	h = len(lines)
	for _, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			w = lw
		}
	}
	return max(0, (m.width-w)/2), max(0, (m.height-h)/2), w, h, true
}

// PromptMouseClick moves the text cursor of an open promptInput to the column
// under a content-local click (the same coordinate space as MouseClick),
// clamped to the input's text bounds. It is a no-op for promptConfirm (no
// movable cursor), a click outside the input row, or when no prompt is open.
func (m *Model) PromptMouseClick(x, y int) {
	p := m.prompt
	if p == nil || p.kind != promptInput {
		return
	}
	bx, by, _, _, ok := m.promptBoxOrigin()
	if !ok {
		return
	}
	// border(1) + title line(1) = the input row; border(1) + padding(1) +
	// the "> " prefix reach the text column.
	inputRow := by + 2
	textX := bx + 2 + len(promptInputPrefix)
	if y != inputRow {
		return
	}
	off, _ := m.promptInputWindow()
	r := []rune(p.input)
	p.pos = clamp(x-textX+off, 0, len(r))
}

// trimSpace trims leading/trailing ASCII spaces and tabs from a filename without
// pulling in strings just for this one call.
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
