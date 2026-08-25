package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/keymap"
	"ike/internal/lang"
)

// keymap_lang_test.go covers the language-scoped keep-both flow (#1876): the
// capture dialog's "keep both, limit to file type" writes an
// editor[<lang>]-qualified override, validated against the language registry.

// TestConflictOffersKeepBothByFileType: the file-type narrowing is offered for
// rows that can live under editor[<lang>] — Global or editor-scoped, not
// already language-scoped — and hidden elsewhere.
func TestConflictOffersKeepBothByFileType(t *testing.T) {
	k, _ := keymapPage(t)
	host := &stubHost{}
	host.stack = append(host.stack, nil)
	c := newKeymapCapture(k, host, keymapRow{Binding: keymap.Binding{
		Command: "palette.keymapHelp", Context: keymap.Global,
	}})
	c.conflict = "palette.recentFiles"
	c.other = keymap.Binding{Command: "palette.recentFiles", Context: keymap.Global}

	if !c.canKeepByLang() {
		t.Fatal("a global row must be narrowable to a file type")
	}
	labels := map[string]bool{}
	for _, btn := range c.Buttons() {
		labels[btn.Label] = true
	}
	if !labels["Keep both, limit to file type"] {
		t.Fatalf("conflict buttons = %v, want the limit-to-file-type option", labels)
	}
	// An editor-scoped row narrows too; a terminal-scoped or already
	// language-scoped one does not.
	c.row.Context = keymap.Editor
	if !c.canKeepByLang() {
		t.Fatal("an editor row must be narrowable to a file type")
	}
	c.row.Context = keymap.Terminal
	if c.canKeepByLang() {
		t.Fatal("a terminal row cannot live under editor[lang]")
	}
	c.row.Context = keymap.WithLang(keymap.Editor, "http")
	if c.canKeepByLang() {
		t.Fatal("an already language-scoped row must not narrow again")
	}
}

// TestKeepBothByFileTypeWritesLangQualifiedOverride is the settings half of the
// #1876 acceptance: taking the file-type option writes
// keymap.bindings.editor[<lang>].<chord>, the chord runs the new command only
// in editors of that language and the colliding command everywhere else.
func TestKeepBothByFileTypeWritesLangQualifiedOverride(t *testing.T) {
	lang.Register(lang.Language{ID: "langscope", Extensions: []string{"langscope"}})
	k, opts := keymapPage(t)

	b := selectChord(t, k, "f1") // palette.keymapHelp, Global
	if b.Context != keymap.Global {
		t.Fatalf("f1 should be global, got %q", b.Context)
	}
	c := newKeymapCapture(k, k.host.(*stubHost), keymapRow{Binding: b})
	c.steps = keymap.MustParseChord("cmd+e").Steps // collides with palette.recentFiles
	if cmd := c.confirm(); cmd != nil {
		t.Fatal("the collision must wait for a decision")
	}
	if c.canKeepBoth() {
		t.Fatal("two global bindings are not separable by context")
	}
	if !c.canKeepByLang() {
		t.Fatal("the file-type narrowing must be on offer")
	}

	// "l" opens the language input; a bad id is rejected with a message, a
	// registered one commits.
	c.Update(tea.KeyPressMsg{Text: "l", Code: 'l'})
	if !c.langMode {
		t.Fatal("l must open the language input")
	}
	c.langField.Set("no such lang")
	if cmd := c.commitLang(); cmd != nil || c.langErr == "" {
		t.Fatalf("a malformed id must be rejected, err = %q", c.langErr)
	}
	c.langField.Set("unregistered")
	if cmd := c.commitLang(); cmd != nil || !strings.Contains(c.langErr, "unknown language") {
		t.Fatalf("an unregistered id must be rejected, err = %q", c.langErr)
	}
	c.langField.Set("langscope")
	cmd := c.commitLang()
	if cmd == nil {
		t.Fatalf("a registered id must commit, err = %q", c.langErr)
	}
	apply(t, cmd)

	table := k.table()
	chord := keymap.MustParseChord("cmd+e")
	if nb, ok := table.Lookup(chord, keymap.WithLang(keymap.Editor, "langscope")); !ok || nb.Command != "palette.keymapHelp" {
		t.Fatalf("editor[langscope] cmd+e = %+v ok=%v, want palette.keymapHelp", nb, ok)
	}
	for _, active := range []keymap.Context{keymap.Editor, keymap.WithLang(keymap.Editor, "go"), keymap.Explorer} {
		if nb, ok := table.Lookup(chord, active); !ok || nb.Command != "palette.recentFiles" {
			t.Fatalf("%q cmd+e = %+v ok=%v, want the untouched palette.recentFiles", active, nb, ok)
		}
	}
	if got := config.Origin(opts, "keymap.bindings.editor[langscope].cmd+e"); got != "user" {
		t.Fatalf("the qualified override must persist, origin = %q", got)
	}
	// The rebound command's old chord is released.
	if _, ok := table.Lookup(keymap.MustParseChord("f1"), keymap.Global); ok {
		t.Fatal("the old chord must be unbound for the rebound command")
	}
}

// TestLangScopedRowRoundTripsThroughPage: a language-scoped binding shows on
// the page with its qualified context, "u" unbinds it through the qualified
// key (sparing the broader levels) and "r" resets it.
func TestLangScopedRowRoundTripsThroughPage(t *testing.T) {
	k, opts := keymapPage(t)
	apply(t, config.WriteAndReload(opts, config.UserScope, "keymap.bindings.editor[http].cmd+e", "http.selectEnvironment"))

	found := false
	for i, r := range k.rows() {
		if r.Command == "http.selectEnvironment" && r.Context == keymap.WithLang(keymap.Editor, "http") {
			k.sel, found = i, true
			break
		}
	}
	if !found {
		t.Fatal("the language-scoped binding must be listed")
	}
	v := k.renderRow(k.rows()[k.sel], false, 120)
	if !strings.Contains(v, "[editor[http]]") {
		t.Fatalf("row must carry the qualified scope tag: %q", v)
	}

	apply(t, unbind(t, k))
	table := k.table()
	// The language-scoped binding is gone: an http-focused editor falls back
	// to the untouched global default.
	for _, active := range []keymap.Context{keymap.WithLang(keymap.Editor, "http"), keymap.Global} {
		if nb, ok := table.Lookup(keymap.MustParseChord("cmd+e"), active); !ok || nb.Command != "palette.recentFiles" {
			t.Fatalf("%q cmd+e = %+v ok=%v, want palette.recentFiles", active, nb, ok)
		}
	}
}

// TestSeparableContextsLangLevel: the language level's overlap rules — the
// basis for both "keep both" offers and the detail column's wording.
func TestSeparableContextsLangLevel(t *testing.T) {
	http := keymap.WithLang(keymap.Editor, "http")
	golang := keymap.WithLang(keymap.Editor, "go")
	cases := []struct {
		a, b keymap.Context
		want bool
	}{
		{http, golang, true},          // disjoint languages
		{http, keymap.Editor, false},  // narrows its own pane
		{http, keymap.Global, false},  // global overlaps everything
		{http, keymap.Diff, true},     // different panes
		{http, http, false},           // same scope
		{keymap.Editor, keymap.Explorer, true}, // the #1312 base case
	}
	for _, c := range cases {
		if got := separableContexts(c.a, c.b); got != c.want {
			t.Errorf("separableContexts(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
		if got := separableContexts(c.b, c.a); got != c.want {
			t.Errorf("separableContexts(%q, %q) = %v, want %v (symmetry)", c.b, c.a, got, c.want)
		}
	}
}
