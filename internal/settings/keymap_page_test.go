package settings

import (
	"os"
	"path/filepath"
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
	k := NewKeymapPage(opts, func(string) bool { return true }, nil)
	return k, opts
}

// selectChord moves the selection onto the binding with the given chord.
func selectChord(t *testing.T, k *KeymapPage, chord string) keymap.Binding {
	t.Helper()
	for i, b := range k.rows() {
		if b.Chord.String() == chord {
			k.sel = i
			return b.Binding
		}
	}
	t.Fatalf("no binding with chord %q", chord)
	return keymap.Binding{}
}

func TestKeymapPageListsEffectiveBindings(t *testing.T) {
	k, _ := keymapPage(t)
	// Tall enough for the whole default table; the assertion is about the
	// listing, not pagination.
	v := k.View(120, 80)
	for _, want := range []string{"ctrl+s", "@default", "chord · command · context · layer"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	// Blocked-ledger ids are shown disabled with their reason, not hidden.
	// The real ledger emptied with 0320 (#466), so the rendering is
	// exercised through a stubbed entry.
	defer keymap.StubBlockedForTest("vcs.commit", "unit-test dependency")()
	if v := k.View(120, 80); !strings.Contains(v, "✗") {
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

// selectUnbound moves the selection onto the unbound row for a command (#736).
func selectUnbound(t *testing.T, k *KeymapPage, command string) keymapRow {
	t.Helper()
	for i, b := range k.rows() {
		if b.unbound && b.Command == command {
			k.sel = i
			return b
		}
	}
	t.Fatalf("no unbound row for command %q", command)
	return keymapRow{}
}

func TestUnboundCommandStaysListedAndRebinds(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	apply(t, k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}))
	// The command must remain reachable as an unbound row (#736).
	row := selectUnbound(t, k, "editor.write")
	if row.Chord.String() != "ctrl+s" {
		t.Fatalf("unbound row should keep the default chord, got %q", row.Chord.String())
	}
	if !strings.Contains(k.View(120, 80), "(unbound)") {
		t.Fatal("unbound row must render as (unbound)")
	}
	// enter starts a capture; a fresh chord binds the command again.
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !k.Capturing() {
		t.Fatal("enter on an unbound row must start chord capture")
	}
	k.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	apply(t, k.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	nb, ok := k.table().Lookup(keymap.MustParseChord("ctrl+o"), keymap.Editor)
	if !ok || nb.Command != "editor.write" {
		t.Fatalf("rebind from unbound must resolve, got %+v ok=%v", nb, ok)
	}
	// The default chord stays unbound — the user removed it deliberately.
	if _, ok := k.table().Lookup(keymap.MustParseChord("ctrl+s"), keymap.Global); ok {
		t.Fatal("default chord must stay unbound after rebinding elsewhere")
	}
}

func TestUnboundCommandResetRestoresDefault(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	apply(t, k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}))
	selectUnbound(t, k, "editor.write")
	// "r" on the unbound row removes the ""-override; the default falls back.
	apply(t, k.Update(tea.KeyPressMsg{Text: "r", Code: 'r'}))
	nb, ok := k.table().Lookup(keymap.MustParseChord("ctrl+s"), keymap.Editor)
	if !ok || nb.Command != "editor.write" {
		t.Fatalf("reset must restore the preset default, got %+v ok=%v", nb, ok)
	}
	for _, b := range k.rows() {
		if b.unbound && b.Command == "editor.write" {
			t.Fatal("unbound row must disappear after the reset")
		}
	}
}

