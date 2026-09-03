package keymap

import (
	"sort"
	"strings"
)

// matrix.go is the acceptance ledger of Roadmap 0081/50: one row per default
// binding command, aggregating everything the audit established — does the
// command exist (live), does its primary chord reach the program
// (reachability, 0081/10), what is the reachable fallback (delivered chord,
// vim equivalent or palette), and how it is surfaced (discoverability,
// 0081/40). The matrix is
// generated, never hand-maintained; the final-gate test asserts that every
// row is resolved: live with a reachable path, or honestly blocked with its
// dependency recorded.

// MatrixRow is one command's audit status.
type MatrixRow struct {
	Command  string
	Title    string
	Primary  string       // shortest default chord (the advertised one)
	Class    Reachability // primary chord's reachability
	Fallback string       // reachable alternative when the primary is fragile ("" when the primary delivers)
	Live     bool         // the command id resolves against the registry
	Blocked  string       // dependency note for ledger-blocked commands
}

// Resolved reports whether the row passes the per-binding Definition of
// Done: a blocked command is resolved by being honestly recorded; a live one
// needs a delivered path — its primary, or a fallback.
func (r MatrixRow) Resolved() bool {
	if r.Blocked != "" {
		return true
	}
	if !r.Live {
		return false
	}
	return r.Class == Delivered || r.Fallback != ""
}

// Status renders the row's resolution for the persisted table.
func (r MatrixRow) Status() string {
	switch {
	case r.Blocked != "":
		return "blocked: " + r.Blocked
	case !r.Live:
		return "UNRESOLVED: command not registered"
	case r.Class == Delivered:
		return "live"
	case r.Fallback != "":
		return "live via " + r.Fallback
	}
	return "UNRESOLVED: fragile with no fallback"
}

