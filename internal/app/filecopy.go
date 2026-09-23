package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/pathcomplete"
	"ike/internal/ui"
)

// filecopy.go implements file.copy (f5, #2696): JetBrains' Copy File, the
// counterpart of file.move (f6) next door in fileops.go.
//
// Where a move only ever needs a target *directory*, a copy needs a whole
// destination path — JetBrains' dialog prefills the source's own directory
// with a "-copy" name, so a plain enter duplicates the entry next to the
// original while typing over the directory part copies it elsewhere. The
// prompt is therefore the shared directory autocomplete (dirPrompt, #2689)
// over a full path: the leading directories complete on tab, the trailing name
// is the new entry's and matches nothing, which is why the "nothing matched"
// hint is re-worded from "new directory" to "new path" here.
//
// The disk work belongs to the explorer (CopyPathMsg): it owns the recursive
// copy, the undo stack and the cursor snap, exactly as for a move. What the
// app owns is the destination — and the overwrite guard in front of it, so an
// existing target is never replaced silently.

// CopyFileMsg asks the root model to copy the explorer's selection (or the
// focused editor's file) to a destination path picked in a prompt (#2696).
// Dispatched by file.copy.
type CopyFileMsg struct{}

// fileCopyState is one pending copy: the source, the live destination prompt,
// and — once the destination turned out to exist — the resolved destination
// waiting for the overwrite guard's answer.
type fileCopyState struct {
	src     string
	dest    dirPrompt
	pending string
}

// fileCopyNewHint replaces dirPrompt's "new directory" line: the last
// component of a copy destination is a name that is *supposed* not to exist.
const fileCopyNewHint = "new path"

// startCopyFile handles CopyFileMsg: resolve the source like a rename/move
// does and open the destination prompt prefilled with the duplicate name.
func (m *Model) startCopyFile() {
	path, ok := m.refactorTarget()
	if !ok {
		m.host.Notify(host.Info, "copy: no file selected")
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		m.host.Notify(host.Error, "copy: "+displayPath(path)+": "+err.Error())
		return
	}
	st := &fileCopyState{
		src:  path,
		dest: newDirPrompt(projectRoot(), displayPath(explorer.CopyDest(path, info.IsDir()))),
	}
	st.dest.NewHint = fileCopyNewHint
	m.fileCopy = st
	m.renderFileCopyPrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// The proposal the prompt opens with — the entry's own directory with "-copy"
// appended to the name stem — is explorer.CopyDest: explorer.duplicate (cmd+d,
// #2697) starts from the same name and then numbers it, so the two commands
// spell a duplicate identically.

// fileCopyPromptOpen reports whether the shell shows the destination prompt.
func (m Model) fileCopyPromptOpen() bool {
	return m.fileCopy != nil && m.fileCopy.pending == "" && m.shell.IsOpen()
}

// fileCopyGuardOpen reports whether the shell shows the overwrite guard.
func (m Model) fileCopyGuardOpen() bool {
	return m.fileCopy != nil && m.fileCopy.pending != "" && m.shell.IsOpen()
}

// renderFileCopyPrompt (re)fills the shell for the current input: the path
// line, the matching directories underneath, and the key legend.
func (m *Model) renderFileCopyPrompt() {
	body := m.fileCopy.dest.Body(
		"relative to the project root · ↑↓ select · tab complete · enter copy · esc cancel")
	heading := "Copy " + displayPath(m.fileCopy.src) + " to"
	m.shell.SetContent(ui.ModelContent{
		Heading: heading,
		Body:    func() string { return body },
	})
}

// updateFileCopyPrompt consumes every key while the destination prompt is
// open: the shared directory autocomplete owns selection, completion and line
// editing, and hands back what the copy must do.
func (m Model) updateFileCopyPrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The prompt state is a pointer shared with the copy of the model this
	// method was called on, so the key is applied to a private clone and
	// stored back — a cancelled prompt must not leave edits behind.
	st := *m.fileCopy
	act, target := st.dest.Key(msg)
	m.fileCopy = &st
	switch act {
	case dirPromptCancel:
		m.closeFileCopyPrompt()
		return m, nil
	case dirPromptAccept:
		return m.acceptFileCopyTarget(target)
	}
	m.renderFileCopyPrompt()
	return m, nil
}

