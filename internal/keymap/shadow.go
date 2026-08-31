package keymap

import "fmt"

// shadow.go detects cross-context binding shadowing (#1875). detectConflicts
// only sees same-chord+same-context clashes; a pane-scoped binding that hides a
// Global one with a *different* command on the same chord ("editor.cmd+e" over
// the global cmd+e → palette.recentFiles) used to pass silently, because
// context layering is also a feature ("keep both, resolve by context", #1312).
// A Shadow makes that overlap visible without breaking the feature: it is a
// non-fatal diagnostic, and deliberate default-set layerings are allowlisted.
// Since #1876 the same detection covers the language level: an editor[http]
// binding shadows editor and Global bindings of the chord, but only inside
// its narrower scope (http buffers) — Context.Shadows encodes which pairs can
// actually overlap.

// Shadow records a more specific binding hiding a less specific one of a
// different command on the same chord: while Winner's scope has focus (its
// pane, and for a language-qualified winner a buffer of its language) the
// chord runs Winner.Command and Hidden.Command is unreachable there;
// everywhere else Hidden still wins.
type Shadow struct {
	Chord  string
	Winner Binding // more specific; wins while its scope has focus
	Hidden Binding // Global or Winner's pane base; unreachable in Winner's scope
}

// String renders the shadow for a diagnostic line, naming which command wins
// where and how to resolve the overlap.
func (s Shadow) String() string {
	return fmt.Sprintf(
		"keymap shadow: %q in %s runs %q (%s), hiding %s %q (%s) there; %q still runs outside %s — rebind one command or unbind %q to resolve",
		s.Chord, contextLabel(s.Winner.Context), s.Winner.Command, s.Winner.Layer,
		contextLabel(s.Hidden.Context), s.Hidden.Command, s.Hidden.Layer,
		s.Hidden.Command, contextLabel(s.Winner.Context),
		ContextName(s.Winner.Context)+"."+s.Chord)
}

// shadowKey identifies one winner/hidden command pair on a chord — the unit
// the intentional-shadow allowlist speaks about.
func shadowKey(chord, winnerCmd, hiddenCmd string) string {
	return chord + "\x00" + winnerCmd + "\x00" + hiddenCmd
}

// intentionalDefaultShadows allowlists the deliberate context layerings of the
// default set: pairs where a pane-scoped default is *meant* to take a chord
// from a Global default while its pane has focus. Only pairs where both sides
// are LayerDefault are eligible — a user/project binding shadowing anything is
// always reported. TestDefaultShadowsAreIntentional keeps the list exact: every
// default shadow must be listed here, and every entry must still exist.
var intentionalDefaultShadows = map[string]string{
	// shift+f6 is JetBrains' context-aware refactor-rename (0082 sheet 13):
	// symbol rename with an editor focused, file rename everywhere else.
	shadowKey("shift+f6", "lsp.rename", "file.rename"): "context-aware refactor-rename",
	// f7 steps into with a paused debug session; in a focused diff pane the
	// JetBrains next-difference binding deliberately wins (0340, #495).
	shadowKey("f7", "diff.nextChange", "debug.stepInto"): "diff next-change over debug step-into",
	// Off macOS the Cmd→Ctrl fold lands tab cycling's ctrl+cmd+arrow on
	// ctrl+arrow, where the editor's line start/end wins with an editor
	// focused — accepted, since ctrl+alt+arrow stays the delivered tab-cycle
	// secondary everywhere.
	shadowKey("ctrl+left", "editor.lineStart", "editor.tab.prev"): "linux Cmd-fold: line start over tab-prev (ctrl+alt+left remains)",
	shadowKey("ctrl+right", "editor.lineEnd", "editor.tab.next"):  "linux Cmd-fold: line end over tab-next (ctrl+alt+right remains)",
	// Off macOS the Cmd→Ctrl fold lands the IntelliJ macOS find-next/previous
	// aliases (#2359) on ctrl+g / ctrl+shift+g, where the editor's multi-caret
	// commands deliberately win — accepted, since f3/shift+f3 stay the
	// delivered match-stepping keys everywhere.
	shadowKey("ctrl+g", "editor.caret.addNext", "search.nextMatch"):      "linux Cmd-fold: add-next-caret over find-next (f3 remains)",
	shadowKey("ctrl+shift+g", "editor.caret.addAll", "search.prevMatch"): "linux Cmd-fold: add-all-carets over find-previous (shift+f3 remains)",
}

// detectShadows scans the effective (post-conflict) binding set for
// cross-context shadowing: two bindings sharing a chord but naming different
// commands, where one context can hide the other (Context.Shadows — pane over
// Global, and since #1876 editor[lang] over editor and Global). Same-command
// overlaps are the dual-chord/fallback pattern, not a shadow. Allowlisted
// pairs where both sides are shipped defaults are skipped. Input order is
// preserved so the result is deterministic.
func detectShadows(bindings []Binding) []Shadow {
	var out []Shadow
	for _, w := range bindings {
		if w.Context == Global {
			continue
		}
		for _, h := range bindings {
			if !w.Context.Shadows(h.Context) || h.Chord.String() != w.Chord.String() || h.Command == w.Command {
				continue
			}
			if w.Layer == LayerDefault && h.Layer == LayerDefault {
				if _, ok := intentionalDefaultShadows[shadowKey(w.Chord.String(), w.Command, h.Command)]; ok {
					continue
				}
			}
			out = append(out, Shadow{Chord: w.Chord.String(), Winner: w, Hidden: h})
		}
	}
	return out
}