// reachableAlternatives documents the escape route for fragile-primary
// commands without a delivered chord: the vim-native equivalent or the
// palette (esc esc, delivered everywhere). Since the leader layer retired
// (#711) the palette is the universal escape for the Cmd/Alt-modified
// JetBrains chords. Data here resolves the matrix row and feeds the
// completeness test.
var reachableAlternatives = map[string]string{
	"editor.copy":                      "vim y",
	"editor.cut":                       "vim d",
	"editor.paste":                     "vim p",
	"editor.selectAll":                 "vim ggVG",
	"editor.duplicateLine":             "vim yyp",
	"editor.redo":                      "vim ctrl+r",
	"editor.commentBlock":              "palette",
	"editor.copyDocPath":               "palette",
	"editor.commentLine":               "palette",
	"editor.lineStart":                 "vim 0",
	"editor.lineEnd":                   "vim $",
	"editor.deleteLine":                "vim dd",
	"editor.deleteWordBackward":        "vim db",
	"editor.find":                      "vim /",
	"search.open":                      "vim / (every pane binds it, #2409)",
	"editor.replace":                   "palette",
	"editor.saveAll":                   "palette",
	"editor.closeTab":                  "palette",
	"editor.caret.addAll":              "palette",
	"editor.caret.addAbove":            "palette",
	"editor.caret.addBelow":            "palette",
	"editor.selection.extend":          "palette",
	"editor.selection.shrink":          "palette",
	"editor.sortLines":                 "vim :sort / Edit menu",
	"editor.case.toggle":               "vim g~ (g~~ linewise, ~ on a selection)",
	"editor.case.cycle":                "palette",
	"editor.escapeSelection":           "palette",
	"editor.unescapeSelection":         "palette",
	"debug.evaluate":                   "palette / Run menu",
	"palette.keymapHelp":               "f1",
	"palette.searchEverywhere":         "palette (esc esc)",
	"palette.recentFiles":              "palette",
	"pane.switcher":                    "tab key",
	"pane.splitDown":                   "palette",
	"pane.splitUp":                     "palette",
	"pane.splitRight":                  "palette",
	"pane.splitLeft":                   "palette",
	"editor.splitViewRight":            "palette",
	"editor.splitViewDown":             "palette",
	"editor.pasteFromHistory":          "palette",
	"view.zenMode":                     "palette / View menu",
	"perf.hud":                         "palette / View menu",
	"json.jqQueryView":                 "palette / Tools menu",
	"view.toggleFollow":                "palette",
	"view.followFilter":                "palette",
	"editor.tab.next":                  "palette",
	"editor.tab.prev":                  "palette",
	"editor.tab.reopenClosed":          "palette",
	"editor.tab.picker":                "palette",
	"editor.tab.select1":               "palette",
	"editor.tab.select2":               "palette",
	"editor.tab.select3":               "palette",
	"editor.tab.select4":               "palette",
	"editor.tab.select5":               "palette",
	"editor.tab.select6":               "palette",
	"editor.tab.select7":               "palette",
	"editor.tab.select8":               "palette",
	"editor.tab.select9":               "palette",
	"pane.maximize":                    "palette",
	"pane.resizeMode":                  "palette / pane context menu",
	"http.diffPreviousRun":             "palette",
	"debug.breakpoints":                "palette / Run menu",
	"lsp.goToSuper":                    "palette / Navigate menu / context menu",
	"lsp.implementations":              "palette / Navigate menu / context menu",
	"nav.pins":                         "palette",
	"window.hideAllTools":              "palette",
	"nav.pinGoto1":                     "palette (or the cmd+2 picker)",
	"nav.pinGoto2":                     "palette (or the cmd+2 picker)",
	"nav.pinGoto3":                     "palette (or the cmd+2 picker)",
	"nav.pinGoto4":                     "palette (or the cmd+2 picker)",
	"explorer.undo":                    "palette",
	"explorer.redo":                    "palette",
	"explorer.reveal":                  "palette",
	"explorer.toggle":                  "palette",
	"project.goToFile":                 "palette",
	"project.goToClass":                "palette",
	"project.switch":                   "palette",
	"project.switchLast":               "palette",
	"project.close":                    "palette / File menu",
	"project.peek.return":              "palette",
	"project.findInPath":               "palette",
	"project.replaceInPath":            "palette",
	"project.findInAllProjects":        "palette",
	"project.findInAllProjectsResults": "palette",
	"lsp.references":                   "palette",
	"lsp.format":                       "palette",
	"lsp.codeAction":                   "palette",
	"lsp.callHierarchy":                "palette",
	"nav.back":                         "palette",
	"nav.forward":                      "palette",
	"settings.open":                    "palette",
	"terminal.toggle":                  "palette",
	"terminal.new":                     "palette",
	"terminal.popup":                   "palette",
	"terminal.popup.pin":               "palette",
	"notifications.history":            "palette",
	"markdown.preview":                 "palette",
	"todo.list":                        "palette",
	"vcs.revertFile":                   "palette",
	"vcs.panel":                        "palette",
	"problems.toggle":                  "palette",
	"deps.toggle":                      "palette",
	"time.toggle":                      "palette / Tools menu",
	"lsp.ignoreDiagnostic":             "palette",
	"structure.toggle":                 "palette",
	"dom.toggle":                       "palette",
	"scratch.panel":                    "palette",
	"explorer.newFile":                 "palette (or a in the explorer)",
	"scratch.new":                      "palette",
	// Unbound-command audit (#1378): Cmd-primary chords without a delivered
	// secondary escape through the palette.
	"lsp.documentSymbols": "palette (or the cmd+3 Structure panel)",
	"lsp.peekDefinition":  "palette",
	"lsp.referencesPanel": "palette",
	"nav.bookmarks":       "palette",
	// Bookmarks (#55): alt+f3 needs a modifier the terminal may swallow;
	// f11/shift+f11/alt+f11 deliver, so only the mnemonic flavour and the
	// palette-only commands need an escape route.
	"bookmark.toggleMnemonic": "palette / Navigate menu",
	"bookmark.jumpMnemonic":   "palette",
	"bookmark.annotate":       "palette / Navigate menu",
	"bookmark.overview":       "palette / Navigate menu",
	// #1374: on darwin plain ctrl+F-keys are macOS system shortcuts (never
	// delivered) and the cmd+F primaries need the Kitty protocol, so these
	// commands have no delivered chord there; off macOS the ctrl forms
	// deliver and these entries go unused.
	"debug.stop":                 "palette / Run menu",
	"debug.toggleBreakpoint":     "palette / Run menu",
	"debug.breakpointProperties": "palette / Run menu",
	// #2405: run-to-cursor joins that family — alt+f9 is the JetBrains chord
	// and cmd+f9 the darwin primary, and both are fragile in a terminal.
	"debug.runToCursor": "palette / Run menu",
	"run.rerun":                  "palette / Run menu",
	// #2081: coverage runs and the mark toggle are palette-only — the run
	// family's chord budget is spent and coverage is an occasional action.
	"run.testsWithCoverage": "palette",
	"coverage.toggle":       "palette",
	"lsp.diagnosticInfo":    "palette",
	"http.run":              "palette",
	// Second unbound-command audit (#2305): every new default is a Cmd- or
	// Alt-modified chord, so all of them escape through the palette; the ones
	// with a menu or context-menu home name it too.
	"file.copyPath":       "palette / context menu",
	"file.openInBrowser":  "palette / context menu",
	"file.openAs":         "palette / context menu",
	"lsp.organizeImports": "palette / context menu",
	// #2415: the dispatcher sits on a Cmd chord like its dialect commands and
	// escapes the same way; the Tools menu names it above them.
	"playground.open":    "palette / Tools menu",
	"json.jqPlayground":  "palette / Tools menu",
	"yaml.yqPlayground":  "palette / Tools menu",
	"scratch.generate":   "palette / File menu",
	"vcs.diff":           "palette",
	"tests.toggle":       "palette / View menu",
	"debug.console":      "palette",
	"run.select":         "palette / Run menu",
	"debug.testAtCursor": "palette / Run menu",
	// #2405: the prompt flavour of run-to-cursor has no chord of its own —
	// the cursor flavour carries alt+f9/cmd+f9 — so the palette is its door.
	"debug.runToLine": "palette / Run menu",
	"pane.close":         "palette / pane context menu",
	"view.toggleWrap":    "palette",
	"window.layouts":     "palette",
	// #2315: the response viewer's copy also has its pane-local keys — "y"
	// for the body, and ctrl+c once a selection exists (#2062) — either of
	// which delivers on every terminal.
	"http.copyResponse": "response pane \"y\" / palette",
	// #2339: both scratch-store connectors sit on cmd+alt+shift chords, so
	// the palette is their escape; promote additionally has the scratch
	// manager's own ctrl+p, which delivers on every terminal.
	"scratch.newFromSelection": "palette",
	"scratch.promote":          "scratch manager ctrl+p / palette",
	// #2400: the two pane copies join the cmd+c family above. Neither takes a
	// ctrl+c secondary (that chord stays the global quit on macOS), so the
	// escape is the palette — the commands are pane-scoped, so they are
	// offered there exactly while their pane has the focus. The issues window
	// additionally keeps its own "y" for the mouse selection.
	"debug.copy":  "palette",
	"issues.copy": "issues pane \"y\" / palette",
}

