package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/fuzzy"
	"ike/internal/host"
	"ike/internal/jqplay"
	"ike/internal/palette"
	"ike/internal/ui"
)

// jqfilters.go is the user-facing half of the named saved-filter library
// (#1995): the save/rename name prompt (the save-layout prompt's shape, #1175)
// and the palette picker listing both scopes' filters — enter inserts the
// program into the playground's query line, shift+delete removes the entry in
// place (#1113's aux convention).
//
// The playground's `↑` history (#1977) is scratch work that rotates and dies
// with the session. A filter that took an afternoon to get right — the
// mapping-flattening one for an Elasticsearch response — is the opposite: it
// is worth a name and a file. Two files, in fact, and which one a filter goes
// into is the only decision the save prompt asks for:
//
//   - **project** — `.ike/jqfilters.json`, next to the HTTP environment
//     selection: filters shaped by *this* project's data.
//   - **global** — `~/.ike/jqfilters-global.json`, next to the saved window
//     layouts: filters that are about jq, not about a project.
//
// The store itself (load, save, add, rename, delete) is
// jqplay.Library — pure and path-agnostic, so the two scopes share one
// implementation; this file owns the two paths, the prompt and the picker.

// jqFiltersPrefix selects the filter picker inside the palette. The root model
// only ever opens it locked, so the rune has no user-facing prefix story; it
// only has to be unique among the registered modes.
const jqFiltersPrefix = '}'

// SaveJQFilterPromptMsg starts the json.jqSaveFilter name prompt over the
// playground's current program.
type SaveJQFilterPromptMsg struct{}

// ShowJQFiltersMsg opens the saved-filter picker. Rename switches enter from
// "insert into the query line" to "rename this entry", which is how the
// picker manages the library without needing a second modal on top of it.
type ShowJQFiltersMsg struct{ Rename bool }

// InsertJQFilterMsg puts one saved filter's program on the query line.
type InsertJQFilterMsg struct {
	Scope jqplay.Scope
	Name  string
}

// RenameJQFilterPromptMsg opens the name prompt over an existing entry.
type RenameJQFilterPromptMsg struct {
	Scope jqplay.Scope
	Name  string
}

// DeleteJQFilterMsg removes one saved filter from its scope's store.
type DeleteJQFilterMsg struct {
	Scope jqplay.Scope
	Name  string
}

// jqFilterFile returns the store path of one scope, following the layout
// store's IKE_CONFIG_DIR redirection seam. The two scopes use *different file
// names* under that redirection (the winsize.json / winsize-global.json
// precedent, #1714), so a test — or a user — pointing IKE_CONFIG_DIR at one
// directory still gets two distinct libraries. Without the redirection the
// project store is relative (`.ike` resolves against the project the process
// chdir'd into) and the global one lives in ~/.ike, never in a project.
func jqFilterFile(scope jqplay.Scope) string {
	if scope == jqplay.ScopeGlobal {
		if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
			return filepath.Join(d, "jqfilters-global.json")
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".ike", "jqfilters-global.json")
	}
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "jqfilters.json")
	}
	return filepath.Join(".ike", "jqfilters.json")
}

// loadJQFilters reads one scope's library. Like every other state store a
// missing or malformed file is an empty library, never an error.
func loadJQFilters(scope jqplay.Scope) *jqplay.Library {
	return jqplay.LoadLibrary(jqFilterFile(scope))
}

// saveJQFilters persists one scope's library. Unlike the swallowed errors of
// the layout and session stores this one is reported: the user asked for the
// save and has to know it did not happen.
func (m *Model) saveJQFilters(scope jqplay.Scope, lib *jqplay.Library) bool {
	if err := lib.Save(jqFilterFile(scope)); err != nil {
		m.host.Notify(host.Warn, "jq filters: "+err.Error())
		return false
	}
	return true
}

