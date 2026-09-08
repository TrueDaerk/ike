package settings

import (
	"strings"
	"testing"
)

// keymap_show_test.go covers the bind-a-key entry point (#2549): the palette's
// offer opens the settings panel on the Keymap page narrowed to one command.

// TestShowCommandNarrowsToCommand: the list is filtered to the id and the
// selection sits on one of its rows, so enter captures a chord for it.
func TestShowCommandNarrowsToCommand(t *testing.T) {
	k, _ := keymapPage(t)
	k.ShowCommand("editor.write")
	rows := k.rows()
	if len(rows) == 0 {
		t.Fatal("editor.write must keep its rows under the filter")
	}
	for _, r := range rows {
		if !strings.Contains(r.Command, "editor.write") {
			t.Fatalf("filter leaked %q", r.Command)
		}
	}
	cur, ok := k.current()
	if !ok || cur.Command != "editor.write" {
		t.Fatalf("selection = %+v, want an editor.write row", cur)
	}
	if k.Capturing() {
		t.Fatal("the filter input must not stay open: keys are the page's actions")
	}
	if !strings.Contains(k.View(80, 20), "filter: editor.write") {
		t.Fatal("the filter must be visible, like a typed one")
	}
}

// An unknown id leaves an empty, filtered list rather than failing.
func TestShowCommandUnknownIDEmptyList(t *testing.T) {
	k, _ := keymapPage(t)
	k.ShowCommand("no.such.command")
	if rows := k.rows(); len(rows) != 0 {
		t.Fatalf("want no rows for an unknown id, got %d", len(rows))
	}
}

// TestOpenKeymapOn: the panel opens on the Keymap page with the form column
// focused and the command filter applied; without a keymap page it reports
// false.
func TestOpenKeymapOn(t *testing.T) {
	restoreConfig(t)
	opts := testOpts(t)
	kp := NewKeymapPage(opts, func(string) bool { return true }, nil)
	kp.SetSubPanelHost(&stubHost{})
	m := New(append(BasePages([]string{"default"}, nil, nil), Page{Title: "Keymap", Custom: kp}), opts)
	m.SetSize(100, 30)
	if !m.OpenKeymapOn("editor.write") {
		t.Fatal("the keymap page must be found")
	}
	if !m.IsOpen() || m.pages[m.cat].Custom != kp || m.focus != formColumn {
		t.Fatalf("panel must open on the keymap page's form column: open=%v cat=%d focus=%v", m.IsOpen(), m.cat, m.focus)
	}
	if !strings.Contains(m.View(), "filter: editor.write") {
		t.Fatal("the view must show the command filter")
	}

	none := New(BasePages([]string{"default"}, nil, nil), opts)
	if none.OpenKeymapOn("editor.write") {
		t.Fatal("without a keymap page the open must report false")
	}
}
