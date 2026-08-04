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
	"http.run":              "JetBrains HTTP client runs via context Run, no dedicated keymap action",
	"markdown.preview":      "no default JetBrains keymap action",
	"menu.open":             "JetBrains main menu is not a keymap action",
	"nav.pins":              "PinActiveEditorTab is a per-tab toggle, not a pin list",
	"notifications.history": "JetBrains notifications tool window has no default shortcut action",
	"palette.keymapHelp":    "no JetBrains equivalent",
	"pane.splitDown":        "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitLeft":        "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitRight":       "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitUp":          "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"run.testAtCursor":      "RunClass is context-sensitive in JetBrains, mapped to run.file",
	"terminal.new":          "JetBrains new terminal tab has no default keymap action",
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
