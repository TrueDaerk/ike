package app

import (
	"strings"
	"testing"

	"ike/internal/domview"
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
	ed := m.activeWS().Panes.FocusedInstance().Editor()

	// Copy line 1 ("one"), then line 2 ("two"): history holds two entries.
	m = dispatch(t, m, editor.ActionMsg{Action: "copy"})
	m.activeWS().Panes.FocusedInstance().Editor().SetCursor(1, 0)
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
	if got := m.activeWS().Panes.FocusedInstance().Editor().Text(); strings.Count(got, "one") != 2 {
		t.Fatalf("older entry must be pasted, text = %q", got)
	}
}

// TestPasteHistorySharedAcrossPanes (#1540): registers are app-wide, so a
// copy in one editor pane is offered by every other editor's history.
func TestPasteHistorySharedAcrossPanes(t *testing.T) {
	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)
	m = dispatch(t, m, editor.ActionMsg{Action: "copy"})

	key2 := m.activeWS().Panes.AddEditor()
	ed2 := m.activeWS().Panes.Get(key2).Editor()
	if h := ed2.RegisterHistory(); len(h) != 1 || !strings.HasPrefix(h[0].Text, "one") {
		t.Fatalf("a fresh pane must see the shared history, got %v", h)
	}
	// A tab added later shares too.
	ed3 := m.activeWS().Panes.Get(key2).AddTab()
	if h := ed3.RegisterHistory(); len(h) != 1 {
		t.Fatalf("a fresh tab must see the shared history, got %v", h)
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

// TestPaneCopiesReachHistory (#2061): a pane's copy action goes through the
// host's copy path, so it lands in the same ring the picker lists and pastes
// like any yank.
func TestPaneCopiesReachHistory(t *testing.T) {
	orig := clipboardWrite
	copied := ""
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)
	m = dispatch(t, m, domview.CopyMsg{Text: "div > span", What: "CSS selector"})
	if copied != "div > span" {
		t.Fatalf("the copy must still reach the system clipboard, got %q", copied)
	}

	ed := m.activeWS().Panes.FocusedInstance().Editor()
	if h := ed.RegisterHistory(); len(h) != 1 || h[0].Text != "div > span" {
		t.Fatalf("pane copy must reach the clipboard history, got %v", h)
	}
	// It pastes like any other entry.
	m = dispatch(t, m, PasteHistoryEntryMsg{Index: 0})
	if got := m.activeWS().Panes.FocusedInstance().Editor().Text(); !strings.Contains(got, "div > span") {
		t.Fatalf("pane copy must paste from the picker, text = %q", got)
	}
}

// TestClipboardHistorySizeSetting (#2061): editor.clipboard_history_size sizes
// the app-wide ring, so the picker lists at most N entries.
func TestClipboardHistorySizeSetting(t *testing.T) {
	orig := clipboardWrite
	clipboardWrite = func(string) {}
	t.Cleanup(func() { clipboardWrite = orig })

	dir := t.TempDir()
	a := writeTemp(t, dir, "a.txt", "one\n")
	m := openApp(t, a)
	m.regs.SetHistoryCap(2)
	for _, text := range []string{"first", "second", "third"} {
		m = dispatch(t, m, domview.CopyMsg{Text: text, What: "node"})
	}
	h := m.activeWS().Panes.FocusedInstance().Editor().RegisterHistory()
	if len(h) != 2 || h[0].Text != "third" || h[1].Text != "second" {
		t.Fatalf("ring must hold the newest two, got %v", h)
	}
	m.pasteHist.Set(h)
	if rows := m.pasteHist.Results("", palette.Context{}); len(rows) != 2 {
		t.Fatalf("picker must list the bounded ring, got %d rows", len(rows))
	}
}

func TestPasteFromHistoryCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("editor.pasteFromHistory"); !ok {
		t.Fatal("editor.pasteFromHistory must be registered")
	}
}
