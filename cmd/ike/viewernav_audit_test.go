package main

import (
	"sort"
	"strings"
	"testing"

	"ike/internal/keymap"
)

// viewernav_audit_test.go is the standing ledger of the viewer-navigation
// audit (#2698). The viewer panes — archive listing, hex dump, notebook, diff,
// data grid, markdown/image preview — hold a file the way an editor does: one
// sits in them and reads. They inherited the Global chords, but not the ones
// the editor context owned alone, so the everyday navigation a user reaches
// for from *any* content pane stopped at their door. Telemetry caught it twice
// in one week: ctrl+e in the archive viewer (recent files) and shift+f11 in
// the notebook (next bookmark), both recorded unbound.
//
// The rule this test enforces: a default binding in the Editor context whose
// command is pure navigation — it opens a picker, walks the project, or
// handles tabs, and never touches buffer text — is available in every viewer
// context too, or carries an entry below saying why it is not. A future
// Editor-only navigation chord therefore fails the build until someone
// decides: viewer rows, or a recorded reason.
//
// The counterpart ledger for commands with no chord at all is
// keybind_audit_test.go; this one is about chords that exist but stop short.

// navFamilies are the command-id prefixes the audit treats as navigation.
// They are the families whose commands resolve a *pane or a project*, never
// the caret: the palette openers, the project/workspace commands, the
// navigation history and bookmarks, the search steppers, the tab and pane and
// window handling. Everything outside them — editor.*, lsp.*, http.*, run.*,
// view.*, terminal.send* — acts on a buffer, a caret or a symbol, and has no
// meaning in a pane that holds neither.
//
// The list is deliberately forward-looking: a family with no Editor-context
// binding today is not stale, it is a guard for the day one lands.
var navFamilies = []string{
	"palette.",
	"project.",
	"nav.",
	"bookmark.",
	"search.",
	"editor.tab.",
	"pane.",
	"window.",
}

// The reasons, grouped so the ledger reads as an audit rather than as an
// opt-out list.
const (
	// The command needs a caret line in a text buffer, which is exactly what
	// a viewer pane has not got.
	reasonCaretLine = "needs a caret line in a text buffer"
	// The viewer spends the chord on a key of its own. A table row would
	// resolve ahead of the pane and take the key away, so the chord stops at
	// that one viewer and the palette is the doorway there.
	reasonViewerKey = "the viewer spends the chord on a pane key of its own"
)

// viewerNavExceptions records the navigation chords that stay editor-only.
// A bare command id excuses the command in every viewer context; a
// "<command>@<context>" key excuses one viewer only, which is the interesting
// case — the chord ships everywhere else.
var viewerNavExceptions = map[string]string{
	// #55: both toggles bookmark the focused editor's cursor line
	// (bookmarkTarget notifies and gives up without one). Stepping through
	// the bookmarks needs no caret and went Global in this audit; setting one
	// stays where a line exists.
	"bookmark.toggle":         reasonCaretLine,
	"bookmark.toggleMnemonic": reasonCaretLine,
	// nbview scrolls one line on ctrl+e / ctrl+y, the vim pair, next to its
	// ctrl+d / ctrl+u half pages.
	"palette.recentFiles@notebook": reasonViewerKey,
	// #496: ctrl+e returns from the diff viewer's edit mode, the chord that
	// opened it.
	"palette.recentFiles@diff": reasonViewerKey,
}

// navCommand reports whether id belongs to one of the navigation families.
func navCommand(id string) bool {
	for _, f := range navFamilies {
		if strings.HasPrefix(id, f) {
			return true
		}
	}
	return false
}

// TestViewerNavChordsReachTheViewers is the audit's guardrail: every
// Editor-context navigation chord in the default table resolves to the same
// command in every viewer context, or is excused above. Both platforms are
// checked — the Cmd→Ctrl fold makes a chord land differently off macOS, and a
// gap that only exists on one platform is still a gap.
func TestViewerNavChordsReachTheViewers(t *testing.T) {
	used := map[string]bool{}
	for _, goos := range []string{"darwin", "linux"} {
		defs := keymap.DefaultsFor(keymap.PresetJetBrains, goos)
		table := keymap.BuildTable(defs, nil, goos)
		for _, b := range defs {
			if b.Context.Base() != keymap.Editor || !navCommand(b.Command) {
				continue
			}
			chord := keymap.NormalizeChord(b.Chord, goos)
			for _, ctx := range keymap.ViewerContexts {
				name := keymap.ContextName(ctx)
				switch {
				case viewerNavExceptions[b.Command] != "":
					used[b.Command] = true
					continue
				case viewerNavExceptions[b.Command+"@"+name] != "":
					used[b.Command+"@"+name] = true
					continue
				}
				got, ok := table.Lookup(chord, ctx)
				if !ok || got.Command != b.Command {
					t.Errorf("%s: %s (%s) is bound in the editor but not in the %s viewer "+
						"(got %+v ok=%v) — add the viewer rows in internal/keymap/defaults.go "+
						"(multiRows), or record why it stays editor-only in viewerNavExceptions (#2698)",
						goos, b.Chord, b.Command, name, got, ok)
				}
			}
		}
	}
	// Keep the ledger honest in the other direction: an entry for a command
	// that has since gained its viewer rows — or that no longer exists — is
	// stale and would silence a future gap.
	var stale []string
	for key := range viewerNavExceptions {
		if !used[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	for _, key := range stale {
		t.Errorf("stale viewerNavExceptions entry %q: no Editor-context navigation "+
			"binding needs it anymore", key)
	}
}

// TestViewerNavAuditList is the audit list itself, checked in rather than run
// once and thrown away (#2698): the Editor-context navigation chords and what
// became of each. It fails when the set changes, so a new one is a decision
// someone takes, not a row that slips in.
func TestViewerNavAuditList(t *testing.T) {
	// command → the chords the Editor context binds it on, plus the verdict.
	want := map[string]string{
		"palette.recentFiles":     "ctrl+e — viewer rows, except notebook/diff (pane key)",
		"editor.tab.new":          "ctrl+t — viewer rows (a viewer is a tab)",
		"editor.tab.togglePin":    "alt+shift+p — viewer rows (a viewer is a tab)",
		"bookmark.toggle":         "f11 — editor-only, " + reasonCaretLine,
		"bookmark.toggleMnemonic": "alt+f3 — editor-only, " + reasonCaretLine,
	}
	got := map[string][]string{}
	for _, b := range keymap.DefaultsFor(keymap.PresetJetBrains, "darwin") {
		if b.Context.Base() != keymap.Editor || !navCommand(b.Command) {
			continue
		}
		got[b.Command] = append(got[b.Command], b.Chord.String())
	}
	for id, chords := range got {
		sort.Strings(chords)
		if _, ok := want[id]; !ok {
			t.Errorf("%s (%s) is a new Editor-context navigation binding: decide whether it "+
				"belongs in the viewer contexts, then record it in this list (#2698)",
				id, strings.Join(chords, ", "))
		}
		t.Logf("%-24s %-14s %s", id, strings.Join(chords, ", "), want[id])
	}
	for id := range want {
		if _, ok := got[id]; !ok {
			t.Errorf("stale audit-list entry %q: the editor no longer binds it", id)
		}
	}
}
