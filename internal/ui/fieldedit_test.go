package ui_test

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/ui"
)

// fieldedit_test.go covers the two chords every one-line input gained with
// #2633: cmd+a (select the whole text, the next insertion replaces it) and
// ctrl+z / cmd+z (undo the last edit). They live on ui.Field, so the
// playground's query line, a find prompt and a filter row answer them the
// same way.

// ansiSeq strips the styling so an assertion reads the characters a row
// shows rather than how they are painted.
var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiSeq.ReplaceAllString(s, "") }

func typed(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func cmdA() tea.KeyPressMsg  { return tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper} }
func ctrlZ() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl} }

func TestFieldSelectAllReplacedByTyping(t *testing.T) {
	f := ui.NewField("hello")
	handled, changed := f.Key(cmdA())
	if !handled || changed {
		t.Fatalf("cmd+a is handled and changes nothing, got handled=%v changed=%v", handled, changed)
	}
	if !f.Selected() || f.Text != "hello" || f.Cur != 5 {
		t.Fatalf("cmd+a selects all and parks the caret at the end, got %+v", f)
	}
	handled, changed = f.Key(typed('x'))
	if !handled || !changed {
		t.Fatal("typing over a selection is a change")
	}
	if f.Text != "x" || f.Cur != 1 || f.Selected() {
		t.Fatalf("typing replaces the whole text, got %q cur=%d sel=%v", f.Text, f.Cur, f.Selected())
	}
}

func TestFieldSelectAllClearedByBackspace(t *testing.T) {
	f := ui.NewField("hello")
	f.Key(cmdA())
	if _, changed := f.Key(tea.KeyPressMsg{Code: tea.KeyBackspace}); !changed {
		t.Fatal("backspace over a selection clears it")
	}
	if f.Text != "" || f.Cur != 0 {
		t.Fatalf("backspace over a selection empties the field, got %q cur=%d", f.Text, f.Cur)
	}
}

// TestFieldSelectAllDroppedByMotion: a key that is not an insertion drops the
// selection and does its ordinary job — the caret moves, the text stands.
func TestFieldSelectAllDroppedByMotion(t *testing.T) {
	f := ui.NewField("hello")
	f.Key(cmdA())
	f.Key(tea.KeyPressMsg{Code: tea.KeyLeft})
	if f.Selected() {
		t.Fatal("a motion drops the selection")
	}
	if f.Text != "hello" || f.Cur != 4 {
		t.Fatalf("the motion still moves the caret, got %q cur=%d", f.Text, f.Cur)
	}
	if _, changed := f.Key(typed('x')); !changed || f.Text != "hellxo" {
		t.Fatalf("typing after the drop inserts, got %q", f.Text)
	}
}

func TestFieldSelectAllOnEmptyFieldSelectsNothing(t *testing.T) {
	var f ui.Field
	if handled, _ := f.Key(cmdA()); !handled {
		t.Fatal("cmd+a stays the field's key even with nothing to select")
	}
	if f.Selected() {
		t.Fatal("an empty field has nothing to select")
	}
}

// TestFieldSelectAllPaints: an armed selection renders differently from the
// same text without one, so the replace-on-type promise is visible.
func TestFieldSelectAllPaints(t *testing.T) {
	f := ui.NewField("hello")
	plain := f.View()
	f.Key(cmdA())
	if sel := f.View(); sel == plain {
		t.Fatalf("an armed selection must render differently, got %q", sel)
	}
	if !strings.Contains(stripANSI(f.View()), "hello") {
		t.Fatalf("the text still reads through the selection, got %q", f.View())
	}
}

// TestFieldUndoCoalescesTypingRun: a run of typed runes is one undo step —
// per-rune undo would make the key useless in a field.
func TestFieldUndoCoalescesTypingRun(t *testing.T) {
	f := ui.NewField(".foo")
	for _, r := range "[]" {
		f.Key(typed(r))
	}
	if f.Text != ".foo[]" {
		t.Fatalf("setup, got %q", f.Text)
	}
	if _, changed := f.Key(ctrlZ()); !changed {
		t.Fatal("ctrl+z undoes the run")
	}
	if f.Text != ".foo" || f.Cur != 4 {
		t.Fatalf("undo restores the text and the caret, got %q cur=%d", f.Text, f.Cur)
	}
}

