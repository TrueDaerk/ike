package explorer

import (
	"testing"

	"ike/internal/theme"
	"ike/internal/vcs"
)

// ignoredSnap builds a snapshot with the given gitignored entries for tests.
func ignoredSnap(root string, paths ...string) *vcs.Snapshot {
	s := vcs.NewSnapshot(root, nil)
	s.AddIgnored(paths...)
	return s
}

// TestNodeStyleIgnoredDimmed guards #1045: gitignored rows render in the
// foreground mixed toward the surface — uniformly dim, JetBrains-style —
// including paths under a collapsed ignored directory entry, while clean
// rows keep the plain foreground.
func TestNodeStyleIgnoredDimmed(t *testing.T) {
	m := New(".")
	m.SetVCS(ignoredSnap(".", "x.log", "build/"))
	pal := theme.DefaultPalette()
	dim := rgb(ignoredFg(pal))
	if dim == rgb(pal.Foreground) {
		t.Fatal("dimmed foreground must differ from the plain foreground")
	}

	for _, n := range []*node{
		{name: "x.log", path: "x.log"},
		{name: "build", path: "build", isDir: true},
		{name: "a.o", path: "build/a.o"},
	} {
		if got := rgb(m.nodeStyle(n).GetForeground()); got != dim {
			t.Errorf("%s: foreground = %v want dimmed ignored colour", n.path, got)
		}
	}
	clean := &node{name: "keep.go", path: "keep.go"}
	if got := rgb(m.nodeStyle(clean).GetForeground()); got != rgb(pal.Foreground) {
		t.Errorf("clean row foreground = %v want plain Foreground", got)
	}
}

// TestIgnoredRanksBelowVCSStatus guards #1045: a real VCS status always wins
// over the ignored dim — including the untracked hue — even if a snapshot
// ever carried both classifications for one path.
func TestIgnoredRanksBelowVCSStatus(t *testing.T) {
	m := New(".")
	snap := vcs.NewSnapshot(".", map[string]vcs.FileStatus{
		"new.txt": vcs.StatusUntracked,
	})
	snap.AddIgnored("new.txt")
	m.SetVCS(snap)

	pal := theme.DefaultPalette()
	n := &node{name: "new.txt", path: "new.txt"}
	want := vcs.StatusColor(pal, vcs.StatusUntracked)
	if got := rgb(m.nodeStyle(n).GetForeground()); got != rgb(want) {
		t.Errorf("foreground = %v want untracked hue, not ignored dim", got)
	}
}

// TestSuffixTintSkipsIgnoredRows guards #1045: ignored rows are uniformly
// dim — the filetype extension tint does not apply.
func TestSuffixTintSkipsIgnoredRows(t *testing.T) {
	m := New(".")
	m.SetVCS(ignoredSnap(".", "gen.go", "build/"))
	if m.suffixTint(&node{name: "gen.go", path: "gen.go"}) != nil {
		t.Error("ignored file must not carry a suffix tint")
	}
	if m.suffixTint(&node{name: "a.go", path: "build/a.go"}) != nil {
		t.Error("file under ignored dir must not carry a suffix tint")
	}
	if m.suffixTint(&node{name: "keep.go", path: "keep.go"}) == nil {
		t.Error("clean file must keep its suffix tint")
	}
}

// TestIgnoredComposesWithHiddenItalic guards #1045 + #1055: a dot-prefixed
// ignored entry stays italic on top of the dim foreground.
func TestIgnoredComposesWithHiddenItalic(t *testing.T) {
	m := New(".")
	m.SetVCS(ignoredSnap(".", ".cache/"))
	s := m.nodeStyle(&node{name: ".cache", path: ".cache", isDir: true})
	if !s.GetItalic() {
		t.Error("hidden ignored entry must stay italic")
	}
	if got := rgb(s.GetForeground()); got != rgb(ignoredFg(theme.DefaultPalette())) {
		t.Errorf("hidden ignored foreground = %v want dimmed", got)
	}
}
