package settings

import (
	"charm.land/lipgloss/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/keymap"
)

// capturePanel fetches the open chord-capture sub-panel (#892).
func capturePanel(t *testing.T, k *KeymapPage) *keymapCapture {
	t.Helper()
	c, ok := k.host.(*stubHost).top().(*keymapCapture)
	if !ok {
		t.Fatal("expected an open capture sub-panel")
	}
	return c
}

// unbind drives "u" through the confirmation sub-panel (#891).
func unbind(t *testing.T, k *KeymapPage) tea.Cmd {
	t.Helper()
	if cmd := k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}); cmd != nil {
		return cmd
	}
	return confirmVia(t, k.host.(*stubHost))
}

func keymapPage(t *testing.T) (*KeymapPage, config.Options) {
	t.Helper()
	restoreConfig(t)
	opts := testOpts(t)
	k := NewKeymapPage(opts, func(string) bool { return true }, nil)
	k.SetSubPanelHost(&stubHost{})
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
	selectChord(t, k, "ctrl+s") // a plain binding, not a folded run (#1300)
	v = k.View(120, 80)
	// Context, layer and provenance moved to the detail column (#1298).
	for _, want := range []string{"ctrl+s", "@default", "chord · command", "bindings ·"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
	// Blocked-ledger ids are shown disabled with their reason, not hidden.
	// The real ledger emptied with 0320 (#466), so the rendering is
	// exercised through a stubbed entry.
	defer keymap.StubBlockedForTest("vcs.revertFile", "unit-test dependency")()
	found := false
	for i, r := range k.rows() {
		if r.Command == "vcs.revertFile" {
			k.sel, found = i, true
			break
		}
	}
	if !found {
		t.Fatal("the stubbed blocked command must be listed")
	}
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
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // push the capture
	c := capturePanel(t, k)
	c.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}) // ctrl+o
	cmd := c.Update(tea.KeyPressMsg{Code: tea.KeyEnter})   // confirm
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
	c := capturePanel(t, k)
	// Capture ctrl+z, which collides with editor.undo.
	c.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if cmd := c.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("conflicting capture must wait for confirmation")
	}
	if c.conflict != "editor.undo" {
		t.Fatalf("conflict should name editor.undo, got %q", c.conflict)
	}
	// Any non-enter key cancels (pops the capture).
	c.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if k.host.(*stubHost).top() != nil {
		t.Fatal("cancel must close the capture")
	}
	// Confirming (enter) writes the override.
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	c = capturePanel(t, k)
	c.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	c.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	apply(t, c.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	nb, ok := k.table().Lookup(keymap.MustParseChord("ctrl+z"), keymap.Editor)
	if !ok || nb.Command != "editor.write" {
		t.Fatalf("confirmed override must win, got %+v ok=%v", nb, ok)
	}
}

