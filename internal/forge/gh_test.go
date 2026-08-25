package forge

import (
	"testing"
	"time"
)

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

// timelineFixture is one GitHub timeline page (#2084): a comment, label
// changes, state changes, assignments, and events outside the neutral
// vocabulary that the parser must drop.
const timelineFixture = `[
  {"event": "commented", "id": 555001, "user": {"login": "TrueDaerk"},
   "body": "Looks **good** to me.", "created_at": "2026-08-20T10:00:00Z"},
  {"event": "labeled", "actor": {"login": "ada"},
   "label": {"name": "bug", "color": "d73a4a"}, "created_at": "2026-08-20T11:00:00Z"},
  {"event": "unlabeled", "actor": {"login": "ada"},
   "label": {"name": "wip", "color": "#ededed"}, "created_at": "2026-08-20T12:00:00Z"},
  {"event": "closed", "actor": {"login": "bo"}, "created_at": "2026-08-21T09:00:00Z"},
  {"event": "reopened", "actor": {"login": "bo"}, "created_at": "2026-08-21T10:00:00Z"},
  {"event": "assigned", "actor": {"login": "ada"},
   "assignee": {"login": "cy"}, "created_at": "2026-08-21T11:00:00Z"},
  {"event": "unassigned", "actor": {"login": "ada"},
   "assignee": {"login": "cy"}, "created_at": "2026-08-21T12:00:00Z"},
  {"event": "cross-referenced", "actor": {"login": "ada"}, "created_at": "2026-08-21T13:00:00Z"},
  {"event": "committed", "created_at": "2026-08-21T14:00:00Z"},
  {"event": "commented", "id": 555002, "user": {"login": "ada"},
   "body": "Someone else.", "created_at": "2026-08-22T10:00:00Z"}
]`

func TestParseGHTimeline(t *testing.T) {
	entries, raw, err := parseGHTimeline([]byte(timelineFixture), "TrueDaerk")
	if err != nil {
		t.Fatal(err)
	}
	if raw != 10 {
		t.Fatalf("raw = %d, want every fixture event counted", raw)
	}
	kinds := []string{TimelineComment, TimelineLabeled, TimelineUnlabeled, TimelineClosed,
		TimelineReopened, TimelineAssigned, TimelineUnassigned, TimelineComment}
	if len(entries) != len(kinds) {
		t.Fatalf("entries = %d, want %d (unknown events dropped)", len(entries), len(kinds))
	}
	for i, k := range kinds {
		if entries[i].Kind != k {
			t.Fatalf("entry %d kind = %q, want %q", i, entries[i].Kind, k)
		}
	}
	c := entries[0]
	if c.Actor != "TrueDaerk" || c.Body != "Looks **good** to me." || c.ID != "555001" || !c.Own {
		t.Fatalf("comment = %+v, want the authenticated user's own comment with a stable ID", c)
	}
	if entries[7].Own || entries[7].ID != "555002" {
		t.Fatalf("other comment = %+v, want Own false", entries[7])
	}
	if entries[1].Body != "bug" || entries[1].LabelColor != "d73a4a" || entries[1].Actor != "ada" {
		t.Fatalf("labeled = %+v", entries[1])
	}
	if entries[2].LabelColor != "ededed" {
		t.Fatalf("label color = %q, want the # stripped", entries[2].LabelColor)
	}
	if entries[5].Body != "cy" {
		t.Fatalf("assigned = %+v, want the assignee in Body", entries[5])
	}
	if entries[0].Time.IsZero() {
		t.Fatal("timestamps must parse")
	}
}

func TestParseGHTimelineBadJSON(t *testing.T) {
	if _, _, err := parseGHTimeline([]byte("gh: HTTP 404"), ""); err == nil {
		t.Fatal("non-JSON must error, not parse")
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

// issueDocFixture is one `gh api repos/{owner}/{repo}/issues/{n}` document,
// cut to what the stale-base check reads (#2087).
const issueDocFixture = `{
  "number": 2087,
  "title": "edit own issue texts",
  "body": "## Context\nThe timeline shows bodies read-only.\n",
  "updated_at": "2026-08-25T09:12:00Z"
}`

// commentDocFixture is one `gh api repos/{owner}/{repo}/issues/comments/{id}`
// document.
const commentDocFixture = `{
  "id": 771122,
  "body": "Looks good \u2014 one nit.",
  "user": {"login": "TrueDaerk"}
}`

func TestParseBodyFieldOnGitHubDocuments(t *testing.T) {
	body, err := parseBodyField([]byte(issueDocFixture))
	if err != nil {
		t.Fatal(err)
	}
	if body != "## Context\nThe timeline shows bodies read-only.\n" {
		t.Fatalf("issue body = %q", body)
	}
	body, err = parseBodyField([]byte(commentDocFixture))
	if err != nil {
		t.Fatal(err)
	}
	if body != "Looks good \u2014 one nit." {
		t.Fatalf("comment body = %q", body)
	}
}

func TestParseGHAPIIssuesSkipsPullRequests(t *testing.T) {
	// The REST issues endpoint the incremental fetch uses (#2108) mixes pull
	// requests into the answer; the parser drops them but the raw count keeps
	// them, so the truncation check sees what GitHub actually sent.
	out := []byte(`[
	 {"number": 7, "title": "an issue", "html_url": "https://e/7", "state": "open",
	  "user": {"login": "ada"}, "created_at": "2026-08-20T10:00:00Z",
	  "updated_at": "2026-08-25T09:00:00Z",
	  "labels": [{"name": "bug", "color": "d73a4a"}],
	  "assignees": [{"login": "dev"}]},
	 {"number": 8, "title": "a pull request", "state": "open",
	  "pull_request": {"url": "https://e/pulls/8"}},
	 {"number": 9, "title": "closed issue", "state": "closed"}
	]`)
	issues, raw, err := parseGHAPIIssues(out)
	if err != nil {
		t.Fatal(err)
	}
	if raw != 3 {
		t.Errorf("raw = %d, want 3 (the PR counts toward truncation)", raw)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %+v, want the PR dropped", issues)
	}
	is := issues[0]
	if is.Number != 7 || is.Author != "ada" || is.State != "OPEN" ||
		len(is.Labels) != 1 || is.Labels[0].Name != "bug" ||
		len(is.Assignees) != 1 || is.Assignees[0] != "dev" || is.UpdatedAt.IsZero() {
		t.Errorf("issue = %+v, want the REST fields mapped", is)
	}
	if issues[1].State != "CLOSED" {
		t.Errorf("state = %q, want CLOSED folded to upper case", issues[1].State)
	}
}

func TestGHSincePathEncodesTheCutoff(t *testing.T) {
	since := time.Date(2026, 8, 25, 9, 30, 0, 0, time.UTC)
	path := ghSincePath(since)
	want := "repos/{owner}/{repo}/issues?state=all&sort=updated&direction=desc&per_page=100&since=2026-08-25T09%3A30%3A00Z"
	if path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}
