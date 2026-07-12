package todoindex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/search"
)

// key builds a plain key press.
func key(s string) tea.KeyPressMsg {
	if len(s) == 1 {
		r := rune(s[0])
		return tea.KeyPressMsg{Code: r, Text: s}
	}
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+t":
		return tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{}
}

// batch feeds the model a streamed batch under its current generation, the way
// the root model's ScanMsg unwrap does.
func batch(m *Model, matches ...search.Match) {
	m.Apply(search.BatchMsg{Gen: m.gen, Matches: matches})
}

func done(m *Model) { m.Apply(search.DoneMsg{Gen: m.gen}) }

// match builds a scanner hit whose range marks tag inside text.
func match(path string, line int, text, tag string) search.Match {
	start := strings.Index(text, tag)
	return search.Match{Path: path, Line: line, Text: text,
		StartCol: len([]rune(text[:start])), EndCol: len([]rune(text[:start])) + len([]rune(tag))}
}

func TestDefaultPatternsAndScanQuery(t *testing.T) {
	m := New(search.New(nil), t.TempDir(), nil)
	if got := strings.Join(m.Patterns(), ","); got != "TODO,FIXME,HACK,XXX" {
		t.Fatalf("default patterns = %q", got)
	}
	if p := m.pattern(); p != "(?:TODO|FIXME|HACK|XXX)" {
		t.Fatalf("pattern = %q", p)
	}
	// Custom patterns are quoted literals.
	m = New(search.New(nil), t.TempDir(), []string{"NOTE", "C++"})
	if p := m.pattern(); p != `(?:NOTE|C\+\+)` {
		t.Fatalf("custom pattern = %q", p)
	}
}

func TestApplyClassifiesAndCounts(t *testing.T) {
	m := New(search.New(nil), t.TempDir(), nil)
	batch(m,
		match("a.go", 1, "// TODO: one", "TODO"),
		match("a.go", 5, "// fixme lower", "fixme"),
		match("b.go", 2, "# HACK here", "HACK"),
	)
	done(m)
	if m.Count() != 3 || m.Total() != 3 || m.Files() != 2 {
		t.Fatalf("count=%d total=%d files=%d, want 3/3/2", m.Count(), m.Total(), m.Files())
	}
	if m.entries[1].Tag != "FIXME" {
		t.Fatalf("lower-case fixme classified as %q", m.entries[1].Tag)
	}
	if !m.Scanned() {
		t.Fatal("DoneMsg must mark the index scanned")
	}
}

func TestStaleGenerationDropped(t *testing.T) {
	m := New(search.New(nil), t.TempDir(), nil)
	m.Apply(search.BatchMsg{Gen: 7, Matches: []search.Match{match("a.go", 1, "TODO x", "TODO")}})
	if m.Count() != 0 {
		t.Fatal("stale generation must be dropped")
	}
}

func TestTagFilterCyclesAndFileFilter(t *testing.T) {
	m := New(search.New(nil), t.TempDir(), nil)
	cur, err := filepath.Abs("cur.go")
	if err != nil {
		t.Fatal(err)
	}
	m.open = true
	m.curPath = cur
	batch(m,
		match(cur, 1, "// TODO: one", "TODO"),
		match("other.go", 2, "// TODO: two", "TODO"),
		match("other.go", 3, "// FIXME: three", "FIXME"),
	)
	// ctrl+t cycles All -> TODO.
	m.Update(key("ctrl+t"))
	if m.Total() != 2 {
		t.Fatalf("TODO filter shows %d, want 2", m.Total())
	}
	m.Update(key("ctrl+t")) // FIXME
	if m.Total() != 1 {
		t.Fatalf("FIXME filter shows %d, want 1", m.Total())
	}
	m.Update(key("ctrl+t")) // HACK
	m.Update(key("ctrl+t")) // XXX
	m.Update(key("ctrl+t")) // back to All
	if m.Total() != 3 {
		t.Fatalf("filter did not cycle back to all, total=%d", m.Total())
	}
	// Current-file-only keeps cur.go's entry.
	m.Update(key("ctrl+o"))
	if m.Total() != 1 || m.Files() != 1 {
		t.Fatalf("current-file filter shows %d in %d files, want 1/1", m.Total(), m.Files())
	}
	// Count stays the unfiltered total for the status line.
	if m.Count() != 3 {
		t.Fatalf("Count = %d, want unfiltered 3", m.Count())
	}
}

