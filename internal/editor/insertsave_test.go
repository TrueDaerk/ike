package editor

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestSaveInInsertModeStaysCleanAfterEsc guards #2188: a save issued while the
// insert session is still open must mark the state the file was written from,
// so leaving insert mode — which commits the session's remaining segment —
// does not resurrect the modified flag.
func TestSaveInInsertModeStaysCleanAfterEsc(t *testing.T) {
	m, path := loaded(t, "hello\n")
	m = typeKeys(m, "i")
	m = typeKeys(m, "abc")
	if !m.Dirty() {
		t.Fatal("typing in insert mode must dirty the buffer")
	}
	m, _ = m.Update(ActionMsg{Action: "write"})
	if m.Dirty() {
		t.Fatal("save must clear the dirty flag")
	}
	m = send(m, special(tea.KeyEsc))
	if m.Dirty() {
		t.Fatal("esc after an insert-mode save must leave the buffer clean (#2188)")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abchello\n" {
		t.Fatalf("disk = %q, want %q", got, "abchello\n")
	}
	if line(m, 0) != "abchello" {
		t.Fatalf("buffer = %q, want abchello", line(m, 0))
	}
}

// TestUndoAfterInsertModeSaveReachesSavedState guards the other half of #2188:
// with the saved state pinned to what was written, an edit made after the save
// dirties the buffer again and undoing it walks back to a clean saved state.
func TestUndoAfterInsertModeSaveReachesSavedState(t *testing.T) {
	m, _ := loaded(t, "hello\n")
	m = typeKeys(m, "i")
	m = typeKeys(m, "abc")
	m, _ = m.Update(ActionMsg{Action: "write"})
	m = typeKeys(m, "de") // typing on after the save opens the next segment
	if !m.Dirty() {
		t.Fatal("editing after an insert-mode save must dirty the buffer")
	}
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "abcdehello" || !m.Dirty() {
		t.Fatalf("after esc: line=%q dirty=%v, want abcdehello/dirty", line(m, 0), m.Dirty())
	}
	m = typeKeys(m, "u")
	if line(m, 0) != "abchello" || m.Dirty() {
		t.Fatalf("undo to the saved state: line=%q dirty=%v, want abchello/clean", line(m, 0), m.Dirty())
	}
	m = typeKeys(m, "u")
	if line(m, 0) != "hello" || !m.Dirty() {
		t.Fatalf("undo past the saved state: line=%q dirty=%v, want hello/dirty", line(m, 0), m.Dirty())
	}
}

// TestAutosaveInInsertModeStaysCleanAfterEsc covers the adjacent auto-save path
// (#174/#2188): an idle/focus auto-save fired mid-insert marks the state it
// wrote, so leaving insert mode afterwards does not resurrect the flag — and
// the now-clean buffer is not written a second time.
func TestAutosaveInInsertModeStaysCleanAfterEsc(t *testing.T) {
	m, path := loaded(t, "one\n")
	m = send(m, key('i'), key('X'))
	if !m.Autosave() {
		t.Fatal("Autosave must write the dirty buffer")
	}
	if m.Dirty() {
		t.Fatal("autosave must clear the dirty flag")
	}
	m = send(m, special(tea.KeyEsc))
	if m.Dirty() {
		t.Fatal("esc after an insert-mode autosave must leave the buffer clean (#2188)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Xone\n" {
		t.Fatalf("disk = %q, want %q", data, "Xone\n")
	}
	if m.Autosave() {
		t.Fatal("a clean buffer must not be auto-saved again")
	}
}

// TestSaveInInsertModeKeepsTypedTextForDotRepeat guards that closing the undo
// segment on save does not truncate the session: "." still replays the whole
// insert, including the text typed after the save.
func TestSaveInInsertModeKeepsTypedTextForDotRepeat(t *testing.T) {
	m, _ := loaded(t, "\n")
	m = typeKeys(m, "i")
	m = typeKeys(m, "ab")
	m, _ = m.Update(ActionMsg{Action: "write"})
	m = typeKeys(m, "cd")
	m = send(m, special(tea.KeyEsc))
	if line(m, 0) != "abcd" {
		t.Fatalf("insert = %q, want abcd", line(m, 0))
	}
	m = typeKeys(m, ".")
	if line(m, 0) != "abcabcdd" {
		t.Fatalf("dot repeat = %q, want abcabcdd", line(m, 0))
	}
}
