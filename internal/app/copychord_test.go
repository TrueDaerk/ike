package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httppane"
	"ike/internal/keymap"
	"ike/internal/pane"
)

// copychord_test.go covers #2315: cmd+c is a listed binding in the response
// viewer and in the explorer, not a pane secret (http) or a dead key
// (explorer). Since #2660 the explorer's cmd+c is its file clipboard rather
// than file.copyPath — the path copy moved to cmd+shift+c and the context
// menu. GOOS is pinned to darwin in the dispatch tests so the built table
// keeps the meta chord the tests feed, regardless of the build platform.

// pinDarwin keeps cmd chords as Meta in the default table for one test.
func pinDarwin(t *testing.T) {
	t.Helper()
	old := keymap.GOOS
	keymap.GOOS = "darwin"
	t.Cleanup(func() { keymap.GOOS = old })
}

// httpPaneApp opens the response viewer on a sample response and focuses it.
// It builds the model through newSized rather than httpApp: these tests
// dispatch through the keymap layer, which needs the app plugin's commands
// registered — httpApp's bare registry has none.
func httpPaneApp(t *testing.T) Model {
	t.Helper()
	m := newSized()
	out, _ := m.Update(HTTPResponseMsg{Request: "one", Resp: sampleResponse("one")})
	m = out.(Model)
	m.focusHTTPPanel()
	if m.focusContext() != string(keymap.HTTP) {
		t.Fatalf("precondition: focus context = %q, want http", m.focusContext())
	}
	return m
}

// TestCopyResponseCommandRegistered: http.copyResponse must be a registry
// command, or the binding is inert and the palette never lists it.
func TestCopyResponseCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("http.copyResponse"); !ok {
		t.Fatal("http.copyResponse must be a registry command")
	}
}

// TestCopyChordDefaultsBound: cmd+c resolves in both contexts, on both
// platforms — where off darwin it folds onto ctrl+c like every cmd row.
func TestCopyChordDefaultsBound(t *testing.T) {
	cases := []struct {
		ctx  keymap.Context
		cmd  string
		want string
	}{
		{keymap.HTTP, "http.copyResponse", "cmd+c"},
		// The explorer's file clipboard (#2660); cmd+x/cmd+v fold onto
		// ctrl+x/ctrl+v off macOS like every other cmd row.
		{keymap.Explorer, "explorer.clipCopy", "cmd+c"},
		{keymap.Explorer, "explorer.clipCut", "cmd+x"},
		{keymap.Explorer, "explorer.clipPaste", "cmd+v"},
	}
	for _, goos := range []string{"darwin", "linux"} {
		table := keymap.BuildTable(keymap.DefaultsFor(keymap.PresetJetBrains, goos), nil, goos)
		for _, c := range cases {
			chord := keymap.NormalizeChord(keymap.MustParseChord(c.want), goos)
			b, ok := table.Lookup(chord, c.ctx)
			if !ok || b.Command != c.cmd {
				t.Errorf("%s: %s in %s = %+v ok=%v, want %s", goos, c.want, c.ctx, b, ok, c.cmd)
			}
		}
	}
	// The editor keeps its own copy, and the global path chord is untouched.
	table := keymap.BuildTable(keymap.DefaultsFor(keymap.PresetJetBrains, "darwin"), nil, "darwin")
	if b, ok := table.Lookup(keymap.MustParseChord("cmd+c"), keymap.Editor); !ok || b.Command != "editor.copy" {
		t.Errorf("cmd+c in the editor = %+v ok=%v, want editor.copy", b, ok)
	}
	if b, ok := table.Lookup(keymap.MustParseChord("cmd+shift+c"), keymap.Global); !ok || b.Command != "file.copyPath" {
		t.Errorf("cmd+shift+c = %+v ok=%v, want file.copyPath", b, ok)
	}
}

