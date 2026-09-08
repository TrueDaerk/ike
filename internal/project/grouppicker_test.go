package project

import (
	"strings"
	"testing"

	"ike/internal/palette"
)

// grouppicker_test.go covers the project-group picker mode (0510, #2571):
// the rows, their detail column, fuzzy matching on the name, the active-group
// badge and the empty state.

func groupPickerFixture(active string) *GroupPickerMode {
	m := NewGroupPickerMode(func() []Group {
		return []Group{
			{Name: "web", Roots: []string{"/home/me/code/api", "/home/me/code/ui", "/home/me/code/www"}},
			{Name: "tools", Roots: []string{"/home/me/code/cli"}},
			{Name: "Weather", Roots: []string{"/home/me/code/wx", "/home/me/code/wx-ui"}},
		}
	})
	m.SetActive(func() string { return active })
	return m
}

func TestGroupPickerListsGroupsWithCountAndMembers(t *testing.T) {
	m := groupPickerFixture("")
	items := m.Results("", palette.Context{})
	if len(items) != 3 {
		t.Fatalf("expected one row per group, got %d", len(items))
	}
	if items[0].Title != "web" || items[1].Title != "tools" || items[2].Title != "Weather" {
		t.Fatalf("an empty query keeps the stored order, got %q %q %q", items[0].Title, items[1].Title, items[2].Title)
	}
	if got, want := items[0].Detail, "3 projects · api, ui, www"; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
	if got, want := items[1].Detail, "1 project · cli"; got != want {
		t.Errorf("singular detail = %q, want %q", got, want)
	}
	msg, ok := items[0].Msg.(OpenGroupMsg)
	if !ok || msg.Name != "web" {
		t.Fatalf("activation must emit OpenGroupMsg{web}, got %#v", items[0].Msg)
	}
	for _, it := range items {
		if it.Inert || it.Badge != "" {
			t.Errorf("row %q: no badge and not inert without an active group, got %+v", it.Title, it)
		}
	}
}

func TestGroupPickerFuzzyMatchesName(t *testing.T) {
	m := groupPickerFixture("")
	items := m.Results("we", palette.Context{})
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	if len(items) != 2 {
		t.Fatalf("\"we\" should match web and Weather, got %v", titles)
	}
	for _, it := range items {
		if len(it.Spans) == 0 {
			t.Errorf("row %q: a name match must carry highlight spans", it.Title)
		}
	}
	if items := m.Results("zzz", palette.Context{}); len(items) != 0 {
		t.Fatalf("a query matching nothing lists nothing (no raw fallback), got %d rows", len(items))
	}
}

func TestGroupPickerBadgesActiveGroup(t *testing.T) {
	m := groupPickerFixture("WEB")
	items := m.Results("", palette.Context{})
	if items[0].Badge != "⦿" {
		t.Errorf("the active group (case-insensitively) carries the ⦿ badge, got %q", items[0].Badge)
	}
	if items[1].Badge != "" {
		t.Errorf("other groups carry no badge, got %q", items[1].Badge)
	}
}

func TestGroupPickerEmptyState(t *testing.T) {
	m := NewGroupPickerMode(func() []Group { return nil })
	items := m.Results("", palette.Context{})
	if len(items) != 1 || !items[0].Inert || items[0].Title != GroupPickerEmpty {
		t.Fatalf("no groups should render the single inert empty-state row, got %+v", items)
	}
	if items[0].Msg != nil {
		t.Error("the empty-state row must not activate anything")
	}
}

func TestGroupDetailBoundsWidth(t *testing.T) {
	var roots []string
	for i := 0; i < 12; i++ {
		roots = append(roots, "/home/me/code/some-long-project-name-"+strings.Repeat("x", i))
	}
	d := GroupDetail(Group{Name: "big", Roots: roots})
	if r := []rune(d); len(r) > maxDetailWidth || !strings.HasSuffix(d, "…") {
		t.Fatalf("an over-long detail is cut to %d cells with an ellipsis, got %q (%d)", maxDetailWidth, d, len(r))
	}
	if !strings.HasPrefix(d, "12 projects · ") {
		t.Fatalf("the count leads the detail, got %q", d)
	}
}
