package editor

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/watch"
)

// changed simulates the watcher reporting an external content change of path.
func changed(m Model, path string) (Model, bool) {
	before := m.docVersion
	m, _ = m.Update(watch.EventMsg{Kind: watch.FileChanged, Path: path})
	return m, m.docVersion != before
}

func TestExternalChangeReloadsCleanBuffer(t *testing.T) {
	m, path := loaded(t, "one\ntwo\n")
	if err := os.WriteFile(path, []byte("one\nTWO\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, bumped := changed(m, path)
	if got := m.buf.String(); !strings.Contains(got, "TWO") || !strings.Contains(got, "three") {
		t.Fatalf("buffer not reloaded: %q", got)
	}
	if m.Dirty() {
		t.Fatal("reloaded buffer must be clean")
	}
	if !bumped {
		t.Fatal("reload must bump docVersion so highlighting/LSP re-sync")
	}
}

func TestExternalChangePreservesCursorAndScroll(t *testing.T) {
	long := strings.Repeat("line\n", 40)
	m, path := loaded(t, long)
	m.SetCursor(30, 2)
	top, _ := m.ScrollOffset()
	if top == 0 {
		t.Fatal("test setup: cursor move should have scrolled")
	}
	if err := os.WriteFile(path, []byte(long+"tail\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = changed(m, path)
	if l, c := m.CursorPos(); l != 30 || c != 2 {
		t.Fatalf("cursor moved: %d,%d want 30,2", l, c)
	}
	if newTop, _ := m.ScrollOffset(); newTop != top {
		t.Fatalf("scroll moved: top=%d want %d", newTop, top)
	}
}

func TestExternalChangeClampsCursorToShorterFile(t *testing.T) {
	m, path := loaded(t, strings.Repeat("line\n", 40))
	m.SetCursor(35, 2)
	if err := os.WriteFile(path, []byte("only\nfour\nlines\nleft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = changed(m, path)
	if l, _ := m.CursorPos(); l != 3 {
		t.Fatalf("cursor line=%d, want clamp to last line 3", l)
	}
	if top, _ := m.ScrollOffset(); top > 3 {
		t.Fatalf("scroll top=%d beyond the shrunk buffer", top)
	}
}

func TestExternalChangeSkipsDirtyBuffer(t *testing.T) {
	m, path := loaded(t, "one\ntwo\n")
	m = send(m, key('i'), key('X'), special(tea.KeyEscape)) // dirty the buffer
	if !m.Dirty() {
		t.Fatal("test setup: buffer should be dirty")
	}
	if err := os.WriteFile(path, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = changed(m, path)
	if !strings.Contains(m.buf.String(), "Xone") {
		t.Fatalf("dirty buffer was reloaded: %q", m.buf.String())
	}
	if !m.Dirty() {
		t.Fatal("dirty flag must survive an external change")
	}
}

func TestExternalChangeRespectsAutoReloadNever(t *testing.T) {
	m, path := loaded(t, "one\n")
	m.Configure(host.MapConfig{"files.auto_reload": "never"})
	if err := os.WriteFile(path, []byte("external\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, bumped := changed(m, path)
	if strings.Contains(m.buf.String(), "external") {
		t.Fatal("auto_reload=never must not reload")
	}
	if bumped {
		t.Fatal("no reload means no docVersion bump")
	}
}

func TestExternalChangeIgnoresOtherPaths(t *testing.T) {
	m, _ := loaded(t, "one\n")
	m, bumped := changed(m, "/somewhere/else.txt")
	if bumped || strings.TrimRight(m.buf.String(), "\n") != "one" {
		t.Fatalf("event for another path must be a no-op: %q", m.buf.String())
	}
}

func TestExternalChangeResetsUndoHistory(t *testing.T) {
	m, path := loaded(t, "one\ntwo\n")
	m = send(m, key('d'), key('d'))  // an undoable edit...
	if err := m.save(); err != nil { // ...saved, so the buffer is clean again
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("fresh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = changed(m, path)
	m = send(m, key('u')) // undo across the reload must be a no-op
	if got := strings.TrimRight(m.buf.String(), "\n"); got != "fresh" {
		t.Fatalf("undo reached across the reload: %q", got)
	}
}

func TestReloadIdenticalContentKeepsUndoHistory(t *testing.T) {
	// An own write whose watcher event slipped past suppression (#1406): the
	// event fires, but disk equals the buffer — nothing may be reset.
	m, path := loaded(t, "one\ntwo\n")
	m = send(m, key('d'), key('d'))
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	m, bumped := changed(m, path)
	if bumped {
		t.Fatal("identical content must not reload (no docVersion bump)")
	}
	m = send(m, key('u')) // undo must still reach past the save
	if got := strings.TrimRight(m.buf.String(), "\n"); got != "one\ntwo" {
		t.Fatalf("undo after save lost: %q, want %q", got, "one\ntwo")
	}
	m = send(m, modKey('r', tea.ModCtrl)) // redo back to the saved state
	if got := strings.TrimRight(m.buf.String(), "\n"); got != "two" {
		t.Fatalf("redo after undo-past-save broken: %q, want %q", got, "two")
	}
	if m.Dirty() {
		t.Fatal("redo landed back on the saved state, buffer must be clean")
	}
}

func TestUndoSurvivesChainedSave(t *testing.T) {
	// The format-on-save chain writes via CompleteChainedSave (#1148); a
	// leaked own-write event after it must not cost the history either.
	var calls []providerCall
	withProvider(t, &calls)
	m, path := dirtyChained(t, "one\n")
	tm, _ := m.Update(ActionMsg{Action: "write"})
	m = tm
	m.CompleteChainedSave()
	if m.Dirty() {
		t.Fatal("test setup: chained save must have written")
	}
	m, bumped := changed(m, path)
	if bumped {
		t.Fatal("identical content after chained save must not reload")
	}
	m = send(m, key('u'))
	if got := strings.TrimRight(m.buf.String(), "\n"); got != "one" {
		t.Fatalf("undo after chained save lost: %q, want %q", got, "one")
	}
}

func TestExternalChangeEmitsChangeEvent(t *testing.T) {
	m, path := loaded(t, "one\n")
	var got []Event
	m.SetEmitter(EmitterFunc(func(e Event) { got = append(got, e) }))
	if err := os.WriteFile(path, []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = changed(m, path)
	var change *Event
	for i := range got {
		if got[i].Kind == EventChange {
			change = &got[i]
		}
	}
	if change == nil {
		t.Fatal("reload must emit EventChange so LSP re-syncs")
	}
	if !strings.Contains(change.Text, "new content") {
		t.Fatalf("EventChange must carry the reloaded text, got %q", change.Text)
	}
}
