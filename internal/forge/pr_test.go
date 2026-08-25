package forge

// pr_test.go covers the PR detail and action layer (#2089): the gh and Gitea
// detail parsers with their per-check folding, the merge argument and method
// construction on both bindings, the comment-then-act order, and the
// Closes-#N link derivation.

import (
	"strings"
	"testing"
)

const ghPRDetailFixture = `{
  "number": 2091,
  "title": "feat: PR detail",
  "body": "## Task\nDo it.\n\nCloses #2089",
  "url": "https://github.com/TrueDaerk/ike/pull/2091",
  "state": "OPEN",
  "author": {"login": "TrueDaerk"},
  "headRefName": "issue/2089-pr-detail",
  "baseRefName": "main",
  "reviewDecision": "APPROVED",
  "mergeable": "CONFLICTING",
  "createdAt": "2026-08-24T10:00:00Z",
  "updatedAt": "2026-08-24T12:00:00Z",
  "statusCheckRollup": [
    {"name": "build", "status": "COMPLETED", "conclusion": "SUCCESS"},
    {"name": "lint", "status": "IN_PROGRESS", "conclusion": ""},
    {"context": "ci/legacy", "state": "FAILURE"}
  ]
}`

func TestParseGHPRDetail(t *testing.T) {
	d, err := parseGHPRDetail([]byte(ghPRDetailFixture))
	if err != nil {
		t.Fatal(err)
	}
	if d.Number != 2091 || d.HeadRef != "issue/2089-pr-detail" || d.BaseRef != "main" {
		t.Fatalf("detail = %+v", d)
	}
	if d.Author != "TrueDaerk" || d.Review != "APPROVED" || d.State != "OPEN" {
		t.Fatalf("detail = %+v", d)
	}
	if d.Mergeable != "conflicting" {
		t.Fatalf("mergeable = %q", d.Mergeable)
	}
	if !strings.Contains(d.Body, "Closes #2089") {
		t.Fatalf("body = %q", d.Body)
	}
	if len(d.CheckRuns) != 3 {
		t.Fatalf("checks = %+v", d.CheckRuns)
	}
	want := []CheckRun{
		{Name: "build", State: ChecksPassing},
		{Name: "lint", State: ChecksPending},
		{Name: "ci/legacy", State: ChecksFailing},
	}
	for i, w := range want {
		if d.CheckRuns[i] != w {
			t.Fatalf("check %d = %+v, want %+v", i, d.CheckRuns[i], w)
		}
	}
	if d.Checks != ChecksFailing {
		t.Fatalf("rollup = %v, want failing", d.Checks)
	}
}

func TestParseGHPRDetailBadJSON(t *testing.T) {
	if _, err := parseGHPRDetail([]byte("gh: no pull request found")); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}

func TestParseGHMergeMethod(t *testing.T) {
	cases := []struct {
		json string
		want string
	}{
		{`{"allow_merge_commit": true, "allow_squash_merge": true}`, "merge"},
		{`{"allow_merge_commit": false, "allow_squash_merge": true}`, "squash"},
		{`{"allow_merge_commit": false, "allow_squash_merge": false, "allow_rebase_merge": true}`, "rebase"},
		{`{}`, ""},
		{`not json`, ""},
	}
	for _, c := range cases {
		if got := parseGHMergeMethod([]byte(c.json)); got != c.want {
			t.Errorf("parseGHMergeMethod(%s) = %q, want %q", c.json, got, c.want)
		}
	}
}

func TestGHMergeArgs(t *testing.T) {
	if got := strings.Join(ghMergeArgs(12, "squash"), " "); got != "pr merge 12 --squash" {
		t.Fatalf("args = %q", got)
	}
	if got := strings.Join(ghMergeArgs(12, "rebase"), " "); got != "pr merge 12 --rebase" {
		t.Fatalf("args = %q", got)
	}
	// Empty and unknown methods fall back to a plain merge commit, never to
	// gh's interactive prompt.
	for _, method := range []string{"", "rebase-merge"} {
		if got := strings.Join(ghMergeArgs(12, method), " "); got != "pr merge 12 --merge" {
			t.Fatalf("args(%q) = %q", method, got)
		}
	}
}

func TestGHPRArgBuilders(t *testing.T) {
	if got := strings.Join(ghPRCommentArgs(7), " "); got != "pr comment 7 --body-file -" {
		t.Fatalf("comment args = %q", got)
	}
	view := strings.Join(ghPRViewArgs(7), " ")
	for _, field := range []string{"body", "baseRefName", "mergeable", "statusCheckRollup"} {
		if !strings.Contains(view, field) {
			t.Fatalf("view args must request %q: %s", field, view)
		}
	}
}

