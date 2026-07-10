package app

import (
	"os"
	"path/filepath"
	"testing"

	ilsp "ike/internal/lsp"
)

// TestFormatEditsMsgRoutesToEditor: formatting edits land in the owning
// editor's buffer as one applied batch.
func TestFormatEditsMsgRoutesToEditor(t *testing.T) {
	m := sized(t, 100, 40)
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("func main(){\nx:=1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(file, false)
	m = tm.(Model)

	out, _ := m.Update(ilsp.FormatEditsMsg{Path: file, Edits: []ilsp.FormatEdit{
		{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 0, Text: "\t"},
	}})
	m = out.(Model)
	ed := m.panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "func main(){\n\tx:=1\n}" {
		t.Fatalf("edits should apply to the buffer, got %q", got)
	}
	if !ed.Dirty() {
		t.Fatal("formatting should mark the buffer dirty (apply, not save)")
	}
}
