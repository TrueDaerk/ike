package app

import (
	"os"
	"path/filepath"
	"testing"

	ilsp "ike/internal/lsp"
)

// TestFormatEditsMsgAmendIsOneUndoUnit (#2253): the save chain delivers the
// organize-imports edits, then the format edits with Amend — undoing once must
// take the buffer back to the pre-save text.
func TestFormatEditsMsgAmendIsOneUndoUnit(t *testing.T) {
	m := sized(t, 100, 40)
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	orig := "import a\ncode\n"
	if err := os.WriteFile(file, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(file, false)
	m = tm.(Model)

	out, _ := m.Update(ilsp.FormatEditsMsg{Path: file, Edits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 1, EndCol: 0, Text: ""}, // organize
	}})
	m = out.(Model)
	out, _ = m.Update(ilsp.FormatEditsMsg{Path: file, Amend: true, Edits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 4, Text: "CODE"}, // format
	}})
	m = out.(Model)

	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "CODE" {
		t.Fatalf("both deliveries should have applied, got %q", got)
	}

	out, _ = m.Update(keyMsg('u'))
	m = out.(Model)
	ed = m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "import a\ncode" {
		t.Fatalf("one undo must revert organize+format, got %q", got)
	}
}
