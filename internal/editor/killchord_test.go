package editor

// killchord_test.go is the editor half of #2602: while a single-line input
// owns the keyboard — the "/" "?" search line, the ":" ex line, the cmd+r
// replace panel — cmd+backspace and alt+backspace edit that input and must
// leave the document alone. They are bound to editor.deleteLine /
// editor.deleteWordBackward in the Editor keymap context, so the app dispatch
// claims the chords for the open input (LineInputOpen, see
// internal/app/searchkill_test.go); these tests pin the pane-side behaviour
// the claim delivers the keys to.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

const killChordDoc = "alpha beta gamma\nsecond line\n"

func altBksp() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
}

func cmdBksp() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}
}

func TestKillChordsOnSearchLineLeaveTheBuffer(t *testing.T) {
	m, _ := loaded(t, killChordDoc)
	want := m.Text()
	m = send(m, key('/'))
	m = typeKeys(m, "alpha beta")

	m = send(m, altBksp())
	if m.cmdline != "alpha " {
		t.Fatalf("alt+backspace: cmdline = %q, want %q", m.cmdline, "alpha ")
	}
	if got := m.Text(); got != want {
		t.Fatalf("alt+backspace edited the buffer: %q", got)
	}

	m = send(m, cmdBksp())
	if m.cmdline != "" || m.cmdCur != 0 {
		t.Fatalf("cmd+backspace: cmdline = %q cur = %d, want empty at 0", m.cmdline, m.cmdCur)
	}
	if got := m.Text(); got != want {
		t.Fatalf("cmd+backspace edited the buffer: %q", got)
	}
	// The line is still open: clearing the query is an edit, not a cancel.
	if m.mode != Command || !m.searching {
		t.Fatal("the search line must stay open after the kills")
	}
}

func TestKillChordsOnExLineLeaveTheBuffer(t *testing.T) {
	m, _ := loaded(t, killChordDoc)
	want := m.Text()
	m = typeKeys(m, ":set number")

	m = send(m, altBksp())
	if m.cmdline != "set " {
		t.Fatalf("alt+backspace: cmdline = %q, want %q", m.cmdline, "set ")
	}
	m = send(m, cmdBksp())
	if m.cmdline != "" {
		t.Fatalf("cmd+backspace: cmdline = %q, want empty", m.cmdline)
	}
	if got := m.Text(); got != want {
		t.Fatalf("the ex line let a kill chord edit the buffer: %q", got)
	}
}

func TestKillChordsOnReplacePanelLeaveTheBuffer(t *testing.T) {
	m := openPanel(t, killChordDoc)
	want := m.Text()
	m = typeKeys(m, "alpha beta")

	m = send(m, altBksp())
	if m.replPanel.find.Text != "alpha " {
		t.Fatalf("alt+backspace: find = %q, want %q", m.replPanel.find.Text, "alpha ")
	}
	m = send(m, cmdBksp())
	if m.replPanel.find.Text != "" {
		t.Fatalf("cmd+backspace: find = %q, want empty", m.replPanel.find.Text)
	}
	if got := m.Text(); got != want {
		t.Fatalf("the replace panel let a kill chord edit the buffer: %q", got)
	}
}

func TestLineInputOpenTracksEveryLineInput(t *testing.T) {
	m, _ := loaded(t, killChordDoc)
	if m.LineInputOpen() {
		t.Fatal("resting normal mode has no line input open")
	}
	s := send(m, key('/'))
	if !s.LineInputOpen() {
		t.Fatal("the search line is a line input")
	}
	e := typeKeys(m, ":")
	if !e.LineInputOpen() {
		t.Fatal("the ex line is a line input")
	}
	if p := openPanel(t, killChordDoc); !p.LineInputOpen() {
		t.Fatal("the replace panel is a line input")
	}
	// Insert mode types into the document, not into a line input: the chords
	// stay the buffer's there.
	i := typeKeys(m, "i")
	if i.LineInputOpen() {
		t.Fatal("insert mode is not a line input")
	}
}
