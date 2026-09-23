package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
)

// triggerRecorder swaps the focused editor's emitter for one recording the
// completion triggers it sends, and returns the slice it fills.
func triggerRecorder(t *testing.T, m Model) *[]editor.Event {
	t.Helper()
	ed := m.activeEditor()
	if ed == nil {
		t.Fatal("no active editor")
	}
	var got []editor.Event
	ed.SetEmitter(editor.EmitterFunc(func(e editor.Event) {
		if e.Kind == editor.EventCompletionTrigger {
			got = append(got, e)
		}
	}))
	return &got
}

// editorApp opens content in an editor with the first-start LSP dialog
// dismissed, so scripted keys reach the editor.
func editorApp(t *testing.T, content string) Model {
	t.Helper()
	m, _ := openedApp(t, content)
	for m.onboardingOpen() {
		m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	}
	return m
}

// TestCtrlSpaceRequestsCompletion guards #2695 end to end: ctrl+space in an
// insert-mode editor resolves through the keymap layer to completion.trigger,
// whose message makes the editor ask for completion at the caret — char-less,
// so no delay and no trigger-character gate applies.
func TestCtrlSpaceRequestsCompletion(t *testing.T) {
	m := editorApp(t, "fmt\n")
	m = drainKey(m, tea.KeyPressMsg{Code: 'i', Text: "i"})
	got := triggerRecorder(t, m)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if len(*got) != 1 {
		t.Fatalf("triggers = %v, want exactly one manual request", *got)
	}
	if (*got)[0].Char != "" {
		t.Errorf("Char = %q, want the empty manual-request marker", (*got)[0].Char)
	}

	// The NUL spelling a terminal on the legacy encoding sends for the same
	// key runs the same command, and a second press re-requests.
	m = drainKey(m, tea.KeyPressMsg{Code: '@', Mod: tea.ModCtrl})
	if len(*got) != 2 {
		t.Fatalf("triggers = %v, want ctrl+@ to re-request too", *got)
	}
}

// TestCtrlSpaceNormalModeIsSilent: the same chord in normal mode is a no-op —
// no request, and no error notification either.
func TestCtrlSpaceNormalModeIsSilent(t *testing.T) {
	m := editorApp(t, "fmt\n")
	got := triggerRecorder(t, m)

	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	if len(*got) != 0 {
		t.Fatalf("normal mode must request nothing, got %v", *got)
	}
}