// jqFilterEntry is one row of the picker: the filter plus the scope it came
// from, which the row renders and every action needs to route back to the
// right store.
type jqFilterEntry struct {
	Scope jqplay.Scope
	jqplay.Filter
}

// jqFilterEntries collects both libraries, project first. A name may exist in
// both scopes — they are separate stores, and shadowing one with the other
// would hide a filter the user saved on purpose — so both rows are listed,
// told apart by their scope badge.
func jqFilterEntries() []jqFilterEntry {
	var out []jqFilterEntry
	for _, scope := range jqplay.Scopes() {
		for _, f := range loadJQFilters(scope).All() {
			out = append(out, jqFilterEntry{Scope: scope, Filter: f})
		}
	}
	return out
}

// jqFilterPreviewWidth caps the program preview in the picker's detail chip.
// The chip is pinned right against the name column, so a whole pipeline would
// crowd out the thing the row is filtered by.
const jqFilterPreviewWidth = 48

// jqFiltersMode is the palette Mode listing the saved filters of both scopes.
// rename is flipped by the root model before each locked open, the way
// layoutsMode flips its set-default action (#1175).
type jqFiltersMode struct {
	entries []jqFilterEntry
	rename  bool
}

func newJQFiltersMode() *jqFiltersMode { return &jqFiltersMode{} }

// Prefix implements palette.Mode.
func (j *jqFiltersMode) Prefix() rune { return jqFiltersPrefix }

// Refresh implements palette.Refresher: the entries are re-read from both
// stores on every open and after every aux delete, so the list never shows a
// filter another window (or the delete that just happened) removed.
func (j *jqFiltersMode) Refresh() { j.entries = jqFilterEntries() }

// Placeholder implements palette.Mode.
func (j *jqFiltersMode) Placeholder() string {
	if j.rename {
		return "Rename saved jq filter…"
	}
	return "Saved jq filter…"
}

// Results implements palette.Mode: rows fuzzy-matched over the *name* — the
// thing a saved filter is remembered by — with the program as the row's
// detail chip so the pipeline behind a name is visible without picking it.
// The scope rides along as the accent badge, which is what tells a project
// filter from a global one at a glance. Every row deletes via the aux action.
func (j *jqFiltersMode) Results(query string, _ palette.Context) []palette.Item {
	var items []palette.Item
	for _, e := range j.entries {
		res, ok := fuzzy.Match(query, e.Name)
		if !ok {
			continue
		}
		it := palette.Item{
			Title:  e.Name,
			Spans:  res.Positions,
			Score:  res.Score,
			Badge:  e.Scope.String(),
			Detail: jqplay.Preview(e.Program, jqFilterPreviewWidth),
			Aux:    DeleteJQFilterMsg{Scope: e.Scope, Name: e.Name},
		}
		if j.rename {
			it.Msg = RenameJQFilterPromptMsg{Scope: e.Scope, Name: e.Name}
		} else {
			it.Msg = InsertJQFilterMsg{Scope: e.Scope, Name: e.Name}
		}
		items = append(items, it)
	}
	return items
}

// openJQFilterPicker fills and opens the picker locked to the filters mode.
// An empty library explains where filters come from instead of showing an
// empty list — the layout picker's rule.
func (m *Model) openJQFilterPicker(rename bool) {
	m.jqFilters.rename = rename
	m.jqFilters.Refresh()
	if len(m.jqFilters.entries) == 0 {
		m.host.Notify(host.Info, "no saved jq filters — use \"Save jq Filter\" in the playground first")
		return
	}
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, jqFiltersPrefix)
}

