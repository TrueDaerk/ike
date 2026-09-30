package crashlog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetDir(dir)
	ResetForTest()
	SetRecent(nil)
	focus.Store(nil)
	t.Cleanup(func() {
		SetDir("")
		ResetForTest()
		SetRecent(nil)
		focus.Store(nil)
	})
	return dir
}

// TestWriteReport: the report carries the version, the panic value, both
// stack dumps, the published focus, the extra context and the recent
// telemetry events, under the crash-<timestamp>-<session>.log name.
func TestWriteReport(t *testing.T) {
	dir := setup(t)
	SetSession("abc123")
	SetFocus("editor[python]", "editor", "/tmp/x.py")
	SetRecent(func() []string { return []string{"key cmd+f editor.find", "cmd project.findInPath"} })
	path, err := Write(Report{
		Where: "update",
		Value: errors.New("boom"),
		Stack: StackHere(),
		Msg:   "tea.KeyPressMsg",
		Extra: map[string]string{"stderr": "line1\nline2\n", "note": "x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir || !strings.HasPrefix(filepath.Base(path), "crash-") || !strings.HasSuffix(path, "-abc123.log") {
		t.Fatalf("path = %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"version:  ", "panic:    boom", "where:    update", "message:  tea.KeyPressMsg",
		"context:  editor[python]", "pane:     editor /tmp/x.py",
		"--- stderr ---\nline1\nline2", "note:     x",
		"--- last 2 telemetry events", "key cmd+f editor.find",
		"--- panicking goroutine ---", "TestWriteReport",
		"--- all goroutines ---", "goroutine ",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("report lacks %q:\n%s", want, s)
		}
	}
	if Last() != path {
		t.Errorf("Last() = %q, want %q", Last(), path)
	}
	if Latest() != path {
		t.Errorf("Latest() = %q, want %q", Latest(), path)
	}
}

// TestPruneKeepsTwenty: the directory never holds more than KeepFiles logs.
func TestPruneKeepsTwenty(t *testing.T) {
	dir := setup(t)
	for i := 0; i < KeepFiles+5; i++ {
		name := fmt.Sprintf("crash-2026010%dT000000.%03dZ-old.log", i/10, i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Write(Report{Where: "test", Value: "new"}); err != nil {
		t.Fatal(err)
	}
	logs := List(dir)
	if len(logs) != KeepFiles {
		t.Fatalf("kept %d logs, want %d", len(logs), KeepFiles)
	}
	if !strings.Contains(logs[len(logs)-1], time.Now().UTC().Format("2006")) {
		t.Errorf("the newest log must survive the prune, got %v", logs)
	}
}

// TestThrottleSameReason: the same panic message is written at most
// maxPerReason times per process; later repeats are counted into the next
// distinct report.
func TestThrottleSameReason(t *testing.T) {
	dir := setup(t)
	for i := 0; i < maxPerReason+4; i++ {
		if _, err := Write(Report{Where: "view", Value: "same"}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(List(dir)); got != maxPerReason {
		t.Fatalf("wrote %d reports for one reason, want %d", got, maxPerReason)
	}
	path, err := Write(Report{Where: "view", Value: "other"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "skipped:  4 earlier repeats") {
		t.Errorf("the next report must count the skipped repeats:\n%s", body)
	}
}

// TestUnacknowledged: a fresh crash log is reported until acknowledged; a
// newer one re-arms the notice.
func TestUnacknowledged(t *testing.T) {
	setup(t)
	if got := Unacknowledged(); got != "" {
		t.Fatalf("empty dir: Unacknowledged = %q", got)
	}
	first, _ := Write(Report{Where: "cmd", Value: "one"})
	if got := Unacknowledged(); got != first {
		t.Fatalf("Unacknowledged = %q, want %q", got, first)
	}
	Acknowledge(first)
	if got := Unacknowledged(); got != "" {
		t.Fatalf("after ack: Unacknowledged = %q", got)
	}
	time.Sleep(2 * time.Millisecond) // a later timestamp in the name
	second, _ := Write(Report{Where: "cmd", Value: "two"})
	if got := Unacknowledged(); got != second {
		t.Fatalf("new crash: Unacknowledged = %q, want %q", got, second)
	}
}

// TestDirDiscovery: IKE_CONFIG_DIR overrides the home directory.
func TestDirDiscovery(t *testing.T) {
	SetDir("")
	t.Setenv("IKE_CONFIG_DIR", "/tmp/ike-cfg")
	if got := Dir(); got != filepath.Join("/tmp/ike-cfg", "logs") {
		t.Fatalf("Dir = %q", got)
	}
}

func TestSummary(t *testing.T) {
	if got := Summary("a\nb"); got != "a b" {
		t.Errorf("Summary newline = %q", got)
	}
	if got := Summary(strings.Repeat("x", 200)); len(got) != 120 || !strings.HasSuffix(got, "...") {
		t.Errorf("Summary long = %q", got)
	}
}