// fileCopyClickRow accepts the clicked candidate: a click on a directory is
// enter on a highlight, like in the extract prompt (#2689). A directory
// destination copies the entry *into* it under its own name.
func (m Model) fileCopyClickRow(row int) (tea.Model, tea.Cmd) {
	cand, ok := m.fileCopy.dest.CandidateAt(row)
	if !ok {
		return m, nil
	}
	return m.acceptFileCopyTarget(cand)
}

// pasteFileCopyPrompt inserts a paste into the path input at its cursor
// (#1873), like every other single-field prompt.
func (m *Model) pasteFileCopyPrompt(text string) bool {
	if !m.fileCopy.dest.Paste(strings.TrimSpace(text)) {
		return false
	}
	m.renderFileCopyPrompt()
	return true
}

// acceptFileCopyTarget resolves the typed destination and either runs the copy
// or raises the overwrite guard — the shared tail of enter and a click.
func (m Model) acceptFileCopyTarget(target string) (tea.Model, tea.Cmd) {
	src := m.fileCopy.src
	if strings.TrimSpace(target) == "" {
		m.closeFileCopyPrompt()
		return m, nil
	}
	dest := fileCopyDest(src, target)
	if _, err := os.Lstat(dest); err == nil {
		m.openFileCopyGuard(dest)
		return m, nil
	}
	m.closeFileCopyPrompt()
	return m, m.runFileCopy(src, dest, false)
}

// fileCopyDest resolves the typed destination into an absolute path: "~"
// expands and a relative path is project-relative (IKE runs in the project
// root). An existing *directory* is a destination the source lands inside,
// keeping its own name — that is what a completed candidate (which always ends
// in a separator) means.
func fileCopyDest(src, target string) string {
	dest := pathcomplete.Expand(strings.TrimSpace(target))
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(projectRoot(), dest)
	}
	dest = filepath.Clean(dest)
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		return filepath.Join(dest, filepath.Base(src))
	}
	return dest
}

// openFileCopyGuard asks before an existing destination is replaced (#2696).
// Keeping it is the primary answer: enter must never be the key that destroys
// a file the user already had.
func (m *Model) openFileCopyGuard(dest string) {
	st := *m.fileCopy
	st.pending = dest
	m.fileCopy = &st
	m.shell.SetContent(ui.ModelContent{
		Heading: "Destination already exists",
		Body: func() string {
			return fmt.Sprintf("%s already exists.\n\n", displayPath(dest)) +
				guardLine("s", "keep it — copy nothing", true) +
				guardLine("o", "overwrite it", false) +
				guardCancel("cancel — copy nothing")
		},
	})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// updateFileCopyGuard consumes every key while the overwrite guard is open;
// anything but the three answers is swallowed, as in the other guards.
func (m Model) updateFileCopyGuard(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	src, dest := m.fileCopy.src, m.fileCopy.pending
	switch guardAnswer(msg, "s") {
	case "o":
		m.closeFileCopyPrompt()
		return m, m.runFileCopy(src, dest, true)
	case "s", "esc":
		m.closeFileCopyPrompt()
		m.host.Notify(host.Info, "copy: cancelled — "+displayPath(dest)+" kept")
	}
	return m, nil
}

// runFileCopy hands the resolved destination to the explorer, which owns the
// recursive copy, the undo entry and the cursor snap onto the new entry. The
// explorer is revealed and focused first, exactly like the other palette-
// invoked file ops (#374), so its error dialog and the new selection are
// visible.
func (m *Model) runFileCopy(src, dest string, overwrite bool) tea.Cmd {
	m.focusExplorer()
	exp := m.explorer()
	var cmd tea.Cmd
	*exp, cmd = exp.Update(explorer.CopyPathMsg{Path: src, Dest: dest, Overwrite: overwrite})
	return cmd
}

// closeFileCopyPrompt drops the prompt/guard state and the shell.
func (m *Model) closeFileCopyPrompt() {
	m.fileCopy = nil
	m.shell.Close()
}
