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

// TestStatusLetterMapping guards #1051: the one-cell non-colour cue.
func TestStatusLetterMapping(t *testing.T) {
	want := map[vcs.FileStatus]string{
		vcs.StatusModified:   "M",
		vcs.StatusRenamed:    "R",
		vcs.StatusAdded:      "A",
		vcs.StatusUntracked:  "U",
		vcs.StatusDeleted:    "D",
		vcs.StatusConflicted: "C",
		vcs.StatusNone:       "",
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
