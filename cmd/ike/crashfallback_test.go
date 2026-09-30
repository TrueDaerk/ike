package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/crashlog"
)

// TestReportProgramEndWritesCrashLog (#2836): a Run that ends in bubbletea's
// ErrProgramPanic leaves a crash report holding the captured stderr — the
// value and stack bubbletea printed into the alternate screen — and the
// restored terminal gets the one line naming the file.
func TestReportProgramEndWritesCrashLog(t *testing.T) {
	dir := t.TempDir()
	crashlog.SetDir(dir)
	crashlog.ResetForTest()
	t.Cleanup(func() { crashlog.SetDir(""); crashlog.ResetForTest() })

	capture := captureStderr()
	if capture == nil {
		t.Skip("no pipe on this platform")
	}
	fmt.Fprint(os.Stderr, "Caught panic:\r\n\r\nruntime error: index out of range [3]\r\n")
	var term bytes.Buffer
	err := fmt.Errorf("%w: %w", tea.ErrProgramKilled, tea.ErrProgramPanic)
	if code := reportProgramEnd(err, capture, &term); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if os.Stderr != capture.orig {
		t.Fatal("stderr must be restored")
	}
	logs := crashlog.List(dir)
	if len(logs) != 1 {
		t.Fatalf("crash logs = %v, want one", logs)
	}
	if !strings.Contains(term.String(), "ike: crashed — crash log: "+logs[0]) {
		t.Fatalf("terminal line = %q", term.String())
	}
	body, _ := os.ReadFile(logs[0])
	for _, want := range []string{"where:    program", "index out of range [3]", "--- stderr ---", "--- all goroutines ---"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("report lacks %q:\n%s", want, body)
		}
	}
}

// TestReportProgramEndPlainError: any other Run error keeps the plain
// message and writes no report; a clean end exits zero.
func TestReportProgramEndPlainError(t *testing.T) {
	dir := t.TempDir()
	crashlog.SetDir(dir)
	t.Cleanup(func() { crashlog.SetDir("") })
	var term bytes.Buffer
	if code := reportProgramEnd(errors.New("tty gone"), nil, &term); code != 1 || term.String() != "ike: tty gone\n" {
		t.Fatalf("code=%d out=%q", code, term.String())
	}
	if code := reportProgramEnd(nil, nil, &term); code != 0 {
		t.Fatalf("clean end code = %d", code)
	}
	if got := crashlog.List(dir); len(got) != 0 {
		t.Fatalf("plain errors must not write crash logs, got %v", got)
	}
}

// TestCaptureStderrTees: what goes to os.Stderr while captured still reaches
// the original stream, and the tail is bounded.
func TestCaptureStderrTees(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = saved })
	capture := captureStderr()
	if capture == nil {
		t.Skip("no pipe on this platform")
	}
	got := make(chan []byte, 1)
	go func() { b, _ := readAll(r); got <- b }()
	big := strings.Repeat("x", stderrTailBytes) + "END"
	fmt.Fprint(os.Stderr, big)
	tail := capture.stop()
	_ = w.Close()
	forwarded := <-got
	if !strings.HasSuffix(tail, "END") || len(tail) != stderrTailBytes {
		t.Fatalf("tail len=%d suffix=%q", len(tail), tail[len(tail)-3:])
	}
	if !strings.HasSuffix(string(forwarded), "END") || len(forwarded) != len(big) {
		t.Fatalf("forwarded len=%d, want %d", len(forwarded), len(big))
	}
}

func readAll(f *os.File) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := f.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, nil
		}
	}
}
