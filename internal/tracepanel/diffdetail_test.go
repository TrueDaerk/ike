package tracepanel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/agenttrace"
)

// TestDiffCountsInRowAndBoxDetail (#2859): a change the transcript holds no
// patch for — an Edit placed in the content a Read returned — still shows
// its "+N −M" in the tree row and the change box, and D asks for its diff.
func TestDiffCountsInRowAndBoxDetail(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "agenttrace", "testdata", "readedit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p := agenttrace.NewParser()
	p.ReadFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if err := p.Feed(f); err != nil {
		t.Fatal(err)
	}
	s := p.Session()
	m := panel(t)
	m.SetSize(120, 40)
	m.Set(agenttrace.BuildTree(s), info(s))
	m.SetPath(agenttrace.BuildPath(s))
	if !m.Select("e2/f0") {
		t.Fatal("select failed")
	}
	if view := plain(m.View()); !strings.Contains(view, "edit +1 −1  /Users/dev/src/proj/cfg.go") {
		t.Fatalf("tree row lacks the counts:\n%s", view)
	}
	if msg, ok := send(m, "D").(DiffMsg); !ok || msg.Key != "e2/f0" || msg.Path != "/Users/dev/src/proj/cfg.go" {
		t.Fatalf("D = %#v", msg)
	}
	// A read row has no diff.
	if !m.Select("e1/f0") || send(m, "D") != nil {
		t.Fatal("D on a read must stay silent")
	}
	m.SetViewMode(ViewGraph)
	if !m.Select("e2/f0") {
		t.Fatal("graph select failed")
	}
	if st := m.CurrentStop(); st == nil || !strings.HasSuffix(st.Detail, "+1 −1") {
		t.Fatalf("change box = %+v", st)
	}
	// The after-only Write knows no counts.
	if !m.Select("e6/f0") || m.CurrentStop().HasDiff {
		t.Fatalf("after-only box = %+v", m.CurrentStop())
	}
}
