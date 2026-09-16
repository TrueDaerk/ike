package app

// searchkill_test.go guards the dispatch half of #2602: cmd+backspace and
// alt+backspace are bound to editor.deleteLine / editor.deleteWordBackward in
// the editor context, so without an explicit claim the keymap layer consumed
// them before the pane — and the chords deleted document text while the user
// was editing an open command line.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

const (
	killDoc = "alpha beta gamma\nsecond line\n"
	// killText is what Text() returns for it: the buffer drops the trailing
	// newline.
	killText = "alpha beta gamma\nsecond line"
)

func altBackspace() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}
}

func cmdBackspace() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}
}

func TestKillChordsEditTheSearchLineNotTheBuffer(t *testing.T) {
	m := openedEditor(t, killDoc)
	m = drainKey(m, tea.KeyPressMsg{Code: '/', Text: "/"})
	m = typeInto(m, "alpha beta")
	if got := m.activeEditor().CommandLine(); got != "/alpha beta" {
		t.Fatalf("search line = %q, want %q", got, "/alpha beta")
	}

	m = drainKey(m, altBackspace())
	if got := m.activeEditor().CommandLine(); got != "/alpha " {
		t.Fatalf("alt+backspace: search line = %q, want %q", got, "/alpha ")
	}
	if got := m.activeEditor().Text(); got != killText {
		t.Fatalf("alt+backspace touched the buffer: %q", got)
	}

	m = drainKey(m, cmdBackspace())
	if got := m.activeEditor().CommandLine(); got != "/" {
		t.Fatalf("cmd+backspace: search line = %q, want an empty query", got)
	}
	if got := m.activeEditor().Text(); got != killText {
		t.Fatalf("cmd+backspace touched the buffer: %q", got)
	}
}

func TestKillChordsEditTheExLineNotTheBuffer(t *testing.T) {
	m := openedEditor(t, killDoc)
	m = drainKey(m, tea.KeyPressMsg{Code: ':', Text: ":"})
	m = typeInto(m, "set number")
	if got := m.activeEditor().CommandLine(); got != ":set number" {
		t.Fatalf("ex line = %q, want %q", got, ":set number")
	}

	m = drainKey(m, altBackspace())
	if got := m.activeEditor().CommandLine(); got != ":set " {
		t.Fatalf("alt+backspace: ex line = %q, want %q", got, ":set ")
	}
	m = drainKey(m, cmdBackspace())
	if got := m.activeEditor().CommandLine(); got != ":" {
		t.Fatalf("cmd+backspace: ex line = %q, want an empty command", got)
	}
	if got := m.activeEditor().Text(); got != killText {
		t.Fatalf("the ex line let a kill chord reach the buffer: %q", got)
	}
}

func TestKillChordsStayBufferCommandsOutsideTheCommandLine(t *testing.T) {
	m := openedEditor(t, killDoc)
	if m.editorLineInput() {
		t.Fatal("no line input should be open in resting normal mode")
	}
	// The cursor rests at the start of line 1: alt+backspace has no word
	// before it, so move to the end of "alpha " first.
	m = typeInto(m, "ww")
	m = drainKey(m, altBackspace())
	if got := m.activeEditor().Text(); got != "alpha gamma\nsecond line" {
		t.Fatalf("alt+backspace in normal mode: %q", got)
	}
	m = drainKey(m, cmdBackspace())
	if got := m.activeEditor().Text(); got != "second line" {
		t.Fatalf("cmd+backspace in normal mode: %q", got)
	}
}
