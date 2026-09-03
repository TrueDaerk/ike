package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/pane"
	"ike/internal/registry"
)

// hexfiles_test.go covers the hex viewer's app side (#2420): the binary
// sniff handler, the files.binary_open redirect, and the "Open file as…"
// chooser's dispatch.

// hexApp is a sized app on cfg, so a test can pick the binary-open mode.
func hexApp(t *testing.T, cfg host.Config) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	// The global registry carries the compile-in file handlers (hex, image,
	// data, …) these tests route through.
	m := NewWith(registry.Global(), cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return dismissOnboarding(out.(Model))
}

// hasKind reports whether any content instance (pane or tab) is of kind.
func hasKind(m Model, kind pane.Kind) bool {
	_, _, _, ok := m.findContent(func(c *pane.Instance) bool { return c.Kind() == kind })
	return ok
}

// TestBinaryOpensInHexPane: a file with a NUL in its head and no dedicated
// viewer opens in the hex viewer by default, never as a text buffer.
func TestBinaryOpensInHexPane(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestBinary(t, "blob.bin")
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	if !hasKind(m, pane.KindHex) {
		t.Fatal("a sniffed binary must open in a hex pane")
	}
	if m.editorWithFile(canonicalPath(path)) != "" {
		t.Fatal("the binary must not additionally land in a text buffer")
	}
}

// TestBinaryOpenSettingEditor: files.binary_open = "editor" keeps the
// pre-#2420 behaviour — a text buffer, with code insight off.
func TestBinaryOpenSettingEditor(t *testing.T) {
	m := hexApp(t, host.MapConfig{"files.binary_open": "editor"})
	path := writeTestBinary(t, "blob.bin")
	m = settle(t, m, m.host.Dispatch(palette.OpenFileMsg{Path: path}))
	if hasKind(m, pane.KindHex) {
		t.Fatal("with binary_open=editor no hex pane must open")
	}
	key := m.editorWithFile(canonicalPath(path))
	if key == "" {
		t.Fatal("the binary must open in a text buffer")
	}
	ed := m.activeWS().Panes.Get(key).EditorForPath(canonicalPath(path))
	if ed == nil || !ed.InsightOff() {
		t.Fatal("a binary opened as text must keep code insight off")
	}
}

// TestOpenAsHexForcedIgnoresSetting: the explicit chooser pick opens the hex
// pane even when the setting prefers the editor.
func TestOpenAsHexForcedIgnoresSetting(t *testing.T) {
	m := hexApp(t, host.MapConfig{"files.binary_open": "editor"})
	m.openAsPath = writeTestBinary(t, "blob.bin")
	out, cmd := m.openFileAs(OpenAsMsg{Mode: openAsHex})
	m = settle(t, out.(Model), cmd)
	if !hasKind(m, pane.KindHex) {
		t.Fatal("Open file as… → Hex must open the hex pane regardless of the setting")
	}
}

// TestOpenAsDataRejectsNonDatabase: an invalid pick notifies and leaves the
// layout untouched.
func TestOpenAsDataRejectsNonDatabase(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	m.openAsPath = writeTestBinary(t, "not-a.db")
	leaves := len(m.leafOrder())
	out, cmd := m.openFileAs(OpenAsMsg{Mode: openAsData})
	m = settle(t, out.(Model), cmd)
	if hasKind(m, pane.KindData) {
		t.Fatal("a non-database must not open a data pane")
	}
	if got := len(m.leafOrder()); got != leaves {
		t.Fatalf("leaf count changed %d → %d — the layout must stay untouched", leaves, got)
	}
	notes := m.host.DrainNotifications()
	if len(notes) == 0 || !strings.Contains(notes[0].Text, "not a SQLite") {
		t.Fatalf("an invalid pick must notify, got %v", notes)
	}
}

// TestOpenAsImageRejectsNonImage: same contract for the image target.
func TestOpenAsImageRejectsNonImage(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	m.openAsPath = writeTestBinary(t, "not-a.png")
	out, cmd := m.openFileAs(OpenAsMsg{Mode: openAsImage})
	m = settle(t, out.(Model), cmd)
	if hasKind(m, pane.KindImage) {
		t.Fatal("a non-image must not open an image pane")
	}
	if notes := m.host.DrainNotifications(); len(notes) == 0 || !strings.Contains(notes[0].Text, "not a recognised image") {
		t.Fatalf("an invalid pick must notify, got %v", notes)
	}
}

// TestOpenAsEditorForcesTextBuffer: the Text editor target bypasses every
// handler — a sniffed binary lands in a buffer with insight off.
func TestOpenAsEditorForcesTextBuffer(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	path := writeTestBinary(t, "blob.bin")
	m.openAsPath = canonicalPath(path)
	out, cmd := m.openFileAs(OpenAsMsg{Mode: openAsEditor})
	m = settle(t, out.(Model), cmd)
	if hasKind(m, pane.KindHex) {
		t.Fatal("the Text editor target must bypass the hex handler")
	}
	key := m.editorWithFile(canonicalPath(path))
	if key == "" {
		t.Fatal("the file must open in a text buffer")
	}
	if ed := m.activeWS().Panes.Get(key).EditorForPath(canonicalPath(path)); ed == nil || !ed.InsightOff() {
		t.Fatal("the forced text open of a binary must keep insight off")
	}
}

// TestOpenAsChooserRows: the chooser lists every registered handler plus the
// two always-available targets, and each row dispatches its OpenAsMsg.
func TestOpenAsChooserRows(t *testing.T) {
	items := openAsMode{}.Results("", palette.Context{})
	want := []string{openAsEditor, openAsHex, openAsImage, openAsArchive, openAsData, openAsMarkdown, openAsGzip}
	if len(items) != len(want) {
		t.Fatalf("chooser rows = %d, want %d", len(items), len(want))
	}
	got := map[string]bool{}
	for _, it := range items {
		msg, ok := it.Msg.(OpenAsMsg)
		if !ok {
			t.Fatalf("row %q must dispatch an OpenAsMsg", it.Title)
		}
		got[msg.Mode] = true
	}
	for _, mode := range want {
		if !got[mode] {
			t.Errorf("chooser misses the %q target", mode)
		}
	}
	if items := (openAsMode{}).Results("hex", palette.Context{}); len(items) != 1 || items[0].Msg.(OpenAsMsg).Mode != openAsHex {
		t.Fatalf("filtering by 'hex' = %v, want just the hex row", items)
	}
}

// TestOpenAsPickerNeedsASubject: without a file to act on, the chooser
// refuses with a notification instead of opening.
func TestOpenAsPickerNeedsASubject(t *testing.T) {
	m := hexApp(t, host.MapConfig{})
	m.openFileAsPicker()
	if notes := m.host.DrainNotifications(); len(notes) == 0 || !strings.Contains(notes[0].Text, "open file as") {
		t.Fatalf("no subject must notify, got %v", notes)
	}
}
