package forge

import (
	"testing"
	"time"
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

// giteaTimelineFixture is one Gitea timeline page (#2084): typed comment
// objects — a comment, label add/remove (body "1" / ""), close/reopen,
// assignment add/remove, and types outside the vocabulary the parser drops.
const giteaTimelineFixture = `[
  {"id": 9001, "type": "comment", "body": "First!", "user": {"login": "dev"},
   "created_at": "2026-08-20T10:00:00Z"},
  {"id": 9002, "type": "label", "body": "1", "user": {"login": "dev"},
   "label": {"name": "bug", "color": "ee0701"}, "created_at": "2026-08-20T11:00:00Z"},
  {"id": 9003, "type": "label", "body": "", "user": {"login": "dev"},
   "label": {"name": "wip", "color": "#cccccc"}, "created_at": "2026-08-20T12:00:00Z"},
  {"id": 9004, "type": "close", "user": {"login": "boss"}, "created_at": "2026-08-21T09:00:00Z"},
  {"id": 9005, "type": "reopen", "user": {"login": "boss"}, "created_at": "2026-08-21T10:00:00Z"},
  {"id": 9006, "type": "assignees", "user": {"login": "boss"},
   "assignee": {"login": "dev"}, "created_at": "2026-08-21T11:00:00Z"},
  {"id": 9007, "type": "assignees", "user": {"login": "boss"},
   "assignee": {"login": "dev"}, "removed_assignee": true, "created_at": "2026-08-21T12:00:00Z"},
  {"id": 9008, "type": "commit_ref", "user": {"login": "dev"}, "created_at": "2026-08-21T13:00:00Z"},
  {"id": 9009, "type": "comment", "body": "By someone else.", "user": {"login": "other"},
   "created_at": "2026-08-22T10:00:00Z"}
]`

func TestParseGiteaTimeline(t *testing.T) {
	entries, raw, err := parseGiteaTimeline([]byte(giteaTimelineFixture), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if raw != 9 {
		t.Fatalf("raw = %d, want every fixture event counted", raw)
	}
	kinds := []string{TimelineComment, TimelineLabeled, TimelineUnlabeled, TimelineClosed,
		TimelineReopened, TimelineAssigned, TimelineUnassigned, TimelineComment}
	if len(entries) != len(kinds) {
		t.Fatalf("entries = %d, want %d (unknown types dropped)", len(entries), len(kinds))
	}
	for i, k := range kinds {
		if entries[i].Kind != k {
			t.Fatalf("entry %d kind = %q, want %q", i, entries[i].Kind, k)
		}
	}
	c := entries[0]
	if c.Actor != "dev" || c.Body != "First!" || c.ID != "9001" || !c.Own {
		t.Fatalf("comment = %+v, want the authenticated user's own comment with a stable ID", c)
	}
	if entries[7].Own {
		t.Fatalf("other comment = %+v, want Own false", entries[7])
	}
	if entries[1].Body != "bug" || entries[1].LabelColor != "ee0701" {
		t.Fatalf("labeled = %+v", entries[1])
	}
	if entries[2].LabelColor != "cccccc" {
		t.Fatalf("label color = %q, want the # stripped", entries[2].LabelColor)
	}
	if entries[5].Body != "dev" || entries[6].Body != "dev" {
		t.Fatalf("assignments = %+v / %+v, want the assignee in Body", entries[5], entries[6])
	}
}

func TestParseGiteaTimelineBadJSON(t *testing.T) {
	if _, _, err := parseGiteaTimeline([]byte("<html>login</html>"), ""); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}

// giteaIssueDocFixture and giteaCommentDocFixture are the Gitea documents the
// stale-base check reads (#2087); the body field carries the same name as on
// GitHub, which is why one parser serves both bindings.
const giteaIssueDocFixture = `{
  "number": 42,
  "title": "gitea issue",
  "body": "first line\r\nsecond line",
  "updated_at": "2026-08-25T09:12:00Z"
}`

const giteaCommentDocFixture = `{
  "id": 9001,
  "body": "a comment on the instance",
  "user": {"login": "wheatley"}
}`

func TestParseBodyFieldOnGiteaDocuments(t *testing.T) {
	body, err := parseBodyField([]byte(giteaIssueDocFixture))
	if err != nil {
		t.Fatal(err)
	}
	// The raw text keeps its CRLF; only the comparison normalizes it, so a
	// Windows-authored body does not read as a concurrent edit.
	if body != "first line\r\nsecond line" {
		t.Fatalf("issue body = %q", body)
	}
	if NormalizeText(body) != "first line\nsecond line" {
		t.Fatalf("normalized = %q", NormalizeText(body))
	}
	body, err = parseBodyField([]byte(giteaCommentDocFixture))
	if err != nil {
		t.Fatal(err)
	}
	if body != "a comment on the instance" {
		t.Fatalf("comment body = %q", body)
	}
}

func TestGiteaSinceQuery(t *testing.T) {
	// The incremental fetch (#2108) reuses the ordinary listing endpoint with
	// the since filter, in every state — a cached open issue that closed must
	// arrive as closed so the merge can drop it.
	q := giteaSinceQuery(time.Date(2026, 8, 25, 9, 30, 0, 0, time.UTC), 2)
	for key, want := range map[string]string{
		"type":  "issues",
		"state": "all",
		"since": "2026-08-25T09:30:00Z",
		"limit": itoa(giteaPageSize),
		"page":  "2",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}
