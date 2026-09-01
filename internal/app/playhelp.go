package app

import (
	"ike/internal/help"
)

// playhelp.go documents the jq/yq playground's keyboard for the cheatsheet
// (#2237). The playground is a mode mounted inside another pane: it owns every
// key while its pane is focused, but it advertises no pane context and its
// keys belong to no registered command, so before this the help overlay
// happily listed the *editor's* bindings — none of which apply — and never
// mentioned that the query line has a history, a completion popup or a filter
// library at all.
//
// The tables below are the inventory the issue asked for, split the way the
// keyboard is actually split: the query line (the default focus) and the
// result buffer (after tab). Both groups are flagged Focused, so the help's
// context view leads with them (help.withExtraLeading) — the playground is a
// context of its own there, ahead of the global bindings.
//
// Keys whose chord is configurable are resolved live where that is cheap
// (the query-view toggle, the palette); the rest are the mode's own fixed
// keys, which no keymap can rebind.
//
// This file is the keyboard's sheet. The **language** has one too since
// #2382 — syntax, one-line example programs and every builtin of the live
// dialect, opened with `ctrl+g` (internal/app/playcheat.go, content in
// jqplay.Cheatsheet) — and the two are listed side by side here so the help
// overlay is a doorway to both: knowing which key runs the program is no use
// to someone who does not know what to type into it.

// playQueryHelpKeys documents the query line, the playground's default focus.
var playQueryHelpKeys = []struct{ Key, Title string }{
	{"(typing)", "Edit the program; every change re-evaluates (debounced)"},
	{"ctrl+space", "Open the completion popup (builtins on an empty line)"},
	{"enter", "Record the program in the history and run it now"},
	{"↑ / ↓", "Program history — the program's rows in the full query view"},
	{"alt+↑ / alt+↓", "Program history from any row of the full query view"},
	{"home / end", "Ends of the program — of the caret's row in the full view"},
	{"ctrl+home / ctrl+end", "Start / end of the whole program"},
	{"tab", "Move the keyboard into the result buffer"},
	{"pgup / pgdn", "Page the result without leaving the query line"},
	{"ctrl+s", "Save the program as a named filter"},
	{"ctrl+l", "Open the saved-filter picker"},
	{"ctrl+g", "Open the language cheatsheet — syntax, examples, builtins"},
	{"ctrl+y", "Copy the whole result"},
	{"ctrl+o", "Open the result as a scratch file"},
	{"esc", "Close the playground (the program is kept in the history)"},
	{"esc esc", "Close and open the command palette"},
}

// playResultHelpKeys documents the read-only result buffer (after tab). It
// lists the editor keys worth knowing there rather than repeating the whole
// editor keymap — that one is a tab away in the flat sheet.
var playResultHelpKeys = []struct{ Key, Title string }{
	{"tab", "Back to the query line"},
	{"j / k · ↑ / ↓", "Scroll — the full editor motions apply"},
	{"g / G", "Top / bottom"},
	{"/ · n / N", "Search in the result, next / previous match"},
	{"v · y", "Visual selection · yank it (the buffer refuses edits)"},
	{"za / zc / zo", "Toggle / close / open the fold under the cursor"},
	{"zM / zR", "Close / open every fold"},
	{"ctrl+y", "Copy the whole result"},
	{"ctrl+o", "Open the result as a scratch file"},
	{"ctrl+g", "Open the language cheatsheet"},
	{"esc", "Close the playground from resting normal mode"},
	{"esc esc", "Close and open the command palette"},
}

// playgroundHelpGroups returns the cheatsheet groups for the open, focused
// playground — none at all when the mode is closed or another pane holds the
// keyboard, in which case the playground's keys do not apply and saying
// otherwise would be a lie. The labels name the live dialect (#2039), so a yq
// session is not told about "jq" keys.
func (m Model) playgroundHelpGroups() []help.Group {
	if !m.playFocused() {
		return nil
	}
	name := m.play.dialect.Name()
	groups := []help.Group{
		playHelpGroup(name+" playground — query line", "play.query.", playQueryHelpKeys),
		playHelpGroup(name+" playground — result buffer", "play.result.", playResultHelpKeys),
	}
	if extra := m.playgroundChordHelp(); len(extra.Entries) > 0 {
		groups = append(groups, extra)
	}
	return groups
}

// playHelpGroup turns one key table into a Focused help group. The ids are
// prefixed per table so the two groups never collide in a filtered sheet.
func playHelpGroup(label, prefix string, keys []struct{ Key, Title string }) help.Group {
	g := help.Group{Label: label, Focused: true}
	for _, k := range keys {
		g.Entries = append(g.Entries, help.Entry{ID: prefix + k.Key, Title: k.Title, Shortcut: k.Key})
	}
	return g
}

// playgroundChordHelp lists the keymap-bound chords that still reach the
// playground, resolved live so a rebind is reflected instead of a hard-coded
// default being advertised: the full-query-view toggle, the copy chord the
// result buffer's selection answers to (#2062), and the code-action chord —
// which the playground answers with an honest "not available here" rather
// than the silent nothing it used to do (#2237).
func (m Model) playgroundChordHelp() help.Group {
	g := help.Group{Label: "playground — keymap chords", Focused: true}
	add := func(command, title string) {
		if chord, ok := m.playChordFor(command); ok {
			g.Entries = append(g.Entries, help.Entry{ID: command, Title: title, Shortcut: chord})
		}
	}
	add("json.jqQueryView", "Toggle the full (multi-line) query view")
	add("editor.copy", "Copy the result buffer's selection (from either focus)")
	add("lsp.codeAction", "Not available in the playground — says so instead of doing nothing")
	return g
}

// playChordFor names the chord bound to command in the live table, whatever
// scope it sits in — the cheatsheet wants the key the user would actually
// press, and the playground's own routing already decides what happens then.
func (m Model) playChordFor(command string) (string, bool) {
	if m.bindings == nil || m.bindings.Table() == nil {
		return "", false
	}
	for _, b := range m.bindings.Table().Bindings() {
		if b.Command == command {
			return b.Chord.String(), true
		}
	}
	return "", false
}
