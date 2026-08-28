package jbimport

import (
	"testing"

	"ike/internal/keymap"
)

// noCounterpart lists default-set commands that deliberately have no
// JetBrains action to import from. Every other default command must appear
// as an actionMap value, so new commands cannot silently drift out of
// import coverage.
var noCounterpart = map[string]string{
	"editor.tab.moveLeft":   "JetBrains reorders tabs by drag only, no keymap action",
	"editor.tab.moveRight":  "JetBrains reorders tabs by drag only, no keymap action",
	"explorer.undo":         "project-view undo rides $Undo in JetBrains, already mapped to editor.undo",
	"explorer.redo":         "project-view redo rides $Redo in JetBrains, already mapped to editor.redo",
	"file.rename":           "RenameElement covers symbol and file renames, mapped to lsp.rename",
	"find.openInPanel":      "JetBrains' Open in Find Window is a popup-local chord, not a keymap action",
	"http.run":              "JetBrains HTTP client runs via context Run, no dedicated keymap action",
	"http.resend":           "IKE-only concept (#1832): repeating a captured request verbatim has no JetBrains keymap action",
	"archive.reload":        "IKE-only concept (#1762): JetBrains re-reads an archive on focus, no keymap action",
	"http.showResponse":     "IKE-only concept (stored response without dispatch), no JetBrains equivalent",
	"http.diffPreviousRun":  "IKE-only concept (response history diff, #2060), no JetBrains equivalent",
	"http.copyResponse":     "IKE-only concept (#2315): JetBrains' $Copy is the editor copy, already mapped to editor.copy",
	"editor.copyDocPath":    "JetBrains copies file references, not a path inside a JSON/YAML document",
	"json.jqQueryView":      "IKE-only concept (#2032): the inline jq playground has no JetBrains equivalent",
	"markdown.preview":      "no default JetBrains keymap action",
	"view.toggleFollow":     "tail -f follow mode is an IKE concept; JetBrains consoles auto-scroll without a keymap action",
	"view.followFilter":     "filtering a live tail (#2255) is an IKE concept; JetBrains console filtering is a tool-window control, not a keymap action",
	"menu.open":             "JetBrains main menu is not a keymap action",
	"nav.pins":              "PinActiveEditorTab is a per-tab toggle, not a pin list",
	"notifications.history": "JetBrains notifications tool window has no default shortcut action",
	"palette.keymapHelp":    "no JetBrains equivalent",
	"perf.hud":              "IKE-only concept (#1999); JetBrains profiles from the IDE's own tooling, not a keymap action",
	"pane.resizeMode":       "IKE-only concept (#2150): JetBrains resizes panes by drag or per-step actions, no sticky mode",
	"pane.splitDown":        "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitLeft":        "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitRight":       "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitUp":          "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"project.peek.return":   "quick-peek (#2136) is an IKE concept; JetBrains has no temporary project switch",
	"run.testAtCursor":      "RunClass is context-sensitive in JetBrains, mapped to run.file",
	"debug.testAtCursor":    "DebugClass is context-sensitive in JetBrains, mapped to debug.start",
	"json.jqPlayground":     "IKE-only concept (#1936): JetBrains has no jq playground",
	"yaml.yqPlayground":     "IKE-only concept (#2039): JetBrains has no yq playground",
	"scratch.generate":      "IKE-only concept (#2134): JetBrains scratch files carry no test-data generator",
	"pane.close":            "closing a whole pane is an IKE concept; JetBrains closes editors, not panes",
	"window.layouts":        "named window layouts (#1175) are an IKE concept; JetBrains only stores one default layout",
	"terminal.new":          "JetBrains new terminal tab has no default keymap action",
	"terminal.newTab":       "JetBrains new terminal tab has no default keymap action",
	"editor.tab.new":        "JetBrains opens editors by navigation only; no new-empty-tab action",
	"editor.tab.picker":     "JetBrains' Switcher spans tool windows and editors, already mapped to pane.switcher; the per-pane tab list (#2151) is an IKE concept",
	"terminal.popup":        "no JetBrains equivalent",
	"nav.pinGoto1":          "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"nav.pinGoto2":          "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"nav.pinGoto3":          "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"nav.pinGoto4":          "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"editor.tab.select1":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select2":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select3":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select4":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select5":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select6":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select7":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select8":    "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select9":    "JetBrains has no select-tab-N keymap actions",
}

// TestActionMapCoversDefaults asserts the doc-comment contract on actionMap:
// every command in the JetBrains default set is either an actionMap value or
// explicitly excused in noCounterpart.
func TestActionMapCoversDefaults(t *testing.T) {
	mapped := make(map[string]bool, len(actionMap))
	for _, cmd := range actionMap {
		mapped[cmd] = true
	}
	seen := make(map[string]bool)
	for _, b := range keymap.Defaults(keymap.PresetJetBrains) {
		if seen[b.Command] {
			continue
		}
		seen[b.Command] = true
		_, excused := noCounterpart[b.Command]
		if mapped[b.Command] && excused {
			t.Errorf("%s is both mapped in actionMap and listed in noCounterpart", b.Command)
		}
		if !mapped[b.Command] && !excused {
			t.Errorf("%s has no actionMap entry; add a JetBrains action id or excuse it in noCounterpart", b.Command)
		}
	}
	for cmd := range noCounterpart {
		if !seen[cmd] {
			t.Errorf("noCounterpart lists %s, which is not in the default set anymore", cmd)
		}
	}
}
