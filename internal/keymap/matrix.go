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
	"editor.copy":              "vim y",
	"editor.cut":               "vim d",
	"editor.paste":             "vim p",
	"editor.duplicateLine":     "vim yyp",
	"editor.redo":              "vim ctrl+r",
	"editor.commentBlock":      "palette",
	"editor.commentLine":       "palette",
	"editor.lineStart":         "vim 0",
	"editor.lineEnd":           "vim $",
	"editor.find":              "vim /",
	"editor.replace":           "palette",
	"editor.saveAll":           "palette",
	"editor.closeTab":          "palette",
	"editor.caret.addAll":      "palette",
	"palette.keymapHelp":       "f1",
	"palette.searchEverywhere": "palette (esc esc)",
	"palette.recentFiles":      "palette",
	"pane.switcher":            "tab key",
	"pane.splitDown":           "palette",
	"pane.splitUp":             "palette",
	"pane.splitRight":          "palette",
	"pane.splitLeft":           "palette",
	"editor.splitViewRight":    "palette",
	"editor.splitViewDown":     "palette",
	"editor.pasteFromHistory":  "palette",
	"view.zenMode":             "palette / View menu",
	"editor.tab.next":          "palette",
	"editor.tab.prev":          "palette",
	"editor.tab.reopenClosed":  "palette",
	"editor.tab.select1":       "palette",
	"editor.tab.select2":       "palette",
	"editor.tab.select3":       "palette",
	"editor.tab.select4":       "palette",
	"editor.tab.select5":       "palette",
	"editor.tab.select6":       "palette",
	"editor.tab.select7":       "palette",
	"editor.tab.select8":       "palette",
	"editor.tab.select9":       "palette",
	"pane.maximize":            "palette",
	"nav.pins":                 "palette",
	"window.hideAllTools":      "palette",
	"nav.pinGoto1":             "palette (or the cmd+2 picker)",
	"nav.pinGoto2":             "palette (or the cmd+2 picker)",
	"nav.pinGoto3":             "palette (or the cmd+2 picker)",
	"nav.pinGoto4":             "palette (or the cmd+2 picker)",
	"explorer.undo":            "palette",
	"explorer.redo":            "palette",
	"explorer.reveal":          "palette",
	"explorer.toggle":          "palette",
	"project.goToFile":         "palette",
	"project.goToClass":        "palette",
	"project.switch":           "palette",
	"project.findInPath":       "palette",
	"project.replaceInPath":    "palette",
	"lsp.references":           "palette",
	"lsp.format":               "palette",
	"lsp.codeAction":           "palette",
	"lsp.callHierarchy":        "palette",
	"nav.back":                 "palette",
	"nav.forward":              "palette",
	"settings.open":            "palette",
	"terminal.toggle":          "palette",
	"terminal.new":             "palette",
	"notifications.history":    "palette",
	"markdown.preview":         "palette",
	"todo.list":                "palette",
	"vcs.commit":               "palette",
	"vcs.updateProject":        "palette",
	"vcs.revertFile":           "palette",
	"vcs.panel":                "palette",
	"problems.toggle":          "palette",
	"structure.toggle":         "palette",
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