// StatusMatrix builds the ledger over the default table. commandExists
// resolves an id against the live registry (nil treats every non-blocked
// command as live — the data-only view).
func StatusMatrix(commandExists func(id string) bool) []MatrixRow {
	rows := Defaults(PresetJetBrains)
	byCmd := map[string]*MatrixRow{}
	for _, b := range rows {
		if b.Command == "" {
			continue
		}
		r, ok := byCmd[b.Command]
		if !ok {
			r = &MatrixRow{Command: b.Command, Title: b.Title}
			byCmd[b.Command] = r
		}
		if r.Title == "" {
			r.Title = b.Title
		}
		chord := b.Chord.String()
		class := Classify(b.Chord)
		// The primary is the shortest delivered chord, else the shortest
		// chord at all; anything delivered beyond the primary is the fallback.
		switch {
		case r.Primary == "":
			r.Primary, r.Class = chord, class
		case class == Delivered && r.Class != Delivered:
			// A delivered chord displaces a fragile primary into... nothing:
			// the fragile one stays advertised (JetBrains muscle memory), the
			// delivered one becomes the fallback below.
		case class == r.Class && shorterThen(chord, r.Primary):
			r.Primary, r.Class = chord, class
		}
		if class == Delivered && chord != r.Primary && (r.Fallback == "" || shorterThen(chord, r.Fallback)) {
			r.Fallback = chord
		}
	}
	for id, r := range byCmd {
		if reason, blocked := BlockedReason(id); blocked {
			r.Blocked = reason
			continue
		}
		if commandExists != nil {
			r.Live = commandExists(id)
		} else {
			r.Live = true
		}
		if r.Class == Delivered {
			r.Fallback = "" // the primary already delivers
		} else if r.Fallback == "" {
			if alt := reachableAlternatives[id]; alt != "" {
				r.Fallback = alt
			}
		}
	}
	out := make([]MatrixRow, 0, len(byCmd))
	for _, r := range byCmd {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Command < out[j].Command })
	return out
}

// MatrixMarkdown renders the ledger as the persisted wiki table.
func MatrixMarkdown(rows []MatrixRow) string {
	var b strings.Builder
	b.WriteString("| command | primary | reachability | fallback | status |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, r := range rows {
		fallback := r.Fallback
		if fallback == "" {
			fallback = "—"
		}
		b.WriteString("| `" + r.Command + "` | `" + r.Primary + "` | " + r.Class.String() +
			" | `" + fallback + "` | " + r.Status() + " |\n")
	}
	return b.String()
}