// TestCopyChordNoCtrlSecondary: ctrl+c stays unbound in both contexts on
// darwin, so a selection-less press keeps its global quit meaning (#2062).
func TestCopyChordNoCtrlSecondary(t *testing.T) {
	table := keymap.BuildTable(keymap.DefaultsFor(keymap.PresetJetBrains, "darwin"), nil, "darwin")
	for _, ctx := range []keymap.Context{keymap.HTTP, keymap.Explorer} {
		if b, ok := table.Lookup(keymap.MustParseChord("ctrl+c"), ctx); ok {
			t.Errorf("ctrl+c in %s must stay unbound, got %s", ctx, b.Command)
		}
	}
}

// TestCopyChordCopiesResponseBody: with no selection the chord copies the
// whole body, dispatched through the keymap layer.
func TestCopyChordCopiesResponseBody(t *testing.T) {
	pinDarwin(t)
	copied := stubClipboard(t, "")
	m := httpPaneApp(t)

	m = drainKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta})
	if !strings.Contains(*copied, `"ok"`) {
		t.Fatalf("cmd+c copied %q, want the response body", *copied)
	}
}

// TestCopyChordCopiesSelection: a live selection wins over the body, the way
// the pane-local key has always behaved.
func TestCopyChordCopiesSelection(t *testing.T) {
	pinDarwin(t)
	copied := stubClipboard(t, "")
	m := httpPaneApp(t)

	p := m.httpPanel()
	p.MousePress(1, 1)
	p.MouseDrag(4, 1)
	p.MouseRelease()
	if !p.HasSelection() {
		t.Fatal("precondition: the drag must leave a selection")
	}
	want := p.SelectionText()

	m = drainKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta})
	if *copied != want || want == "" {
		t.Fatalf("cmd+c copied %q, want the selection %q", *copied, want)
	}
	if m.httpPanel().HasSelection() {
		t.Error("copying the selection must clear it, like the pane key does")
	}
}

// TestCopyResponseCommandWithoutPane: the command is reachable from the
// palette with no viewer open; it must say so rather than copy nothing.
func TestCopyResponseCommandWithoutPane(t *testing.T) {
	m := httpApp(t)
	if _, cmd := m.Update(HTTPCopyResponseMsg{}); cmd != nil {
		if _, ok := cmd().(httppane.CopyMsg); ok {
			t.Fatal("no pane open: nothing may be copied")
		}
	}
}

// TestCopyChordFillsExplorerClipboard: cmd+c on a tree selection puts the
// entry on the explorer's file clipboard (#2660) and leaves the OS clipboard
// — which carries text, never paths — untouched.
func TestCopyChordFillsExplorerClipboard(t *testing.T) {
	pinDarwin(t)
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m := newSized()
	m.setFocus("explorer")
	if inst := m.activeWS().Panes.FocusedInstance(); inst == nil || inst.Kind() != pane.KindExplorer {
		t.Fatal("precondition: the explorer must have focus")
	}
	// The tree scans in the background; feed the results in, then step off
	// the root row, which Selected() deliberately excludes.
	for _, sd := range scanResults(t, m.explorer().Init()) {
		out, _ := m.Update(sd)
		m = out.(Model)
	}
	m = drainKey(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if _, _, ok := m.explorer().Selected(); !ok {
		t.Fatal("precondition: the tree must have a selection")
	}

	m = drainKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta})
	if n := m.explorer().ClipCount(); n != 1 {
		t.Fatalf("cmd+c in the explorer put %d entries on the file clipboard, want 1", n)
	}
	if m.explorer().ClipCut() {
		t.Error("cmd+c must arm copy mode, not cut")
	}
	if copied != "" {
		t.Errorf("the explorer's cmd+c must not touch the OS clipboard, wrote %q", copied)
	}

	// cmd+shift+c still copies the path — nothing was lost in the rebind.
	m = drainKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta | tea.ModShift})
	path, _, _ := m.explorer().Selected()
	if copied != path {
		t.Fatalf("cmd+shift+c copied %q, want the path %q", copied, path)
	}
}
