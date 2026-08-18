package forge

import "testing"

// gh_test.go covers the gh --json parsing on fixture documents (#1934): the
// issue listing with labels and assignees, and the PR listing with the mixed
// CheckRun/StatusContext rollup shapes.

const issuesFixture = `[
  {
    "number": 1934,
    "title": "vcs: GitHub Issues tool window",
    "body": "## Context\nIssue-driven work.",
    "url": "https://github.com/TrueDaerk/ike/issues/1934",
    "labels": [
      {"name": "model:fable", "color": "1d76db"},
      {"name": "size:1-7d", "color": "8250df"}
    ],
    "assignees": [{"login": "TrueDaerk"}]
  },
  {
    "number": 12,
    "title": "plain issue",
    "body": "",
    "url": "https://github.com/TrueDaerk/ike/issues/12",
    "labels": [],
    "assignees": []
  }
]`

func TestParseIssues(t *testing.T) {
	issues, err := parseIssues([]byte(issuesFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %d, want 2", len(issues))
	}
	is := issues[0]
	if is.Number != 1934 || is.Title != "vcs: GitHub Issues tool window" {
		t.Fatalf("issue = %+v", is)
	}
	if len(is.Labels) != 2 || is.Labels[0].Name != "model:fable" || is.Labels[0].Color != "1d76db" {
		t.Fatalf("labels = %+v", is.Labels)
	}
	if len(is.Assignees) != 1 || is.Assignees[0] != "TrueDaerk" {
		t.Fatalf("assignees = %v", is.Assignees)
	}
	if issues[1].Labels != nil && len(issues[1].Labels) != 0 {
		t.Fatalf("plain issue labels = %+v", issues[1].Labels)
	}
}

func TestParseIssuesBadJSON(t *testing.T) {
	if _, err := parseIssues([]byte("gh: not logged in")); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}

const prsFixture = `[
  {
    "number": 1941,
    "title": "fix things",
    "state": "OPEN",
    "url": "https://github.com/TrueDaerk/ike/pull/1941",
    "headRefName": "issue/1927-esconsole",
    "statusCheckRollup": [
      {"status": "COMPLETED", "conclusion": "SUCCESS"},
      {"state": "SUCCESS"}
    ]
  },
  {
    "number": 1900,
    "title": "older",
    "state": "MERGED",
    "url": "https://github.com/TrueDaerk/ike/pull/1900",
    "headRefName": "issue/1898-thing",
    "statusCheckRollup": []
  },
  {
    "number": 1950,
    "title": "red ci",
    "state": "OPEN",
    "url": "https://github.com/TrueDaerk/ike/pull/1950",
    "headRefName": "issue/1949-broken",
    "statusCheckRollup": [
      {"status": "COMPLETED", "conclusion": "SUCCESS"},
      {"status": "COMPLETED", "conclusion": "FAILURE"}
    ]
  },
  {
    "number": 1951,
    "title": "running ci",
    "state": "OPEN",
    "url": "https://github.com/TrueDaerk/ike/pull/1951",
    "headRefName": "issue/1952-running",
    "statusCheckRollup": [
      {"status": "IN_PROGRESS", "conclusion": ""}
    ]
  }
]`

func TestParsePRs(t *testing.T) {
	prs, err := parsePRs([]byte(prsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 4 {
		t.Fatalf("prs = %d, want 4", len(prs))
	}
	want := []CheckState{ChecksPassing, ChecksNone, ChecksFailing, ChecksPending}
	for i, w := range want {
		if prs[i].Checks != w {
			t.Fatalf("pr #%d checks = %v, want %v", prs[i].Number, prs[i].Checks, w)
		}
	}
	if prs[0].State != "OPEN" || prs[0].HeadRef != "issue/1927-esconsole" {
		t.Fatalf("pr = %+v", prs[0])
	}
}

func TestPRForIssue(t *testing.T) {
	prs, err := parsePRs([]byte(prsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if pr := PRForIssue(prs, 1927); pr == nil || pr.Number != 1941 {
		t.Fatalf("pr for 1927 = %+v", pr)
	}
	if pr := PRForIssue(prs, 1934); pr != nil {
		t.Fatalf("pr for 1934 = %+v, want nil", pr)
	}
	// A prefix must bind to the whole number: issue 19 has no PR even though
	// issue/1949-broken starts with the digits.
	if pr := PRForIssue(prs, 19); pr != nil {
		t.Fatalf("pr for 19 = %+v, want nil", pr)
	}
}

func TestPRForIssuePrefersOpen(t *testing.T) {
	prs := []PR{
		{Number: 1, State: "CLOSED", HeadRef: "issue/7-first-try"},
		{Number: 2, State: "MERGED", HeadRef: "issue/7-second"},
		{Number: 3, State: "OPEN", HeadRef: "issue/7"},
	}
	if pr := PRForIssue(prs, 7); pr == nil || pr.Number != 3 {
		t.Fatalf("pr = %+v, want the open one", pr)
	}
}
