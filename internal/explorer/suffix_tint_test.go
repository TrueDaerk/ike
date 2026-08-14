package explorer

import (
	"strings"
	"testing"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// rgb flattens a color for comparison.
func rgb(c interface{ RGBA() (r, g, b, a uint32) }) [3]uint32 {
	r, g, b, _ := c.RGBA()
	return [3]uint32{r, g, b}
}

// TestNodeStylePlainForegroundByDefault guards #1051: rows render in the
// plain foreground — the filetype colour no longer paints whole rows, and
// directories carry no colour of their own (#1054).
func TestNodeStylePlainForegroundByDefault(t *testing.T) {
	m := New(".")
	pal := theme.DefaultPalette()
	for _, n := range []*node{
		{name: "main.go"},
		{name: "sub", isDir: true},
	} {
		got := m.nodeStyle(n).GetForeground()
		if rgb(got) != rgb(pal.Foreground) {
			t.Errorf("%s: foreground = %v want plain Foreground", n.name, got)
		}
	}
}

// TestSuffixTintOnlyOnCleanFiles guards #1051: the extension tint applies to
// clean files only — a VCS status owns the whole row, directories never tint.
func TestSuffixTintOnlyOnCleanFiles(t *testing.T) {
	m := New(".")
	if m.suffixTint(&node{name: "main.go"}) == nil {
		t.Fatal("clean .go file must carry a suffix tint")
	}
	if m.suffixTint(&node{name: "sub", isDir: true}) != nil {
		t.Fatal("directories must not tint")
	}
	m.SetVCS(vcs.NewSnapshot(".", map[string]vcs.FileStatus{"main.go": vcs.StatusModified}))
	if m.suffixTint(&node{name: "main.go", path: "main.go"}) != nil {
		t.Fatal("a VCS-statused file must not tint — the status owns the row")
	}
}

// TestFilenameTint guards #1366: extensionless well-known filenames
// (Makefile, Dockerfile) and dotted ones (go.mod) tint via their exact-name
// key, and the exact key wins over the extension fallback. The "dir" and
// "default" keys never act as filename matches.
func TestFilenameTint(t *testing.T) {
	m := New(".")
	for _, name := range []string{"Makefile", "Dockerfile", "Containerfile", "go.mod", "go.sum"} {
		if m.suffixTint(&node{name: name}) == nil {
			t.Errorf("%s must carry a filename tint", name)
		}
	}
	for _, name := range []string{"app.py", "index.php", "main.ts", "style.css", "query.sql"} {
		if m.suffixTint(&node{name: name}) == nil {
			t.Errorf("%s must carry an extension tint", name)
		}
	}
	for _, name := range []string{"dir", "default"} {
		if m.suffixTint(&node{name: name}) != nil {
			t.Errorf("file literally named %q must not tint via the fallback keys", name)
		}
	}
}

// TestStatusLetterMapping guards #1051: the one-cell non-colour cue.
func TestStatusLetterMapping(t *testing.T) {
	want := map[vcs.FileStatus]string{
		vcs.StatusModified: "M",
		// Without porcelain detail a half-staged file falls back to the
		// worktree half of its pair (#1868); rowVCS.letter renders "AM".
		vcs.StatusPartiallyStaged: "M",
		vcs.StatusRenamed:         "R",
		vcs.StatusAdded:           "A",
		vcs.StatusUntracked:       "U",
		vcs.StatusDeleted:         "D",
		vcs.StatusConflicted:      "C",
		vcs.StatusNone:            "",
	}
	for st, w := range want {
		if got := statusLetter(st); got != w {
			t.Errorf("statusLetter(%v) = %q want %q", st, got, w)
		}
	}
}

// TestStatusLetterRenderedAtRightEdge guards #1051: a statused row ends in
// its letter at the last text column.
func TestStatusLetterRenderedAtRightEdge(t *testing.T) {
	m := New(".")
	m.rows = []*node{{name: "main.go", path: "main.go"}}
	m.width, m.height = 20, 4
	m.SetVCS(vcs.NewSnapshot(".", map[string]vcs.FileStatus{"main.go": vcs.StatusUntracked}))
	first := strings.Split(m.View(), "\n")[0]
	if !strings.Contains(first, "U") {
		t.Fatalf("first row %q must carry the U status letter", first)
	}
	plain := stripANSI(first)
	if !strings.HasSuffix(strings.TrimRight(plain, " "), "U") {
		t.Fatalf("status letter must sit at the right edge, row = %q", plain)
	}
}

// TestPartiallyStagedCodeRenderedAtRightEdge guards #1868: a file staged and
// then edited again ends in its two-cell "AM" code, and the name it belongs
// to still fits beside it.
func TestPartiallyStagedCodeRenderedAtRightEdge(t *testing.T) {
	row := func(width int) string {
		m := New(".")
		m.rows = []*node{{name: "main.go", path: "main.go"}}
		m.width, m.height = width, 4
		m.invalidateWidth() // the fixture bypasses the rebuild that measures rows
		m.SetVCS(vcs.NewSnapshotFromEntries(".", vcs.FileEntry{
			Path: "main.go", Status: vcs.StatusPartiallyStaged, X: 'A', Y: 'M',
		}))
		return stripANSI(strings.Split(m.View(), "\n")[0])
	}
	wide := row(20)
	if !strings.HasSuffix(strings.TrimRight(wide, " "), "AM") {
		t.Fatalf("partially staged row must end in AM, row = %q", wide)
	}
	if !strings.Contains(wide, "main.go") {
		t.Fatalf("the two-cell code must not eat the name, row = %q", wide)
	}
	// The code takes exactly its own cells from the name, never more than the
	// pane holds: at 10 columns two name cells go, at 2 there is no room left
	// for it at all and the row falls back to the clipping ellipsis.
	if got := row(10); got != "  main.gAM" {
		t.Errorf("clipped row = %q want %q", got, "  main.gAM")
	}
	if got := row(2); strings.Contains(got, "A") || len([]rune(got)) != 2 {
		t.Errorf("row too narrow for the code = %q", got)
	}
}

// TestStatusCodeFallsBackToStatusLetter guards #1868: snapshots without
// porcelain detail (synthetic states) keep the single-letter cue.
func TestStatusCodeFallsBackToStatusLetter(t *testing.T) {
	m := New(".")
	m.SetVCS(vcs.NewSnapshot(".", map[string]vcs.FileStatus{"main.go": vcs.StatusModified}))
	rv := m.resolveVCS(&node{name: "main.go", path: "main.go"})
	if got := rv.letter(); got != "M" {
		t.Fatalf("letter = %q want M", got)
	}
	// Directories have no X/Y pair: their dominant status decides.
	dir := m.resolveVCS(&node{name: "sub", path: "sub", isDir: true})
	if got := dir.letter(); got != "" {
		t.Fatalf("clean dir letter = %q want empty", got)
	}
}

// stripANSI removes SGR sequences for positional assertions.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestDirTakesDominantSubtreeStatus guards #1053 at the explorer level.
func TestDirTakesDominantSubtreeStatus(t *testing.T) {
	m := New(".")
	m.SetVCS(vcs.NewSnapshot(".", map[string]vcs.FileStatus{"assets/logo.png": vcs.StatusUntracked}))
	if got := m.nodeVCSStatus(&node{name: "assets", path: "assets", isDir: true}); got != vcs.StatusUntracked {
		t.Fatalf("untracked-only dir status = %v want untracked", got)
	}
}
