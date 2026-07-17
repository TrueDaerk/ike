package jbimport

import (
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/config"
	"ike/internal/keymap"
)

// macFixture is a macOS-style export: meta modifiers, a two-keystroke chord,
// an unmapped action, an untranslatable keystroke and a mouse-only action.
const macFixture = `<?xml version="1.0" encoding="UTF-8"?>
<keymap version="1" name="macOS copy" parent="macOS">
  <action id="SaveDocument">
    <keyboard-shortcut first-keystroke="meta pressed S"/>
  </action>
  <action id="GotoDeclaration">
    <keyboard-shortcut first-keystroke="meta pressed B"/>
  </action>
  <action id="FindInPath">
    <keyboard-shortcut first-keystroke="shift meta F"/>
  </action>
  <action id="ReformatCode">
    <keyboard-shortcut first-keystroke="meta alt pressed L"/>
  </action>
  <action id="SaveAll">
    <keyboard-shortcut first-keystroke="meta pressed K" second-keystroke="meta pressed S"/>
  </action>
  <action id="EditorCompleteStatement">
    <keyboard-shortcut first-keystroke="shift meta ENTER"/>
  </action>
  <action id="GotoNextError">
    <keyboard-shortcut first-keystroke="meta pressed PERIOD"/>
  </action>
  <action id="GotoDeclarationOnly">
    <mouse-shortcut keystroke="meta button1"/>
  </action>
</keymap>`

// winFixture is a Windows/Linux-style export: ctrl/alt modifiers, named keys.
const winFixture = `<?xml version="1.0" encoding="UTF-8"?>
<keymap version="1" name="Windows copy" parent="$default">
  <action id="SaveDocument">
    <keyboard-shortcut first-keystroke="ctrl pressed S"/>
  </action>
  <action id="ReformatCode">
    <keyboard-shortcut first-keystroke="ctrl alt pressed L"/>
  </action>
  <action id="FindUsages">
    <keyboard-shortcut first-keystroke="alt pressed F7"/>
  </action>
  <action id="ShowIntentionActions">
    <keyboard-shortcut first-keystroke="alt pressed ENTER"/>
  </action>
  <action id="CommentByLineComment">
    <keyboard-shortcut first-keystroke="ctrl pressed SLASH"/>
  </action>
  <action id="ActivateTerminalToolWindow">
    <keyboard-shortcut first-keystroke="alt pressed F12"/>
  </action>
</keymap>`

func TestParseKeystroke(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"meta pressed S", "cmd+s"},
		{"shift meta F", "cmd+shift+f"},
		{"ctrl alt pressed L", "ctrl+alt+l"},
		{"control pressed B", "ctrl+b"},
		{"alt pressed F7", "alt+f7"},
		{"shift pressed F6", "shift+f6"},
		{"alt pressed ENTER", "alt+enter"},
		{"ctrl pressed SLASH", "ctrl+/"},
		{"meta pressed OPEN_BRACKET", "cmd+left-bracket"},
		{"meta pressed BACK_SPACE", "cmd+backspace"},
		{"shift ctrl pressed PAGE_UP", "ctrl+shift+pgup"},
		{"meta pressed 7", "cmd+7"},
		{"pressed F10", "f10"},
	}
	for _, c := range cases {
		got, err := ParseKeystroke(c.in)
		if err != nil {
			t.Fatalf("ParseKeystroke(%q): %v", c.in, err)
		}
		if got != c.want {
			t.Fatalf("ParseKeystroke(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "meta pressed", "meta pressed WEIRD_KEY", "meta pressed PERIOD"} {
		if got, err := ParseKeystroke(bad); err == nil {
			t.Fatalf("ParseKeystroke(%q) = %q, want error", bad, got)
		}
	}
}

func TestPlanMacOSFixture(t *testing.T) {
	res, err := Plan(strings.NewReader(macFixture), keymap.Defaults(keymap.PresetJetBrains))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.Name != "macOS copy" {
		t.Fatalf("Name = %q", res.Name)
	}
	want := map[string]string{
		"cmd+s":       "editor.write",
		"cmd+b":       "lsp.definition",
		"cmd+shift+f": "project.findInPath",
		"cmd+alt+l":   "lsp.format",
		"cmd+k cmd+s": "editor.saveAll", // second-keystroke chord
	}
	for chord, cmd := range want {
		if got := res.Bind[chord]; got != cmd {
			t.Fatalf("Bind[%q] = %q, want %q (all: %v)", chord, got, cmd, res.Bind)
		}
	}
	// The unmapped action is collected, not fatal.
	if len(res.Unmapped) != 1 || res.Unmapped[0] != "EditorCompleteStatement" {
		t.Fatalf("Unmapped = %v", res.Unmapped)
	}
	// The PERIOD keystroke is skipped with a reason, its action stays mapped.
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "GotoNextError") {
		t.Fatalf("Skipped = %v", res.Skipped)
	}
	// Unbind lists the replaced defaults: editor.write keeps cmd+s (imported)
	// but loses ctrl+s; lsp.definition keeps cmd+b but loses f4.
	unbound := map[string]bool{}
	for _, c := range res.Unbind {
		unbound[c] = true
	}
	for _, wantGone := range []string{"ctrl+s", "f4", "cmd+shift+s"} {
		if !unbound[wantGone] {
			t.Fatalf("Unbind missing %q: %v", wantGone, res.Unbind)
		}
	}
	for _, kept := range []string{"cmd+s", "cmd+b", "cmd+shift+f"} {
		if unbound[kept] {
			t.Fatalf("Unbind must not contain imported chord %q: %v", kept, res.Unbind)
		}
	}
}

