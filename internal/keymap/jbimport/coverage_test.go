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
	"editor.tab.moveLeft":              "JetBrains reorders tabs by drag only, no keymap action",
	"editor.tab.moveRight":             "JetBrains reorders tabs by drag only, no keymap action",
	"explorer.undo":                    "project-view undo rides $Undo in JetBrains, already mapped to editor.undo",
	"explorer.redo":                    "project-view redo rides $Redo in JetBrains, already mapped to editor.redo",
	"file.rename":                      "RenameElement covers symbol and file renames, mapped to lsp.rename",
	"find.openInPanel":                 "JetBrains' Open in Find Window is a popup-local chord, not a keymap action",
	"project.findInAllProjects":        "IKE-only concept (#2394): JetBrains searches one project at a time, no cross-project keymap action",
	"project.findInAllProjectsResults": "IKE-only concept (#2394): re-opening a cross-project result set has no JetBrains equivalent",
	"http.run":                         "JetBrains HTTP client runs via context Run, no dedicated keymap action",
	"http.resend":                      "IKE-only concept (#1832): repeating a captured request verbatim has no JetBrains keymap action",
	"archive.reload":                   "IKE-only concept (#1762): JetBrains re-reads an archive on focus, no keymap action",
	"http.showResponse":                "IKE-only concept (stored response without dispatch), no JetBrains equivalent",
	"http.diffPreviousRun":             "IKE-only concept (response history diff, #2060), no JetBrains equivalent",
	"http.copyResponse":                "IKE-only concept (#2315): JetBrains' $Copy is the editor copy, already mapped to editor.copy",
	"http.search":                      "IKE-only concept (#2400): the response viewer's in-pane search is a pane key, not a JetBrains keymap action",
	"http.cancel":                      "IKE-only concept (#2404): JetBrains stops a request from the run tool's button, no keymap action",
	"debug.copy":                       "IKE-only concept (#2400): JetBrains' debugger copies from its own context menu, no keymap action",
	"issues.copy":                      "IKE-only concept (#2400): the issues window has no JetBrains counterpart",
	"issues.selectPrev":                "IKE-only concept (#2400): the issues window has no JetBrains counterpart",
	"issues.selectNext":                "IKE-only concept (#2400): the issues window has no JetBrains counterpart",
	"editor.copyDocPath":               "JetBrains copies file references, not a path inside a JSON/YAML document",
	"editor.sortLines":                 "IntelliJ ships no Sort Lines keymap action (#2417); it lives in the String Manipulation plugin, which an exported keymap does not carry",
	"editor.case.cycle":                "IKE-only concept (#2418): IntelliJ toggles case but never rotates identifier styles; that lives in the String Manipulation plugin, which an exported keymap does not carry",
	"json.jqQueryView":                 "IKE-only concept (#2032): the inline jq playground has no JetBrains equivalent",
	"time.toggle":                      "IKE-only concept (#2426): JetBrains has no time report; time tracking lives in third-party plugins an exported keymap does not carry",
	"markdown.preview":                 "no default JetBrains keymap action",
	"file.openAs":                      "IKE-only concept (#2420): JetBrains' Override File Type is a context-menu popup, not a keymap action",
	"search.open":                      "IKE-only concept (#2409): JetBrains' Find is the editor find, already mapped to editor.find; the pane-wide chord has no keymap action",
	"view.toggleFollow":                "tail -f follow mode is an IKE concept; JetBrains consoles auto-scroll without a keymap action",
	"view.followFilter":                "filtering a live tail (#2255) is an IKE concept; JetBrains console filtering is a tool-window control, not a keymap action",
	"menu.open":                        "JetBrains main menu is not a keymap action",
	"nav.pins":                         "PinActiveEditorTab is a per-tab toggle, not a pin list",
	"notifications.history":            "JetBrains notifications tool window has no default shortcut action",
	"palette.keymapHelp":               "no JetBrains equivalent",
	"perf.hud":                         "IKE-only concept (#1999); JetBrains profiles from the IDE's own tooling, not a keymap action",
	"pane.resizeMode":                  "IKE-only concept (#2150): JetBrains resizes panes by drag or per-step actions, no sticky mode",
	"pane.splitDown":                   "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitLeft":                   "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitRight":                  "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"pane.splitUp":                     "JetBrains has only the two editor splits, mapped to editor.splitView*",
	"project.peek.return":              "quick-peek (#2136) is an IKE concept; JetBrains has no temporary project switch",
	"project.switchLast":               "IKE-only concept (#2398): JetBrains only offers the Recent Projects popup, no last-project toggle action",
	"run.testAtCursor":                 "RunClass is context-sensitive in JetBrains, mapped to run.file",
	"debug.testAtCursor":               "DebugClass is context-sensitive in JetBrains, mapped to debug.start",
	"playground.open":                  "IKE-only concept (#2415): JetBrains has no playground, let alone a dialect dispatcher over one",
	"json.jqPlayground":                "IKE-only concept (#1936): JetBrains has no jq playground",
	"yaml.yqPlayground":                "IKE-only concept (#2039): JetBrains has no yq playground",
	"scratch.generate":                 "IKE-only concept (#2134): JetBrains scratch files carry no test-data generator",
	"scratch.newFromSelection":         "IKE-only concept (#2339): JetBrains' New Scratch File takes no selection",
	"scratch.promote":                  "IKE-only concept (#2339): JetBrains has no scratch-to-project action",
	"editor.escapeSelection":           "IKE-only concept (#2338): JetBrains has no unicode-escape rewrite action",
	"editor.unescapeSelection":         "IKE-only concept (#2338): JetBrains has no unicode-escape rewrite action",
	"pane.close":                       "closing a whole pane is an IKE concept; JetBrains closes editors, not panes",
	"window.layouts":                   "named window layouts (#1175) are an IKE concept; JetBrains only stores one default layout",
	"terminal.new":                     "JetBrains new terminal tab has no default keymap action",
	"terminal.newTab":                  "JetBrains new terminal tab has no default keymap action",
	"editor.tab.new":                   "JetBrains opens editors by navigation only; no new-empty-tab action",
	"editor.tab.picker":                "JetBrains' Switcher spans tool windows and editors, already mapped to pane.switcher; the per-pane tab list (#2151) is an IKE concept",
	"terminal.popup":                   "no JetBrains equivalent",
	"nav.pinGoto1":                     "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"nav.pinGoto2":                     "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"nav.pinGoto3":                     "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"nav.pinGoto4":                     "pins are an IKE concept; GotoBookmark* toggles mnemonic bookmarks instead",
	"editor.tab.select1":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select2":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select3":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select4":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select5":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select6":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select7":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select8":               "JetBrains has no select-tab-N keymap actions",
	"editor.tab.select9":               "JetBrains has no select-tab-N keymap actions",
	"pane.focus1":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus2":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus3":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus4":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus5":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus6":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus7":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus8":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
	"pane.focus9":                      "numbered panes (#2407) are an IKE concept; JetBrains numbers tool windows, not layout panes",
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
	// Both platforms: the macOS-only rows (keymap.darwinRows) are defaults
	// too, and the ledger above is platform-independent — judging it against
	// one platform's table alone would demand an entry on macOS and call the
	// same entry stale on Linux (#2407).
	var defaults []keymap.Binding
	for _, goos := range []string{"darwin", "linux"} {
		defaults = append(defaults, keymap.DefaultsFor(keymap.PresetJetBrains, goos)...)
	}
	for _, b := range defaults {
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
