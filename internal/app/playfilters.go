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

// playfilters.go is the user-facing half of the named saved-filter library
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
// Since #2039 the same two scopes exist per **dialect**: the yq playground
// writes `yqfilters.json` and `yqfilters-global.json`. The libraries are kept
// apart rather than tagged, because a saved filter is written against a shape
// of document — `.spec.template.spec.containers[0].image` is a Kubernetes
// manifest filter and has no business in the picker over a JSON API response.
// Nothing else is duplicated: the store, the prompt and the picker are one
// implementation parameterized by the dialect, which only ever decides the two
// file names and the words on the screen.
//
// The store itself (load, save, add, rename, delete) is jqplay.Library — pure
// and path-agnostic, so all four libraries share one implementation; this file
// owns the paths, the prompt and the picker.

// playFiltersPrefix selects the filter picker inside the palette. The root model
// only ever opens it locked, so the rune has no user-facing prefix story; it
// only has to be unique among the registered modes.
const playFiltersPrefix = '}'

// SaveFilterPromptMsg starts the json.jqSaveFilter name prompt over the
// playground's current program. It carries no dialect: the program being named
// is the open playground's, so its library is the one to write.
type SaveFilterPromptMsg struct{}

// ShowFiltersMsg opens the saved-filter picker over one dialect's libraries.
// Rename switches enter from "insert into the query line" to "rename this
// entry", which is how the picker manages the library without needing a second
// modal on top of it.
type ShowFiltersMsg struct {
	Dialect jqplay.Dialect
	Rename  bool
}

// InsertFilterMsg puts one saved filter's program on the query line.
type InsertFilterMsg struct {
	Dialect jqplay.Dialect
	Scope   jqplay.Scope
	Name    string
}

// RenameFilterPromptMsg opens the name prompt over an existing entry.
type RenameFilterPromptMsg struct {
	Dialect jqplay.Dialect
	Scope   jqplay.Scope
	Name    string
}

// DeleteFilterMsg removes one saved filter from its scope's store.
type DeleteFilterMsg struct {
	Dialect jqplay.Dialect
	Scope   jqplay.Scope
	Name    string
}

// playFilterFile returns the store path of one dialect's scope, following the
// layout store's IKE_CONFIG_DIR redirection seam. Every store uses a
// *different file name* under that redirection (the winsize.json /
// winsize-global.json precedent, #1714), so a test — or a user — pointing
// IKE_CONFIG_DIR at one directory still gets four distinct libraries. Without
// the redirection the project store is relative (`.ike` resolves against the
// project the process chdir'd into) and the global one lives in ~/.ike, never
// in a project.
func playFilterFile(d jqplay.Dialect, scope jqplay.Scope) string {
	name := d.Name() + "filters.json"
	if scope == jqplay.ScopeGlobal {
		name = d.Name() + "filters-global.json"
	}
	if dir := os.Getenv("IKE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, name)
	}
	if scope == jqplay.ScopeGlobal {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, ".ike", name)
	}
	return filepath.Join(".ike", name)
}

// loadPlayFilters reads one library. Like every other state store a missing or
// malformed file is an empty library, never an error.
func loadPlayFilters(d jqplay.Dialect, scope jqplay.Scope) *jqplay.Library {
	return jqplay.LoadLibrary(playFilterFile(d, scope))
}

// savePlayFilters persists one library. Unlike the swallowed errors of the
// layout and session stores this one is reported: the user asked for the save
// and has to know it did not happen.
func (m *Model) savePlayFilters(d jqplay.Dialect, scope jqplay.Scope, lib *jqplay.Library) bool {
	if err := lib.Save(playFilterFile(d, scope)); err != nil {
		m.host.Notify(host.Warn, d.Name()+" filters: "+err.Error())
		return false
	}
	return true
}

// playFilterEntry is one row of the picker: the filter plus the scope it came
// from, which the row renders and every action needs to route back to the
// right store.
type playFilterEntry struct {
	Scope jqplay.Scope
	jqplay.Filter
}

