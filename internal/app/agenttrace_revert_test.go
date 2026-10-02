package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/tracepanel"
)

// agenttrace_revert_test.go covers V in the agent trace without a feed link
// (#2877): an edit reverts from the transcript's diff after the
// confirmation, a conflict leaves the file alone with a notice, and rows
// with nothing to revert say why.

// runTraceRevert presses V on the trace row keyed key and runs the
// off-loop reconstruction it may start.
func runTraceRevert(t *testing.T, m Model, key string) Model {
	t.Helper()
	p := m.agentTracePanel()
	if !p.Select(key) {
		t.Fatalf("select %q failed", key)
	}
	msg, ok := p.Update(tea.KeyPressMsg{Code: 'V', Text: "V"})().(tracepanel.ChangeRevertMsg)
	if !ok || msg.Key != key {
		t.Fatalf("V on %q = %#v", key, msg)
	}
	out, cmd := m.Update(msg)
	m = out.(Model)
	if cmd != nil {
		out, _ = m.Update(cmd())
		m = out.(Model)
	}
	return m
}

func TestTraceRevertFromTranscript(t *testing.T) {
	m, dir := traceApp(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	root := t.TempDir()
	main := filepath.Join(root, "main.go")
	if err := os.WriteFile(main, []byte("a\nb\nd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "new.go")
	if err := os.WriteFile(created, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The patch-only edit turned 1 into 2; the file moved on since.
	other := filepath.Join(root, "other.go")
	if err := os.WriteFile(other, []byte("9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(diffTranscript(t, "sess-1", projectRoot(), main, created, other)), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	if !m.agentTracePanel().HasSession() {
		t.Fatalf("no session:\n%s", m.agentTracePanel().View())
	}
	m.host.DrainNotifications()

	// An unlinked edit: the confirmation, then the buffer holds the
	// pre-change line, dirty, and the disk is untouched until the save.
	m = runTraceRevert(t, m, "e2/f0")
	if m.cfRevertTrace == nil || !m.changeFeedRevertOpen() {
		t.Fatalf("no revert prompt: %q", notices(m))
	}
	if body := m.shell.Content().Render(100); !strings.Contains(body, "from old/new string") || !strings.Contains(body, "[enter] revert") {
		t.Fatalf("prompt body:\n%s", body)
	}
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	ed := m.editorForPath(main)
	if ed == nil || strings.TrimSuffix(ed.Text(), "\n") != "a\nb\nc" || !ed.Dirty() {
		t.Fatalf("buffer after revert: open=%v %q notices=%q", ed != nil, func() string {
			if ed == nil {
				return ""
			}
			return ed.Text()
		}(), notices(m))
	}
	if data, _ := os.ReadFile(main); string(data) != "a\nb\nd\n" {
		t.Fatalf("the revert wrote the disk: %q", data)
	}
	if m.changeFeedRevertOpen() || m.cfRevertTrace != nil {
		t.Fatal("the prompt must close")
	}
	// Pressed again, the change is already gone from the buffer.
	m.host.DrainNotifications()
	m = runTraceRevert(t, m, "e2/f0")
	if m.cfRevertTrace != nil || !strings.Contains(strings.Join(notices(m), "\n"), "already holds the content from before") {
		t.Fatalf("second V: prompt=%v notices=%q", m.cfRevertTrace != nil, notices(m))
	}

	// A conflict: a notice, no prompt, the file untouched.
	m = runTraceRevert(t, m, "e4/f0")
	if m.cfRevertTrace != nil || !strings.Contains(strings.Join(notices(m), "\n"), "no longer contains the change") {
		t.Fatalf("conflict: prompt=%v notices=%q", m.cfRevertTrace != nil, notices(m))
	}
	if data, _ := os.ReadFile(other); string(data) != "9\n" || m.editorForPath(other) != nil {
		t.Fatalf("conflict touched the file: %q", data)
	}

	// Nothing to revert: a create, a read, a row that is no change.
	for key, want := range map[string]string{
		"e3/f0": "created the file",
		"e1/f0": "a read does not change the file",
		"t1":    "not a change",
	} {
		m = runTraceRevert(t, m, key)
		if got := strings.Join(notices(m), "\n"); m.cfRevertTrace != nil || !strings.Contains(got, "nothing to revert") || !strings.Contains(got, want) {
			t.Errorf("V on %s: prompt=%v notices=%q", key, m.cfRevertTrace != nil, got)
		}
	}
}

// TestTraceRevertFileGone: a change whose file was deleted has no snapshot
// to revert to.
func TestTraceRevertFileGone(t *testing.T) {
	m, dir := traceApp(t)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	root := t.TempDir()
	main := filepath.Join(root, "main.go")
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(diffTranscript(t, "sess-1", projectRoot(), main, filepath.Join(root, "new.go"), filepath.Join(root, "other.go"))), 0o644); err != nil {
		t.Fatal(err)
	}
	m = openTrace(t, m)
	m.host.DrainNotifications()
	m = runTraceRevert(t, m, "e2/f0")
	if got := strings.Join(notices(m), "\n"); m.cfRevertTrace != nil || !strings.Contains(got, "no longer exists") || !strings.Contains(got, "no snapshot to revert to") {
		t.Fatalf("file gone: prompt=%v notices=%q", m.cfRevertTrace != nil, got)
	}
}
