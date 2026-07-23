package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// z joins porcelain v2 records with NUL, the way `git status -z` emits them.
func z(records ...string) []byte {
	return []byte(strings.Join(records, "\x00") + "\x00")
}

func TestParseStatusBranchAndFiles(t *testing.T) {
	out := z(
		"# branch.oid 1234567890abcdef1234567890abcdef12345678",
		"# branch.head main",
		"# branch.ab +2 -1",
		"1 .M N... 100644 100644 100644 aaaa bbbb internal/app/app.go",
		"1 A. N... 000000 100644 100644 0000 cccc internal/vcs/vcs.go",
		"1 .D N... 100644 100644 000000 dddd 0000 docs/old.md",
		"? notes.txt",
	)
	s := parseStatus(out)
	if s.Branch != "main" || s.Detached {
		t.Fatalf("branch = %q detached=%v", s.Branch, s.Detached)
	}
	if s.Ahead != 2 || s.Behind != 1 {
		t.Fatalf("ahead/behind = %d/%d", s.Ahead, s.Behind)
	}
	want := map[string]FileStatus{
		"internal/app/app.go": StatusModified,
		"internal/vcs/vcs.go": StatusAdded,
		"docs/old.md":         StatusDeleted,
		"notes.txt":           StatusUntracked,
	}
	for p, st := range want {
		if got := s.Files[p]; got != st {
			t.Errorf("Files[%q] = %v, want %v", p, got, st)
		}
	}
	if len(s.Files) != len(want) {
		t.Errorf("got %d files, want %d", len(s.Files), len(want))
	}
}

func TestParseStatusRenameConsumesOrigPath(t *testing.T) {
	out := z(
		"# branch.head main",
		"2 R. N... 100644 100644 100644 aaaa aaaa R100 new/name.go",
		"old/name.go",
		"1 .M N... 100644 100644 100644 aaaa bbbb other.go",
	)
	s := parseStatus(out)
	if got := s.Files["new/name.go"]; got != StatusRenamed {
		t.Errorf("renamed file = %v", got)
	}
	if _, ok := s.Files["old/name.go"]; ok {
		t.Errorf("orig path leaked into Files")
	}
	if got := s.Files["other.go"]; got != StatusModified {
		t.Errorf("entry after rename = %v", got)
	}
}

func TestParseStatusUnmergedAndDetached(t *testing.T) {
	out := z(
		"# branch.oid 1234567890abcdef1234567890abcdef12345678",
		"# branch.head (detached)",
		"u UU N... 100644 100644 100644 100644 a b c conflicted.go",
	)
	s := parseStatus(out)
	if !s.Detached || s.Branch != "1234567" {
		t.Fatalf("detached=%v branch=%q", s.Detached, s.Branch)
	}
	if got := s.Files["conflicted.go"]; got != StatusConflicted {
		t.Errorf("conflicted = %v", got)
	}
}

func TestParseStatusPathWithSpaces(t *testing.T) {
	out := z("1 .M N... 100644 100644 100644 aaaa bbbb dir with space/my file.go")
	s := parseStatus(out)
	if got := s.Files["dir with space/my file.go"]; got != StatusModified {
		t.Errorf("spaced path = %v", got)
	}
}

func TestSnapshotStatusAndDirDirty(t *testing.T) {
	s := parseStatus(z("1 .M N... 100644 100644 100644 aaaa bbbb internal/app/app.go"))
	s.Root = filepath.FromSlash("/work/repo")

	if got := s.Status("internal/app/app.go"); got != StatusModified {
		t.Errorf("rel status = %v", got)
	}
	if got := s.Status(filepath.FromSlash("/work/repo/internal/app/app.go")); got != StatusModified {
		t.Errorf("abs status = %v", got)
	}
	if got := s.Status(filepath.FromSlash("/elsewhere/app.go")); got != StatusNone {
		t.Errorf("outside repo = %v", got)
	}
	if got := s.Status("internal/app/clean.go"); got != StatusNone {
		t.Errorf("clean file = %v", got)
	}

	for _, dir := range []string{"internal", "internal/app", ""} {
		if !s.DirDirty(dir) {
			t.Errorf("DirDirty(%q) = false", dir)
		}
	}
	if !s.DirDirty(filepath.FromSlash("/work/repo/internal")) {
		t.Errorf("abs DirDirty = false")
	}
	if s.DirDirty("docs") {
		t.Errorf("clean dir reported dirty")
	}
}

func TestNilSnapshotIsClean(t *testing.T) {
	var s *Snapshot
	if s.Status("x.go") != StatusNone || s.DirDirty("x") {
		t.Fatal("nil snapshot must be clean")
	}
}

func TestStatusBadges(t *testing.T) {
	want := map[FileStatus]string{
		StatusNone: "", StatusModified: "M", StatusAdded: "A", StatusDeleted: "D",
		StatusRenamed: "R", StatusUntracked: "?", StatusConflicted: "U",
	}
	for st, badge := range want {
		if st.String() != badge {
			t.Errorf("%d.String() = %q, want %q", st, st.String(), badge)
		}
	}
}

