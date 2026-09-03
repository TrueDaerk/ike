package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/scratch"
	"ike/internal/ui"
)

// scratch_promote.go is the way *out* of the scratch store (#2339): a scratch
// that turned into something worth keeping gets a real path in the project.
//
// The manager could rename, delete and re-language a scratch, but never let it
// leave — so the last step of "quick experiment → actual code" was a manual
// copy through the file system. This command does it: pick a project path, and
// the file moves there, the store entry goes, and the open tab keeps working
// on the same document at its new location.
//
// The prompt is the untitled save-as prompt's twin (#730): one line of path
// input relative to the project root, enter accepts, esc cancels — the same
// gesture, because it answers the same question. It is a separate state rather
// than a reuse of saveAsKey because the two differ in what they act on: save-as
// *binds an unnamed buffer*, promote *moves a named file that may not even be
// open*, which is the manager's case.
//
// The move itself is scratch.Promote, and the re-pointing of open tabs is the
// explorer's FileMovedMsg — the very message the manager's rename already
// emits, so buffers, undo history, bookmarks, the watcher and the tab title
// follow through the one path that exists for a moved file (#175). Saving after
// a promote therefore writes to the project file, not into the store.

// startScratchPromote opens the target-path prompt for the scratch at path.
// The input starts on the scratch's own file name, so accepting straight away
// lands it in the project root under the name it already has.
func (m *Model) startScratchPromote(path string) {
	m.promotePath = path
	m.promoteInput.Set(filepath.Base(path))
	m.promoteErr = ""
	m.renderScratchPromote()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// promoteScratchOpen reports whether the shell currently shows the prompt.
func (m Model) promoteScratchOpen() bool { return m.promotePath != "" && m.shell.IsOpen() }

// closeScratchPromote clears the prompt state and the shell.
func (m *Model) closeScratchPromote() {
	m.promotePath = ""
	m.promoteInput.Clear()
	m.promoteErr = ""
	m.shell.Close()
}

// renderScratchPromote (re)fills the shell for the current input; called on
// open and after every accepted key.
func (m *Model) renderScratchPromote() {
	line := "> " + m.promoteInput.View()
	errLine := ""
	if m.promoteErr != "" {
		errLine = "\nE: " + m.promoteErr
	}
	name := filepath.Base(m.promotePath)
	m.shell.SetContent(ui.ModelContent{
		Heading: "Promote " + name + " — path relative to " + displayPath(m.explorer().Root()),
		Body: func() string {
			return line + errLine +
				"\n\nThe scratch leaves the store: the file moves to this path and the open tab follows it." +
				"\n\nenter promote · esc cancel"
		},
	})
}

// promoteScratchTarget resolves the typed path the way the save-as prompt
// does: a relative path is taken against the project root, an absolute one is
// used as typed.
func (m Model) promoteScratchTarget(name string) string {
	path := filepath.FromSlash(name)
	if !filepath.IsAbs(path) {
		path = filepath.Join(m.explorer().Root(), path)
	}
	return filepath.Clean(path)
}

// updateScratchPromote consumes every key while the prompt is open: enter
// promotes, esc cancels, everything else is line editing.
func (m Model) updateScratchPromote(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape:
		m.closeScratchPromote()
		return m, nil
	case msg.Code == tea.KeyEnter:
		return m.applyScratchPromote(strings.TrimSpace(m.promoteInput.Text))
	}
	if handled, changed := m.promoteInput.Key(msg); handled {
		if changed {
			m.promoteErr = ""
		}
		m.renderScratchPromote()
	}
	return m, nil
}

// applyScratchPromote performs the move. Every refusal keeps the prompt open
// with its reason on the error line, so a taken path or an unwritable
// directory costs one correction rather than a lost command.
func (m Model) applyScratchPromote(name string) (tea.Model, tea.Cmd) {
	if name == "" {
		m.promoteErr = "the target path is required"
		m.renderScratchPromote()
		return m, nil
	}
	src := m.promotePath
	target := m.promoteScratchTarget(name)
	if _, err := os.Lstat(target); err == nil {
		// Never silently clobber an existing file; the prompt stays open for
		// a different path.
		m.promoteErr = "file exists: " + displayPath(target)
		m.renderScratchPromote()
		return m, nil
	}
	// An open scratch with unsaved edits is flushed first, so the promoted
	// file carries what is on screen rather than the last written state — the
	// buffer keeps its history and follows the move afterwards.
	if ed := m.editorForPath(src); ed != nil && ed.Dirty() {
		m.watcher.MarkSaved(src)
		if err := ed.SaveTo(src); err != nil {
			m.promoteErr = err.Error()
			m.renderScratchPromote()
			return m, nil
		}
	}
	if err := scratch.Promote(src, target); err != nil {
		m.promoteErr = err.Error()
		m.renderScratchPromote()
		return m, nil
	}
	m.closeScratchPromote()
	m.recent.Touch(target)
	// The Scratches section loses a row; the project tree picks the new file
	// up through the directory watcher like any other externally created one.
	m.explorer().RefreshScratches()
	m.host.Notify(host.Info, "promoted to "+displayPath(target))
	// The move announcement does the rest: open tabs (and deferred ones)
	// re-point, the watcher follows, bookmarks re-key, the layout is saved.
	return m, func() tea.Msg { return explorer.FileMovedMsg{Old: src, New: target} }
}

// promoteScratch is the command entry point (scratch.promote). msg.Path names
// the scratch when the manager dispatches it for its selected row; otherwise
// the focused editor's file is the subject, and it has to be a scratch —
// promoting anything else is not a thing this command can mean.
func (m Model) promoteScratch(msg PromoteScratchMsg) (tea.Model, tea.Cmd) {
	path := msg.Path
	if path == "" {
		ed := m.activeEditor()
		if ed == nil || !ed.HasFile() {
			m.host.Notify(host.Info, "promote: open a scratch file first")
			return m, nil
		}
		path = ed.Path()
	}
	if !scratch.IsScratch(path) {
		m.host.Notify(host.Info, "promote: "+baseName(path)+" is not a scratch file")
		return m, nil
	}
	if _, err := os.Lstat(path); err != nil {
		m.host.Notify(host.Warn, "promote: "+err.Error())
		return m, nil
	}
	m.startScratchPromote(path)
	return m, nil
}

// pasteScratchPromote inserts a paste into the path input at its cursor
// (#1873), like every other overlay that owns a text field.
func (m *Model) pasteScratchPromote(text string) bool {
	if !m.promoteInput.Paste(text) {
		return false
	}
	m.renderScratchPromote()
	return true
}
