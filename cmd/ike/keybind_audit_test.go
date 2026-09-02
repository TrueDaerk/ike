package main

import (
	"sort"
	"strings"
	"testing"

	"ike/internal/keymap"
	"ike/internal/registry"
)

// keybind_audit_test.go is the standing ledger of the unbound-command audit
// (#2305). Commands are driven by keybind far more often than by the palette,
// so an everyday action that ships palette-only is effectively invisible: the
// working agreement is that a new command ships with a default keybind, and
// staying keybind-less needs a recorded reason.
//
// This test is the guardrail for that agreement. Every command the shipped
// binary registers is either bound in the default keymap
// (internal/keymap/defaults.go) or carries an entry here saying why it is not.
// A new command therefore fails the build until someone decides — chord or
// justification. The ledger is also kept honest in the other direction: an
// entry for a command that has since been bound, or that no longer exists, is
// stale and fails too.

// The audit also records the reverse case — a *chord* users press for which no
// command exists yet. #2400's telemetry caught `cmd+ctrl+down` in the editor,
// JetBrains' "move to next method": there is no symbol-stepping command to
// bind it to (only `lsp.documentSymbols`' popup), so the chord stays unbound
// until one lands rather than being aliased onto something adjacent.

// The reasons, grouped so the ledger reads as an audit rather than as an
// opt-out list.
const (
	// The editor's own vim keys reach the command; a chord would be a second
	// name for a gesture users already have (internal/editor/commands.go).
	reasonVimKey = "vim-native key in the editor"
	// The owning pane binds a single key while it is focused, which is the
	// only place the command means anything.
	reasonPaneKey = "single-key binding inside the owning pane"
	// One enumerated entry of a picker (a theme, a scratch language, an
	// encoding …). The picker is the doorway; the entries are its payload.
	reasonPickerItem = "one entry of a picker; the picker is the doorway"
	// A second flavour of a command that already has a chord — one chord per
	// everyday form is the standing rule for the default table.
	reasonFlavour = "flavour of a command that already has a chord"
	// alt+enter's intention popup offers it exactly where it applies, which is
	// a better doorway than a chord the user has to remember (#2020).
	reasonIntention = "offered by the alt+enter intention popup where it applies"
	// A menu or context menu is the natural home: the command is discovered
	// where it acts, not from muscle memory.
	reasonMenu = "lives in the menu or a context menu where it acts"
	// Run once in a blue moon — setup, diagnostics, one-off maintenance.
	reasonOccasional = "occasional one-off; the palette is the right doorway"
)

