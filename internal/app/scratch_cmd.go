package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/palette"
	"ike/internal/plugin"
	"ike/internal/scratch"
)

// scratch_cmd.go surfaces the scratch store (Roadmap 0280, #351) as palette
// commands: "New Scratch File" — which asks for the language through the
// picker in scratch_new_mode.go (#1223) — plus one no-prompt variant per
// registered language, so a bound chord or a script can create a Python or
// PHP scratch without going through the overlay.

// scratchTextCommandID is the no-prompt plain-text creator. scratch.new used
// to be that command; it now opens the language picker, so the direct .txt
// path needs an id of its own (the picker's "Plain Text" row runs it).
const scratchTextCommandID = "scratch.new.text"

// scratchCommands builds the scratch.new command family. It runs on every
// registry query (Capabilities is lazy), so languages registered later —
// plugins, tests — appear without ordering constraints.
func scratchCommands() []plugin.Command {
	cmds := []plugin.Command{
		appCommand("scratch.new", "New Scratch File…", ShowNewScratchMsg{}),
		appCommand(scratchTextCommandID, "New Scratch File: Plain Text", NewScratchMsg{Ext: "txt"}),
		appCommand("scratch.list", "Open Scratch File…", ShowScratchFilesMsg{}),
		appCommand(scratchManageCommandID, "Manage Scratch Files…", ShowScratchManagerMsg{}),
	}
	// The test-data generator (#2134) is a scratch producer too, so its
	// command family is built alongside the creators.
	cmds = append(cmds, generateCommands()...)
	for _, l := range lang.All() {
		if len(l.Extensions) == 0 {
			continue
		}
		cmds = append(cmds, appCommand(
			"scratch.new."+l.ID,
			"New Scratch File: "+langTitle(l.ID),
			NewScratchMsg{Ext: l.Extensions[0]},
		))
	}
	return cmds
}

// langTitle renders a language id for command titles: short ids read as
// acronyms ("go" → "GO", "php" → "PHP"), longer ones capitalize ("python" →
// "Python"). The registry has no display-name field; this heuristic keeps it
// a leaf concern of the palette surface.
func langTitle(id string) string {
	if len(id) <= 3 {
		return strings.ToUpper(id)
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

// scratchList adapts scratch.List for the '@' finder's injected source: a
// store error just lists nothing (the palette shows its empty hint), matching
// how the MRU list degrades.
func scratchList() []string {
	paths, err := scratch.List()
	if err != nil {
		return nil
	}
	return paths
}

// scratchEmptyTitle is the row title for a scratch with no content yet (or
// none within FirstLine's scan cap) — the empty-scratch placeholder AC of
// #2057.
const scratchEmptyTitle = "Empty scratch"

// scratchEntries adapts scratch.Entries for scratch.list's palette source
// (#2057): each row's title is the scratch's first non-empty content line —
// read lazily by scratch.FirstLine, never a full load — falling back to
// scratchEmptyTitle, and its Detail chip names the language resolved from the
// path, falling back to "Plain Text" for an unregistered extension. A store
// error just lists nothing, matching how the MRU list degrades.
func scratchEntries() []palette.ScratchEntry {
	entries, err := scratch.Entries()
	if err != nil {
		return nil
	}
	out := make([]palette.ScratchEntry, len(entries))
	for i, e := range entries {
		title := scratch.FirstLine(e.Path)
		if title == "" {
			title = scratchEmptyTitle
		}
		langName := "Plain Text"
		if l, ok := lang.ByPath(e.Path); ok {
			langName = langTitle(l.ID)
		}
		out[i] = palette.ScratchEntry{Path: e.Path, Title: title, Lang: langName}
	}
	return out
}

// newScratch creates a scratch with the requested extension and opens it
// through the standard funnel, so highlighting, LSP, tabs and session restore
// all apply unchanged and the new scratch ends focused.
func (m Model) newScratch(ext string) (tea.Model, tea.Cmd) {
	path, err := scratch.Create(ext)
	if err != nil {
		m.host.Notify(host.Warn, "scratch: "+err.Error())
		return m, nil
	}
	// The explorer's Scratches section (#1963) shows the new file right away.
	m.explorer().RefreshScratches()
	return m.openPath(path, false)
}