func TestUnbindOnUnboundRowIsNoop(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	apply(t, k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}))
	selectUnbound(t, k, "editor.write")
	if cmd := k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}); cmd != nil {
		t.Fatal("unbind on an already-unbound row must be a no-op")
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
	k.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	if !k.Capturing() {
		t.Fatal("the open filter input must capture keys verbatim")
	}
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

// TestKeymapFilterActionLetters guards #531: filter text may contain the
// page's action letters (u/r/j/k) without firing them — "r" used to reset the
// selected binding mid-typing.
func TestKeymapFilterActionLetters(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	k.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	for _, r := range "rujk" {
		if cmd := k.Update(tea.KeyPressMsg{Text: string(r), Code: r}); cmd != nil {
			t.Fatalf("%q while filtering must not run an action", string(r))
		}
	}
	if k.filter != "rujk" {
		t.Fatalf("filter = %q, want %q", k.filter, "rujk")
	}
	// Enter keeps the filter and returns to the list; esc from the reopened
	// input clears it.
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if k.Capturing() || k.filter != "rujk" {
		t.Fatalf("enter must keep the filter, got capturing=%v filter=%q", k.Capturing(), k.filter)
	}
	k.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	k.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if k.Capturing() || k.filter != "" {
		t.Fatalf("esc must clear the filter, got capturing=%v filter=%q", k.Capturing(), k.filter)
	}
}

// TestKeymapActionsAfterFilter guards #531: after leaving the filter input the
// single-letter actions work on the filtered rows.
func TestKeymapActionsAfterFilter(t *testing.T) {
	k, _ := keymapPage(t)
	k.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	for _, r := range "write" {
		k.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	selectChord(t, k, "ctrl+s")
	if cmd := k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}); cmd == nil {
		t.Fatal("u after leaving the filter input must unbind")
	}
}

