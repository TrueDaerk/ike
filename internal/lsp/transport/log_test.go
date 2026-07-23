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

// TestMarkersStartOnFreshLine (#990): an unterminated final stderr write must
// not swallow the exit footer, and the next start's header must not glue onto
// a previous run's unterminated tail.
func TestMarkersStartOnFreshLine(t *testing.T) {
	log := filepath.Join(t.TempDir(), "lsp-sh.log")
	p, err := Start(Spec{
		Command: "sh",
		Args:    []string{"-c", `printf 'tail-without-newline' >&2; exit 3`},
		LogPath: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.WaitErr()
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "tail-without-newline\n") {
		t.Fatalf("stderr tail must be newline-terminated before the footer:\n%s", data)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "--- exited") && strings.Contains(line, "tail-without-newline") {
			t.Fatalf("footer glued onto the stderr tail: %q", line)
		}
	}

	// Second start appends its header to the same file — also on a fresh line.
	p2, err := Start(Spec{Command: "sh", Args: []string{"-c", "true"}, LogPath: log})
	if err != nil {
		t.Fatal(err)
	}
	_ = p2.WaitErr()
	data, _ = os.ReadFile(log)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "--- started") && !strings.HasPrefix(line, "20") {
			t.Fatalf("header must start the line: %q", line)
		}
	}
}

// TestHugeStderrLineStillLogsError (#990): a crash whose stderr starts with a
// huge single line (Node dumping the minified source line) must still land
// the error message and stack in the log.
func TestHugeStderrLineStillLogsError(t *testing.T) {
	log := filepath.Join(t.TempDir(), "lsp-sh.log")
	p, err := Start(Spec{
		Command: "sh",
		Args: []string{"-c",
			`head -c 300000 /dev/zero | tr '\0' 'x' >&2; printf '\nSyntaxError: Unexpected token\n    at wrapSafe (loader:1281:20)\n' >&2; exit 1`},
		LogPath: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.WaitErr()
	data, _ := os.ReadFile(log)
	for _, want := range []string{"SyntaxError: Unexpected token", "at wrapSafe", "--- exited: exit status 1"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log missing %q (len=%d)", want, len(data))
		}
	}
	// And the ring-buffer tail yields the decisive line for the status toast.
	if got := ErrorLine(p.Stderr()); got != "SyntaxError: Unexpected token" {
		t.Errorf("ErrorLine = %q", got)
	}
}