// unboundFamilies covers whole command families by id prefix. A family entry
// must match at least one registered, unbound command.
var unboundFamilies = []struct{ prefix, reason string }{
	{"themes.select.", reasonPickerItem},          // the theme picker's entries
	{"scratch.new.", reasonPickerItem},            // cmd+shift+n's language picker
	{"file.setEncoding.", reasonPickerItem},       // the status line's encoding picker
	{"file.setLineEndings.", reasonPickerItem},    // the status line's line-ending picker
	{"editor.fold.", reasonVimKey},                // za / zo / zc / zR / zM / zy
	{"merge.", reasonVimKey},                      // go / gt / … in the merge view
	{"explorer.", reasonPaneKey},                  // the tree's own keys while it is focused
	{"http.", reasonPaneKey},                      // the response pane's single keys
	{"view.toggle", reasonMenu},                   // the View menu's rendering toggles
	{"data.", reasonPaneKey},                      // the grid pane's keys
	{"csv.", reasonPaneKey},                       // the CSV grid's keys
	{"archive.", reasonPaneKey},                   // the archive pane's entry list
	{"nav.pinSlot", reasonMenu},                   // pinning goes through cmd+2's picker (#788)
	{"bookmark.", reasonFlavour},                  // mnemonic/annotation flavours of f11
	{"keymap.", reasonOccasional},                 // keymap doctor and the JetBrains import
	{"diag.", reasonOccasional},                   // heap dump and memory statistics
	{"json.jqFilters", reasonPaneKey},             // the playground's ctrl+l library
	{"json.jqRenameFilter", reasonPaneKey},        // …and its rename flavour
	{"json.jqSaveFilter", reasonPaneKey},          // the playground's ctrl+s
	{"json.jqCheatsheet", reasonPaneKey},          // the playground's ctrl+g language sheet (#2382)
	{"yaml.yqFilters", reasonPaneKey},             // the yq twins of the four above
	{"yaml.yqRenameFilter", reasonPaneKey},        //
	{"yaml.yqCheatsheet", reasonPaneKey},          //
	{"json.jqPlaygroundAtPath", reasonIntention},  // "jq Playground at Cursor Path"
	{"yaml.yqPlaygroundAtPath", reasonIntention},  //
	{"editor.copyDocPathJQ", reasonFlavour},       // cmd+alt+shift+c is the everyday form
	{"editor.copyDocPathYQ", reasonFlavour},       //
	{"editor.undoChrono", reasonVimKey},           // g-
	{"editor.redoChrono", reasonVimKey},           // g+
	{"file.copyRelPath", reasonFlavour},           // cmd+shift+c is the everyday form
	{"file.copyReference", reasonFlavour},         //
	{"run.testsWithCoverage", reasonOccasional},   // #2081: the run family's chord budget is spent
	{"lsp.formatRange", reasonFlavour},            // cmd+alt+l already reformats a selection
	{"project.peek.keep", reasonMenu},             // offered by the peek pane itself
	{"view.exportWindowScreenshot", reasonMenu},   // View menu, next to the pane flavour
	{"view.exportScreenshot", reasonMenu},         //
	{"view.clearFollowFilter", reasonFlavour},     // alt+shift+g sets and clears the filter
	{"view.followHighlight", reasonFlavour},       // the highlight-only flavour of alt+shift+g
	{"window.saveLayout", reasonMenu},             // saved from the layout picker
	{"window.setDefaultLayout", reasonMenu},       //
	{"terminal.clear", reasonPaneKey},             // the terminal's own clear
	{"terminal.ssh", reasonOccasional},            //
	{"vcs.nextChange", reasonVimKey},              // ]c
	{"vcs.prevChange", reasonVimKey},              // [c
	{"editor.increment", reasonVimKey},            // ctrl+a
	{"editor.decrement", reasonVimKey},            // ctrl+x
	{"editor.toggleValue", reasonVimKey},          // g!
	{"editor.explainConceal", reasonVimKey},       // g?
	{"editor.labelJump", reasonVimKey},            // gs
	{"editor.quit", reasonVimKey},                 // :q
	{"editor.write_quit", reasonVimKey},           // :wq
	{"editor.tab.closeOthers", reasonMenu},        // the tab context menu
	{"editor.tab.togglePin", reasonMenu},          //
	{"editor.forceCodeInsight", reasonMenu},       // the status line's large-file badge
	{"editor.largeFileDetails", reasonMenu},       //
	{"editor.setBufferLanguage", reasonMenu},      // the status line's language picker
	{"editor.materializeBuffer", reasonIntention}, //
	{"editor.decodeJWT", reasonIntention},         //
	{"editor.undoTree", reasonOccasional},         //
	{"vault.treatAsFile", reasonIntention},        // #2293: offered on an encrypted buffer
	{"lsp.ignoreDiagnostic", reasonIntention},     //
	{"lsp.quickFixProblem", reasonIntention},      //
	{"lsp.codeLens", reasonIntention},             //
	{"lsp.doctor", reasonOccasional},              //
	{"lsp.installMissing", reasonOccasional},      //
	{"lsp.restart", reasonOccasional},             //
	{"lsp.showLog", reasonOccasional},             //
	{"debug.doctor", reasonOccasional},            //
	{"debug.listen", reasonOccasional},            //
	{"diff.files", reasonMenu},                    //
	{"diff.compareWithClipboard", reasonMenu},     //
	{"vcs.blameLine", reasonMenu},                 // the gutter's context menu
	{"vcs.historyForSelection", reasonMenu},       //
	{"vcs.mergeFile", reasonMenu},                 // opened from the conflict notification
	{"vcs.mergeApply", reasonPaneKey},             // the merge view's own key
	{"vcs.revertHunk", reasonMenu},                // the gutter's context menu
	{"vcs.undoRevert", reasonOccasional},          //
	{"project.clone", reasonOccasional},           // File menu, once per repository
	{"project.new", reasonOccasional},             //
	{"project.open_link", reasonOccasional},       // links normally arrive via the OS ike:// handler
	{"project.peek", reasonOccasional},            // cmd+shift+b returns from a peek
	{"run.editConfig", reasonMenu},                // reached from alt+shift+f10's picker
	{"run.task", reasonMenu},                      // Run menu
	{"run.taskPromote", reasonOccasional},         //
	{"run.testsInFile", reasonFlavour},            // ctrl+shift+f10 runs the file's tests too
	{"scratch.list", reasonMenu},                  // File menu / the scratch panel
	{"scratch.manage", reasonOccasional},          //
	{"scratch.panel", reasonMenu},                 //
	{"coverage.toggle", reasonOccasional},         // #2081
	{"dom.toggle", reasonOccasional},              //
	{"es.run", reasonPaneKey},                     // the Elasticsearch buffer's own key
	{"help.welcomeTour", reasonOccasional},        // shown once, on first start
	{"history.projectTimeline", reasonMenu},       // VCS/File menu
	{"file.localHistory", reasonMenu},             //
	{"file.timeline", reasonMenu},                 //
	{"file.openPath", reasonFlavour},              // cmd+shift+o is the everyday open
	{"issues.toggle", reasonOccasional},           //
	{"log.openRotatedSet", reasonOccasional},      //
	{"perf.snapshot", reasonPaneKey},              // a key inside ctrl+alt+p's HUD
	{"python.newEnvironment", reasonOccasional},   //
	{"remote.browse", reasonOccasional},           //
	{"tool.lazygit", reasonMenu},                  // Tools menu / the configured tool pane
	{"tools.regexTester", reasonMenu},             // Tools menu
	{"tools.setup", reasonOccasional},             //
	{"usages.toggle", reasonFlavour},              // cmd+alt+f7 opens the panel with results
	{"watch.changeFeed", reasonMenu},              // the external-changes notification
	{"themes.syncTerminal", reasonOccasional},     //
}

