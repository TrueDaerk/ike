package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commitTraced makes one commit in a fresh repository under env and returns
// git's trace2 event log of it — the spawned children included, since they
// inherit GIT_TRACE2_EVENT.
func commitTraced(t *testing.T, env []string) string {
	t.Helper()
	dir := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace2.json")
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(env,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TRACE2_EVENT="+trace)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "base")
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// withoutGitConfigEnv is the process environment minus the GIT_CONFIG_*
// entries DisableAutoMaintenance sets.
func withoutGitConfigEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_CONFIG_") {
			env = append(env, kv)
		}
	}
	return env
}

// TestDisableAutoMaintenanceSpawnsNoDaemon: a commit under the environment
// runs no `git maintenance` child at all — where the same git would without
// it (a git older than 2.29 runs none either way and cannot tell).
func TestDisableAutoMaintenanceSpawnsNoDaemon(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if !strings.Contains(commitTraced(t, withoutGitConfigEnv()), `"maintenance"`) {
		t.Skip("this git runs no maintenance after a commit")
	}
	DisableAutoMaintenance()
	if got := commitTraced(t, os.Environ()); strings.Contains(got, `"maintenance"`) {
		t.Fatalf("commit still spawned git maintenance:\n%s", got)
	}
}
