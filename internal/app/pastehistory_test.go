package app

import (
	"strings"
	"testing"

	"ike/internal/editor"
	"ike/internal/editor/register"
	"ike/internal/palette"
)

// pastehistory_test.go covers paste-from-history (#57): the palette mode over
// the register history and the app round-trip into the focused editor.

func TestPasteHistModePreviewAndFilter(t *testing.T) {
	m := &pasteHistMode{}
	m.Set([]register.Entry{
		{Text: "alpha line\nsecond\n", Linewise: true},
		{Text: "beta"},
		{Text: "\t  spaced  "},
	})
	items := m.Results("", palette.Context{})
	if len(items) != 3 {
		t.Fatalf("want 3 rows, got %d", len(items))
	}
	if items[0].Title != "alpha line" || items[0].Detail != "2 lines" {
		t.Fatalf("row 0 = %q / %q", items[0].Title, items[0].Detail)
	}
	if items[1].Detail != "4 chars" {
		t.Fatalf("row 1 detail = %q", items[1].Detail)
	}
	if msg, ok := items[1].Msg.(PasteHistoryEntryMsg); !ok || msg.Index != 1 {
		t.Fatalf("row 1 must carry its history index, msg = %#v", items[1].Msg)
	}
	// Filtering keeps the original history index on the row.
	filtered := m.Results("beta", palette.Context{})
	if len(filtered) != 1 {
		t.Fatalf("filter = %v", filtered)
	}
	if msg := filtered[0].Msg.(PasteHistoryEntryMsg); msg.Index != 1 {
		t.Fatalf("filtered row must keep index 1, got %d", msg.Index)
	}
}

func TestPasteFromHistoryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\ntwo\nthree\n")
	m := openApp(t, a)
	ed := m.panes.FocusedInstance().Editor()

	// Copy line 1 ("one"), then line 2 ("two"): history holds two entries.
	m = dispatch(t, m, editor.ActionMsg{Action: "copy"})
	m.panes.FocusedInstance().Editor().SetCursor(1, 0)
	m = dispatch(t, m, editor.ActionMsg{Action: "copy"})
	if h := ed.RegisterHistory(); len(h) != 2 || !strings.HasPrefix(h[0].Text, "two") {
		t.Fatalf("history = %v", h)
	}

	// Opening the picker snapshots the history into the mode…
	m = dispatch(t, m, ShowPasteHistoryMsg{})
	if !m.palette.IsOpen() {
		t.Fatal("picker must open with history present")
	}
	m.palette.Close()

	// …and activating index 1 pastes the OLDER entry ("one") like Cmd+V.
	m = dispatch(t, m, PasteHistoryEntryMsg{Index: 1})
	if got := m.panes.FocusedInstance().Editor().Text(); strings.Count(got, "one") != 2 {
		t.Fatalf("older entry must be pasted, text = %q", got)
	}
}

func TestPasteFromHistoryEmptyIsToastNoPalette(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)
	m = dispatch(t, m, ShowPasteHistoryMsg{})
	if m.palette.IsOpen() {
		t.Fatal("empty history must not open the picker")
	}
}

func TestPasteFromHistoryCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("editor.pasteFromHistory"); !ok {
		t.Fatal("editor.pasteFromHistory must be registered")
	}
}
