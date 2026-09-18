package app

// diff_copychord_test.go covers diff.copy (#2628): cmd+c in the diff viewer
// is a listed binding rather than a pane secret, so the chord resolves
// through the keymap layer instead of being recorded unbound while the pane
// quietly handled it — and it still copies what the pane-local key copies.

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/keymap"
	"ike/internal/pane"
	"ike/internal/telemetry"
)

// diffPaneApp opens a two-file diff and leaves its pane focused.
func diffPaneApp(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	left := filepath.Join(dir, "l.txt")
	right := filepath.Join(dir, "r.txt")
	os.WriteFile(left, []byte("alpha\nbeta\ngamma\n"), 0o644)
	os.WriteFile(right, []byte("alpha\nbetaX\ngamma\n"), 0o644)

	m := newSized()
	// The first-start LSP dialog owns the keyboard while it is up (#301) and
	// would swallow the chord under test; esc is what a user presses too.
	for m.onboardingOpen() {
		out, _ := m.updateOnboarding(tea.KeyPressMsg{Code: tea.KeyEscape})
		m = out.(Model)
	}
	m.openDiffPane(left, right)
	inst := m.activeWS().Panes.FocusedInstance()
	if inst == nil || inst.Kind() != pane.KindDiff {
		t.Fatal("precondition: the diff pane must have focus")
	}
	if got := m.focusContext(); got != string(keymap.Diff) {
		t.Fatalf("precondition: focus context = %q, want diff", got)
	}
	return m
}

// TestDiffCopyCommandRegistered: diff.copy must be a registry command, or the
// binding is inert and the palette never lists it.
func TestDiffCopyCommandRegistered(t *testing.T) {
	m := newSized()
	if _, ok := m.reg.Command("diff.copy"); !ok {
		t.Fatal("diff.copy must be a registry command")
	}
}

// TestDiffCopyChordBound: cmd+c resolves to diff.copy in the diff context on
// both platforms — off darwin it folds onto ctrl+c like every cmd row — and
// no ctrl+c secondary is added on darwin, where it stays the quit chord.
func TestDiffCopyChordBound(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		table := keymap.BuildTable(keymap.DefaultsFor(keymap.PresetJetBrains, goos), nil, goos)
		chord := keymap.NormalizeChord(keymap.MustParseChord("cmd+c"), goos)
		if b, ok := table.Lookup(chord, keymap.Diff); !ok || b.Command != "diff.copy" {
			t.Errorf("%s: cmd+c in the diff context = %+v ok=%v, want diff.copy", goos, b, ok)
		}
	}
	table := keymap.BuildTable(keymap.DefaultsFor(keymap.PresetJetBrains, "darwin"), nil, "darwin")
	if b, ok := table.Lookup(keymap.MustParseChord("ctrl+c"), keymap.Diff); ok {
		t.Errorf("ctrl+c in the diff context must stay unbound, got %s", b.Command)
	}
}

// TestDiffCopyChordNotUnbound: the press resolves, so the usage log records
// it as a resolved diff.copy and never as an unbound chord (the telemetry
// signal this issue came from).
func TestDiffCopyChordNotUnbound(t *testing.T) {
	pinDarwin(t)
	stubClipboard(t, "")
	m := diffPaneApp(t)

	m = drainKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta})
	if m.pendUnbound != nil {
		t.Fatalf("cmd+c left a pending unbound event: %+v", *m.pendUnbound)
	}
	resolved := false
	for _, ev := range usageEvents(t, m) {
		if ev.Type != telemetry.TypeKey {
			continue
		}
		if ev.Data["status"] == "unbound" && ev.Data["context"] == string(keymap.Diff) {
			t.Fatalf("cmd+c in the diff context recorded unbound: %+v", ev.Data)
		}
		if ev.Data["status"] == "resolved" && ev.Data["command"] == "diff.copy" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("no resolved diff.copy key event recorded")
	}
}

// TestDiffCopyChordCopiesSelection: a live selection reaches the clipboard
// through the shared diff.CopyMsg path, toast included.
func TestDiffCopyChordCopiesSelection(t *testing.T) {
	pinDarwin(t)
	copied := stubClipboard(t, "")
	m := diffPaneApp(t)

	d := m.activeWS().Panes.FocusedInstance().Diff()
	d.View() // build the visual-line map the selection resolves against
	d.MousePress(0, 0)
	d.MouseDrag(200, 10)
	d.MouseRelease()
	if !d.HasSelection() {
		t.Fatal("precondition: the drag must leave a selection")
	}
	want := d.SelectionText()

	m = drainKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta})
	if want == "" || *copied != want {
		t.Fatalf("cmd+c copied %q, want the selection %q", *copied, want)
	}
	if m.activeWS().Panes.FocusedInstance().Diff().HasSelection() {
		t.Error("copying the selection must clear it, like the pane key does")
	}
}

// TestDiffCopyChordCopiesHunk: with no selection the chord copies the current
// hunk as a unified patch — the pane key's fallback.
func TestDiffCopyChordCopiesHunk(t *testing.T) {
	pinDarwin(t)
	copied := stubClipboard(t, "")
	m := diffPaneApp(t)

	m = drainKey(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModMeta})
	if *copied == "" {
		t.Fatal("cmd+c without a selection must copy the current hunk")
	}
	_ = m
}

// TestDiffCopyCommandWithoutPane: the command is reachable from the palette
// with no diff focused; it must say so rather than copy nothing.
func TestDiffCopyCommandWithoutPane(t *testing.T) {
	copied := stubClipboard(t, "")
	m := newSized()
	m.setFocus(pane.ExplorerKey)
	m = dispatch(t, m, DiffCopyMsg{})
	if *copied != "" {
		t.Fatalf("no diff focused: clipboard written with %q", *copied)
	}
}
