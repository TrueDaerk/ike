package app

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/allfind"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/project"
	"ike/internal/search"
)

// allfind.go is the root-model side of Find in All Projects (#2394): the form
// opens over the editor like the finder, confirming starts a background
// multi-root scan (allSearch, a search.MultiService separate from m.searcher
// so the open project's find-in-path is never disturbed), and the results
// arrive in a popup that never steals the keyboard. The form state — query,
// toggles, globs, excluded roots — persists in the user config layer
// (project.find_all.*), like the project history it draws its list from.

// openAllFind seeds the form from the persisted state and the recent-projects
// history and shows it.
func (m *Model) openAllFind() {
	fa := config.Get().Project.FindAll
	st := allfind.State{
		Query:         fa.Query,
		Include:       strings.Join(fa.Include, ","),
		Exclude:       strings.Join(fa.Exclude, ","),
		CaseSensitive: fa.CaseSensitive,
		WholeWord:     fa.WholeWord,
		Regex:         fa.Regex,
		ExcludedRoots: fa.ExcludedRoots,
	}
	m.allFind.SetSize(m.width, m.height)
	m.allFind.Open(st, allFindProjects(project.History(config.Get()), fa.ExcludedRoots, os.Stat), m.activeSelectionText())
}

// allFindProjects maps the history onto the form's project list: history
// order, exclusion seeded from the persisted set, and a root that fails the
// stat probe marked missing (greyed out, never scanned).
func allFindProjects(entries []project.Entry, excluded []string, stat func(string) (os.FileInfo, error)) []allfind.Project {
	ex := make(map[string]bool, len(excluded))
	for _, r := range excluded {
		ex[r] = true
	}
	out := make([]allfind.Project, len(entries))
	for i, e := range entries {
		missing := false
		if fi, err := stat(e.Path); err != nil || !fi.IsDir() {
			missing = true
		}
		out[i] = allfind.Project{Root: e.Path, Name: e.Name, Missing: missing, Excluded: ex[e.Path]}
	}
	return out
}

// startAllFind persists the confirmed form state to the user layer and starts
// the background scan. The form already closed itself — the editor has the
// keyboard back while the roots are scanned.
func (m *Model) startAllFind(msg allfind.ConfirmMsg) tea.Cmd {
	st := msg.State
	roots := make([]string, len(msg.Roots))
	for i, r := range msg.Roots {
		roots[i] = r.Root
	}
	m.allResults.SetSize(m.width, m.height)
	m.allResults.Begin(st.Query, msg.Roots)
	m.allFindGen = m.allSearch.ScanMulti(search.MultiQuery{
		Query: search.Query{
			Pattern:       st.Query,
			CaseSensitive: st.CaseSensitive,
			WholeWord:     st.WholeWord,
			Regex:         st.Regex,
			Include:       splitAllFindGlobs(st.Include),
			Exclude:       splitAllFindGlobs(st.Exclude),
			MaxResults:    config.Get().Project.FindAll.MaxResults,
		},
		Roots: roots,
	})
	m.host.Notify(host.Info, "searching "+plural(len(roots), "project", "projects")+" for \""+st.Query+"\"…")

	// One batched write + reload for the whole remembered state (#2394):
	// user scope on purpose — the search spans projects, so its memory does.
	excluded := st.ExcludedRoots
	if excluded == nil {
		excluded = []string{}
	}
	muts := []config.Mutation{
		{Scope: config.UserScope, Key: "project.find_all.query", Value: st.Query},
		{Scope: config.UserScope, Key: "project.find_all.case_sensitive", Value: st.CaseSensitive},
		{Scope: config.UserScope, Key: "project.find_all.whole_word", Value: st.WholeWord},
		{Scope: config.UserScope, Key: "project.find_all.regex", Value: st.Regex},
		{Scope: config.UserScope, Key: "project.find_all.include", Value: splitAllFindGlobs(st.Include)},
		{Scope: config.UserScope, Key: "project.find_all.exclude", Value: splitAllFindGlobs(st.Exclude)},
		{Scope: config.UserScope, Key: "project.find_all.excluded_roots", Value: excluded},
	}
	return config.ApplyAndReload(m.cfgOpts, muts)
}

// splitAllFindGlobs turns a comma-separated glob field into a slice — the
// finder's splitGlobs, shared shape. Empty input yields the empty (not nil)
// slice, so the config write stores a real TOML array.
func splitAllFindGlobs(s string) []string {
	out := []string{}
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// finishAllFind consumes the scan's DoneMsg: stale generations drop, the
// current one opens the popup — visible, never focused (#2394).
func (m *Model) finishAllFind(msg search.MultiDoneMsg) {
	if msg.Gen != m.allFindGen {
		return
	}
	m.allResults.SetSize(m.width, m.height)
	m.allResults.Finish(msg.Truncated, msg.Errs)
}

// showAllFindResults re-opens the popup with the keyboard — the show-results
// command / keybind (#2394).
func (m *Model) showAllFindResults() {
	if !m.allResults.HasResults() && !m.allResults.Scanning() {
		m.host.Notify(host.Info, "no all-projects search results — run Find in All Projects first")
		return
	}
	m.allResults.SetSize(m.width, m.height)
	m.allResults.Focus()
}

// openAllFindMatch routes an activated match: in the current project it is a
// plain open; in another project the open is parked on the model, the switch
// runs, and the SwitchedMsg handler finishes the job — the pending open rides
// the model rebuild via the carry-over block in performSwitchOpts.
func (m Model) openAllFindMatch(msg allfind.OpenMatchMsg) (tea.Model, tea.Cmd) {
	if cwd, err := os.Getwd(); err == nil && cwd == msg.Root {
		return m.openPathAt(msg.Path, msg.Line-1, msg.Col)
	}
	m.allPendingOpen = &msg
	return m, project.SwitchTo(msg.Root)
}
