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
	promptNotice                    // dismissable error dialog (#1030); any key closes
)

// prompt is the explorer's modal overlay. accept runs on acceptance with the
// trimmed input (empty for confirmations) and returns any Cmd the action needs
// (typically a re-scan of the affected directory). pos is the cursor's rune
// index into input (promptInput only); it starts at the end of any prefilled
// text and can be moved with the arrow keys or a click.
//
// selStart/selEnd mark a preselected rune range (selStart < selEnd), used by
// rename to preselect the name stem JetBrains-style (#1047): the first
// printable key replaces the whole range, Backspace/Delete remove it, and any
// other key drops the selection and edits normally. Outside rename both stay
// zero and the prompt behaves as a plain input line.
type prompt struct {
	kind     promptKind
	title    string
	input    string
	pos      int
	selStart int
	selEnd   int
	accept   func(m *Model, input string) tea.Cmd
}

// fail records a failed user-initiated file operation and opens a dismissable
// error dialog over the intact tree (#1030) — the project convention for
// actionable pane states; the tree never gets replaced by raw error text.
// Any key dismisses.
func (m *Model) fail(err error) {
	m.err = err
	m.prompt = &prompt{kind: promptNotice, title: "error", input: err.Error()}
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

	// batch groups the per-entry sub-operations of one multi-select delete
	// (#1044) into a single undo step: each sub-op is a plain opDelete, and
	// undo/redo walk the batch as a unit so one undo restores the whole
	// selection. Non-empty batch outranks kind in the undo/redo walk.
	batch []fileOp
}

// Prompting reports whether a modal prompt is open, so the root model can route
// raw keys straight to the explorer instead of the keymap/global layer.
func (m Model) Prompting() bool { return m.prompt != nil }

// handlePromptKey feeds one key to the open prompt and returns any Cmd produced
// by accepting it. It clears the prompt on accept or cancel.
func (m *Model) handlePromptKey(msg tea.KeyPressMsg) tea.Cmd {
	p := m.prompt
	if p.kind == promptNotice {
		// Any key dismisses the error dialog (#1030) and clears the error.
		m.prompt = nil
		m.err = nil
		return nil
	}
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
	if p.selStart < p.selEnd {
		// A preselected range (rename's name stem, #1047): typing replaces it,
		// Backspace/Delete remove it, any other key drops the selection and
		// falls through to the normal cursor mechanics below.
		switch {
		case msg.Code == tea.KeyBackspace || msg.Code == tea.KeyDelete:
			r := []rune(p.input)
			p.input = string(append(r[:p.selStart:p.selStart], r[p.selEnd:]...))
			p.pos = p.selStart
			p.selStart, p.selEnd = 0, 0
			return nil
		case msg.Code == tea.KeyEnter || msg.Code == tea.KeyEscape:
			p.selStart, p.selEnd = 0, 0 // accept/cancel act on the full text
		case msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0:
			r := []rune(p.input)
			ins := []rune(msg.Text)
			p.input = string(append(append(append([]rune{}, r[:p.selStart]...), ins...), r[p.selEnd:]...))
			p.pos = p.selStart + len(ins)
			p.selStart, p.selEnd = 0, 0
			return nil
		default:
			p.selStart, p.selEnd = 0, 0
		}
	}
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
		m.fail(fmt.Errorf("already exists: %s", name))
		return nil
	}
	var err error
	if isDir {
		err = os.MkdirAll(path, 0o755)
	} else {
		// A pathy name ("nested/newfile.txt", #1031) creates the intermediate
		// directories, JetBrains-style — the parent of the file, not the
		// prompt's anchor dir (which always exists).
		if err = os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			err = os.WriteFile(path, []byte(lang.TemplateFor(path)), 0o644)
		}
	}
	if err != nil {
		m.fail(err)
		return nil
	}
	m.pushOp(fileOp{kind: opCreate, path: path, isDir: isDir})
	m.pendingSel = path
	return m.refreshDir(dir)
}