// playFilterEntries collects one dialect's two libraries, project first. A
// name may exist in both scopes — they are separate stores, and shadowing one
// with the other would hide a filter the user saved on purpose — so both rows
// are listed, told apart by their scope badge.
func playFilterEntries(d jqplay.Dialect) []playFilterEntry {
	var out []playFilterEntry
	for _, scope := range jqplay.Scopes() {
		for _, f := range loadPlayFilters(d, scope).All() {
			out = append(out, playFilterEntry{Scope: scope, Filter: f})
		}
	}
	return out
}

// playFilterPreviewWidth caps the program preview in the picker's detail chip.
// The chip is pinned right against the name column, so a whole pipeline would
// crowd out the thing the row is filtered by.
const playFilterPreviewWidth = 48

// playFiltersMode is the palette Mode listing one dialect's saved filters in
// both scopes. dialect and rename are flipped by the root model before each
// locked open, the way layoutsMode flips its set-default action (#1175) — one
// mode object serves both playgrounds, since only the store paths and the
// placeholder differ.
type playFiltersMode struct {
	entries []playFilterEntry
	dialect jqplay.Dialect
	rename  bool
}

func newPlayFiltersMode() *playFiltersMode { return &playFiltersMode{} }

// Prefix implements palette.Mode.
func (j *playFiltersMode) Prefix() rune { return playFiltersPrefix }

// Refresh implements palette.Refresher: the entries are re-read from both
// stores on every open and after every aux delete, so the list never shows a
// filter another window (or the delete that just happened) removed.
func (j *playFiltersMode) Refresh() { j.entries = playFilterEntries(j.dialect) }

// Placeholder implements palette.Mode.
func (j *playFiltersMode) Placeholder() string {
	if j.rename {
		return "Rename saved " + j.dialect.Name() + " filter…"
	}
	return "Saved " + j.dialect.Name() + " filter…"
}

// Results implements palette.Mode: rows fuzzy-matched over the *name* — the
// thing a saved filter is remembered by — with the program as the row's
// detail chip so the pipeline behind a name is visible without picking it.
// The scope rides along as the accent badge, which is what tells a project
// filter from a global one at a glance. Every row deletes via the aux action.
func (j *playFiltersMode) Results(query string, _ palette.Context) []palette.Item {
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
			Detail: jqplay.Preview(e.Program, playFilterPreviewWidth),
			Aux:    DeleteFilterMsg{Dialect: j.dialect, Scope: e.Scope, Name: e.Name},
		}
		if j.rename {
			it.Msg = RenameFilterPromptMsg{Dialect: j.dialect, Scope: e.Scope, Name: e.Name}
		} else {
			it.Msg = InsertFilterMsg{Dialect: j.dialect, Scope: e.Scope, Name: e.Name}
		}
		items = append(items, it)
	}
	return items
}

// openPlayFilterPicker fills and opens the picker locked to the filters mode,
// over the dialect asked for. The commands name their own (json.jqFilters vs
// yaml.yqFilters); the query line's ctrl+l passes the open playground's, which
// is what makes the chord mean "my filters" in either mode. An empty library
// explains where filters come from instead of showing an empty list — the
// layout picker's rule.
func (m *Model) openPlayFilterPicker(d jqplay.Dialect, rename bool) {
	m.playFilters.dialect, m.playFilters.rename = d, rename
	m.playFilters.Refresh()
	if len(m.playFilters.entries) == 0 {
		m.host.Notify(host.Info, "no saved "+d.Name()+" filters — use \"Save Playground Filter\" in the playground first")
		return
	}
	m.palette.SetSize(m.width, m.height)
	m.palette.OpenLocked(palette.Context{ContextID: m.focusContext(), Root: "."}, playFiltersPrefix)
}

