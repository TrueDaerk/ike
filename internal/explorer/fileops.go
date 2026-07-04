package explorer

// fileops.go implements the explorer's file operations: create a file/folder,
// delete an entry, and undo the last operation. Every destructive step is gated
// behind a modal confirmation; creating an entry prompts for its name. Deletes
// move the entry into a per-session trash directory rather than removing it, so
// an undo can move it back. The undo stack is linear (create/delete only); it is
// independent of the editor's text undo.

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
// (typically a re-scan of the affected directory).
type prompt struct {
	kind   promptKind
	title  string
	input  string
	accept func(m *Model, input string) tea.Cmd
}

// opKind distinguishes the two reversible file operations.
type opKind int

const (
	opCreate opKind = iota // created path; undo deletes it
	opDelete               // moved path to trashPath; undo moves it back
)

// fileOp is one entry on the undo stack.
type fileOp struct {
	kind      opKind
	path      string // the entry's location in the tree
	trashPath string // where a deleted entry now lives (opDelete only)
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
	case msg.Code == tea.KeyBackspace:
		if r := []rune(p.input); len(r) > 0 {
			p.input = string(r[:len(r)-1])
		}
	case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
		// Printable input, including a bare space (Text == " ").
		p.input += msg.Text
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

// createEntry creates an empty file or a directory under dir, records the create
// for undo, and re-scans dir so the new entry appears (and is selected).
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
			var f *os.File
			if f, err = os.Create(path); err == nil {
				_ = f.Close()
			}
		}
	}
	if err != nil {
		m.err = err
		return nil
	}
	m.ops = append(m.ops, fileOp{kind: opCreate, path: path, isDir: isDir})
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
	m.ops = append(m.ops, fileOp{kind: opDelete, path: path, trashPath: tp, isDir: isDir})
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

// promptUndo opens a confirmation for reversing the last file operation: delete
// the last created entry, or restore the last deleted one. It is a no-op when the
// undo stack is empty.
func (m *Model) promptUndo() {
	if len(m.ops) == 0 {
		m.err = nil
		return
	}
	op := m.ops[len(m.ops)-1]
	switch op.kind {
	case opCreate:
		m.prompt = &prompt{
			kind:  promptConfirm,
			title: fmt.Sprintf("Undo: delete %q?", filepath.Base(op.path)),
			accept: func(mm *Model, _ string) tea.Cmd {
				return mm.undoCreate(op)
			},
		}
	case opDelete:
		m.prompt = &prompt{
			kind:  promptConfirm,
			title: fmt.Sprintf("Undo: restore %q?", filepath.Base(op.path)),
			accept: func(mm *Model, _ string) tea.Cmd {
				return mm.undoDelete(op)
			},
		}
	}
}

// undoCreate removes the entry a create added and pops the op.
func (m *Model) undoCreate(op fileOp) tea.Cmd {
	var err error
	if op.isDir {
		err = os.RemoveAll(op.path)
	} else {
		err = os.Remove(op.path)
	}
	if err != nil {
		m.err = err
		return nil
	}
	m.popOp()
	return tea.Batch(m.refreshDir(filepath.Dir(op.path)), deletedCmd(op.path, op.isDir))
}

// undoDelete moves a trashed entry back to its original location and pops the op.
func (m *Model) undoDelete(op fileOp) tea.Cmd {
	if err := os.Rename(op.trashPath, op.path); err != nil {
		m.err = err
		return nil
	}
	m.popOp()
	m.pendingSel = op.path
	return m.refreshDir(filepath.Dir(op.path))
}

// popOp drops the most recent operation from the undo stack.
func (m *Model) popOp() {
	if len(m.ops) > 0 {
		m.ops = m.ops[:len(m.ops)-1]
	}
}

// refreshDir expands and re-scans dir so a just-changed directory reloads in
// place. It falls back to the standard refresh when dir is not a node in the
// current tree (e.g. a collapsed ancestor).
func (m *Model) refreshDir(dir string) tea.Cmd {
	n := nodeByPath(m.root, dir)
	if n == nil {
		return m.refresh()
	}
	n.expanded = true
	n.loaded = false
	n.loading = true
	n.children = nil
	m.rebuild()
	return scanCmd(dir)
}

// promptBox renders the active prompt as a bordered box for View to overlay.
func (m Model) promptBox() string {
	p := m.prompt
	body := p.title
	switch p.kind {
	case promptInput:
		body += "\n> " + p.input + "▏"
	default:
		body += "\n[y]es  [n]o"
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5f87ff")).
		Padding(0, 1).
		Render(body)
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
