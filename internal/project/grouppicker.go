package project

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"ike/internal/config"
	"ike/internal/fuzzy"
	"ike/internal/palette"
)

// grouppicker.go is the project-group picker behind project.group.open
// (Epic 0510, #2571): a palette Mode listing the stored groups — name, member
// count and the member names compact in the detail column — fuzzy-matched on
// the name. Like the recent-projects picker it only ranks rows and names the
// msg a selection dispatches; the root model runs the open chain.

// OpenGroupPickerMsg asks the root model to open the group picker: the palette
// locked to the group mode. Dispatched by project.group.open.
type OpenGroupPickerMsg struct{}

// OpenGroupMsg is emitted when a group row is activated: Name is the stored
// group name. The root model resolves the members and runs the warm-up chain
// (internal/app/project_group.go).
type OpenGroupMsg struct{ Name string }

// GroupPickerPrefix selects the group picker mode inside the palette. The root
// model opens the palette locked to it, so the rune has no user-facing prefix
// story (the `#` picker pattern).
const GroupPickerPrefix = '\\'

// GroupPickerEmpty is the row shown when no group is stored: an inert hint
// pointing at the Settings page that creates one.
const GroupPickerEmpty = "no project groups · Settings → Project Groups"

// GroupPickerMode is the palette Mode listing project groups. groups is
// injectable for tests; by default it reads the process-wide config on every
// open, so a Settings reload re-shapes the list live.
type GroupPickerMode struct {
	groups func() []Group
	// active reports the name of the active group (""), so its row carries
	// the ⦿ marker. Nil marks nothing; the app injects the model's marker.
	active func() string
}

// NewGroupPickerMode builds the group picker. A nil groups source reads the
// stored list from the live config.
func NewGroupPickerMode(groups func() []Group) *GroupPickerMode {
	if groups == nil {
		groups = func() []Group { return Groups(config.Get()) }
	}
	// The persisted marker is the default badge source: the root model
	// reloads the config as soon as the write lands, so the picker is only
	// ever a beat behind the model's own marker.
	active := func() string {
		if c := config.Get(); c != nil {
			return c.Project.ActiveGroup
		}
		return ""
	}
	return &GroupPickerMode{groups: groups, active: active}
}

// SetActive installs the active-group source (the model's marker), so the
// picker badges the group one is standing in.
func (m *GroupPickerMode) SetActive(active func() string) { m.active = active }

// Prefix implements palette.Mode.
func (m *GroupPickerMode) Prefix() rune { return GroupPickerPrefix }

// Placeholder implements palette.Mode.
func (m *GroupPickerMode) Placeholder() string {
	return "Open project group — name…"
}

// Results implements palette.Mode: the stored groups fuzzy-matched on their
// name (an empty query lists all in stored order), each row carrying the
// member count and the compact member names in the detail column. With no
// group stored the single inert empty-state row points at the Settings page.
func (m *GroupPickerMode) Results(query string, _ palette.Context) []palette.Item {
	groups := m.groups()
	if len(groups) == 0 {
		return []palette.Item{{Title: GroupPickerEmpty, Inert: true}}
	}
	type scored struct {
		g     Group
		score int
		spans []int
	}
	var out []scored
	for _, g := range groups {
		if r, ok := fuzzy.Match(query, g.Name); ok {
			out = append(out, scored{g: g, score: r.Score, spans: r.Positions})
		}
	}
	// Stable on score only: equal scores keep the stored order.
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })
	active := ""
	if m.active != nil {
		active = m.active()
	}
	items := make([]palette.Item, 0, len(out))
	for _, s := range out {
		it := palette.Item{
			Title:  s.g.Name,
			Detail: GroupDetail(s.g),
			Spans:  s.spans,
			Score:  s.score,
			Msg:    OpenGroupMsg{Name: s.g.Name},
			Key:    "group:" + s.g.Name,
		}
		if active != "" && strings.EqualFold(active, s.g.Name) {
			it.Badge = "⦿"
		}
		items = append(items, it)
	}
	return items
}

// GroupDetail renders a group's detail chip: `3 projects · api, ui, web` —
// the member count and the member directory names in list order, bounded to
// the picker's detail width so an over-long group keeps its head and a "…".
func GroupDetail(g Group) string {
	names := make([]string, 0, len(g.Roots))
	for _, r := range g.Roots {
		names = append(names, filepath.Base(r))
	}
	s := pluralProjects(len(g.Roots)) + " · " + strings.Join(names, ", ")
	if r := []rune(s); len(r) > maxDetailWidth {
		return string(r[:maxDetailWidth-1]) + "…"
	}
	return s
}

// pluralProjects renders "1 project" / "3 projects".
func pluralProjects(n int) string {
	if n == 1 {
		return "1 project"
	}
	return strconv.Itoa(n) + " projects"
}