// insertJQFilter puts the saved program on the query line and evaluates it.
// With no playground up the command still works: it opens one over the JSON
// at hand first, so picking a filter is a complete action from anywhere — and
// when there is no JSON to query, startJQPlayground has already said so.
func (m *Model) insertJQFilter(msg InsertJQFilterMsg) tea.Cmd {
	f, ok := loadJQFilters(msg.Scope).Get(msg.Name)
	if !ok {
		m.host.Notify(host.Warn, "jq filter "+msg.Name+" is gone")
		return nil
	}
	var cmds []tea.Cmd
	if !m.jqPlayOpen() {
		cmds = append(cmds, m.startJQPlayground(false))
		if !m.jqPlayOpen() {
			return nil
		}
	}
	s := m.jqPlay
	s.program, s.pos = f.Program, len([]rune(f.Program))
	s.histIdx, s.comp = -1, nil
	s.setBufFocus(false)
	s.status = "inserted filter " + f.Name
	m.sizeJQResult() // the inserted program may change the expanded header's height (#2032)
	// A newer generation supersedes the seed run the open above may have
	// started, so the filter — not the seed — is what the buffer shows.
	return tea.Batch(append(cmds, m.runJQNow())...)
}

// deleteJQFilter removes one entry and refreshes the open picker in place.
func (m *Model) deleteJQFilter(msg DeleteJQFilterMsg) {
	lib := loadJQFilters(msg.Scope)
	if err := lib.Delete(msg.Name); err != nil {
		return
	}
	if !m.saveJQFilters(msg.Scope, lib) {
		return
	}
	m.palette.Refresh()
	m.host.Notify(host.Info, "deleted the "+msg.Scope.String()+" jq filter "+msg.Name)
}

// jqNamePrompt is the shell prompt naming a filter, in its two spellings: the
// save (a fresh name for the program on the query line, with a scope to pick)
// and the rename (an existing entry's name, its scope fixed). One prompt for
// both — they differ only in the heading and in what enter does.
type jqNamePrompt struct {
	open    bool
	rename  bool
	input   string
	pos     int
	err     string
	scope   jqplay.Scope
	program string // save: the query line snapshot the name is given to
	from    string // rename: the entry's current name
}

// jqNamePromptOpen reports whether the shell shows the filter name prompt.
func (m Model) jqNamePromptOpen() bool { return m.jqName.open && m.shell.IsOpen() }

// startJQSavePrompt opens the save prompt over the playground's current
// program (json.jqSaveFilter, ctrl+s in the query line). It refuses early
// when there is nothing to name, so the prompt is never typed for nothing.
func (m *Model) startJQSavePrompt() {
	if !m.jqPlayOpen() {
		m.host.Notify(host.Info, "jq filters: open the jq playground first")
		return
	}
	program := strings.TrimSpace(m.jqPlay.program)
	if program == "" || program == "." {
		// The identity program is the playground's default, not a filter.
		m.jqPlay.status = "nothing to save — write a program first"
		return
	}
	m.jqName = jqNamePrompt{open: true, program: program, scope: jqplay.ScopeProject}
	m.openJQNamePrompt()
}

// startJQRenamePrompt opens the prompt over an existing entry, prefilled with
// its current name so a typo is fixed by editing rather than by retyping.
func (m *Model) startJQRenamePrompt(msg RenameJQFilterPromptMsg) {
	if !loadJQFilters(msg.Scope).Has(msg.Name) {
		m.host.Notify(host.Warn, "jq filter "+msg.Name+" is gone")
		return
	}
	m.jqName = jqNamePrompt{open: true, rename: true, scope: msg.Scope, from: msg.Name, input: msg.Name}
	m.jqName.pos = len([]rune(m.jqName.input))
	m.openJQNamePrompt()
}

