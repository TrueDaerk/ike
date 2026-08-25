package ghissues

// savedfilter.go is the configured side of the unified filter (#2115): the
// issues.default_filter expression a freshly opened pane is seeded with, and
// the issues.saved_filters list the filter overlay's "saved" row cycles
// through. Both are written in the same qualifier syntax (internal/issuefilter)
// and both drive exactly the three dimensions a filter narrows by — the state
// gate, the label selection and the match text. The sort order and the
// grouping keep their own settings and are never touched from here.
//
// Seeding follows the pane's existing rule: it only applies while the user has
// not filtered by hand in this session, so a live config reload never
// re-narrows a list somebody is working in.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/issuefilter"
)

// savedFilter is one entry of issues.saved_filters: the name the row shows
// and the expression picking it applies.
type savedFilter struct {
	name string
	spec issuefilter.Spec
}

// savedNone is the saved row's first value — the one that clears the filter
// again, so a saved filter is never a one-way door.
const savedNone = "(none)"

// parseStateFilter maps an is:/state: qualifier onto the pane's gate. An
// expression that names no state leaves the gate alone, which is why the
// second result exists. The mapping itself is parseStateName's (#2110).
func parseStateFilter(s string) (StateFilter, bool) {
	if s == "" {
		return FilterOpen, false
	}
	for _, name := range issuefilter.States {
		if name == s {
			return parseStateName(s), true
		}
	}
	return FilterOpen, false
}

// seedFilter applies issues.default_filter to a pane the user has not
// filtered by hand yet. It writes the state gate directly rather than through
// setState: Configure runs before the pane's first fetch, so the seeded gate
// is simply the state that fetch asks for — there is nothing to refetch.
func (m *Model) seedFilter(expr string) {
	spec, err := issuefilter.Parse(expr)
	if err != nil {
		// The config layer already dropped a broken expression with a
		// diagnostic; a pane fed one anyway (a test, an embedder) opens
		// unfiltered rather than half filtered.
		return
	}
	m.fInput, m.fCur = spec.Match, len([]rune(spec.Match))
	m.labelSel = map[string]bool{}
	m.labelAll = false // an expression's labels are any-of (#2112)
	for _, name := range spec.Labels {
		m.labelSel[name] = true
	}
	if state, ok := parseStateFilter(spec.State); ok {
		m.state = state
	}
	if spec.Sort != "" {
		// A sort: qualifier is the more specific setting, so it wins over
		// issues.default_sort — and it seeds under the same rule, since
		// Configure only reaches here while nothing was touched by hand.
		m.sort = parseSort(spec.Sort)
	}
}

// seedSavedFilters reads issues.saved_filters — the config layer hands the
// list over comma-joined — keeping only the entries that parse. The active
// row is re-clamped, so a filter dropped from the config cannot leave the row
// pointing past the end.
func (m *Model) seedSavedFilters(list string) {
	m.saved = m.saved[:0]
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		name, spec, err := issuefilter.ParseSaved(entry)
		if err != nil {
			continue
		}
		m.saved = append(m.saved, savedFilter{name: name, spec: spec})
	}
}

// SavedFilters lists the configured saved filters' names (tests).
func (m *Model) SavedFilters() []string {
	out := make([]string, 0, len(m.saved))
	for _, s := range m.saved {
		out = append(out, s.name)
	}
	return out
}

// SavedFilter names the saved filter the pane's current narrowing *is*, or
// "(none)" when it is none of them (tests, the saved row and the action
// menu). It is derived, never remembered: editing the match text by hand
// after picking "triage" leaves a filter that is no longer triage, and a row
// still reading "triage" would be a lie.
func (m *Model) SavedFilter() string {
	if i := m.savedIndex(); i > 0 {
		return m.saved[i-1].name
	}
	return savedNone
}

// savedIndex is the saved row's position: 0 for "(none)", i+1 for the first
// saved filter the current narrowing matches exactly.
func (m *Model) savedIndex() int {
	for i, s := range m.saved {
		if m.matchesSpec(s.spec) {
			return i + 1
		}
	}
	return 0
}

// matchesSpec reports whether the pane's filter is exactly what spec asks
// for. A spec naming no state means the pane's default gate, which is what
// applying it produces; a spec naming no sort order says nothing about the
// order, so the order is not compared then. The labels are read any-of,
// because that is how an expression's labels are defined (#2112).
func (m *Model) matchesSpec(spec issuefilter.Spec) bool {
	state, ok := parseStateFilter(spec.State)
	if !ok {
		state = FilterOpen
	}
	if m.state != state || m.fInput != spec.Match || m.labelAll {
		return false
	}
	if spec.Sort != "" && m.sort != parseSort(spec.Sort) {
		return false
	}
	if len(m.LabelFilter()) != len(spec.Labels) {
		return false
	}
	for _, name := range spec.Labels {
		if !m.labelSel[name] {
			return false
		}
	}
	return true
}

// savedFilterActions is the action menu's entry for the saved filters
// (#2115): a menu-only action naming what is applied right now, mirroring the
// filter overlay's saved row. Nothing is offered when the config names no
// saved filter — the feature stays invisible until it is configured.
func (m *Model) savedFilterActions() []action {
	if len(m.saved) == 0 {
		return nil
	}
	return []action{act("", "", "Saved filter (now "+m.SavedFilter()+")",
		func(m *Model) tea.Cmd { return m.cycleSaved(1) })}
}

// cycleSaved moves the saved row by delta over "(none)" plus the saved
// filters and applies what it lands on. Applying is a hand change like any
// other, so it wins over the configured default for the rest of the session.
func (m *Model) cycleSaved(delta int) tea.Cmd {
	n := len(m.saved) + 1
	if n == 1 {
		return nil
	}
	return m.applySaved(((m.savedIndex()+delta)%n + n) % n)
}

// applySaved writes the saved filter at position i (0 = "(none)") over every
// narrowing dimension — all of them, so switching between two saved filters
// never leaves the leftovers of the previous one behind, and "(none)" is the
// empty filter. The sort order is the exception: it only moves when the
// expression names one, because an order is not a narrowing and the user's
// own 'a' should survive a filter switch.
func (m *Model) applySaved(i int) tea.Cmd {
	var spec issuefilter.Spec
	if i > 0 && i <= len(m.saved) {
		spec = m.saved[i-1].spec
	}
	m.filterTouched = true
	m.fInput, m.fCur = spec.Match, len([]rune(spec.Match))
	m.labelSel = map[string]bool{}
	m.labelAll = false // an expression's labels are any-of (#2112)
	for _, name := range spec.Labels {
		m.labelSel[name] = true
	}
	if spec.Sort != "" {
		m.sortTouched = true
		m.sort = parseSort(spec.Sort)
	}
	state, ok := parseStateFilter(spec.State)
	if !ok {
		// A saved filter that names no state clears the gate back to the
		// pane's default, exactly as it clears the labels it does not name.
		state = FilterOpen
	}
	if cmd := m.setState(state); cmd != nil {
		return cmd
	}
	m.keepSelection()
	return nil
}