func TestPlanWindowsFixture(t *testing.T) {
	res, err := Plan(strings.NewReader(winFixture), keymap.Defaults(keymap.PresetJetBrains))
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	want := map[string]string{
		"ctrl+s":     "editor.write",
		"ctrl+alt+l": "lsp.format",
		"alt+f7":     "lsp.references",
		"alt+enter":  "lsp.codeAction",
		"ctrl+/":     "editor.commentLine",
		"alt+f12":    "terminal.toggle",
	}
	for chord, cmd := range want {
		if got := res.Bind[chord]; got != cmd {
			t.Fatalf("Bind[%q] = %q, want %q (all: %v)", chord, got, cmd, res.Bind)
		}
	}
	if len(res.Unmapped) != 0 || len(res.Skipped) != 0 {
		t.Fatalf("Unmapped/Skipped = %v / %v", res.Unmapped, res.Skipped)
	}
	// editor.write loses the cmd+s default; the imported ctrl+s stays.
	unbound := map[string]bool{}
	for _, c := range res.Unbind {
		unbound[c] = true
	}
	if !unbound["cmd+s"] || unbound["ctrl+s"] {
		t.Fatalf("Unbind = %v, want cmd+s unbound and ctrl+s kept", res.Unbind)
	}
}

func TestPlanRejectsNonKeymapXML(t *testing.T) {
	if _, err := Plan(strings.NewReader("not xml at all"), nil); err == nil {
		t.Fatal("Plan must fail on a non-XML document")
	}
}

// TestApplyEndToEnd runs the full pipeline: Apply writes through
// config.WriteKey into a user settings file, config.Load merges it, and
// keymap.BuildTable resolves the imported bindings while replaced defaults
// are gone.
func TestApplyEndToEnd(t *testing.T) {
	opts := config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")}
	defaults := keymap.Defaults(keymap.PresetJetBrains)
	res, err := Apply(strings.NewReader(macFixture), defaults, func(key, value string) error {
		return config.WriteKey(opts, config.UserScope, key, value)
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res.Bind) == 0 {
		t.Fatal("Apply imported nothing")
	}
	c, diags := config.Load(opts)
	for _, d := range diags {
		t.Fatalf("Load diagnostic: %+v", d)
	}
	table := keymap.BuildTable(defaults, c.Keymap.Bindings, "darwin")
	byChord := map[string]string{}
	for _, b := range table.Bindings() {
		byChord[b.Chord.String()] = b.Command
	}
	if byChord["cmd+b"] != "lsp.definition" {
		t.Fatalf("cmd+b = %q, want lsp.definition", byChord["cmd+b"])
	}
	if byChord["cmd+s"] != "editor.write" {
		t.Fatalf("cmd+s = %q, want editor.write", byChord["cmd+s"])
	}
	// Replaced defaults are unbound: f4 (lsp.definition) and ctrl+s
	// (editor.write) are gone from the effective table.
	if _, ok := byChord["f4"]; ok {
		t.Fatal("f4 default must be unbound after import")
	}
	if _, ok := byChord["ctrl+s"]; ok {
		t.Fatal("ctrl+s default must be unbound after import")
	}
	if !strings.Contains(res.Summary(), "imported") {
		t.Fatalf("Summary = %q", res.Summary())
	}
}
