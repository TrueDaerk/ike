package vcscore

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// churn_test.go covers the hotspot change frequency of #2957: Churn on a
// real repository with backdated commits, and the -z record parse.

// TestChurnRealRepo: three commits touching two files — one of them a year
// before the window — count per file; the window drops the old commit, a
// zero since counts all three, and a subdirectory root lists its own paths
// relative to itself.
func TestChurnRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(date string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if date != "" {
			cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	stamp := func(t time.Time) string { return t.Format(time.RFC3339) }
	run("", "init", "-q", "-b", "main")
	write("a.go", "package p\n")
	write("sub/b.go", "package p\n")
	run("", "add", ".")
	run(stamp(now.AddDate(-1, 0, 0)), "commit", "-q", "-m", "old")
	write("a.go", "package p\n\n// two\n")
	run("", "add", ".")
	run(stamp(now.AddDate(0, 0, -10)), "commit", "-q", "-m", "recent a")
	write("a.go", "package p\n\n// three\n")
	write("sub/b.go", "package p\n\n// two\n")
	run("", "add", ".")
	run(stamp(now.AddDate(0, 0, -2)), "commit", "-q", "-m", "recent both")

	got, err := Churn(dir, now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["a.go"] != 2 || got["sub/b.go"] != 1 {
		t.Fatalf("30-day churn = %v, want a.go:2 sub/b.go:1", got)
	}
	all, err := Churn(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if all["a.go"] != 3 || all["sub/b.go"] != 2 {
		t.Fatalf("whole-history churn = %v, want a.go:3 sub/b.go:2", all)
	}
	sub, err := Churn(filepath.Join(dir, "sub"), now.AddDate(0, 0, -30))
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 1 || sub["b.go"] != 1 {
		t.Fatalf("subdirectory churn = %v, want b.go:1 relative to the subdirectory", sub)
	}
	none, err := Churn(dir, now.AddDate(0, 0, -1))
	if err != nil || len(none) != 0 {
		t.Fatalf("a window past every commit = %v (%v), want empty", none, err)
	}
}

// TestChurnOutsideRepo: a directory git does not track is an error, so the
// callers can tell "no history" from "no repository".
func TestChurnOutsideRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if _, err := Churn(t.TempDir(), time.Now().AddDate(0, 0, -90)); err == nil {
		t.Fatal("Churn outside a repository must fail")
	}
}

func TestParseChurn(t *testing.T) {
	out := []byte("\na.go\x00b c.go\x00\x00\na.go\x00\n")
	got := parseChurn(out)
	if len(got) != 2 || got["a.go"] != 2 || got["b c.go"] != 1 {
		t.Fatalf("parseChurn = %v", got)
	}
}