// TestFieldUndoSeparatesKinds: a deletion is its own step, so undo does not
// swallow the typing that preceded it.
func TestFieldUndoSeparatesKinds(t *testing.T) {
	f := ui.NewField("")
	for _, r := range "abc" {
		f.Key(typed(r))
	}
	f.Key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if f.Text != "ab" {
		t.Fatalf("setup, got %q", f.Text)
	}
	f.Key(ctrlZ())
	if f.Text != "abc" {
		t.Fatalf("the first undo takes back the deletion, got %q", f.Text)
	}
	f.Key(ctrlZ())
	if f.Text != "" {
		t.Fatalf("the second undo takes back the typed run, got %q", f.Text)
	}
	if _, changed := f.Key(ctrlZ()); changed {
		t.Fatal("an exhausted history reports no change")
	}
}

// TestFieldUndoIsBounded: the stack is capped, so a long-lived field cannot
// grow without bound.
func TestFieldUndoIsBounded(t *testing.T) {
	f := ui.NewField("")
	// Each pair (type, delete) is two steps; 200 pairs is far past the cap.
	for i := 0; i < 200; i++ {
		f.Key(typed('x'))
		f.Key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	steps := 0
	for {
		if _, changed := f.Key(ctrlZ()); !changed {
			break
		}
		steps++
		if steps > 200 {
			t.Fatal("the undo stack must be capped")
		}
	}
	if steps == 0 {
		t.Fatal("a capped stack still holds the recent edits")
	}
}

// TestFieldPasteIsItsOwnUndoStep: a paste never coalesces into the typing
// around it, and it replaces an armed selection.
func TestFieldPasteIsItsOwnUndoStep(t *testing.T) {
	f := ui.NewField("ab")
	if !f.Paste("cd") || f.Text != "abcd" {
		t.Fatalf("paste appends at the caret, got %q", f.Text)
	}
	f.Key(ctrlZ())
	if f.Text != "ab" {
		t.Fatalf("undo takes back the whole paste, got %q", f.Text)
	}
	f.Key(cmdA())
	if !f.Paste("zz") || f.Text != "zz" {
		t.Fatalf("a paste over a selection replaces it, got %q", f.Text)
	}
}

// TestFieldSetDropsTheHistory: content put into the field from outside (a
// history step, a prefill) starts a fresh history — undo must not resurrect
// what an unrelated use of the prompt held.
func TestFieldSetDropsTheHistory(t *testing.T) {
	f := ui.NewField("")
	f.Key(typed('a'))
	f.Set("fresh")
	if _, changed := f.Key(ctrlZ()); changed {
		t.Fatalf("Set starts a fresh history, got %q", f.Text)
	}
	f.Key(typed('!'))
	f.Clear()
	if _, changed := f.Key(ctrlZ()); changed {
		t.Fatalf("Clear starts a fresh history, got %q", f.Text)
	}
}

// TestFieldUndoIsNotSharedBetweenCopies: a Field is a value hosts copy; two
// copies must not write into one backing array.
func TestFieldUndoIsNotSharedBetweenCopies(t *testing.T) {
	f := ui.NewField("base")
	f.Key(typed('1'))
	g := f
	g.Key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	g.Key(typed('2'))
	f.Key(ctrlZ())
	if f.Text != "base" {
		t.Fatalf("the original's history is its own, got %q", f.Text)
	}
}

// TestLineSearchGainsSelectAllAndUndo is the second field the sweep asks for
// (#2633): LineSearch embeds Field, so the pane searches that use it — the
// diff, markdown, HTTP, notebook and hex viewers, the terminal scrollback —
// answer both chords without a line of their own.
func TestLineSearchGainsSelectAllAndUndo(t *testing.T) {
	var s ui.LineSearch
	s.Start()
	for _, r := range "err" {
		s.Key(typed(r))
	}
	if s.Text != "err" {
		t.Fatalf("setup, got %q", s.Text)
	}
	if _, changed, _ := s.Key(cmdA()); changed {
		t.Fatal("cmd+a changes no text")
	}
	if !s.Selected() {
		t.Fatal("cmd+a selects the whole query")
	}
	if _, changed, _ := s.Key(typed('x')); !changed || s.Text != "x" {
		t.Fatalf("typing replaces the selected query, got %q", s.Text)
	}
	if _, changed, _ := s.Key(ctrlZ()); !changed || s.Text != "err" {
		t.Fatalf("ctrl+z restores the previous query, got %q", s.Text)
	}
}
