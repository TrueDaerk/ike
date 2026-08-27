package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/diff"
	ilsp "ike/internal/lsp"
)

// lsprenamepreview.go is the multi-file rename confirmation (#2149). A rename
// that stays inside one file applies instantly, as it always has; one that
// reaches further arrives as a RenamePreviewMsg and opens this dialog first:
// every affected file with its edit count, and a diff of the selected file's
// current text against what the rename would make of it. Enter applies exactly
// those edits through the normal apply path (open buffers as one undo unit,
// closed files on disk), esc cancels — the bridge has written nothing yet, so
// cancelling leaves every buffer and every file untouched.

// renamePreviewMaxDiffLines caps the rendered diff of one file. The dialog is
// a decision aid, not a diff viewer: a rename touching hundreds of lines in a
// single file shows the first of them and says how many were left out. The
// shell scrolls what fits (ctrl+d/ctrl+u), so the cap only bounds the work per
// frame, never hides that there is more.
const renamePreviewMaxDiffLines = 200

// lspRenamePreviewState is the open dialog: the files the pending rename would
// rewrite, the cursor, the bridge continuation that applies them, and the
// selected file's cached diff. Like the recovery prompt the diff is recomputed
// on cursor moves only, never per frame (#409).
type lspRenamePreviewState struct {
	oldName string
	newName string
	files   []ilsp.RenamePreviewFile
	cursor  int
	apply   func() tea.Cmd
	diff    diff.Result
}