// TestLoadRealRepo exercises DetectRoot/Load against a throwaway repository;
// skipped when git is not on PATH.
func TestLoadRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "--allow-empty", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// An ignored file plus an ignored-only directory: Load's --ignored flag
	// must surface both through Ignored (#1045).
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"debug.log", filepath.Join("logs", "a.log")} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	root, err := DetectRoot(dir)
	if err != nil {
		t.Fatalf("DetectRoot: %v", err)
	}
	snap, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap.Root != root {
		t.Errorf("root %q != %q", snap.Root, root)
	}
	if snap.Branch != "main" {
		t.Errorf("branch = %q", snap.Branch)
	}
	if got := snap.Status("new.txt"); got != StatusUntracked {
		t.Errorf("new.txt = %v", got)
	}
	for _, p := range []string{"debug.log", "logs", "logs/a.log", filepath.Join(dir, "debug.log")} {
		if !snap.Ignored(p) {
			t.Errorf("Ignored(%q) = false, want true", p)
		}
	}
	if snap.Ignored("new.txt") || snap.Status("debug.log") != StatusNone {
		t.Error("ignored/untracked classification crossed over")
	}

	// Not a repo: Load must fail, Refresh must resolve to a nil snapshot.
	outside := t.TempDir()
	if _, err := Load(outside); err == nil {
		t.Error("Load outside a repo did not fail")
	}
	if msg, ok := Refresh(outside)().(SnapshotMsg); !ok || msg.Snap != nil {
		t.Errorf("Refresh outside a repo = %#v", msg)
	}
}

// TestParseStatusIgnored covers `--ignored` records (#1045): "! <path>" file
// entries and collapsed "dir/" entries land in the ignored set — never in
// Files/Entries — and Ignored answers exact matches, dir-prefix matches and
// non-matches.
func TestParseStatusIgnored(t *testing.T) {
	out := z(
		"# branch.head main",
		"? keep.txt",
		"! x.log",
		"! build/",
	)
	s := parseStatus(out)
	if len(s.Files) != 1 || s.Files["keep.txt"] != StatusUntracked {
		t.Fatalf("Files = %v; ignored entries must not become changes", s.Files)
	}
	if len(s.Entries) != 1 {
		t.Fatalf("Entries = %v; ignored entries must not reach the commit UI", s.Entries)
	}
	for _, p := range []string{"x.log", "build", "build/a.o", "build/deep/b.o"} {
		if !s.Ignored(p) {
			t.Errorf("Ignored(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"keep.txt", "buildx/a.o", "", "src/x.log"} {
		if s.Ignored(p) {
			t.Errorf("Ignored(%q) = true, want false", p)
		}
	}
}

// TestSnapshotIgnoredPathForms covers Ignored's relPath normalization
// (#1045): absolute paths inside the repo resolve, paths outside report
// false, and a nil snapshot is a clean no-op.
func TestSnapshotIgnoredPathForms(t *testing.T) {
	root := filepath.FromSlash("/work/repo")
	s := NewSnapshot(root, nil)
	s.AddIgnored("x.log", "build/")

	if !s.Ignored(filepath.Join(root, "x.log")) {
		t.Error("absolute ignored file not recognised")
	}
	if !s.Ignored(filepath.Join(root, "build", "obj", "a.o")) {
		t.Error("absolute path under ignored dir not recognised")
	}
	if s.Ignored(filepath.Join(root, "keep.txt")) {
		t.Error("clean absolute path reported ignored")
	}
	if s.Ignored(filepath.FromSlash("/somewhere/else/x.log")) {
		t.Error("path outside the repo reported ignored")
	}

	var nilSnap *Snapshot
	if nilSnap.Ignored("x.log") {
		t.Error("nil snapshot reported ignored")
	}
}

// TestDirStatusDominance guards #1053: a directory reports the strongest
// status in its subtree, so untracked-only dirs stop reading as modified.
func TestDirStatusDominance(t *testing.T) {
	s := NewSnapshot("/repo", map[string]FileStatus{
		"only/new.txt":    StatusUntracked,
		"mixed/new.txt":   StatusUntracked,
		"mixed/edit.go":   StatusModified,
		"staged/fresh.go": StatusAdded,
	})
	cases := map[string]FileStatus{
		"only":   StatusUntracked,
		"mixed":  StatusModified,
		"staged": StatusAdded,
		"":       StatusModified, // root: strongest across the tree
		"clean":  StatusNone,
	}
	for dir, want := range cases {
		if got := s.DirStatus(dir); got != want {
			t.Errorf("DirStatus(%q) = %v want %v", dir, got, want)
		}
	}
	if !s.DirDirty("only") || s.DirDirty("clean") {
		t.Error("DirDirty must mirror DirStatus != none")
	}
}
