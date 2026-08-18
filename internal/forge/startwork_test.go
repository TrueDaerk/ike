package forge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// startwork_test.go drives the start-work flow against throwaway repos
// (#1934), the internal/vcs ops_test pattern: a bare "origin" plus a clone,
// so fetch and origin/HEAD behave like a real checkout.

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestStartWorkCreatesBranchFromOrigin(t *testing.T) {
	dir := setupClone(t)
	branch, warning, err := startWork(dir, 12, "project picker")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "issue/12-project-picker" {
		t.Fatalf("branch = %q", branch)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want none", warning)
	}
	if cur := strings.TrimSpace(gitIn(t, dir, "branch", "--show-current")); cur != branch {
		t.Fatalf("current branch = %q, want %q", cur, branch)
	}
}

func TestStartWorkRefusesDirtyWorktree(t *testing.T) {
	dir := setupClone(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := startWork(dir, 13, "x")
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("err = %v, want the dirty-worktree message", err)
	}
	if cur := strings.TrimSpace(gitIn(t, dir, "branch", "--show-current")); cur != "main" {
		t.Fatalf("a refused start must not switch branches (on %q)", cur)
	}
}

func TestStartWorkOfflineFallsBackToLocalDefault(t *testing.T) {
	dir := setupClone(t)
	// Point origin somewhere dead so the fetch fails like an offline run.
	gitIn(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	branch, warning, err := startWork(dir, 14, "offline case")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "issue/14-offline-case" {
		t.Fatalf("branch = %q", branch)
	}
	if !strings.Contains(warning, "branching from local main") {
		t.Fatalf("warning = %q", warning)
	}
	if cur := strings.TrimSpace(gitIn(t, dir, "branch", "--show-current")); cur != branch {
		t.Fatalf("current branch = %q", cur)
	}
}

func TestStartWorkExistingBranchErrors(t *testing.T) {
	dir := setupClone(t)
	gitIn(t, dir, "branch", "issue/15-taken")
	if _, _, err := startWork(dir, 15, "taken"); err == nil {
		t.Fatal("an existing branch must surface git's error")
	}
}

// setupClone builds origin (bare) + seed + clone and returns the clone path.
func setupClone(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	if err := os.Mkdir(seed, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "seed")
	origin := filepath.Join(base, "origin.git")
	gitIn(t, base, "clone", "--bare", seed, origin)
	clone := filepath.Join(base, "clone")
	gitIn(t, base, "clone", origin, clone)
	return clone
}