// insertPlayFilter puts the saved program on the query line and evaluates it.
// With no playground up the command still works: it opens one of the filter's
// own dialect over the document at hand first, so picking a filter is a
// complete action from anywhere — and when there is nothing to query,
// startPlayground has already said so. A filter picked while the *other*
// dialect's playground is open replaces it, which is the only way its program
// can run against the right kind of document.
func (m *Model) insertPlayFilter(msg InsertFilterMsg) tea.Cmd {
	f, ok := loadPlayFilters(msg.Dialect, msg.Scope).Get(msg.Name)
	if !ok {
		m.host.Notify(host.Warn, msg.Dialect.Name()+" filter "+msg.Name+" is gone")
		return nil
	}
	var cmds []tea.Cmd
	if s := m.play; s == nil || s.dialect != msg.Dialect {
		cmds = append(cmds, m.startPlayground(msg.Dialect, false))
		if !m.playOpen() {
			return nil
		}
	}
	s := m.play
	s.program, s.pos = f.Program, len([]rune(f.Program))
	s.histIdx, s.comp = -1, nil
	s.setBufFocus(false)
	s.status = "inserted filter " + f.Name
	m.sizePlayResult() // the inserted program may change the expanded header's height (#2032)
	// A newer generation supersedes the seed run the open above may have
	// started, so the filter — not the seed — is what the buffer shows.
	return tea.Batch(append(cmds, m.runPlayNow())...)
}

// deletePlayFilter removes one entry and refreshes the open picker in place.
func (m *Model) deletePlayFilter(msg DeleteFilterMsg) {
	lib := loadPlayFilters(msg.Dialect, msg.Scope)
	if err := lib.Delete(msg.Name); err != nil {
		return
	}
	if !m.savePlayFilters(msg.Dialect, msg.Scope, lib) {
		return
	}
	m.palette.Refresh()
	m.host.Notify(host.Info, "deleted the "+msg.Scope.String()+" "+msg.Dialect.Name()+" filter "+msg.Name)
}

// playNamePrompt is the shell prompt naming a filter, in its two spellings: the
// save (a fresh name for the program on the query line, with a scope to pick)
// and the rename (an existing entry's name, its scope fixed). One prompt for
// both — they differ only in the heading and in what enter does.
type playNamePrompt struct {
	open    bool
	rename  bool
	input   string
	pos     int
	err     string
	dialect jqplay.Dialect // which pair of libraries the name lands in (#2039)
	scope   jqplay.Scope
	program string // save: the query line snapshot the name is given to
	from    string // rename: the entry's current name
}

// playNamePromptOpen reports whether the shell shows the filter name prompt.
func (m Model) playNamePromptOpen() bool { return m.playName.open && m.shell.IsOpen() }

// startPlaySavePrompt opens the save prompt over the playground's current
// program (json.jqSaveFilter, ctrl+s in the query line). The library it will
// land in is the open playground's — the program was written against that
// dialect's documents. It refuses early when there is nothing to name, so the
// prompt is never typed for nothing.
func (m *Model) startPlaySavePrompt() {
	if !m.playOpen() {
		m.host.Notify(host.Info, "filters: open the jq or yq playground first")
		return
	}
	program := strings.TrimSpace(m.play.program)
	if program == "" || program == "." {
		// The identity program is the playground's default, not a filter.
		m.play.status = "nothing to save — write a program first"
		return
	}
	m.playName = playNamePrompt{open: true, program: program, dialect: m.play.dialect, scope: jqplay.ScopeProject}
	m.openPlayNamePrompt()
}

// startPlayRenamePrompt opens the prompt over an existing entry, prefilled with
// its current name so a typo is fixed by editing rather than by retyping.
func (m *Model) startPlayRenamePrompt(msg RenameFilterPromptMsg) {
	if !loadPlayFilters(msg.Dialect, msg.Scope).Has(msg.Name) {
		m.host.Notify(host.Warn, msg.Dialect.Name()+" filter "+msg.Name+" is gone")
		return
	}
	m.playName = playNamePrompt{open: true, rename: true, dialect: msg.Dialect, scope: msg.Scope, from: msg.Name, input: msg.Name}
	m.playName.pos = len([]rune(m.playName.input))
	m.openPlayNamePrompt()
}

// openPlayNamePrompt renders the prompt and puts it on screen.
func (m *Model) openPlayNamePrompt() {
	m.renderPlayNamePrompt()
	m.shell.SetSize(m.width, m.height)
	m.shell.Open()
}