// TestKeymapDetailFooterPinnedAndScrolls guards #537: the detail line renders
// as a footer pinned to the window's last line, moving the selection does not
// shift the other rows, and the list scrolls so the selection stays visible.
func TestKeymapDetailFooterPinnedAndScrolls(t *testing.T) {
	k, _ := keymapPage(t)
	rows := k.rows()
	if len(rows) < 8 {
		t.Fatalf("default table too small for the test: %d rows", len(rows))
	}
	const h = 8
	lines := strings.Split(k.View(160, h), "\n")
	if len(lines) != h {
		t.Fatalf("view height = %d, want %d", len(lines), h)
	}
	if !strings.Contains(lines[h-2], rows[0].Command) { // 2-line wrapped footer (#553)
		t.Fatalf("footer must show the selected command:\n%s", strings.Join(lines, "\n"))
	}
	// Moving the selection must not shift unselected rows: line 3 (third
	// binding) is identical before and after one step down.
	before := lines[3]
	k.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	lines = strings.Split(k.View(160, h), "\n")
	if lines[3] != before {
		t.Fatalf("selection move shifted an unselected row:\n%q\n%q", before, lines[3])
	}
	// Walking to the last binding scrolls the list so it stays visible.
	for range rows {
		k.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	last := rows[len(rows)-1]
	v := k.View(160, h)
	if !strings.Contains(v, last.Chord.String()) {
		t.Fatalf("list must scroll to the selected binding %q:\n%s", last.Chord.String(), v)
	}
	if !strings.Contains(strings.Split(v, "\n")[h-2], last.Command) {
		t.Fatalf("footer must follow the selection:\n%s", v)
	}
}

// TestKeymapPageImportsJetBrainsXML drives the "i" import flow (#677): the
// inline path input captures keys, enter runs the import, the overrides land
// at user scope and the effective table re-resolves.
func TestKeymapPageImportsJetBrainsXML(t *testing.T) {
	k, opts := keymapPage(t)
	xml := `<keymap version="1" name="test">
  <action id="GotoDeclaration">
    <keyboard-shortcut first-keystroke="meta pressed B"/>
  </action>
  <action id="SomeUnknownAction">
    <keyboard-shortcut first-keystroke="meta pressed Y"/>
  </action>
</keymap>`
	path := filepath.Join(t.TempDir(), "export.xml")
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatal(err)
	}
	k.Update(key("i"))
	if !k.Capturing() {
		t.Fatal("import input must capture keys")
	}
	// Replace the seeded "~/" with the fixture's absolute path.
	k.importInput = ""
	for _, r := range path {
		k.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	apply(t, k.Update(key("enter")))
	if k.Capturing() {
		t.Fatal("import input must close on enter")
	}
	if !strings.Contains(k.importNote, "imported 1 binding") || !strings.Contains(k.importNote, "1 action(s) unmapped") {
		t.Fatalf("importNote = %q", k.importNote)
	}
	// The effective table now binds cmd+b to lsp.definition at the user layer
	// and the replaced f4 default is gone.
	var found bool
	for _, b := range k.table().Bindings() {
		switch b.Chord.String() {
		case "cmd+b":
			found = b.Command == "lsp.definition" && b.Layer == keymap.LayerUser
		case "f4":
			t.Fatalf("f4 default must be unbound after import: %+v", b)
		}
	}
	if !found {
		t.Fatal("cmd+b → lsp.definition override missing after import")
	}
	// The write landed in the user settings file.
	raw, err := os.ReadFile(opts.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"cmd+b"`) || !strings.Contains(string(raw), "lsp.definition") {
		t.Fatalf("user settings missing imported binding:\n%s", raw)
	}
}

// TestKeymapPageImportEscCancels ensures esc closes the input untouched.
func TestKeymapPageImportEscCancels(t *testing.T) {
	k, opts := keymapPage(t)
	k.Update(key("i"))
	if cmd := k.Update(key("esc")); cmd != nil {
		t.Fatal("esc must not write anything")
	}
	if k.Capturing() {
		t.Fatal("esc must close the import input")
	}
	if _, err := os.Stat(opts.UserPath); !os.IsNotExist(err) {
		t.Fatalf("no settings file expected, err=%v", err)
	}
}

// keymapPageWithCommands builds the page with a registry-command lister
// (#771): never-bound registered commands appear as "(no binding)" rows.
func keymapPageWithCommands(t *testing.T, cmds []CommandEntry) (*KeymapPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	opts := testOpts(t)
	k := NewKeymapPage(opts, func(string) bool { return true }, func() []CommandEntry { return cmds })
	return k, opts
}

func TestNeverBoundCommandsListedAndFilterable(t *testing.T) {
	k, _ := keymapPageWithCommands(t, []CommandEntry{
		{ID: "tool.lazygit", Title: "Tool: lazygit"},
		{ID: "editor.write", Title: "Save file"}, // already bound: no extra row
	})
	rows := k.rows()
	var found int
	for _, r := range rows {
		if r.Command == "tool.lazygit" {
			if !r.nobind {
				t.Fatal("tool.lazygit must be a nobind row")
			}
			found++
		}
	}
	if found != 1 {
		t.Fatalf("tool.lazygit rows = %d, want 1", found)
	}
	// A bound command must not gain a duplicate nobind row.
	for _, r := range rows {
		if r.Command == "editor.write" && r.nobind {
			t.Fatal("editor.write is bound; no nobind row expected")
		}
	}
	// Nobind rows sort last.
	if last := rows[len(rows)-1]; !last.nobind {
		t.Fatalf("nobind rows must trail the list, last = %+v", last)
	}
	// The / filter matches them by id and by the "no binding" keyword.
	for _, needle := range []string{"lazygit", "no binding"} {
		k.filter = needle
		ok := false
		for _, r := range k.rows() {
			if r.Command == "tool.lazygit" {
				ok = true
			}
		}
		if !ok {
			t.Fatalf("filter %q must match the nobind row", needle)
		}
	}
	k.filter = "lazygit"
	if v := k.View(160, 100); !strings.Contains(v, "(no binding)") {
		t.Fatalf("view must render the (no binding) chord cell:\n%s", v)
	}
}

func TestNeverBoundCommandCapturesFirstChord(t *testing.T) {
	k, _ := keymapPageWithCommands(t, []CommandEntry{{ID: "tool.lazygit", Title: "Tool: lazygit"}})
	rows := k.rows()
	for i, r := range rows {
		if r.Command == "tool.lazygit" {
			k.sel = i
		}
	}
	// u and r are no-ops on a never-bound row.
	if cmd := k.Update(tea.KeyPressMsg{Code: 'u'}); cmd != nil {
		t.Fatal("u must be a no-op on a never-bound row")
	}
	if cmd := k.Update(tea.KeyPressMsg{Code: 'r'}); cmd != nil {
		t.Fatal("r must be a no-op on a never-bound row")
	}
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !k.Capturing() {
		t.Fatal("enter must start chord capture on a never-bound row")
	}
	k.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl | tea.ModAlt})
	apply(t, k.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	nb, ok := k.table().Lookup(keymap.MustParseChord("ctrl+alt+g"), keymap.Global)
	if !ok || nb.Command != "tool.lazygit" {
		t.Fatalf("ctrl+alt+g must resolve to tool.lazygit, got %+v ok=%v", nb, ok)
	}
	// Once bound, the command shows as a normal user-layer row, not nobind.
	for _, r := range k.rows() {
		if r.Command == "tool.lazygit" && r.nobind {
			t.Fatal("bound command must lose its nobind row")
		}
	}
}
