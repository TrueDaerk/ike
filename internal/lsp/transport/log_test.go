package transport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStderrTeesToLogFile(t *testing.T) {
	log := filepath.Join(t.TempDir(), "logs", "lsp-sh.log")
	p, err := Start(Spec{
		Command: "sh",
		Args:    []string{"-c", "echo boom >&2; exit 3"},
		LogPath: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.WaitErr() == nil {
		t.Fatal("exit 3 must surface as a wait error")
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("log file must exist: %v", err)
	}
	for _, want := range []string{"--- started: ", "boom", "--- exited: exit status 3"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log missing %q:\n%s", want, data)
		}
	}
	// The ring buffer keeps working alongside the tee.
	if !strings.Contains(p.Stderr(), "boom") {
		t.Errorf("ring buffer = %q", p.Stderr())
	}
}

func TestLogRotatesWhenOversized(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "lsp-sh.log")
	if err := os.WriteFile(log, make([]byte, logRotateBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := Start(Spec{Command: "sh", Args: []string{"-c", "true"}, LogPath: log})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.WaitErr()
	if _, err := os.Stat(log + ".old"); err != nil {
		t.Fatalf("oversized log must rotate to .old: %v", err)
	}
	if st, err := os.Stat(log); err != nil || st.Size() > logRotateBytes {
		t.Fatalf("fresh log must restart small: %v %v", st, err)
	}
}

func TestNoLogPathMeansNoFile(t *testing.T) {
	p, err := Start(Spec{Command: "sh", Args: []string{"-c", "true"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.WaitErr()
	if p.log != nil {
		t.Fatal("no LogPath must leave the process without a log file")
	}
}