// renderPlayNamePrompt (re)fills the shell for the current input. The save
// spelling carries the program being named and the scope toggle; the rename
// spelling neither — its scope is the entry's, and moving a filter between
// stores is a save under the other scope.
func (m *Model) renderPlayNamePrompt() {
	p := m.playName
	line := "> " + ui.CursorView(p.input, p.pos)
	body := line
	if p.rename {
		body += "\n\nrenaming the " + p.scope.String() + " filter " + p.from
	} else {
		body += "\n\nprogram: " + jqplay.Preview(p.program, playFilterPreviewWidth)
		body += "\nscope:   " + p.scope.String()
	}
	if p.err != "" {
		body += "\n\n" + p.err
	}
	hint := "\n\nenter save · esc cancel"
	if !p.rename {
		hint = "\n\nenter save · tab " + p.scope.Other().String() + " scope · esc cancel"
	}
	heading := "Save " + p.dialect.Name() + " filter as"
	if p.rename {
		heading = "Rename " + p.dialect.Name() + " filter"
	}
	m.shell.SetContent(ui.ModelContent{
		Heading: heading,
		Body:    func() string { return body + hint },
	})
}

// updatePlayNamePrompt consumes every key while the prompt is open: enter
// commits (a name already taken in the target scope asks for a second enter
// before overwriting, the save-layout guard), tab switches the scope of a
// save, esc cancels, everything else is line editing.
func (m Model) updatePlayNamePrompt(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Code == tea.KeyEscape:
		m.closePlayNamePrompt()
		return m, nil
	case msg.Code == tea.KeyTab && !m.playName.rename:
		m.playName.scope = m.playName.scope.Other()
		m.playName.err = "" // the other scope's store re-arms the overwrite guard
		m.renderPlayNamePrompt()
		return m, nil
	case msg.Code == tea.KeyEnter:
		if m.commitPlayNamePrompt() {
			m.closePlayNamePrompt()
		} else {
			m.renderPlayNamePrompt()
		}
		return m, nil
	}
	if out, pos, handled, changed := ui.EditKey(msg, m.playName.input, m.playName.pos); handled {
		m.playName.input, m.playName.pos = out, pos
		if changed {
			m.playName.err = "" // a new name re-arms the overwrite guard
		}
		m.renderPlayNamePrompt()
	}
	return m, nil
}

// commitPlayNamePrompt applies the prompt, reporting whether it is done — false
// leaves it open holding the message it just set (a taken name, a refused
// overwrite, a store that would not write).
func (m *Model) commitPlayNamePrompt() bool {
	p := &m.playName
	name := strings.TrimSpace(p.input)
	if name == "" {
		p.err = "a saved filter needs a name"
		return false
	}
	lib := loadPlayFilters(p.dialect, p.scope)
	if p.rename {
		switch err := lib.Rename(p.from, name); {
		case errors.Is(err, jqplay.ErrNameTaken):
			p.err = "that name is taken in the " + p.scope.String() + " library"
			return false
		case err != nil:
			p.err = err.Error()
			return false
		}
		if !m.savePlayFilters(p.dialect, p.scope, lib) {
			return false
		}
		m.palette.Refresh()
		m.host.Notify(host.Info, "renamed the "+p.dialect.Name()+" filter to "+name)
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
	if !m.savePlayFilters(p.dialect, p.scope, lib) {
		return false
	}
	if m.playOpen() {
		m.play.status = "saved " + p.scope.String() + " filter " + name
	}
	m.host.Notify(host.Info, "saved the "+p.scope.String()+" "+p.dialect.Name()+" filter "+name)
	return true
}

// closePlayNamePrompt drops the prompt and its shell.
func (m *Model) closePlayNamePrompt() {
	m.playName = playNamePrompt{}
	m.shell.Close()
}

// pastePlayNamePrompt inserts a bracketed paste into the name input at its
// cursor (#1873), flattened like every other single-field prompt.
func (m *Model) pastePlayNamePrompt(text string) bool {
	out, pos, changed := ui.PasteText(m.playName.input, m.playName.pos, text)
	if !changed {
		return false
	}
	m.playName.input, m.playName.pos = out, pos
	m.playName.err = "" // a new name re-arms the overwrite guard
	m.renderPlayNamePrompt()
	return true
}
