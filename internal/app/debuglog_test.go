package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// TestSlowUpdateLogsEntry guards #125: a slow Update pass leaves a
// timestamped entry naming the message type and duration in the state log.
func TestSlowUpdateLogsEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	logSlowUpdate(tea.KeyPressMsg{}, 350*time.Millisecond)
	data, err := os.ReadFile(filepath.Join(dir, "debug.log"))
	if err != nil {
		t.Fatal(err)
	}
	line := string(data)
	if !strings.Contains(line, "slow update: tea.KeyPressMsg took 350ms") {
		t.Fatalf("entry missing type/duration: %q", line)
	}
	if !strings.Contains(line, time.Now().Format("2006-01-02")) {
		t.Fatalf("entry missing timestamp: %q", line)
	}
}

// TestFastUpdateDoesNotLog: a normal Update pass writes nothing.
func TestFastUpdateDoesNotLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	m := newSized() // rotates IKE_CONFIG_DIR itself; re-pin for the assertion
	t.Setenv("IKE_CONFIG_DIR", dir)
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	_ = tm
	if _, err := os.Stat(filepath.Join(dir, "debug.log")); !os.IsNotExist(err) {
		t.Fatal("a fast Update pass must not log")
	}
}

// TestDebugSessionLog verifies debuggee output is appended verbatim to
// debug-session.log, with stderr chunks prefixed (#624).
func TestDebugSessionLog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	logDebugOutput(false, "hello world\n")
	logDebugOutput(true, "a warning\n")
	data, err := os.ReadFile(filepath.Join(dir, "debug-session.log"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if got != "hello world\n[stderr] a warning\n" {
		t.Fatalf("session log = %q", got)
	}
}
