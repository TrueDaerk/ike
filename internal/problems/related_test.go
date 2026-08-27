package problems

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
)

// relDiag builds a diagnostic carrying one related-information entry (#2147).
func relDiag(msg string, rel ilsp.RelatedInfo) ilsp.Diagnostic {
	d := diag(4, 2, 1, msg, "E1")
	d.Related = []ilsp.RelatedInfo{rel}
	return d
}

// TestRelatedRowsListUnderDiagnostic guards #2147: a diagnostic's related
// information is listed as its own row beneath it, showing the note and the
// location it points at — without inflating the problem counts.
func TestRelatedRowsListUnderDiagnostic(t *testing.T) {
	s := NewStore()
	rel := ilsp.RelatedInfo{Path: "/proj/other.go", Line: 9, Col: 4, Message: "declared here"}
	s.Set("/proj/main.go", []ilsp.Diagnostic{relDiag("redeclared", rel)})
	m := New(nil)
	m.SetStore(s)
	m.SetSize(80, 10)

	if m.Rows() != 3 {
		t.Fatalf("Rows = %d, want header + diagnostic + related", m.Rows())
	}
	if m.rows[2].rel == nil {
		t.Fatalf("third row should be the related entry: %+v", m.rows[2])
	}
	v := m.View()
	if !strings.Contains(v, "declared here") || !strings.Contains(v, "other.go:10") {
		t.Errorf("related row missing note or location:\n%s", v)
	}
	if errs, warns := m.visibleCounts(); errs != 1 || warns != 0 {
		t.Errorf("counts = %d/%d, related row must not count", errs, warns)
	}
}

// TestRelatedRowOpensItsOwnLocation guards #2147's navigation criterion: enter
// on a related row opens the file the entry points at — another file than the
// diagnostic's — at the entry's position.
func TestRelatedRowOpensItsOwnLocation(t *testing.T) {
	s := NewStore()
	rel := ilsp.RelatedInfo{Path: "/proj/other.go", Line: 9, Col: 4, Message: "declared here"}
	s.Set("/proj/main.go", []ilsp.Diagnostic{relDiag("redeclared", rel)})
	m := New(nil)
	m.SetStore(s)
	m.SetSize(80, 10)
	m.SetFocused(true)

	m.cursor = 2
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on a related row should emit an open command")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok {
		t.Fatalf("want OpenLocationMsg, got %T", cmd())
	}
	if msg.Path != "/proj/other.go" || msg.Line != 9 || msg.Col != 4 {
		t.Errorf("open target = %+v, want other.go 9:4", msg)
	}
}

// TestRelatedRowCopiesItsLocation guards that the copy action (#2071) yanks
// the related entry itself, not the diagnostic it hangs under.
func TestRelatedRowCopiesItsLocation(t *testing.T) {
	s := NewStore()
	rel := ilsp.RelatedInfo{Path: "/proj/other.go", Line: 9, Col: 4, Message: "declared here"}
	s.Set("/proj/main.go", []ilsp.Diagnostic{relDiag("redeclared", rel)})
	m := New(nil)
	m.SetStore(s)
	m.SetSize(80, 10)

	cmd := m.copyRow(2)
	if cmd == nil {
		t.Fatal("copy on a related row should emit a CopyMsg")
	}
	msg, ok := cmd().(CopyMsg)
	if !ok {
		t.Fatalf("want CopyMsg, got %T", cmd())
	}
	if msg.Text != "/proj/other.go:10:5: declared here" {
		t.Errorf("copied text = %q", msg.Text)
	}
}

// TestRefreshKeepsCursorOnRelatedRow guards that a re-publish leaves the
// selection on the same related entry instead of snapping back to its parent
// diagnostic, which shares the row's path and position.
func TestRefreshKeepsCursorOnRelatedRow(t *testing.T) {
	s := NewStore()
	d := diag(4, 2, 1, "redeclared", "E1")
	d.Related = []ilsp.RelatedInfo{
		{Path: "/proj/other.go", Line: 9, Message: "declared here"},
		{Path: "/proj/third.go", Line: 3, Message: "and here"},
	}
	s.Set("/proj/main.go", []ilsp.Diagnostic{d})
	m := New(nil)
	m.SetStore(s)
	m.SetSize(80, 10)
	m.cursor = 3 // the second related entry

	m.Refresh()
	if m.cursor != 3 || m.rows[m.cursor].rel == nil || m.rows[m.cursor].rel.Message != "and here" {
		t.Errorf("cursor = %d (%+v), want the second related row", m.cursor, m.rows[m.cursor])
	}
}
