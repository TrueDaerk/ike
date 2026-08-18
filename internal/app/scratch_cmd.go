package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/lang"
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
	}
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

// scratchList adapts scratch.List for the palette's injected source: a store
// error just lists nothing (the palette shows its empty hint), matching how
// the MRU list degrades.
func scratchList() []string {
	paths, err := scratch.List()
	if err != nil {
		return nil
	}
	return paths
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
	// An open Scratch Files panel (#1932) shows the new file right away.
	m.refreshScratchPanel()
	return m.openPath(path, false)
}
