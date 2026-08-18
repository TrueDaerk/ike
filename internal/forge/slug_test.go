package forge

import (
	"strings"
	"testing"
)

func TestBranchSlug(t *testing.T) {
	cases := []struct{ title, want string }{
		{"project picker", "project-picker"},
		{"vcs: GitHub Issues tool window — list/filter issues, start issue branch, PR state",
			"vcs-github-issues-tool-window-list-filter-issues-s"},
		{"  Weird   spacing!!  ", "weird-spacing"},
		{"ÜbersichtÄnderung", "bersicht-nderung"},
		{"---", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := BranchSlug(c.title); got != c.want {
			t.Errorf("BranchSlug(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

func TestBranchSlugCapNeverEndsOnDash(t *testing.T) {
	got := BranchSlug(strings.Repeat("ab ", 40))
	if len(got) > slugMax {
		t.Fatalf("len = %d, want <= %d", len(got), slugMax)
	}
	if strings.HasSuffix(got, "-") {
		t.Fatalf("slug %q must not end on a dash", got)
	}
}

func TestBranchName(t *testing.T) {
	if got := BranchName(12, "project picker"); got != "issue/12-project-picker" {
		t.Fatalf("got %q", got)
	}
	if got := BranchName(9, "···"); got != "issue/9" {
		t.Fatalf("empty slug: got %q", got)
	}
}
