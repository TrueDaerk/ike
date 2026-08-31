package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/scratch"
)

// scratch_from_selection_test.go covers #2339's way into the store:
// scratch.newFromSelection turns the active selection into a scratch that
// inherits the source file's extension, and says so when there is no
// selection instead of creating an empty file.

// selectionApp opens a file named name holding body and leaves a visual
// selection on the first word.
func selectionApp(t *testing.T, name, body string) Model {
	t.Helper()
	m := newSized()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return selectWord(out.(Model))
}

func TestScratchFromSelectionCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{scratchFromSelectionCommandID, scratchPromoteCommandID} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must be registered", id)
		}
	}
}

// TestNewScratchFromSelectionInheritsExtension is the headline AC: a selection
// in a .py file yields a .py scratch holding exactly the selected text, opened
// and focused, with no language picker in between.
func TestNewScratchFromSelectionInheritsExtension(t *testing.T) {
	m := selectionApp(t, "source.py", "needle one\ntwo\n")
	if sel := m.activeSelectionText(); sel != "needle" {
		t.Fatalf("setup: selection = %q, want %q", sel, "needle")
	}

	m = dispatch(t, m, NewScratchFromSelectionMsg{})
	if m.palette.IsOpen() {
		t.Fatal("the language picker must not open — the extension is inherited")
	}
	ed := m.activeWS().Panes.FocusedInstance().Editor()
	path := ed.Path()
	if filepath.Base(path) != "scratch-1.py" {
		t.Fatalf("scratch path = %q, want scratch-1.py", path)
	}
	if !scratch.IsScratch(path) {
		t.Fatalf("%q must live in the scratch store", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("scratch must exist on disk: %v", err)
	}
	if string(data) != "needle" {
		t.Fatalf("scratch content = %q, want exactly the selected text", data)
	}
	if ed.Text() != "needle" {
		t.Fatalf("open buffer = %q, want the selected text", ed.Text())
	}
}

// TestNewScratchFromSelectionKeepsUnknownExtension is the "no whitelist" AC:
// the store takes any extension, so a selection out of a file whose suffix has
// no picker row still produces a scratch of that suffix.
func TestNewScratchFromSelectionKeepsUnknownExtension(t *testing.T) {
	m := selectionApp(t, "source.zzq", "needle one\n")

	m = dispatch(t, m, NewScratchFromSelectionMsg{})
	if got := filepath.Base(m.activeWS().Panes.FocusedInstance().Editor().Path()); got != "scratch-1.zzq" {
		t.Fatalf("scratch path = %q, want the source extension kept", got)
	}
}

// TestNewScratchFromSelectionWithoutSelectionRefuses is the empty-scratch
// guard: without a selection the command reports it and creates nothing.
func TestNewScratchFromSelectionWithoutSelectionRefuses(t *testing.T) {
	m := selectionApp(t, "source.py", "needle one\n")
	// Leave visual mode again, so nothing is selected.
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if sel := m.activeSelectionText(); sel != "" {
		t.Fatalf("setup: selection = %q, want none", sel)
	}
	before := m.activeWS().Panes.FocusedInstance().Editor().Path()
	clearNotices(&m)

	m = dispatch(t, m, NewScratchFromSelectionMsg{})
	if got := m.activeWS().Panes.FocusedInstance().Editor().Path(); got != before {
		t.Fatalf("nothing must open, focused buffer = %q", got)
	}
	if got, err := scratch.List(); err != nil || len(got) != 0 {
		t.Fatalf("no scratch may be created, store = %v (%v)", got, err)
	}
	if !containsSubstr(notices(m), "select some text first") {
		t.Fatalf("the refusal must be reported, notices = %v", notices(m))
	}
}

// TestNewScratchFromSelectionFallsBackToPlainText covers a selection whose
// source carries no extension at all (an untitled buffer, a Dockerfile): the
// scratch is plain text rather than nothing.
func TestNewScratchFromSelectionFallsBackToPlainText(t *testing.T) {
	m := selectionApp(t, "Dockerfile", "needle one\n")

	m = dispatch(t, m, NewScratchFromSelectionMsg{})
	if got := filepath.Base(m.activeWS().Panes.FocusedInstance().Editor().Path()); got != "scratch-1.txt" {
		t.Fatalf("scratch path = %q, want the plain-text fallback", got)
	}
}

// TestNewScratchMsgContentSeedsFile covers the message extension itself: a
// carried content is written instead of the language template, and an empty
// one leaves the existing behaviour untouched.
func TestNewScratchMsgContentSeedsFile(t *testing.T) {
	m := newSized()

	m = dispatch(t, m, NewScratchMsg{Ext: "sct", Content: "seeded\n"})
	path := m.activeWS().Panes.FocusedInstance().Editor().Path()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "seeded\n" {
		t.Fatalf("seeded scratch = %q, %v; want the carried content", data, err)
	}

	m = dispatch(t, m, NewScratchMsg{Ext: "sct"})
	plain := m.activeWS().Panes.FocusedInstance().Editor().Path()
	if strings.HasSuffix(plain, filepath.Base(path)) {
		t.Fatal("the second scratch must get its own name")
	}
	if data, err := os.ReadFile(plain); err != nil || len(data) != 0 {
		t.Fatalf("a content-less scratch must stay empty: %q, %v", data, err)
	}
}
