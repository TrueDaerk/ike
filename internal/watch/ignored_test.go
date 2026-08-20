package watch

import (
	"path/filepath"
	"testing"
)

// TestIgnoredMatchesTheWatchWalk (#2000): the exported noise rule agrees with
// what the recursive walk would have descended into, judged below the root —
// a project living under a dotted directory is not wholesale ignored.
func TestIgnoredMatchesTheWatchWalk(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "home", "me", ".config", "proj")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(root, "main.go"), false},
		{filepath.Join(root, "internal", "app", "app.go"), false},
		{filepath.Join(root, ".gitignore"), false},  // a dot *file* is a real edit
		{filepath.Join(root, ".git", "HEAD"), true}, // repository metadata
		{filepath.Join(root, ".venv", "lib", "site-packages", "x.py"), true},
		{filepath.Join(root, "node_modules", "pkg", "index.js"), true},
		{filepath.Join(root, "web", "vendor", "dep.php"), true},
		{filepath.Join(root, "src", "__pycache__", "m.pyc"), true},
		{filepath.Join(string(filepath.Separator), "elsewhere", ".venv", "x.py"), false}, // outside the root
	}
	for _, tc := range cases {
		if got := Ignored(root, tc.path); got != tc.want {
			t.Errorf("Ignored(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestIgnoredWithoutRootJudgesEverySegment: a caller with no watch root yet
// still gets the vendored-noise rule over the whole path.
func TestIgnoredWithoutRootJudgesEverySegment(t *testing.T) {
	noise := filepath.Join(string(filepath.Separator), "p", "node_modules", "x.js")
	if !Ignored("", noise) {
		t.Errorf("Ignored(%q) = false, want true", noise)
	}
	clean := filepath.Join(string(filepath.Separator), "p", "src", "x.go")
	if Ignored("", clean) {
		t.Errorf("Ignored(%q) = true, want false", clean)
	}
}

// TestRootReportsTheWatchedDirectory: consumers filtering watcher-derived
// lists need the root Ignored is relative to.
func TestRootReportsTheWatchedDirectory(t *testing.T) {
	s := New(nil)
	if s.Root() != "" {
		t.Fatalf("Root = %q before Start, want empty", s.Root())
	}
	dir := t.TempDir()
	if err := s.Start(dir); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	abs, _ := filepath.Abs(dir)
	if s.Root() != abs {
		t.Fatalf("Root = %q, want %q", s.Root(), abs)
	}
}
