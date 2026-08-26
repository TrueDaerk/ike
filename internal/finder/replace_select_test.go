package finder

// replace_select_test.go covers replace-in-path selective apply and the
// per-row preview (#2154): the exclusion toggles, apply-selected semantics,
// the live rewrite hook, and the scan-time mtime capture for the stale-file
// guard.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/locations"
	"ike/internal/search"
)

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func TestCtrlTExcludesMatchFromReplaceAll(t *testing.T) {
	m := openedReplace(t) // a.go:1, a.go:5, b.go:2 — cursor on a.go:1
	m.Update(ctrl('t'))   // exclude a.go:1, cursor steps to a.go:5
	if it, _ := m.list.Current(); it.Line != 5 {
		t.Fatalf("toggle must step to the next match, cursor on %+v", it)
	}
	cmd := m.Update(ctrl('a'))
	req := cmd().(ReplaceRequestMsg)
	if len(req.Items) != 2 {
		t.Fatalf("replace-all must skip the excluded match: %+v", req.Items)
	}
	for _, it := range req.Items {
		if it.Path == "a.go" && it.Line == 1 {
			t.Fatal("the excluded match must not be in the batch")
		}
	}
	if m.list.Total() != 1 || m.list.ExcludedCount() != 1 {
		t.Fatalf("the excluded row must stay listed: total=%d excluded=%d",
			m.list.Total(), m.list.ExcludedCount())
	}
	// Nothing included left: replace-all is a no-op until re-included.
	if cmd := m.Update(ctrl('a')); cmd != nil {
		t.Fatal("replace-all with only excluded rows must do nothing")
	}
	m.Update(ctrl('t')) // re-include the survivor
	req = m.Update(ctrl('a'))().(ReplaceRequestMsg)
	if len(req.Items) != 1 || req.Items[0].Line != 1 {
		t.Fatalf("re-included match must apply: %+v", req.Items)
	}
}

func TestCtrlGExcludesWholeFile(t *testing.T) {
	m := openedReplace(t)
	m.Update(ctrl('g')) // exclude all of a.go
	if m.list.ExcludedCount() != 2 {
		t.Fatalf("ctrl+g must exclude the cursor's file, got %d", m.list.ExcludedCount())
	}
	if cmd := m.Update(ctrl('f')); cmd != nil {
		t.Fatal("replace-file on a fully excluded file must do nothing")
	}
	req := m.Update(ctrl('a'))().(ReplaceRequestMsg)
	if len(req.Items) != 1 || req.Items[0].Path != "b.go" {
		t.Fatalf("only the other file's match applies: %+v", req.Items)
	}
	if !strings.Contains(m.statusRow(80), "2 excluded") {
		t.Fatalf("status must report the excluded count: %q", m.statusRow(80))
	}
}

func TestReplaceModeSetsPerRowRewriteHook(t *testing.T) {
	m := openedReplace(t)
	m.Update(key("tab")) // focus the replace field
	typeText(m, "thread")
	m.View()
	if m.list.Rewrite == nil {
		t.Fatal("replace mode must install the per-row rewrite hook")
	}
	it := locations.Item{Text: "needle text", StartCol: 0, EndCol: 6}
	seg, ok := m.list.Rewrite(it, locations.Range{Start: 0, End: 6})
	if !ok || seg != "thread" {
		t.Fatalf("hook must yield the replacement segment, got %q ok=%v", seg, ok)
	}
}

func TestPerRowRewriteHookExpandsCaptureGroups(t *testing.T) {
	m := New(search.New(nil))
	m.SetSize(100, 40)
	m.OpenReplace(t.TempDir())
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}) // regex on
	typeText(m, `ne(ed)le`)
	feed(m, match("a.go", 1))
	m.Update(key("tab"))
	typeText(m, "$1-x")
	m.View()
	seg, ok := m.list.Rewrite(
		locations.Item{Text: "needle text", StartCol: 0, EndCol: 6},
		locations.Range{Start: 0, End: 6})
	if !ok || seg != "ed-x" {
		t.Fatalf("capture groups must expand in the preview, got %q ok=%v", seg, ok)
	}
}

func TestFindModeClearsRewriteHook(t *testing.T) {
	m := opened(t)
	typeText(m, "needle")
	feed(m, match("a.go", 1))
	m.View()
	if m.list.Rewrite != nil {
		t.Fatal("find mode must not preview replacements")
	}
}

func TestBatchRecordsMtimeForStaleGuard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(path, []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(search.New(nil))
	m.SetSize(100, 40)
	m.OpenReplace(dir)
	typeText(m, "needle")
	feed(m, search.Match{Path: path, Line: 1, Text: "needle", StartCol: 0, EndCol: 6})
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	req := m.Update(ctrl('a'))().(ReplaceRequestMsg)
	got, ok := req.Mtimes[path]
	if !ok || !got.Equal(fi.ModTime()) {
		t.Fatalf("request must carry the scan-time mtime: %v ok=%v", got, ok)
	}
}
