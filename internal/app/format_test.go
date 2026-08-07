package app

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/highlight"
	"ike/internal/layout"
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
	ed := m.activeWS().Panes.Get(m.activeEditorKey()).Editor()
	if got := ed.Text(); got != "func main(){\n\tx:=1\n}" {
		t.Fatalf("edits should apply to the buffer, got %q", got)
	}
	if !ed.Dirty() {
		t.Fatal("formatting should mark the buffer dirty (apply, not save)")
	}
}

// TestFormatEditsMsgSchedulesReparse (#1683): applying formatter edits bypasses
// the editor's Update loop, so its highlight/conceal caches went stale until
// the next keystroke. The handler must return a parse command for the applying
// view so the reformatted content is re-highlighted immediately.
func TestFormatEditsMsgSchedulesReparse(t *testing.T) {
	m := sized(t, 100, 40)
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	if err := os.WriteFile(file, []byte("func main(){\nx:=1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.openPath(file, false)
	m = tm.(Model)

	_, cmd := m.Update(ilsp.FormatEditsMsg{Path: file, Edits: []ilsp.FormatEdit{
		{StartLine: 1, StartCol: 0, EndLine: 1, EndCol: 0, Text: "\t"},
	}})
	if cmd == nil {
		t.Fatal("FormatEditsMsg must schedule a reparse of the edited buffer")
	}
	sp, ok := cmd().(highlight.SpansMsg)
	if !ok || sp.Path != file {
		t.Fatalf("reparse must yield a SpansMsg for %s, got %#v", file, sp)
	}
}

// TestFormatEditsMsgAppliesOncePerDocument (#366): views of a shared document
// alias one buffer, so a FormatEditsMsg must be applied through exactly one
// view — routing it to every view applied every edit once per view (an LSP
// rename of z -> match1 with a split open produced match1atch1).
func TestFormatEditsMsgAppliesOncePerDocument(t *testing.T) {
	dir := t.TempDir()
	file := writeTemp(t, dir, "a.py", "print(z)\n")
	m := openApp(t, file)
	src := m.activeWS().Panes.Get(m.activeWS().Panes.Focused()).Editor()

	m = dispatch(t, m, SplitViewMsg{Zone: layout.ZoneRight})
	other := m.activeWS().Panes.Get(m.activeWS().Panes.Focused()).Editor()
	if !other.SharesBufferWith(src) {
		t.Fatal("precondition: views must alias one buffer")
	}

	// The rename edit: z (line 0, cols 6-7) -> match1.
	out, _ := m.Update(ilsp.FormatEditsMsg{Path: file, Edits: []ilsp.FormatEdit{
		{StartLine: 0, StartCol: 6, EndLine: 0, EndCol: 7, Text: "match1"},
	}})
	m = out.(Model)

	if got := src.Text(); got != "print(match1)" {
		t.Fatalf("edit must apply exactly once, got %q", got)
	}
	if got := other.Text(); got != "print(match1)" {
		t.Fatalf("second view must read the same document, got %q", got)
	}
}