func TestUnbindAndResetRoundTrip(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	apply(t, unbind(t, k))
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
	apply(t, unbind(t, k))
	// The command must remain reachable as an unbound row (#736).
	row := selectUnbound(t, k, "editor.write")
	if row.Chord.String() != "ctrl+s" {
		t.Fatalf("unbound row should keep the default chord, got %q", row.Chord.String())
	}
	if !strings.Contains(k.View(120, 80), "(unbound)") {
		t.Fatal("unbound row must render as (unbound)")
	}
	// enter pushes a capture; a fresh chord binds the command again.
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	c := capturePanel(t, k)
	c.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	apply(t, c.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
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
	apply(t, unbind(t, k))
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
	apply(t, unbind(t, k))
	selectUnbound(t, k, "editor.write")
	if cmd := k.Update(tea.KeyPressMsg{Text: "u", Code: 'u'}); cmd != nil {
		t.Fatal("unbind on an already-unbound row must be a no-op")
	}
}

func TestFragileWarningOnCapture(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	c, ok := k.host.(*stubHost).top().(*keymapCapture)
	if !ok {
		t.Fatal("enter must push the capture sub-panel")
	}
	c.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModSuper}) // cmd+s
	if c.warn == "" {
		t.Fatal("capturing a cmd chord must raise the honesty warning")
	}
	if !strings.Contains(c.View(120, 10), "⚠") {
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
	if cmd := unbind(t, k); cmd == nil {
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
	// binding) is identical before and after one step down. Only the
	// settings column is compared — the detail column is *supposed* to
	// change with the selection (#1298).
	listCol := func(line string) string {
		return stripANSI(lipgloss.NewStyle().MaxWidth(k.listW).Render(line))
	}
	before := listCol(lines[3])
	k.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	lines = strings.Split(k.View(160, h), "\n")
	if listCol(lines[3]) != before {
		t.Fatalf("selection move shifted an unselected row:\n%q\n%q", before, listCol(lines[3]))
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
	imp, ok := k.host.(*stubHost).top().(*keymapImport)
	if !ok {
		t.Fatal("i must push the import sub-panel")
	}
	for _, r := range path {
		imp.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	apply(t, imp.Update(key("enter")))
	if k.host.(*stubHost).top() != nil {
		t.Fatal("import must close on enter")
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
	k.SetSubPanelHost(&stubHost{})
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
	c := capturePanel(t, k)
	c.Update(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl | tea.ModAlt})
	apply(t, c.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
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

// --- the grid (0460, #1298) ---

// TestKeymapDetailColumnExplainsTheCommand: browsing without editing, the
// third column names the command, lists every binding with context and
// provenance, and reports the conflict state.
func TestKeymapDetailColumnExplainsTheCommand(t *testing.T) {
	k, _ := keymapPage(t)
	b := selectChord(t, k, "ctrl+s")
	v := k.View(130, 40)
	for _, want := range []string{b.Command, "bindings ·", "@default", "no conflicts"} {
		if !strings.Contains(v, want) {
			t.Fatalf("detail column missing %q:\n%s", want, v)
		}
	}
}

// TestKeymapDetailReportsCollisions: two commands on one chord is named in the
// detail column, with free chords offered.
func TestKeymapDetailReportsCollisions(t *testing.T) {
	k, opts := keymapPage(t)
	b := selectChord(t, k, "ctrl+s")
	// Bind a second command to the same chord behind the page's back.
	if err := config.WriteKey(opts, config.UserScope, "keymap.bindings."+b.Chord.String(), "editor.write"); err != nil {
		t.Fatal(err)
	}
	if free := k.suggestChords(2); len(free) != 2 {
		t.Fatalf("suggestChords must offer unbound chords, got %v", free)
	}
	for _, c := range k.suggestChords(3) {
		for _, bound := range k.table().Bindings() {
			if bound.Chord.String() == c {
				t.Fatalf("suggested chord %q is already bound to %s", c, bound.Command)
			}
		}
	}
}

// TestKeymapGridSplitsColumns: a wide page splits into table + detail, a
// narrow one keeps the single column.
func TestKeymapGridSplitsColumns(t *testing.T) {
	listW, detailW, side := splitGrid(130)
	if !side || listW != formWidth || detailW != 130-formWidth-sepWidth {
		t.Fatalf("wide split = %d/%d side=%v", listW, detailW, side)
	}
	if _, _, side := splitGrid(40); side {
		t.Fatal("a narrow page must not split")
	}
	k, _ := keymapPage(t)
	if v := k.View(40, 20); strings.Contains(v, "│") {
		t.Fatalf("a narrow keymap page must render one column:\n%s", v)
	}
}

// TestKeymapDetailClicksAreInert: the detail column is read-only chrome — a
// press there must not move the table's selection.
func TestKeymapDetailClicksAreInert(t *testing.T) {
	k, _ := keymapPage(t)
	k.View(130, 40) // records the column widths
	selectChord(t, k, "ctrl+s")
	before := k.sel
	k.Click(k.listW+5, 3)
	if k.sel != before {
		t.Fatalf("sel = %d, want it unchanged at %d", k.sel, before)
	}
}

// TestConflictOffersResolutions guards #1298: a colliding chord is a decision
// — replace, or record a different chord — not a bare yes/no.
func TestConflictOffersResolutions(t *testing.T) {
	k, _ := keymapPage(t)
	b := selectChord(t, k, "ctrl+s")
	host := &stubHost{}
	host.stack = append(host.stack, nil) // the capture level itself
	c := newKeymapCapture(k, host, keymapRow{Binding: b})
	c.conflict = "some.other.command"

	labels := map[string]bool{}
	for _, btn := range c.Buttons() {
		labels[btn.Label] = true
	}
	for _, want := range []string{"Replace & unbind other", "Pick a different chord", "Cancel"} {
		if !labels[want] {
			t.Fatalf("conflict buttons = %v, want %q", labels, want)
		}
	}
	// "p" clears the recorded chord and returns to capture rather than
	// writing anything.
	if cmd := c.Update(key("p")); cmd != nil {
		t.Fatal("picking a different chord must not write")
	}
	if c.conflict != "" || len(c.steps) != 0 {
		t.Fatalf("after p: conflict=%q steps=%v", c.conflict, c.steps)
	}
	if len(host.stack) == 0 {
		t.Fatal("picking a different chord must stay in the capture")
	}
}

// --- numbered-run folding (0460, #1300) ---

// findRange returns the folded row for a chord prefix, if the list has one.
func findRange(k *KeymapPage, chordPrefix string) (keymapRow, int, bool) {
	for i, r := range k.rows() {
		if r.rangeKey != "" && strings.HasPrefix(r.Chord.String(), chordPrefix) {
			return r, i, true
		}
	}
	return keymapRow{}, 0, false
}

// TestNumberedBindingsFoldIntoOneRow: alt+1 … alt+9 is one fact, not nine
// lines.
func TestNumberedBindingsFoldIntoOneRow(t *testing.T) {
	k, _ := keymapPage(t)
	row, idx, ok := findRange(k, "alt+")
	if !ok {
		t.Skip("the default preset has no numbered run to fold")
	}
	if row.rangeCount < minRange {
		t.Fatalf("a folded run must stand for at least %d bindings, got %d", minRange, row.rangeCount)
	}
	chord, label := row.rangeLabel()
	if !strings.Contains(chord, "…") || !strings.Contains(label, "–") {
		t.Fatalf("range label = %q / %q, want a spanned chord and title", chord, label)
	}
	if v := k.View(130, 40); !strings.Contains(v, "▸") {
		t.Fatalf("a folded run must advertise that it opens:\n%s", v)
	}

	// z unfolds it in place; the rows it stood for are now individual.
	before := len(k.rows())
	k.sel = idx
	k.Update(tea.KeyPressMsg{Text: "z", Code: 'z'})
	after := len(k.rows())
	if after != before+row.rangeCount-1 {
		t.Fatalf("unfolding must add the run's rows: %d → %d (run of %d)", before, after, row.rangeCount)
	}
	// …and the fold state survives (it is remembered per page for the session).
	if len(k.rows()) != after {
		t.Fatal("the unfolded state must persist across rows() calls")
	}
	// z again folds it back.
	k.Update(tea.KeyPressMsg{Text: "z", Code: 'z'})
	if len(k.rows()) != before {
		t.Fatalf("z must fold the run again, got %d rows want %d", len(k.rows()), before)
	}
}

// TestFoldedRunDescribesItself: the detail column lists what the folded row
// stands for.
func TestFoldedRunDescribesItself(t *testing.T) {
	k, _ := keymapPage(t)
	row, idx, ok := findRange(k, "alt+")
	if !ok {
		t.Skip("the default preset has no numbered run to fold")
	}
	k.sel = idx
	v := k.View(130, 40)
	if !strings.Contains(v, strconv.Itoa(row.rangeCount)+" bindings") {
		t.Fatalf("detail must count the run:\n%s", v)
	}
	if got := k.rangeBindings(row); len(got) != row.rangeCount {
		t.Fatalf("rangeBindings = %d, want %d", len(got), row.rangeCount)
	}
}

// TestSearchNeverFolds: a filter must show exactly what it matched.
func TestSearchNeverFolds(t *testing.T) {
	k, _ := keymapPage(t)
	if _, _, ok := findRange(k, "alt+"); !ok {
		t.Skip("the default preset has no numbered run to fold")
	}
	k.filter = "tab"
	for _, r := range k.rows() {
		if r.rangeKey != "" {
			t.Fatalf("a filtered list must not fold, found %q", r.Chord.String())
		}
	}
}

// TestShortRunsDoNotFold: folding two rows into one plus a caption saves
// nothing, so it does not happen.
func TestShortRunsDoNotFold(t *testing.T) {
	if _, _, ok := splitTrailingDigits("alt+"); ok {
		t.Fatal("a chord without a trailing number must not split")
	}
	if p, n, ok := splitTrailingDigits("alt+12"); !ok || p != "alt+" || n != 12 {
		t.Fatalf("splitTrailingDigits(alt+12) = %q,%d,%v", p, n, ok)
	}
	if _, _, ok := splitTrailingDigits("123"); ok {
		t.Fatal("an all-digit string has no prefix to fold on")
	}
}

// TestCapturePlusChord guards #1331: a "+" press records like any other key
// and the written binding parses back, instead of leaving the field empty.
func TestCapturePlusChord(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	c := capturePanel(t, k)
	c.Update(tea.KeyPressMsg{Code: '+', Mod: tea.ModSuper}) // cmd++
	if got := c.chord().String(); got != "cmd++" {
		t.Fatalf("captured chord = %q, want %q", got, "cmd++")
	}
	apply(t, c.Update(tea.KeyPressMsg{Code: tea.KeyEnter}))
	nb, ok := k.table().Lookup(keymap.MustParseChord("cmd++"), keymap.Global)
	if !ok || nb.Command != "editor.write" {
		t.Fatalf("cmd++ must resolve to editor.write, got %+v ok=%v", nb, ok)
	}
}

// TestCaptureUnrecordableKeyWarns guards #1331: a press the chord format
// cannot carry is reported, never silently dropped.
func TestCaptureUnrecordableKeyWarns(t *testing.T) {
	k, _ := keymapPage(t)
	selectChord(t, k, "ctrl+s")
	k.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	c := capturePanel(t, k)
	c.Update(tea.KeyPressMsg{Text: "a_b", Code: 'a'}) // underscore bases are rejected
	if len(c.steps) != 0 {
		t.Fatalf("unrecordable key must not become a step: %+v", c.steps)
	}
	if c.warn == "" || !strings.Contains(c.View(120, 10), "⚠") {
		t.Fatalf("unrecordable key must warn, warn = %q", c.warn)
	}
}

// --- context-qualified overrides (0460, #1312) ---

// TestConflictOffersKeepBothWhenContextsAreDisjoint: two pane-scoped commands
// can share a chord, so the dialog offers the third resolution — and hides it
// when either side is global and would shadow the other.
func TestConflictOffersKeepBothWhenContextsAreDisjoint(t *testing.T) {
	k, _ := keymapPage(t)
	host := &stubHost{}
	host.stack = append(host.stack, nil) // the capture level itself
	c := newKeymapCapture(k, host, keymapRow{Binding: keymap.Binding{
		Command: "editor.write", Context: keymap.Editor,
	}})
	c.conflict = "explorer.delete"
	c.other = keymap.Binding{Command: "explorer.delete", Context: keymap.Explorer}

	if !c.canKeepBoth() {
		t.Fatal("editor vs explorer must be separable by context")
	}
	labels := map[string]bool{}
	for _, btn := range c.Buttons() {
		labels[btn.Label] = true
	}
	if !labels["Keep both, resolve by context"] {
		t.Fatalf("conflict buttons = %v, want the keep-both option", labels)
	}
	// A global counterpart matches in every pane: keeping both would just
	// shadow one of them, so the option is hidden.
	c.other = keymap.Binding{Command: "app.quit", Context: keymap.Global}
	if c.canKeepBoth() {
		t.Fatal("a global binding overlaps every context")
	}
	for _, btn := range c.Buttons() {
		if btn.Label == "Keep both, resolve by context" {
			t.Fatal("the keep-both option must be hidden for overlapping contexts")
		}
	}
}

// TestKeepBothWritesContextQualifiedOverride: taking "keep both" writes the
// context-qualified key, and both commands stay reachable — each in its pane.
func TestKeepBothWritesContextQualifiedOverride(t *testing.T) {
	k, opts := keymapPage(t)
	// An explorer-only command on an otherwise free chord, to collide with.
	free := k.suggestChords(1)[0]
	apply(t, config.WriteAndReload(opts, config.UserScope, "keymap.bindings.explorer."+free, "explorer.thing"))

	b := selectChord(t, k, "ctrl+s") // an editor-scoped command
	if b.Context != keymap.Editor {
		t.Fatalf("ctrl+s should be editor-scoped, got %q", b.Context)
	}
	c := newKeymapCapture(k, k.host.(*stubHost), keymapRow{Binding: b})
	c.steps = keymap.MustParseChord(free).Steps
	if cmd := c.confirm(); cmd != nil {
		t.Fatal("the collision must wait for a decision")
	}
	if !c.canKeepBoth() {
		t.Fatalf("editor vs explorer must be separable, other = %+v", c.other)
	}
	apply(t, c.keepBoth())

	table := k.table()
	if nb, ok := table.Lookup(keymap.MustParseChord(free), keymap.Editor); !ok || nb.Command != b.Command {
		t.Fatalf("editor %s = %+v ok=%v, want %s", free, nb, ok, b.Command)
	}
	if nb, ok := table.Lookup(keymap.MustParseChord(free), keymap.Explorer); !ok || nb.Command != "explorer.thing" {
		t.Fatalf("explorer %s = %+v ok=%v, want the untouched explorer.thing", free, nb, ok)
	}
	if got := config.Origin(opts, "keymap.bindings.editor."+free); got != "user" {
		t.Fatalf("the qualified override must persist, origin = %q", got)
	}
	// The rebound command's old chord is released in its context only.
	if _, ok := table.Lookup(keymap.MustParseChord("ctrl+s"), keymap.Editor); ok {
		t.Fatal("the old chord must be unbound for the rebound command")
	}
}

// TestDetailShowsBothBindingsWithContexts: a chord shared by two contexts is
// reported as resolved-by-context, naming both commands and their panes.
func TestDetailShowsBothBindingsWithContexts(t *testing.T) {
	k, opts := keymapPage(t)
	b := selectChord(t, k, "ctrl+s")
	apply(t, config.WriteAndReload(opts, config.UserScope, "keymap.bindings.explorer."+b.Chord.String(), "explorer.thing"))
	selectChord(t, k, "ctrl+s")

	v := k.View(130, 40)
	for _, want := range []string{"resolved by context", "explorer.thing", "explorer", "editor"} {
		if !strings.Contains(v, want) {
			t.Fatalf("detail column missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "free:") {
		t.Fatalf("a context-resolved chord needs no replacement suggestions:\n%s", v)
	}
}

// TestUnbindOfASharedChordSparesTheOtherContext: "u" on one half of a
// keep-both pair writes the qualified unbind, so the other half survives.
func TestUnbindOfASharedChordSparesTheOtherContext(t *testing.T) {
	k, opts := keymapPage(t)
	b := selectChord(t, k, "ctrl+s")
	apply(t, config.WriteAndReload(opts, config.UserScope, "keymap.bindings.explorer."+b.Chord.String(), "explorer.thing"))
	selectChord(t, k, "ctrl+s")
	apply(t, unbind(t, k))

	if _, ok := k.table().Lookup(keymap.MustParseChord("ctrl+s"), keymap.Editor); ok {
		t.Fatal("the editor binding must be gone")
	}
	if nb, ok := k.table().Lookup(keymap.MustParseChord("ctrl+s"), keymap.Explorer); !ok || nb.Command != "explorer.thing" {
		t.Fatalf("explorer ctrl+s = %+v ok=%v, want explorer.thing untouched", nb, ok)
	}
}

// The row list and the binding table are memoized (#1396): rebuilding both on
// every key event and render frame is what froze the settings panel. The cache
// must survive repeated reads and drop on exactly the inputs it derives from —
// the live config, the filter text and the fold state.
func TestKeymapRowsAndTableAreMemoized(t *testing.T) {
	k, _ := keymapPage(t)
	a, b := k.rows(), k.rows()
	if len(a) == 0 {
		t.Fatal("expected default bindings")
	}
	if &a[0] != &b[0] {
		t.Fatal("rows must be served from the cache between reads")
	}
	if k.table() != k.table() {
		t.Fatal("table must be served from the cache between reads")
	}

	// A filter change rebuilds the rows.
	k.Update(tea.KeyPressMsg{Text: "/", Code: '/'})
	k.Update(tea.KeyPressMsg{Text: "w", Code: 'w'})
	filtered := k.rows()
	if len(filtered) >= len(a) {
		t.Fatalf("filter must narrow the list: %d -> %d", len(a), len(filtered))
	}
	k.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	// A fold toggle rebuilds the rows.
	before := len(k.rows())
	var folded bool
	for i, r := range k.rows() {
		if r.rangeKey != "" {
			k.sel, folded = i, true
			break
		}
	}
	if folded {
		k.toggleRange()
		if len(k.rows()) == before {
			t.Fatal("expanding a fold must rebuild the rows")
		}
	}

	// A config reload (pointer swap) rebuilds table and rows.
	tbl := k.table()
	cfg := *config.Get()
	config.Set(&cfg)
	if k.table() == tbl {
		t.Fatal("a config reload must rebuild the table")
	}
}
