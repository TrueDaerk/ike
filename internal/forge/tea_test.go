package forge

import (
	"errors"
	"testing"
)

// tea_test.go covers the Gitea/Forgejo binding's parsers on fixture JSON
// (#2083): the REST issue and PR listings, and the capability fold for both
// forges' permission shapes.

const giteaIssuesFixture = `[
  {
    "number": 42,
    "title": "gitea issue",
    "body": "some **body**",
    "html_url": "https://gitea.example.com/org/repo/issues/42",
    "labels": [
      {"name": "bug", "color": "ee0701"},
      {"name": "ui", "color": "#8250df"}
    ],
    "assignees": [{"login": "dev"}, {"login": ""}]
  },
  {
    "number": 7,
    "title": "bare issue",
    "body": "",
    "html_url": "https://gitea.example.com/org/repo/issues/7",
    "labels": null,
    "assignees": null
  }
]`

func TestParseGiteaIssues(t *testing.T) {
	issues, err := parseGiteaIssues([]byte(giteaIssuesFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(issues))
	}
	is := issues[0]
	if is.Number != 42 || is.Title != "gitea issue" || is.URL != "https://gitea.example.com/org/repo/issues/42" {
		t.Fatalf("issue = %+v", is)
	}
	if len(is.Labels) != 2 || is.Labels[0] != (Label{Name: "bug", Color: "ee0701"}) {
		t.Fatalf("labels = %+v", is.Labels)
	}
	// A leading '#' (some Forgejo versions) is stripped to the bare hex the
	// pane expects.
	if is.Labels[1].Color != "8250df" {
		t.Fatalf("color = %q, want bare hex", is.Labels[1].Color)
	}
	if len(is.Assignees) != 1 || is.Assignees[0] != "dev" {
		t.Fatalf("assignees = %v", is.Assignees)
	}
}

func TestParseGiteaIssuesBadJSON(t *testing.T) {
	if _, err := parseGiteaIssues([]byte("<html>login required</html>")); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}

const giteaPRsFixture = `[
  {
    "number": 50,
    "title": "open pr",
    "state": "open",
    "merged": false,
    "merged_at": null,
    "html_url": "https://gitea.example.com/org/repo/pulls/50",
    "head": {"ref": "issue/42-gitea-issue"}
  },
  {
    "number": 40,
    "title": "merged pr",
    "state": "closed",
    "merged": true,
    "merged_at": "2026-08-01T10:00:00Z",
    "html_url": "https://gitea.example.com/org/repo/pulls/40",
    "head": {"ref": "issue/39-done"}
  },
  {
    "number": 30,
    "title": "abandoned pr",
    "state": "closed",
    "merged": false,
    "merged_at": null,
    "html_url": "https://gitea.example.com/org/repo/pulls/30",
    "head": {"ref": "issue/29-nope"}
  }
]`

func TestParseGiteaPRs(t *testing.T) {
	prs, err := parseGiteaPRs([]byte(giteaPRsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 3 {
		t.Fatalf("prs = %d, want 3", len(prs))
	}
	want := []string{"OPEN", "MERGED", "CLOSED"}
	for i, w := range want {
		if prs[i].State != w {
			t.Fatalf("pr #%d state = %q, want %q", prs[i].Number, prs[i].State, w)
		}
	}
	if prs[0].HeadRef != "issue/42-gitea-issue" || prs[0].Checks != ChecksNone {
		t.Fatalf("pr = %+v", prs[0])
	}
	// The mapped states feed the shared branch-convention join.
	if pr := PRForIssue(prs, 42); pr == nil || pr.Number != 50 {
		t.Fatalf("pr for 42 = %+v", pr)
	}
}

func TestParseGiteaPermissions(t *testing.T) {
	cases := []struct {
		json string
		want Capabilities
	}{
		{`{"permissions": {"admin": false, "push": true, "pull": true}}`, Capabilities{Triage: true, Push: true}},
		{`{"permissions": {"admin": true, "push": false, "pull": true}}`, Capabilities{Triage: true, Push: true}},
		{`{"permissions": {"admin": false, "push": false, "pull": true}}`, Capabilities{}},
	}
	for _, c := range cases {
		got, err := parseGiteaPermissions([]byte(c.json))
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("parseGiteaPermissions(%s) = %+v, want %+v", c.json, got, c.want)
		}
	}
	if _, err := parseGiteaPermissions([]byte("not json")); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}

func TestParseGHPermissions(t *testing.T) {
	cases := []struct {
		json string
		want Capabilities
	}{
		{`{"admin": true, "maintain": true, "push": true, "triage": true, "pull": true}`, Capabilities{Triage: true, Push: true}},
		{`{"admin": false, "maintain": false, "push": false, "triage": true, "pull": true}`, Capabilities{Triage: true, Push: false}},
		{`{"admin": false, "maintain": false, "push": false, "triage": false, "pull": true}`, Capabilities{}},
		{`{"admin": false, "maintain": true, "push": false, "triage": false, "pull": true}`, Capabilities{Triage: true, Push: true}},
	}
	for _, c := range cases {
		got, err := parseGHPermissions([]byte(c.json))
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("parseGHPermissions(%s) = %+v, want %+v", c.json, got, c.want)
		}
	}
	if _, err := parseGHPermissions([]byte("gh: HTTP 404")); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}

func TestUnsupportedStubs(t *testing.T) {
	for _, f := range []Forge{&ghForge{dir: "."}, &teaForge{dir: "."}} {
		err := f.MergePR(1)
		var unsup *ErrUnsupported
		if !errors.As(err, &unsup) || unsup.Op != "merge PR" {
			t.Fatalf("%T MergePR err = %v, want *ErrUnsupported", f, err)
		}
	}
}
