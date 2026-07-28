package vcs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestCloneName covers the default directory name derived from the URL forms
// users paste: https, ssh (scp-style), file and a trailing separator.
func TestCloneName(t *testing.T) {
	cases := map[string]string{
		"https://github.com/TrueDaerk/ike.git": "ike",
		"https://github.com/TrueDaerk/ike":     "ike",
		"https://github.com/TrueDaerk/ike/":    "ike",
		"git@github.com:TrueDaerk/ike.git":     "ike",
		"ssh://git@github.com/org/sub-repo":    "sub-repo",
		"file:///tmp/repos/thing.git":          "thing",
		"/tmp/repos/thing":                     "thing",
		"":                                     "",
		"   ":                                  "",
		"/":                                    "",
	}
	for url, want := range cases {
		if got := CloneName(url); got != want {
			t.Errorf("CloneName(%q) = %q, want %q", url, got, want)
		}
	}
}

// TestCloneCmdClonesLocalRepo drives a real clone of a local repository and
// checks the resulting message and working tree.
func TestCloneCmdClonesLocalRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := initRepo(t)
	dest := filepath.Join(t.TempDir(), "clone")

	msg, ok := CloneCmd(src, dest)().(CloneDoneMsg)
	if !ok {
		t.Fatal("CloneCmd did not resolve to a CloneDoneMsg")
	}
	if msg.Err != nil {
		t.Fatalf("clone failed: %v", msg.Err)
	}
	if msg.Dest != dest {
		t.Fatalf("Dest = %q, want %q", msg.Dest, dest)
	}
	if _, err := os.Stat(filepath.Join(dest, "file.txt")); err != nil {
		t.Fatalf("clone has no working tree: %v", err)
	}
}

// TestCloneCmdReportsFailureAndCleansUp verifies that a bad URL surfaces git's
// message and leaves no directory behind, so retrying the same name works.
func TestCloneCmdReportsFailureAndCleansUp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	missing := filepath.Join(t.TempDir(), "not-a-repo")
	dest := filepath.Join(t.TempDir(), "clone")

	msg := CloneCmd(missing, dest)().(CloneDoneMsg)
	if msg.Err == nil {
		t.Fatal("cloning a non-repository succeeded")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("failed clone left %s behind (%v)", dest, err)
	}
}

// TestCloneRejectsEmptyURL keeps the guard in front of the subprocess.
func TestCloneRejectsEmptyURL(t *testing.T) {
	if err := clone("  ", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("clone accepted an empty URL")
	}
}

// initRepo creates a repository with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "-qm", "initial")
	return dir
}