// promptDelete opens a confirmation for deleting the selected entry — or, with
// a multi-select range active (#1044), for the whole selection at once: one
// prompt, one batch, one undo step. The root is never deletable.
func (m *Model) promptDelete() {
	if lo, hi, ok := m.selRange(); ok && hi > lo {
		// A nested child of a selected directory is filtered out, so the
		// count can be smaller than the row span — it names what actually
		// gets trashed.
		targets := m.selTargets()
		if len(targets) == 0 {
			return // the range covers only the root
		}
		noun := "entries"
		if len(targets) == 1 {
			noun = "entry" // a dir plus its own children boils down to one
		}
		m.prompt = &prompt{
			kind:  promptConfirm,
			title: fmt.Sprintf("Delete %d %s?", len(targets), noun),
			accept: func(mm *Model, _ string) tea.Cmd {
				return mm.deleteEntries(targets)
			},
		}
		return
	}
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
		m.fail(err)
		return nil
	}
	m.pushOp(fileOp{kind: opDelete, path: path, trashPath: tp, isDir: isDir})
	return tea.Batch(m.refreshDir(filepath.Dir(path)), deletedCmd(path, isDir))
}

// deleteEntries trashes every entry of a multi-select delete (#1044) and
// records the whole batch as ONE undo step, so a single undo restores the
// full selection. A mid-batch failure still records the already-trashed
// entries (they stay undoable) before the error dialog opens; the untouched
// remainder is simply left in place.
func (m *Model) deleteEntries(targets []delTarget) tea.Cmd {
	var subs []fileOp
	var announce []tea.Cmd
	dirs := map[string]bool{}
	var failed error
	for _, t := range targets {
		tp, err := m.toTrash(t.path)
		if err != nil {
			failed = err
			break
		}
		subs = append(subs, fileOp{kind: opDelete, path: t.path, trashPath: tp, isDir: t.isDir})
		dirs[filepath.Dir(t.path)] = true
		announce = append(announce, deletedCmd(t.path, t.isDir))
	}
	m.clearSel()
	switch {
	case len(subs) == 1:
		m.pushOp(subs[0]) // a one-entry batch is just a plain delete
	case len(subs) > 1:
		m.pushOp(fileOp{kind: opDelete, batch: subs})
	}
	var cmds []tea.Cmd
	for d := range dirs {
		cmds = append(cmds, m.refreshDir(d))
	}
	cmds = append(cmds, announce...)
	if failed != nil {
		m.fail(failed)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// deletedCmd announces a removed path so the app can close editors on it.
func deletedCmd(path string, isDir bool) tea.Cmd {
	return func() tea.Msg { return FileDeletedMsg{Path: path, IsDir: isDir} }
}

// trashBase resolves the trash directory (#1038): inside the state store
// (IKE_CONFIG_DIR when set, else the project's ".ike"), never a bare
// ".ike-trash" polluting the project root and git status.
func (m *Model) trashBase() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "trash")
	}
	return filepath.Join(m.root.path, ".ike", "trash")
}

// projectTrash is the same-filesystem fallback under the project's own state
// dir, used when an IKE_CONFIG_DIR on another device makes the rename fail.
func (m *Model) projectTrash() string {
	return filepath.Join(m.root.path, ".ike", "trash")
}

// purgeStaleTrash removes leftover trash from previous sessions (#1038): the
// undo/redo stacks live in memory only, so entries surviving a restart are
// unreachable garbage. The legacy project-root ".ike-trash" is removed too.
// Synchronous: a goroutine could race the first delete's MkdirAll.
func (m *Model) purgeStaleTrash() {
	_ = os.RemoveAll(m.trashBase())
	_ = os.RemoveAll(filepath.Join(m.root.path, ".ike-trash"))
}

