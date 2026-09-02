package deeplink

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// gitProject fakes a checkout: dir/.git/config with the given remote URLs.
func gitProject(t *testing.T, dir string, remotes ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[core]\n\trepositoryformatversion = 0\n"
	for i, r := range remotes {
		name := "origin"
		if i > 0 {
			name = "fork"
		}
		cfg += "[remote \"" + name + "\"]\n\turl = " + r + "\n\tfetch = +refs/heads/*:refs/remotes/" + name + "/*\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// worktreeOf fakes a linked worktree of main: wt/.git is a file pointing at
// main/.git/worktrees/x, whose commondir points back at main/.git.
func worktreeOf(t *testing.T, wt, main string) {
	t.Helper()
	gd := filepath.Join(main, ".git", "worktrees", "x")
	if err := os.MkdirAll(gd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gd, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gd+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRemotesReadsConfig(t *testing.T) {
	dir := t.TempDir()
	gitProject(t, dir, "git@github.com:A/B.git", "https://github.com/c/d")
	got := Remotes(dir)
	want := []string{"github.com/a/b", "github.com/c/d"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Remotes = %v, want %v", got, want)
	}
}

func TestRemotesWorktree(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main")
	gitProject(t, main, "git@github.com:A/B.git")
	wt := filepath.Join(base, "wt")
	worktreeOf(t, wt, main)
	got := Remotes(wt)
	if len(got) != 1 || got[0] != "github.com/a/b" {
		t.Errorf("worktree Remotes = %v", got)
	}
}

func TestRemotesNonRepo(t *testing.T) {
	if got := Remotes(t.TempDir()); got != nil {
		t.Errorf("Remotes on a plain dir = %v, want nil", got)
	}
}

func mustLink(t *testing.T, raw string) Link {
	t.Helper()
	l, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestResolveHistoryHit(t *testing.T) {
	dir := t.TempDir()
	gitProject(t, dir, "git@github.com:A/B.git")
	history := []Candidate{{Path: dir, Name: "b", Remotes: []string{"github.com/a/b"}}}
	res := Resolve(mustLink(t, "ike://open?remote=https://github.com/a/b"), history, "")
	if res.Kind != KindSwitch || res.Path != dir {
		t.Errorf("got %+v", res)
	}
}

func TestResolveHistoryLazyRemotes(t *testing.T) {
	// A pre-#2396 entry has no recorded remotes: the resolver reads the
	// checkout live.
	dir := t.TempDir()
	gitProject(t, dir, "git@github.com:A/B.git")
	history := []Candidate{{Path: dir, Name: "b"}}
	res := Resolve(mustLink(t, "ike://open?remote=https://github.com/a/b"), history, "")
	if res.Kind != KindSwitch || res.Path != dir {
		t.Errorf("got %+v", res)
	}
}

func TestResolveSkipsStaleEntries(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	history := []Candidate{{Path: gone, Name: "ike", Remotes: []string{"github.com/a/b"}}}
	res := Resolve(mustLink(t, "ike://open?remote=https://github.com/a/b"), history, "")
	if res.Kind != KindClone {
		t.Errorf("stale entry must not match: %+v", res)
	}
}

func TestResolveMultipleHitsChooser(t *testing.T) {
	older, newer := t.TempDir(), t.TempDir()
	gitProject(t, older, "git@github.com:A/B.git")
	gitProject(t, newer, "git@github.com:A/B.git")
	history := []Candidate{
		{Path: older, LastOpened: time.Now().Add(-time.Hour), Remotes: []string{"github.com/a/b"}},
		{Path: newer, LastOpened: time.Now(), Remotes: []string{"github.com/a/b"}},
	}
	res := Resolve(mustLink(t, "ike://open?remote=https://github.com/a/b"), history, "")
	if res.Kind != KindChoose || len(res.Choices) != 2 {
		t.Fatalf("got %+v", res)
	}
	if res.Choices[0].Path != newer {
		t.Errorf("default must be the most recently opened, got %q", res.Choices[0].Path)
	}
}

func TestResolveProjectsDirScan(t *testing.T) {
	projects := t.TempDir()
	hit := filepath.Join(projects, "ike")
	gitProject(t, hit, "git@github.com:A/B.git")
	gitProject(t, filepath.Join(projects, "other"), "git@github.com:x/y.git")

	res := Resolve(mustLink(t, "ike://open?remote=https://github.com/a/b"), nil, projects)
	if res.Kind != KindSwitch || res.Path != hit {
		t.Errorf("remote scan: got %+v", res)
	}
	res = Resolve(mustLink(t, "ike://open?project=IKE"), nil, projects)
	if res.Kind != KindSwitch || res.Path != hit {
		t.Errorf("name scan (case-insensitive): got %+v", res)
	}
}

func TestResolveCloneAndNotFound(t *testing.T) {
	res := Resolve(mustLink(t, "ike://open?remote=https://github.com/no/where"), nil, "")
	if res.Kind != KindClone {
		t.Errorf("remote miss must offer clone: %+v", res)
	}
	res = Resolve(mustLink(t, "ike://open?project=nowhere"), nil, "")
	if res.Kind != KindNotFound {
		t.Errorf("project miss has nothing to clone: %+v", res)
	}
}

func TestResolveHistoryBeatsScan(t *testing.T) {
	projects := t.TempDir()
	scanHit := filepath.Join(projects, "b")
	gitProject(t, scanHit, "git@github.com:A/B.git")
	histHit := t.TempDir()
	gitProject(t, histHit, "git@github.com:A/B.git")
	history := []Candidate{{Path: histHit, Remotes: []string{"github.com/a/b"}}}
	res := Resolve(mustLink(t, "ike://open?remote=https://github.com/a/b"), history, projects)
	if res.Kind != KindSwitch || res.Path != histHit {
		t.Errorf("history must win: %+v", res)
	}
}