const giteaPRDetailFixture = `{
  "number": 41,
  "title": "fix a thing",
  "body": "Fixes #40",
  "state": "open",
  "merged": false,
  "html_url": "https://git.example.com/o/r/pulls/41",
  "user": {"login": "dev"},
  "created_at": "2026-08-24T10:00:00Z",
  "updated_at": "2026-08-24T11:00:00Z",
  "base": {"ref": "main"},
  "head": {"ref": "issue/40-fix", "sha": "abc123"},
  "mergeable": false
}`

func TestParseGiteaPRDetail(t *testing.T) {
	d, sha, err := parseGiteaPRDetail([]byte(giteaPRDetailFixture))
	if err != nil {
		t.Fatal(err)
	}
	if d.Number != 41 || d.HeadRef != "issue/40-fix" || d.BaseRef != "main" || sha != "abc123" {
		t.Fatalf("detail = %+v, sha = %q", d, sha)
	}
	if d.Mergeable != "conflicting" {
		t.Fatalf("mergeable = %q", d.Mergeable)
	}
	if d.State != "OPEN" || d.MergeMethod != "merge" {
		t.Fatalf("detail = %+v", d)
	}
}

func TestParseGiteaMergeStyle(t *testing.T) {
	if got := parseGiteaMergeStyle([]byte(`{"default_merge_style": "squash"}`)); got != "squash" {
		t.Fatalf("style = %q", got)
	}
	if got := parseGiteaMergeStyle([]byte(`{}`)); got != "" {
		t.Fatalf("style = %q", got)
	}
}

func TestParseGiteaCommitStatus(t *testing.T) {
	fixture := `{"state": "failure", "statuses": [
		{"context": "build", "status": "success"},
		{"context": "lint", "status": "pending"},
		{"context": "e2e", "status": "failure"}
	]}`
	runs, state, ok := parseGiteaCommitStatus([]byte(fixture))
	if !ok || len(runs) != 3 {
		t.Fatalf("runs = %+v, ok = %v", runs, ok)
	}
	if runs[0].State != ChecksPassing || runs[1].State != ChecksPending || runs[2].State != ChecksFailing {
		t.Fatalf("runs = %+v", runs)
	}
	if state != ChecksFailing {
		t.Fatalf("rollup = %v", state)
	}
	if _, _, ok := parseGiteaCommitStatus([]byte("nope")); ok {
		t.Fatal("an unreadable document must not report ok")
	}
}

func TestGiteaErrorMessage(t *testing.T) {
	if got := giteaErrorMessage([]byte(`{"message": "Please try again later"}`)); got != "Please try again later" {
		t.Fatalf("message = %q", got)
	}
	if got := giteaErrorMessage([]byte("<html>")); got != "" {
		t.Fatalf("message = %q", got)
	}
}

func TestApplyPRActionOrder(t *testing.T) {
	f := &fakeForge{}
	if err := applyPRAction(f, PRAction{PR: 5, Kind: PRMerge, Method: "squash", Comment: "ship it"}); err != nil {
		t.Fatal(err)
	}
	want := "prcomment:ship it|merge:squash"
	if got := strings.Join(f.calls, "|"); got != want {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	// A failing comment stops before the irreversible half.
	f = &fakeForge{fail: "prcomment:nope"}
	if err := applyPRAction(f, PRAction{PR: 5, Kind: PRMerge, Comment: "nope"}); err == nil {
		t.Fatal("a failed comment must abort the action")
	}
	if strings.Contains(strings.Join(f.calls, "|"), "merge") {
		t.Fatalf("the merge must not run after a failed comment: %v", f.calls)
	}
	// Close without a comment is one call.
	f = &fakeForge{}
	if err := applyPRAction(f, PRAction{PR: 5, Kind: PRClose}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls, "|"); got != "closepr" {
		t.Fatalf("calls = %q", got)
	}
}

func TestLinkedIssue(t *testing.T) {
	cases := []struct {
		body string
		want int
	}{
		{"Closes #2089", 2089},
		{"closes #7\nmore text", 7},
		{"Fixes #12", 12},
		{"resolved #3", 3},
		{"Close: #44", 44},
		{"see #12", 0},
		{"Encloses #12", 0},
		{"", 0},
	}
	for _, c := range cases {
		if got := LinkedIssue(c.body); got != c.want {
			t.Errorf("LinkedIssue(%q) = %d, want %d", c.body, got, c.want)
		}
	}
}
