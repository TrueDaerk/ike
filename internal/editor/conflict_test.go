package editor

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/watch"
)

// staleEditor builds the conflict precondition: a dirty buffer whose file was
// then changed externally, so the watcher event marked it stale.
func staleEditor(t *testing.T) (Model, string) {
	t.Helper()
	m, path := loaded(t, "one\ntwo\n")
	m = send(m, key('i'), key('X'), special(tea.KeyEscape))
	if err := os.WriteFile(path, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	return m, path
}

func TestExternalChangeMarksDirtyBufferStale(t *testing.T) {
	m, _ := staleEditor(t)
	if !m.Stale() {
		t.Fatal("dirty buffer + external change must mark stale")
	}
	if !strings.Contains(m.buf.String(), "Xone") {
		t.Fatalf("stale marking must not touch the buffer: %q", m.buf.String())
	}
}

func TestSaveWhileStaleYieldsConflictPrompt(t *testing.T) {
	m, path := staleEditor(t)
	m, cmd := m.Update(ActionMsg{Action: "write"})
	if cmd == nil {
		t.Fatal("saving a stale buffer must yield a command")
	}
	msg, ok := cmd().(ConflictMsg)
	if !ok || !samePath(msg.Path, path) {
		t.Fatalf("expected ConflictMsg for %q, got %#v", path, cmd())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "external\n" {
		t.Fatalf("guarded save must not write: disk=%q", data)
	}
	if !m.Stale() || !m.Dirty() {
		t.Fatal("an unanswered conflict leaves the buffer dirty and stale")
	}
}

func TestWriteQuitWhileStaleKeepsPaneOpen(t *testing.T) {
	m, _ := staleEditor(t)
	_, cmd := m.Update(ActionMsg{Action: "write_quit"})
	if cmd == nil {
		t.Fatal("expected a conflict command")
	}
	if _, isClose := cmd().(CloseMsg); isClose {
		t.Fatal(":wq on a stale buffer must prompt, not close")
	}
}

func TestSaveAsOtherPathBypassesConflict(t *testing.T) {
	m, path := staleEditor(t)
	other := path + ".copy"
	m = send(m, key(':'))
	for _, r := range "w " + other {
		m, _ = m.Update(key(r))
	}
	m, _ = m.Update(special(tea.KeyEnter))
	if _, err := os.Stat(other); err != nil {
		t.Fatalf(":w to another path must write despite staleness: %v", err)
	}
}

func TestKeepMineOverwritesAndClearsStale(t *testing.T) {
	m, path := staleEditor(t)
	m.ResolveConflictKeepMine()
	if m.Stale() || m.Dirty() {
		t.Fatal("keep-mine must clear stale and dirty")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Xone") {
		t.Fatalf("keep-mine must write the buffer over the external change: %q", data)
	}
}

func TestConflictReloadDiscardsEdits(t *testing.T) {
	m, _ := staleEditor(t)
	cmd := m.ResolveConflictReload()
	if got := strings.TrimRight(m.buf.String(), "\n"); got != "external" {
		t.Fatalf("reload must adopt the on-disk content, got %q", got)
	}
	if m.Stale() || m.Dirty() {
		t.Fatal("conflict reload must clear stale and dirty")
	}
	_ = cmd // reparse command; may be nil for plain text
}
