package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScanLinks guards the reference regex (#1168): the Go compiler/test
// shapes, relative and absolute paths, optional columns — and the shapes that
// must NOT read as links.
func TestScanLinks(t *testing.T) {
	cases := []struct {
		in         string
		path       string // "" = no link expected
		line, col  int
		start, end int
	}{
		{in: "file.go:12", path: "file.go", line: 12, start: 0, end: 10},
		{in: "\tfile.go:12", path: "file.go", line: 12, start: 1, end: 11},
		{in: "./pkg/x.go:3:14", path: "./pkg/x.go", line: 3, col: 14, start: 0, end: 15},
		{in: "/abs/dir/file.go:42:1", path: "/abs/dir/file.go", line: 42, col: 1, start: 0, end: 21},
		{in: "sub/dir/mod.rs:7", path: "sub/dir/mod.rs", line: 7, start: 0, end: 16},
		{in: "error at main.c:9: boom", path: "main.c", line: 9, start: 9, end: 17},
		{in: "(x_test.go:33)", path: "x_test.go", line: 33, start: 1, end: 13},
		{in: "12:30", path: ""},          // a clock time, not a file
		{in: "localhost:8080", path: ""}, // host:port
		{in: "v1.2:3", path: ""},         // version string: digit-led "extension"
		{in: "no links here", path: ""},
	}
	for _, c := range cases {
		links := scanLinks(c.in)
		if c.path == "" {
			if len(links) != 0 {
				t.Errorf("%q: unexpected link %+v", c.in, links[0])
			}
			continue
		}
		if len(links) != 1 {
			t.Errorf("%q: got %d links, want 1", c.in, len(links))
			continue
		}
		l := links[0]
		if l.path != c.path || l.line != c.line || l.col != c.col ||
			l.start != c.start || l.end != c.end {
			t.Errorf("%q: got %+v, want path=%q line=%d col=%d span=[%d,%d)",
				c.in, l, c.path, c.line, c.col, c.start, c.end)
		}
	}
}

// TestResolveLinkExistenceGate guards the click-time stat gate: relative
// paths resolve against the cwd, missing files and directories never resolve.
func TestResolveLinkExistenceGate(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "pkg.go"), 0o755); err != nil { // a dir named like a file
		t.Fatal(err)
	}
	if p, ok := resolveLink(link{path: "main.go"}, dir); !ok || p != filepath.Join(dir, "main.go") {
		t.Fatalf("relative existing file: got %q, %v", p, ok)
	}
	if p, ok := resolveLink(link{path: filepath.Join(dir, "main.go")}, "/elsewhere"); !ok || p != filepath.Join(dir, "main.go") {
		t.Fatalf("absolute path must ignore cwd: got %q, %v", p, ok)
	}
	if _, ok := resolveLink(link{path: "missing.go"}, dir); ok {
		t.Fatal("missing file must not resolve")
	}
	if _, ok := resolveLink(link{path: "pkg.go"}, dir); ok {
		t.Fatal("a directory must not resolve")
	}
}

// TestLinkAtResolvesClick guards the cmd+click seam end to end: a printed
// reference under the pointer resolves to the absolute path with 0-based
// line/col; clicks off the span or on non-existing files return ok=false.
func TestLinkAtResolvesClick(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	dir := s.Dir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n\n\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, r := range "printf 'main.go:3:7\\n'\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "reference output", func() bool {
		return findRow(s, "main.go:3:7") >= 0
	})
	m := Model{sess: s, w: 80, h: 24}
	row := screenRow(s, findRow(s, "main.go:3:7"))

	p, line, col, ok := m.LinkAt(2, row) // inside "main.go"
	if !ok || p != filepath.Join(dir, "main.go") || line != 2 || col != 6 {
		t.Fatalf("LinkAt = %q %d %d %v, want %q 2 6 true", p, line, col, ok, filepath.Join(dir, "main.go"))
	}
	if _, _, _, ok := m.LinkAt(9, row); !ok { // on the ":3" digits: still the link
		t.Fatal("the line/col suffix must be part of the clickable span")
	}
	if _, _, _, ok := m.LinkAt(40, row); ok {
		t.Fatal("a click on blank space must not resolve")
	}
	// A reference to a file that does not exist stays inert.
	for _, r := range "printf 'ghost.go:1\\n'\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "ghost output", func() bool { return findRow(s, "ghost.go:1") >= 0 })
	if _, _, _, ok := m.LinkAt(2, screenRow(s, findRow(s, "ghost.go:1"))); ok {
		t.Fatal("a non-existing file must not resolve (stat gate)")
	}
}

// findRow returns the virtual line whose trimmed text equals want, -1 when
// absent — skipping the echoed command line (which contains want mid-line).
func findRow(s *Session, want string) int {
	total := s.ScrollbackLen() + 24
	for v := 0; v < total; v++ {
		if s.LineText(v) == want {
			return v
		}
	}
	return -1
}

// TestViewUnderlinesLinksCached guards the affordance (#1168) and its cache
// contract (#803): the rendered view underlines the reference span, and an
// unchanged grid returns the identical decorated string (no second scan
// changing the output).
func TestViewUnderlinesLinksCached(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	for _, r := range "printf 'go.mod:4 ok\\n'\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "underlined reference", func() bool {
		return strings.Contains(s.View(), sgrUnderline)
	})
	v1 := s.View()
	if s.View() != v1 {
		t.Fatal("cached render must be stable")
	}
	if !strings.Contains(v1, sgrNoUnderline) {
		t.Fatal("underline must be closed with SGR 24")
	}
}

// TestDecorateLinkLine: the ANSI splice underlines exactly the reference
// runes and survives styled content around and inside the span.
func TestDecorateLinkLine(t *testing.T) {
	got := decorateLinkLine("see \x1b[31mmain.go:7\x1b[39m end")
	want := "see \x1b[31m" +
		"\x1b[4mm\x1b[24m\x1b[4ma\x1b[24m\x1b[4mi\x1b[24m\x1b[4mn\x1b[24m" +
		"\x1b[4m.\x1b[24m\x1b[4mg\x1b[24m\x1b[4mo\x1b[24m\x1b[4m:\x1b[24m" +
		"\x1b[4m7\x1b[24m\x1b[39m end"
	if got != want {
		t.Fatalf("decorateLinkLine:\n got %q\nwant %q", got, want)
	}
	if plain := "no reference 12:30"; decorateLinkLine(plain) != plain {
		t.Fatal("a line without references must pass through untouched")
	}
}

// TestScrolledViewUnderlinesHistory: the underline works in scrollback rows,
// not just the live tail.
func TestScrolledViewUnderlinesHistory(t *testing.T) {
	c := &collector{}
	s := startSh(t, c)
	for _, r := range "printf 'deep.go:2\\n'; seq 1 40\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "reference scrolled into history", func() bool {
		v := findRow(s, "deep.go:2")
		return v >= 0 && v < s.ScrollbackLen()
	})
	m := Model{sess: s, w: 80, h: 24}
	v := findRow(s, "deep.go:2")
	m.ScrollBy(s.ScrollbackLen() - v) // bring the row into the window
	if !strings.Contains(m.View(), sgrUnderline) {
		t.Fatal("scrollback rendering must underline references too")
	}
}
