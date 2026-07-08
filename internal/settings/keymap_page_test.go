package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/keymap"
)

func keymapPage(t *testing.T) (*KeymapPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	opts := testOpts(t)
	k := NewKeymapPage(opts, func(string) bool { return true })
	return k, opts
}

// selectChord moves the selection onto the binding with the given chord.
func selectChord(t *testing.T, k *KeymapPage, chord string) keymap.Binding {
	t.Helper()
	for i, b := range k.rows() {
		if b.Chord.String() == chord {
			k.sel = i
			return b
		}
	}
	t.Fatalf("no binding with chord %q", chord)
	return keymap.Binding{}
}

func TestKeymapPageListsEffectiveBindings(t *testing.T) {
	k, _ := keymapPage(t)
	v := k.View(120, 60)
	for _, want := range []string{"ctrl+s", "@default", "chord · command · context · layer"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	// Blocked-ledger ids are shown disabled with their reason, not hidden.
	if !strings.Contains(v, "✗") {
		t.Fatalf("blocked bindings must render disabled-with-reason:\n%s", v)
	}
}

func TestCaptureRebindWritesOverrideAndReResolves(t *testing.T) {
	k, opts := keymapPage(t)
	b := selectChord(t, k, "ctrl+s")
	if b.Command != "editor.write" {
		t.Fatalf("precondition: ctrl+s is editor.write, got %s", b.Command)
	}
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // start capture
	if !k.Capturing() {
		t.Fatal("enter must start chord capture")
	}
	k.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}) // ctrl+o
	cmd := k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})   // confirm
	apply(t, cmd)                                          // write + reload
	table := k.table()
	if _, ok := table.Lookup(keymap.MustParseChord("ctrl+s"), keymap.Global); ok {
		t.Fatal("old chord must be unbound after the rebind")
	}
	nb, ok := table.Lookup(keymap.MustParseChord("ctrl+o"), keymap.Global)
	if !ok || nb.Command != "editor.write" {
		t.Fatalf("ctrl+o must re-resolve to editor.write, got %+v ok=%v", nb, ok)
	}
	if nb.Layer != keymap.LayerUser {
		t.Fatalf("override must carry the user layer, got %v", nb.Layer)
	}
	if got := config.Origin(opts, "keymap.bindings.ctrl+o"); got != "user" {
		t.Fatalf("override origin = %q", got)
	}
}

func TestCaptureConflictNeedsConfirmation(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// Capture ctrl+z, which collides with editor.undo.
	k.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if cmd := k.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("conflicting capture must wait for confirmation")
	}
	if k.conflict != "editor.undo" {
		t.Fatalf("conflict should name editor.undo, got %q", k.conflict)
	}
	// Any non-enter key cancels.
	k.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if k.Capturing() || k.conflict != "" {
		t.Fatal("cancel must leave capture mode")
	}
	// Confirming (enter) writes the override.
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	k.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	apply(t, k.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	nb, ok := k.table().Lookup(keymap.MustParseChord("ctrl+z"), keymap.Editor)
	if !ok || nb.Command != "editor.write" {
		t.Fatalf("confirmed override must win, got %+v ok=%v", nb, ok)
	}
}

func TestUnbindAndResetRoundTrip(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	apply(t, k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}))
	if _, ok := k.table().Lookup(keymap.MustParseChord("ctrl+s"), keymap.Global); ok {
		t.Fatal("unbind must drop the chord")
	}
	// Reset removes the override; the preset default falls back through the
	// layers (the same RemoveAndReload the page's 'r' key issues).
	apply(t, config.RemoveAndReload(k.opts, config.UserScope, "keymap.bindings.ctrl+s"))
	nb, ok := k.table().Lookup(keymap.MustParseChord("ctrl+s"), keymap.Editor)
	if !ok || nb.Command != "editor.write" {
		t.Fatal("reset must restore the preset default")
	}
}

func TestFragileWarningOnCapture(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	k.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModSuper}) // cmd+s
	if k.warn == "" {
		t.Fatal("capturing a cmd chord must raise the honesty warning")
	}
	if !strings.Contains(k.View(120, 60), "⚠") {
		t.Fatal("warning must render")
	}
}

func TestKeymapFilter(t *testing.T) {
	k, _ := keymapPage(t)
	for _, r := range "comment" {
		k.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	rows := k.rows()
	if len(rows) == 0 {
		t.Fatal("filter should match the comment bindings")
	}
	for _, b := range rows {
		if !strings.Contains(b.Command+b.Title, "omment") {
			t.Fatalf("filter leaked %q", b.Command)
		}
	}
}