// openJQNamePrompt renders the prompt and puts it on screen.
func (m *Model) openJQNamePrompt() {
	m.renderJQNamePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// renderJQNamePrompt (re)fills the shell for the current input. The save
// spelling carries the program being named and the scope toggle; the rename
// spelling neither — its scope is the entry's, and moving a filter between
// stores is a save under the other scope.
func (m *Model) renderJQNamePrompt() {
	p := m.jqName
	line := "> " + ui.CursorView(p.input, p.pos)
	body := line
	if p.rename {
		body += "\n\nrenaming the " + p.scope.String() + " filter " + p.from
	} else {
		body += "\n\nprogram: " + jqplay.Preview(p.program, jqFilterPreviewWidth)
		body += "\nscope:   " + p.scope.String()
	}
	if p.err != "" {
		body += "\n\n" + p.err
	}
	hint := "\n\nenter save · esc cancel"
	if !p.rename {
		hint = "\n\nenter save · tab " + p.scope.Other().String() + " scope · esc cancel"
	}
	heading := "Save jq filter as"
	if p.rename {
		heading = "Rename jq filter"
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: heading,
		Body:    func() string { return body + hint },
	})
}

// updateJQNamePrompt consumes every key while the prompt is open: enter
// commits (a name already taken in the target scope asks for a second enter
// before overwriting, the save-layout guard), tab switches the scope of a
// save, esc cancels, everything else is line editing.
func (m Model) updateJQNamePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape:
		m.closeJQNamePrompt()
		return m, nil
	case msg.Code == tea.KeyTab && !m.jqName.rename:
		m.jqName.scope = m.jqName.scope.Other()
		m.jqName.err = "" // the other scope's store re-arms the overwrite guard
		m.renderJQNamePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		if m.commitJQNamePrompt() {
			m.closeJQNamePrompt()
		} else {
			m.renderJQNamePrompt()
		}
		return m, nil
	}
	if out, pos, handled, changed := ui.EditKey(msg, m.jqName.input, m.jqName.pos); handled {
		m.jqName.input, m.jqName.pos = out, pos
		if changed {
			m.jqName.err = "" // a new name re-arms the overwrite guard
		}
		m.renderJQNamePrompt()
	}
	return m, nil
}

// commitJQNamePrompt applies the prompt, reporting whether it is done — false
// leaves it open holding the message it just set (a taken name, a refused
// overwrite, a store that would not write).
func (m *Model) commitJQNamePrompt() bool {
	p := &m.jqName
	name := strings.TrimSpace(p.input)
	if name == "" {
		p.err = "a saved filter needs a name"
		return false
	}
	lib := loadJQFilters(p.scope)
	if p.rename {
		switch err := lib.Rename(p.from, name); {
		case errors.Is(err, jqplay.ErrNameTaken):
			p.err = "that name is taken in the " + p.scope.String() + " library"
			return false
		case err != nil:
			p.err = err.Error()
			return false
		}
		if !m.saveJQFilters(p.scope, lib) {
			return false
		}
		m.palette.Refresh()
		m.host.Notify(host.Info, "renamed the jq filter to "+name)
		return true
	}
	if lib.Has(name) && p.err == "" {
		// Never silently clobber a saved filter: the second enter confirms —
		// which is also how an edited filter is written back under its name.
		p.err = "filter exists — enter again overwrites"
		return false
	}
	if err := lib.Set(name, p.program); err != nil {
		p.err = err.Error()
		return false
	}
	if !m.saveJQFilters(p.scope, lib) {
		return false
	}
	if m.jqPlayOpen() {
		m.jqPlay.status = "saved " + p.scope.String() + " filter " + name
	}
	m.host.Notify(host.Info, "saved the "+p.scope.String()+" jq filter "+name)
	return true
}

// closeJQNamePrompt drops the prompt and its shell.
func (m *Model) closeJQNamePrompt() {
	m.jqName = jqNamePrompt{}
	m.shell.Close()
}

// pasteJQNamePrompt inserts a bracketed paste into the name input at its
// cursor (#1873), flattened like every other single-field prompt.
func (m *Model) pasteJQNamePrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.jqName.input, m.jqName.pos, text)
	if !changed {
		return false
	}
	m.jqName.input, m.jqName.pos = out, pos
	m.jqName.err = "" // a new name re-arms the overwrite guard
	m.renderJQNamePrompt()
	return true
}