// openLSPRenamePreview raises the dialog for a pending multi-file rename. A
// message that carries nothing to preview (every file dropped) is ignored: the
// bridge only sends one for two or more files, but the dialog must not open on
// an empty list either way.
func (m *Model) openLSPRenamePreview(msg ilsp.RenamePreviewMsg) {
	if len(msg.Files) == 0 || msg.Apply == nil {
		return
	}
	m.lspRenamePreview = &lspRenamePreviewState{
		oldName: msg.OldName,
		newName: msg.NewName,
		files:   msg.Files,
		apply:   msg.Apply,
	}
	m.refreshLSPRenamePreviewDiff()
	m.shell.SetContent(lspRenamePreviewContent{m: *m})
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// lspRenamePreviewContent implements ui.Content (not ModelContent) so the body
// learns the shell's width budget and can clip paths and diff lines to it. The
// model copy is bound once at open time; the dialog's state lives behind the
// pointer, so cursor moves show on the very next frame (#409).
type lspRenamePreviewContent struct{ m Model }

// Title implements ui.Content.
func (c lspRenamePreviewContent) Title() string { return "Rename preview" }

// Render implements ui.Content.
func (c lspRenamePreviewContent) Render(width int) string {
	return c.m.lspRenamePreviewBody(width)
}

// lspRenamePreviewOpen reports whether the confirmation dialog is showing.
func (m Model) lspRenamePreviewOpen() bool {
	return m.lspRenamePreview != nil && m.shell.IsOpen()
}

// lspRenamePreviewSel returns the highlighted file, false when out of range.
func (m Model) lspRenamePreviewSel() (ilsp.RenamePreviewFile, bool) {
	p := m.lspRenamePreview
	if p == nil || p.cursor < 0 || p.cursor >= len(p.files) {
		return ilsp.RenamePreviewFile{}, false
	}
	return p.files[p.cursor], true
}

// refreshLSPRenamePreviewDiff recomputes the selected file's preview: its
// current text on the left, the renamed text on the right, so the "+" side is
// exactly what confirming would write.
func (m *Model) refreshLSPRenamePreviewDiff() {
	p := m.lspRenamePreview
	if p == nil {
		return
	}
	p.diff = diff.Result{}
	f, ok := m.lspRenamePreviewSel()
	if !ok {
		return
	}
	p.diff = diff.Compute(f.Before, f.After)
}

// lspRenamePreviewBody renders the dialog: the summary line, every affected
// file with its edit count, the selected file's diff and the key legend. The
// file list is full width — a file is identified by its path, and truncating
// it into a side column is what the preview exists to avoid.
func (m Model) lspRenamePreviewBody(width int) string {
	p := m.lspRenamePreview
	cur, ok := m.lspRenamePreviewSel()
	if !ok {
		return ""
	}
	pal := m.pal()
	sel := lipgloss.NewStyle().Foreground(pal.Foreground).Bold(true)
	dim := lipgloss.NewStyle().Foreground(pal.Hint)

	var b strings.Builder
	b.WriteString(ansi.Truncate(renamePreviewHeadline(p), max(1, width), "…") + "\n\n")
	for i, f := range p.files {
		marker := "  "
		if i == p.cursor {
			marker = "▸ "
		}
		line := marker + clipPathLeft(displayPath(f.Path)+"  "+renameEditCount(f.Edits)+renameOpenNote(f.Open), max(1, width-2))
		if i == p.cursor {
			line = sel.Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	b.WriteString(dim.Render("  "+clipPathLeft(
		"diff for "+displayPath(cur.Path)+" (- current, + after rename)", max(1, width-2))) + "\n")
	for _, line := range m.renderLSPRenamePreviewDiff(max(10, width-2)) {
		b.WriteString("  " + line + "\n")
	}

	b.WriteString("\n")
	b.WriteString(dim.Render("  [enter] apply   [esc] cancel   [j/k] move   [ctrl+d/ctrl+u] scroll"))
	return b.String()
}

// renamePreviewHeadline summarizes what confirming would do: the rename itself
// plus the total edit and file counts.
func renamePreviewHeadline(p *lspRenamePreviewState) string {
	edits := 0
	for _, f := range p.files {
		edits += f.Edits
	}
	name := "Rename"
	if p.oldName != "" {
		name = "Rename " + quoteName(p.oldName)
	}
	return fmt.Sprintf("%s to %s — %s in %s", name, quoteName(p.newName),
		renameEditCount(edits), renameFileCount(len(p.files)))
}

// quoteName quotes a symbol name for the headline.
func quoteName(s string) string { return "\"" + s + "\"" }

// renameEditCount phrases an edit total: "1 edit" / "4 edits".
func renameEditCount(n int) string {
	if n == 1 {
		return "1 edit"
	}
	return fmt.Sprintf("%d edits", n)
}

// renameFileCount phrases a file total: "1 file" / "3 files".
func renameFileCount(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// renameOpenNote marks files an editor buffer holds — they are edited in-buffer as
// one undo unit and stay dirty, the rest are rewritten on disk.
func renameOpenNote(open bool) string {
	if open {
		return " · open buffer"
	}
	return ""
}

// renderLSPRenamePreviewDiff renders the selected file's diff as styled lines
// — the shared inline renderer behind local history, the change feed and the
// recovery prompt — capped at renamePreviewMaxDiffLines with a note for the
// remainder.
func (m Model) renderLSPRenamePreviewDiff(width int) []string {
	pal := m.pal()
	dim := lipgloss.NewStyle().Foreground(pal.Hint)
	p := m.lspRenamePreview
	if len(p.diff.Hunks) == 0 {
		return []string{dim.Render(ansi.Truncate("no visible change in this file", width, "…"))}
	}
	lines := miniDiffLines(pal, p.diff, width)
	if len(lines) > renamePreviewMaxDiffLines {
		rest := len(lines) - renamePreviewMaxDiffLines
		lines = append(lines[:renamePreviewMaxDiffLines:renamePreviewMaxDiffLines],
			dim.Render(ansi.Truncate(fmt.Sprintf("… %d more diff line(s)", rest), width, "…")))
	}
	return lines
}

// updateLSPRenamePreview consumes every key while the dialog is open: j/k move
// (the diff follows the selection live), enter applies the previewed edits,
// esc cancels without touching anything. ctrl+d/ctrl+u scroll the shell for
// previews taller than the box; every other key is swallowed so nothing leaks
// past the modal.
func (m Model) updateLSPRenamePreview(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.lspRenamePreview
	if p == nil || len(p.files) == 0 {
		return m.closeLSPRenamePreview(), nil
	}
	// Shared list semantics (#1666): steps wrap, page jumps clamp. Every move
	// recomputes the diff for the newly selected file.
	if m.pickerNav(msg.String(), &p.cursor, len(p.files), m.refreshLSPRenamePreviewDiff) {
		return m, nil
	}
	switch msg.String() {
	case "enter":
		apply := p.apply
		m = m.closeLSPRenamePreview()
		if apply == nil {
			return m, nil
		}
		return m, apply()
	case "esc", "q":
		return m.closeLSPRenamePreview(), nil
	case "ctrl+d", "ctrl+u":
		m.shell.Update(msg)
	}
	return m, nil
}

// closeLSPRenamePreview dismisses the dialog. Nothing has been applied at this
// point, so a cancelled rename leaves every buffer and file exactly as it was.
func (m Model) closeLSPRenamePreview() Model {
	m.lspRenamePreview = nil
	m.shell.Close()
	return m
}