func TestEnterDispatchesOpenLocationAndCloses(t *testing.T) {
	m := New(search.New(nil), t.TempDir(), nil)
	m.open = true
	batch(m, match("a.go", 4, "// TODO: jump", "TODO"))
	cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter on an entry must dispatch")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok {
		t.Fatalf("enter dispatched %T", cmd())
	}
	if msg.Line != 4 || msg.Col != 3 || filepath.Base(msg.Path) != "a.go" {
		t.Fatalf("open location = %+v", msg)
	}
	if m.IsOpen() {
		t.Fatal("enter must close the overlay")
	}
}

func TestRescanFileSplicesEntries(t *testing.T) {
	root := t.TempDir()
	m := New(search.New(nil), root, nil)
	path := filepath.Join(root, "f.go")
	other := filepath.Join(root, "g.go")
	batch(m,
		match(path, 1, "// TODO: old one", "TODO"),
		match(path, 2, "// TODO: old two", "TODO"),
		match(other, 1, "// HACK: keep", "HACK"),
	)
	if err := os.WriteFile(path, []byte("// FIXME: new\nclean line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := m.RescanFile(path)
	if cmd == nil {
		t.Fatal("in-root file must produce a rescan command")
	}
	msg, ok := cmd().(FileScanMsg)
	if !ok || len(msg.Items) != 1 || msg.Items[0].Tag != "FIXME" {
		t.Fatalf("file scan = %+v", msg)
	}
	m.ApplyFileScan(msg)
	if m.Count() != 2 {
		t.Fatalf("splice left %d entries, want 2", m.Count())
	}
	// The rescanned file keeps its position ahead of g.go.
	if m.entries[0].Tag != "FIXME" || filepath.Base(m.entries[1].Item.Path) != "g.go" {
		t.Fatalf("splice order wrong: %+v", m.entries)
	}
}

func TestRescanFileStaleGenerationDropped(t *testing.T) {
	root := t.TempDir()
	m := New(search.New(nil), root, nil)
	path := filepath.Join(root, "f.go")
	if err := os.WriteFile(path, []byte("// TODO: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := m.RescanFile(path)
	m.gen++ // a full rescan started in between
	m.ApplyFileScan(cmd().(FileScanMsg))
	if m.Count() != 0 {
		t.Fatal("stale file scan must be dropped")
	}
}

func TestRescanFileSkipsOutsideRootAndHidden(t *testing.T) {
	root := t.TempDir()
	m := New(search.New(nil), root, nil)
	if cmd := m.RescanFile(filepath.Join(root, "..", "outside.go")); cmd != nil {
		t.Fatal("files outside the root must be skipped")
	}
	if cmd := m.RescanFile(filepath.Join(root, ".hidden", "f.go")); cmd != nil {
		t.Fatal("hidden paths must be skipped like the project scan")
	}
}

func TestViewRendersFiltersAndStatus(t *testing.T) {
	m := New(search.New(nil), t.TempDir(), nil)
	m.SetSize(100, 30)
	m.open = true
	batch(m,
		match("a.go", 1, "// TODO: one", "TODO"),
		match("a.go", 2, "// FIXME: two", "FIXME"),
	)
	done(m)
	v := m.View()
	// The title renders styled per rune; assert on the filter row, the group
	// header and the status row instead.
	for _, want := range []string{"Tag: All", "Current file", "a.go", "2 tags in 1 file"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
}