// toTrash moves path into the trash (#1038) and returns its new location; the
// rename stays on the same filesystem in the common case, with a project-local
// fallback when a cross-device IKE_CONFIG_DIR makes it fail. A sequence
// number keeps entries with equal basenames distinct.
func (m *Model) toTrash(path string) (string, error) {
	if m.trashDir == "" {
		d := m.trashBase()
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
		m.trashDir = d
	}
	m.trashSeq++
	tp := filepath.Join(m.trashDir, fmt.Sprintf("%d-%s", m.trashSeq, filepath.Base(path)))
	if err := os.Rename(path, tp); err != nil {
		if fb := m.projectTrash(); m.trashDir != fb {
			// Cross-device state dir: fall back to the project-local trash.
			if mkErr := os.MkdirAll(fb, 0o755); mkErr == nil {
				m.trashDir = fb
				tp = filepath.Join(fb, fmt.Sprintf("%d-%s", m.trashSeq, filepath.Base(path)))
				if err2 := os.Rename(path, tp); err2 == nil {
					return tp, nil
				}
			}
		}
		return "", err
	}
	return tp, nil
}

// promptRename opens the name prompt for renaming the selected entry, prefilled
// with its current name and the name stem preselected (#1047, JetBrains-style):
// typing immediately replaces the stem while the extension survives. Folders
// (and extension-only names like ".gitignore") preselect the whole name. The
// root is never renameable.
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
	sel := len([]rune(n.name))
	if !isDir {
		if stem := strings.TrimSuffix(n.name, filepath.Ext(n.name)); stem != "" {
			sel = len([]rune(stem))
		}
	}
	m.prompt = &prompt{
		kind:   promptInput,
		title:  fmt.Sprintf("Rename %s %q to:", what, n.name),
		input:  n.name,
		pos:    sel,
		selEnd: sel,
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
		m.fail(fmt.Errorf("cannot move a folder into itself"))
		return nil
	}
	if _, err := os.Lstat(newPath); err == nil {
		m.fail(fmt.Errorf("already exists: %s", filepath.Base(newPath)))
		return nil
	}
	if err := os.Rename(path, newPath); err != nil {
		m.fail(err)
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
	if len(op.batch) > 0 {
		// A multi-select delete (#1044): restore the whole batch as one step.
		c, ok := m.undoBatchDelete(op.batch)
		if !ok {
			return nil // the batch stays on the stack; the dialog shows why
		}
		m.ops = m.ops[:len(m.ops)-1]
		m.redoOps = append(m.redoOps, op)
		m.err = nil
		return c
	}
	switch op.kind {
	case opCreate:
		tp, err := m.toTrash(op.path)
		if err != nil {
			m.fail(err)
			return nil
		}
		op.trashPath = tp
		cmd = tea.Batch(m.refreshDir(filepath.Dir(op.path)), deletedCmd(op.path, op.isDir))
	case opDelete:
		if err := os.Rename(op.trashPath, op.path); err != nil {
			m.fail(err)
			return nil
		}
		m.pendingSel = op.path
		cmd = m.refreshDir(filepath.Dir(op.path))
	case opRename:
		if err := os.Rename(op.newPath, op.path); err != nil {
			m.fail(err)
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
	if len(op.batch) > 0 {
		// Re-apply a multi-select delete (#1044) as one step.
		c, ok := m.redoBatchDelete(op.batch)
		if !ok {
			return nil
		}
		m.redoOps = m.redoOps[:len(m.redoOps)-1]
		m.ops = append(m.ops, op)
		m.err = nil
		return c
	}
	switch op.kind {
	case opCreate:
		if err := os.Rename(op.trashPath, op.path); err != nil {
			m.fail(err)
			return nil
		}
		op.trashPath = ""
		m.pendingSel = op.path
		cmd = m.refreshDir(filepath.Dir(op.path))
	case opDelete:
		if err := os.Rename(op.path, op.trashPath); err != nil {
			m.fail(err)
			return nil
		}
		cmd = tea.Batch(m.refreshDir(filepath.Dir(op.path)), deletedCmd(op.path, op.isDir))
	case opRename:
		if err := os.Rename(op.path, op.newPath); err != nil {
			m.fail(err)
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

// undoBatchDelete restores every entry of a batch delete (#1044), last
// trashed first. On a failing restore it opens the error dialog and reports
// false so the batch stays on the undo stack — the already-restored entries
// are back in place, and retrying simply fails fast on them without loss.
func (m *Model) undoBatchDelete(subs []fileOp) (tea.Cmd, bool) {
	dirs := map[string]bool{}
	for i := len(subs) - 1; i >= 0; i-- {
		s := subs[i]
		if err := os.Rename(s.trashPath, s.path); err != nil {
			m.fail(err)
			return nil, false
		}
		dirs[filepath.Dir(s.path)] = true
	}
	m.pendingSel = subs[0].path
	var cmds []tea.Cmd
	for d := range dirs {
		cmds = append(cmds, m.refreshDir(d))
	}
	return tea.Batch(cmds...), true
}

// redoBatchDelete re-trashes every entry of an undone batch delete (#1044),
// in the original deletion order, announcing each removal so editors close.
func (m *Model) redoBatchDelete(subs []fileOp) (tea.Cmd, bool) {
	dirs := map[string]bool{}
	var announce []tea.Cmd
	for _, s := range subs {
		if err := os.Rename(s.path, s.trashPath); err != nil {
			m.fail(err)
			return nil, false
		}
		dirs[filepath.Dir(s.path)] = true
		announce = append(announce, deletedCmd(s.path, s.isDir))
	}
	var cmds []tea.Cmd
	for d := range dirs {
		cmds = append(cmds, m.refreshDir(d))
	}
	return tea.Batch(append(cmds, announce...)...), true
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

// promptInputHint is the affordance line under a promptInput's text (#1047),
// mirroring the confirm prompt's "[y]es  [n]o" and the notice's dismiss hint.
const promptInputHint = "enter accept · esc cancel"

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
	case promptNotice:
		// Dismissable error dialog (#1030): message in the Error colour,
		// hint row mirrors the confirm prompt's affordance line.
		msg := lipgloss.NewStyle().Foreground(m.theme().Error).
			Render(ansi.Truncate(p.input, inner, "…"))
		body += "\n" + msg + "\n" + ansi.Truncate("any key to dismiss", inner, "…")
	case promptInput:
		r := []rune(p.input)
		off, avail := m.promptInputWindow()
		before := string(r[off:p.pos])
		if p.selStart < p.selEnd {
			// Preselected stem (#1047): render the visible selected slice on
			// the theme's selection colours so the replace-on-type affordance
			// is visible. The selection always ends at the cursor, so only the
			// "before" part needs splitting.
			selStyle := lipgloss.NewStyle().
				Background(m.theme().Selection).Foreground(m.theme().SelectionText)
			s, e := clamp(p.selStart, off, p.pos), clamp(p.selEnd, off, p.pos)
			before = string(r[off:s]) + selStyle.Render(string(r[s:e])) + string(r[e:p.pos])
		}
		after := ""
		cur := " " // past the last rune: a blank cursor cell
		if p.pos < len(r) {
			cur = string(r[p.pos])
			if end := off + avail; p.pos+1 < end {
				after = string(r[p.pos+1 : min(end, len(r))])
			}
		}
		body += "\n" + promptInputPrefix + before + promptCursorStyle.Render(cur) + after
		body += "\n" + ansi.Truncate(promptInputHint, inner, "…")
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
	if p == nil {
		return
	}
	if p.kind == promptNotice {
		// A click dismisses the error dialog like any key (#1030).
		m.prompt = nil
		m.err = nil
		return
	}
	if p.kind != promptInput {
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
	p.selStart, p.selEnd = 0, 0 // a click drops rename's preselection (#1047)
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