// boundCommands is the set of command ids the default keymap binds.
func boundCommands() map[string]bool {
	out := map[string]bool{}
	for _, b := range keymap.Defaults(keymap.PresetJetBrains) {
		out[b.Command] = true
	}
	return out
}

// TestEveryCommandIsBoundOrJustified is the audit's guardrail: a registered
// command has a default chord, or an entry in the ledger above.
func TestEveryCommandIsBoundOrJustified(t *testing.T) {
	bound := boundCommands()
	var missing []string
	for _, c := range registry.Global().Commands() {
		if bound[c.ID] || unboundReason(c.ID) != "" {
			continue
		}
		missing = append(missing, c.ID)
	}
	sort.Strings(missing)
	for _, id := range missing {
		t.Errorf("%s has no default keybind and no entry in unboundFamilies: "+
			"give it a chord in internal/keymap/defaults.go, or record why it stays "+
			"palette-only (#2305)", id)
	}
}

// TestUnboundLedgerIsCurrent keeps the ledger honest: every entry must still
// match a registered command, so a renamed or deleted command cannot leave a
// dead justification behind.
func TestUnboundLedgerIsCurrent(t *testing.T) {
	ids := make([]string, 0, 512)
	for _, c := range registry.Global().Commands() {
		ids = append(ids, c.ID)
	}
	for _, f := range unboundFamilies {
		matched := false
		for _, id := range ids {
			if strings.HasPrefix(id, f.prefix) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("stale unboundFamilies entry %q: no registered command starts with it", f.prefix)
		}
	}
}

// unboundReason returns the recorded justification for id, or "" when there is
// none. The longest matching prefix wins, so a single command can carve itself
// out of its family's reason.
func unboundReason(id string) string {
	best, reason := "", ""
	for _, f := range unboundFamilies {
		if !strings.HasPrefix(id, f.prefix) {
			continue
		}
		if len(f.prefix) > len(best) {
			best, reason = f.prefix, f.reason
		}
	}
	return reason
}
