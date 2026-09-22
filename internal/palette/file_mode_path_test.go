package palette

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// file_mode_path_test.go covers #2636's paste half: a query that already *is*
// a path to an existing file opens that file, whatever the fuzzy index of the
// project thinks — the telemetry showed a 41-rune pasted path narrowed to a
// single row and still dismissed.

// pathTree writes rel under a fresh temp root and returns the root.
func pathTree(t *testing.T, rel ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, r := range rel {
		p := filepath.Join(root, filepath.FromSlash(r))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte("package foo\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

// TestFileModePastedPathWithLine: the acceptance case — a path copied with its
// ":42" suffix and its trailing newline is the first row and carries the line.
func TestFileModePastedPathWithLine(t *testing.T) {
	root := pathTree(t, "internal/foo/bar.go")
	f := fileMode("internal/foo/bar.go", "internal/other/bar.go")
	cx := Context{Root: root}

	items := f.Results("internal/foo/bar.go:42\n", cx)
	if len(items) == 0 {
		t.Fatal("no rows for a pasted path")
	}
	if items[0].Title != "internal/foo/bar.go" {
		t.Fatalf("first row = %q, want the pasted file", items[0].Title)
	}
	msg, ok := items[0].Msg.(OpenFileMsg)
	if !ok {
		t.Fatalf("first row msg = %T, want OpenFileMsg", items[0].Msg)
	}
	if msg.Path != filepath.Join(root, "internal/foo/bar.go") {
		t.Fatalf("path = %q, want the file under the root", msg.Path)
	}
	if msg.Line != 42 {
		t.Fatalf("line = %d, want 42", msg.Line)
	}
	if items[0].Preview.Line != 42 {
		t.Fatalf("preview line = %d, want the pasted line", items[0].Preview.Line)
	}
	// The fuzzy ranking still lists the file itself once, not twice.
	for _, it := range items[1:] {
		if m, ok := it.Msg.(OpenFileMsg); ok && m.Path == msg.Path {
			t.Fatalf("the pasted file is listed twice: %v", titles(items))
		}
	}
}

// A ":line:col" suffix places the column too, in the command line's grammar.
func TestFileModePastedPathWithLineCol(t *testing.T) {
	root := pathTree(t, "a/b.go")
	f := fileMode("a/b.go")

	items := f.Results("  a/b.go:12:7  ", Context{Root: root})
	msg := items[0].Msg.(OpenFileMsg)
	if msg.Line != 12 || msg.Col != 7 {
		t.Fatalf("position = %d:%d, want 12:7", msg.Line, msg.Col)
	}
}

// An absolute path inside the project resolves to the relative file — the row
// reads like a project row and opens the same file.
func TestFileModePastedAbsolutePathInsideProject(t *testing.T) {
	root := pathTree(t, "internal/foo/bar.go")
	f := fileMode("internal/foo/bar.go")
	abs := filepath.Join(root, "internal/foo/bar.go")

	items := f.Results(abs, Context{Root: root})
	if len(items) == 0 {
		t.Fatal("no rows for an absolute in-project path")
	}
	if items[0].Title != "internal/foo/bar.go" {
		t.Fatalf("first row = %q, want the project-relative title", items[0].Title)
	}
	if got := items[0].Msg.(OpenFileMsg).Path; got != abs {
		t.Fatalf("path = %q, want %q", got, abs)
	}
}

// A path outside the project keeps its absolute title and opens like any
// out-of-root file — the ';' picker's behaviour, unchanged.
func TestFileModePastedPathOutsideProject(t *testing.T) {
	outside := pathTree(t, "notes.txt")
	root := pathTree(t, "a.go")
	f := fileMode("a.go")
	abs := filepath.Join(outside, "notes.txt")

	items := f.Results(abs, Context{Root: root})
	if len(items) == 0 || items[0].Title != abs {
		t.Fatalf("rows = %v, want the absolute out-of-project path first", titles(items))
	}
	if got := items[0].Msg.(OpenFileMsg).Path; got != abs {
		t.Fatalf("path = %q, want %q", got, abs)
	}
}

// A non-existent path gets no path row: the finder falls back to its ordinary
// fuzzy state, which for a path nothing matches is the empty "no match" list.
func TestFileModePastedPathMissingFile(t *testing.T) {
	root := pathTree(t, "internal/foo/bar.go")
	f := fileMode("internal/foo/bar.go")

	if items := f.Results("internal/foo/nope.go:42", Context{Root: root}); len(items) != 0 {
		t.Fatalf("rows for a missing path = %v, want the no-match state", titles(items))
	}
}

// A directory is not a file row — descending stays the path query's job.
func TestFileModePastedDirectoryIsNoPathRow(t *testing.T) {
	root := pathTree(t, "internal/foo/bar.go")
	f := fileMode("internal/foo/bar.go")

	items := f.Results("internal/foo", Context{Root: root})
	for _, it := range items {
		if m, ok := it.Msg.(OpenFileMsg); ok && m.Path == filepath.Join(root, "internal/foo") {
			t.Fatalf("a directory was offered as a file row: %v", titles(items))
		}
	}
}

// TestPastedPathOpensAtLine guards the whole acceptance path through the
// palette: paste, enter, and the emitted message names the file and the line.
func TestPastedPathOpensAtLine(t *testing.T) {
	root := pathTree(t, "internal/foo/bar.go")
	p := New(Config{DefaultPrefix: ':'}, NewCommandMode(fakeSource{}, nil, false), fileMode("internal/foo/bar.go"))
	p.SetSize(100, 40)
	p.Open(Context{Root: root})
	p.Update(runes("@"))
	if _, ok := p.Paste("internal/foo/bar.go:42\n"); !ok {
		t.Fatal("Paste reported not handled")
	}
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter activated nothing")
	}
	msg, ok := cmd().(OpenFileMsg)
	if !ok {
		t.Fatalf("activation msg = %T, want OpenFileMsg", cmd())
	}
	if msg.Path != filepath.Join(root, "internal/foo/bar.go") || msg.Line != 42 {
		t.Fatalf("activation = %+v, want the pasted file at line 42", msg)
	}
}

// TestFileModeLongQueryFrecencyBoost guards #2636's ranking half: over a big
// tree a 12-rune query no longer buries the file one actually works on — the
// telemetry had picks at rank 28 and rank 114.
func TestFileModeLongQueryFrecencyBoost(t *testing.T) {
	const query = "servicehandl" // 12 runes
	paths := make([]string, 0, 1000)
	for i := 0; i < 999; i++ {
		// Every decoy matches the query better than the recent file does.
		paths = append(paths, fmt.Sprintf("pkg%03d/service/handler_%03d.go", i, i))
	}
	recent := "legacy/user_service/old_handler_shim.go"
	paths = append(paths, recent)

	cx := Context{Root: "/proj"}
	cold := fileMode(paths...)
	coldRank := indexOf(titles(cold.Results(query, cx)), recent)
	if coldRank < 3 {
		t.Fatalf("test setup: the recent file already ranks %d without frecency", coldRank)
	}

	f := fileMode(paths...)
	f.SetFrecency(frecStore(cx.Root, map[string]int{recent: 3}))
	got := titles(f.Results(query, cx))
	if r := indexOf(got, recent); r < 0 || r > 2 {
		t.Fatalf("recent file at rank %d, want the top 3 (head: %v)", r, got[:3])
	}
}

// A cold file that matches better still wins on a moderate query: the boost
// lifts, it does not override (#2155's rule stays).
func TestFileModeBoostDoesNotBeatClearlyBetterMatch(t *testing.T) {
	f := fileMode("note.go", "xx-note.go")
	cx := Context{Root: "/proj"}
	f.SetFrecency(frecStore(cx.Root, map[string]int{"xx-note.go": 10}))

	if got := titles(f.Results("note", cx)); got[0] != "note.go" {
		t.Fatalf("long query = %v, want the better match first", got)
	}
}

// TestFileModeEmptyQueryCapped guards the third anomaly: the empty listing is
// the frecency order, capped — not ten thousand rows nobody browses.
func TestFileModeEmptyQueryCapped(t *testing.T) {
	paths := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		paths = append(paths, fmt.Sprintf("pkg/file_%03d.go", i))
	}
	cx := Context{Root: "/proj"}
	f := fileMode(paths...)
	f.SetFrecency(frecStore(cx.Root, map[string]int{"pkg/file_200.go": 3, "pkg/file_100.go": 1}))

	got := titles(f.Results("", cx))
	if len(got) != maxEmptyRows {
		t.Fatalf("empty listing = %d rows, want the %d-row cap", len(got), maxEmptyRows)
	}
	if got[0] != "pkg/file_200.go" || got[1] != "pkg/file_100.go" {
		t.Fatalf("empty listing head = %v, want the frecency order", got[:2])
	}
	if got[2] != "pkg/file_000.go" {
		t.Fatalf("cold tail = %q, want the path order to follow", got[2])
	}
}

// indexOf returns the position of want in list, or -1.
func indexOf(list []string, want string) int {
	for i, s := range list {
		if s == want || strings.EqualFold(s, want) {
			return i
		}
	}
	return -1
}
