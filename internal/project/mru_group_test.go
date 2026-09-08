package project

import (
	"testing"

	"ike/internal/palette"
)

// mru_group_test.go covers the group half of the MRU order (0510, #2574):
// with a group active every recent-projects list puts its members first and
// badges them, and the digit chords address exactly those rows because they
// all resolve through MRUOrder/MRUTargets.

// webGroup is the epic's example group over fixedHistory: the members are
// listed in an order deliberately unlike their MRU order, so a test that
// passes cannot be reading the group's own order.
func webGroup() Group {
	return Group{Name: "web", Roots: []string{"/work/intra", "/code/ike"}}
}

// TestMRUTargetsPutsGroupMembersFirst is the ordering rule: members first in
// *their* MRU order, the current project dropped, non-members after.
func TestMRUTargetsPutsGroupMembersFirst(t *testing.T) {
	history := fixedHistory() // ike, website, intra — newest first.
	got := MRUTargets(history, "", webGroup())
	want := []string{"/code/ike", "/work/intra", "/code/website"}
	if len(got) != len(want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("target %d = %q, want %q", i+1, got[i], w)
		}
	}

	// Standing in a member drops it, so target 1 is the *other* member: the
	// project one alternates with inside the group.
	got = MRUTargets(history, "/code/ike", webGroup())
	want = []string{"/work/intra", "/code/website"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("from a member: targets = %v, want %v", got, want)
	}

	// Standing outside the group keeps both members in front of the rest.
	got = MRUTargets(history, "/code/website", webGroup())
	want = []string{"/code/ike", "/work/intra"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("from outside: targets = %v, want %v", got, want)
	}

	// A group whose members are not in the history at all changes nothing.
	other := Group{Name: "other", Roots: []string{"/elsewhere/thing"}}
	if got := MRUTargets(history, "", other); len(got) != 3 || got[0] != "/code/ike" {
		t.Errorf("a group with no history members must not reorder: %v", got)
	}
}

// TestGroupContainsAndBadge covers the membership test the ordering and the
// badge share: paths compare cleaned, and the zero group marks nothing.
func TestGroupContainsAndBadge(t *testing.T) {
	g := webGroup()
	if !g.Contains("/code/ike/") {
		t.Error("an unnormalised member root must still match")
	}
	if g.Contains("/code/website") {
		t.Error("a non-member must not match")
	}
	if (Group{}).Contains("/code/ike") {
		t.Error("the zero group contains nothing")
	}
	if got := GroupBadge(g, "/work/intra"); got != "⦿ web" {
		t.Errorf("member badge = %q, want %q", got, "⦿ web")
	}
	if got := GroupBadge(g, "/code/website"); got != "" {
		t.Errorf("non-member badge = %q, want none", got)
	}
	if got := GroupBadge(Group{}, "/code/ike"); got != "" {
		t.Errorf("no active group must badge nothing, got %q", got)
	}
}

// TestPickerHoistsAndBadgesGroupMembers is the picker half of the acceptance:
// with the group active the member rows lead the list and carry the badge, in
// both flavours; without a group the order is the plain MRU one.
func TestPickerHoistsAndBadgesGroupMembers(t *testing.T) {
	for _, flavour := range []struct {
		name string
		peek bool
	}{{"switch", false}, {"peek", true}} {
		t.Run(flavour.name, func(t *testing.T) {
			m, _ := newPicker(t, fixedHistory)
			m.peek = flavour.peek
			m.group = webGroup

			items := m.Results("", palette.Context{Root: "/code/website"})
			want := []string{"ike", "intra"}
			if len(items) != len(want) {
				t.Fatalf("expected %d rows, got %+v", len(want), items)
			}
			for i, w := range want {
				if items[i].Title != w {
					t.Errorf("row %d = %q, want %q", i, items[i].Title, w)
				}
				if items[i].Badge != "⦿ web" {
					t.Errorf("member row %q badge = %q, want %q", items[i].Title, items[i].Badge, "⦿ web")
				}
			}

			// Standing in a member: the other member leads, the non-member
			// follows unbadged.
			items = m.Results("", palette.Context{Root: "/code/ike"})
			if len(items) != 2 || items[0].Title != "intra" || items[1].Title != "website" {
				t.Fatalf("from a member: rows = %+v", items)
			}
			if items[1].Badge != "" {
				t.Errorf("non-member row carries badge %q, want none", items[1].Badge)
			}
		})
	}
}

// TestPickerWithoutGroupKeepsPlainMRUOrder is the other half: no marker, no
// reordering and no badge — the pre-#2574 list.
func TestPickerWithoutGroupKeepsPlainMRUOrder(t *testing.T) {
	m, _ := newPicker(t, fixedHistory)
	m.group = func() Group { return Group{} }
	items := m.Results("", palette.Context{Root: "/code/website"})
	want := []string{"ike", "intra"}
	if len(items) != len(want) {
		t.Fatalf("expected %d rows, got %+v", len(want), items)
	}
	for i, w := range want {
		if items[i].Title != w {
			t.Errorf("row %d = %q, want %q", i, items[i].Title, w)
		}
		if items[i].Badge != "" {
			t.Errorf("row %q carries badge %q without a group", items[i].Title, items[i].Badge)
		}
	}
}

// TestPickerGroupBadgeSharesTheColumn guards the badge column's composition
// (#820, #2178, #2574): the in-memory dot, the group marker and the git
// context render as one "● ⦿ web ⎇ main*" string — and because Results
// rebuilds it, the async git enrichment's RefreshRows cannot drop the marker.
func TestPickerGroupBadgeSharesTheColumn(t *testing.T) {
	m, _ := newPicker(t, fixedHistory)
	m.group = webGroup
	m.SetOpen(func(path string) bool { return path == "/code/ike" })
	cache := NewGitCache()
	m.SetGitCache(cache)

	// Before the probe answers: dot + group marker only.
	items := m.Results("", palette.Context{Root: "/code/website"})
	if len(items) == 0 || items[0].Badge != "● ⦿ web" {
		t.Fatalf("badge before the git probe = %+v", items)
	}

	// The probe lands (what GitInfoMsg does) and the rows are recomputed the
	// way palette.RefreshRows recomputes them.
	info := GitInfo{Path: "/code/ike", Branch: "main", Dirty: true, Repo: true}
	cache.Set(info)
	items = m.Results("", palette.Context{Root: "/code/website"})
	want := "● ⦿ web " + info.Badge()
	if len(items) == 0 || items[0].Badge != want {
		t.Fatalf("badge after the git probe = %q, want %q", items[0].Badge, want)
	}
}
