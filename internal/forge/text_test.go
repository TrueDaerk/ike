package forge

import (
	"errors"
	"strings"
	"testing"
)

// text_test.go covers the editable-text layer (#2087): the target vocabulary,
// the normalization the stale check and the push agree on, the dispatch onto
// the three mutations, the check-then-push order including the stale verdict,
// and the two shared parsing helpers both bindings decode a forge text with.

// stubForge is a Forge whose text operations are recorded rather than sent.
// Every other method is an unimplemented stub — this file only exercises the
// text half of the interface.
type stubForge struct {
	issueBody   string
	commentBody string
	readErr     error
	pushErr     error

	// what the last push did, so the dispatch can be asserted.
	editedIssue   int
	editedBody    string
	editedComment string
	createdOn     int
}

func (s *stubForge) Issues(IssueState) ([]Issue, error) { return nil, nil }
func (s *stubForge) PRs() ([]PR, error)                 { return nil, nil }
func (s *stubForge) Timeline(int, int) ([]TimelineEntry, bool, error) {
	return nil, false, nil
}
func (s *stubForge) CreateComment(issue int, body string) error {
	s.createdOn, s.editedBody = issue, body
	return s.pushErr
}
func (s *stubForge) EditComment(id string, body string) error {
	s.editedComment, s.editedBody = id, body
	return s.pushErr
}
func (s *stubForge) EditIssueBody(issue int, body string) error {
	s.editedIssue, s.editedBody = issue, body
	return s.pushErr
}
func (s *stubForge) IssueBody(int) (string, error)       { return s.issueBody, s.readErr }
func (s *stubForge) CommentBody(string) (string, error)  { return s.commentBody, s.readErr }
func (s *stubForge) AddLabels(int, []string) error       { return nil }
func (s *stubForge) RemoveLabels(int, []string) error    { return nil }
func (s *stubForge) SetAssignees(int, []string) error    { return nil }
func (s *stubForge) CloseIssue(int) error                { return nil }
func (s *stubForge) ReopenIssue(int) error               { return nil }
func (s *stubForge) MergePR(int) error                   { return nil }
func (s *stubForge) ClosePR(int) error                   { return nil }
func (s *stubForge) Capabilities() (Capabilities, error) { return Capabilities{}, nil }

func TestTextTargetLabelAndSlug(t *testing.T) {
	cases := []struct {
		target TextTarget
		label  string
		slug   string
	}{
		{TextTarget{Kind: TextIssueBody, Issue: 12}, "issue #12 body", "issue-12-body"},
		{TextTarget{Kind: TextComment, Issue: 12, ID: "98"}, "comment on #12", "issue-12-comment-98"},
		{TextTarget{Kind: TextNewComment, Issue: 12}, "new comment on #12", "issue-12-comment-new"},
	}
	for _, c := range cases {
		if got := c.target.Label(); got != c.label {
			t.Errorf("Label() = %q, want %q", got, c.label)
		}
		if got := c.target.Slug(); got != c.slug {
			t.Errorf("Slug() = %q, want %q", got, c.slug)
		}
	}
}

func TestNormalizeText(t *testing.T) {
	// The editor's trailing newline and CRLF endings must not read as a
	// change — otherwise every save would report a conflict.
	if NormalizeText("a\r\nb\n\n") != "a\nb" {
		t.Fatalf("normalized = %q", NormalizeText("a\r\nb\n\n"))
	}
	if NormalizeText("") != "" {
		t.Fatal("empty must normalize to empty")
	}
	// Leading whitespace is content — only the trailing end is trimmed.
	if NormalizeText("  indented") != "  indented" {
		t.Fatalf("leading whitespace lost: %q", NormalizeText("  indented"))
	}
}

func TestPushTextDispatch(t *testing.T) {
	f := &stubForge{}
	if err := PushText(f, TextTarget{Kind: TextIssueBody, Issue: 7}, "body"); err != nil {
		t.Fatal(err)
	}
	if f.editedIssue != 7 || f.editedBody != "body" {
		t.Fatalf("issue body push = %d/%q", f.editedIssue, f.editedBody)
	}
	if err := PushText(f, TextTarget{Kind: TextComment, Issue: 7, ID: "42"}, "edited"); err != nil {
		t.Fatal(err)
	}
	if f.editedComment != "42" || f.editedBody != "edited" {
		t.Fatalf("comment push = %q/%q", f.editedComment, f.editedBody)
	}
	if err := PushText(f, TextTarget{Kind: TextNewComment, Issue: 7}, "fresh"); err != nil {
		t.Fatal(err)
	}
	if f.createdOn != 7 || f.editedBody != "fresh" {
		t.Fatalf("create push = %d/%q", f.createdOn, f.editedBody)
	}
}

func TestFetchTextDispatch(t *testing.T) {
	f := &stubForge{issueBody: "the body", commentBody: "the comment"}
	if got, _ := FetchText(f, TextTarget{Kind: TextIssueBody, Issue: 1}); got != "the body" {
		t.Fatalf("issue body = %q", got)
	}
	if got, _ := FetchText(f, TextTarget{Kind: TextComment, Issue: 1, ID: "2"}); got != "the comment" {
		t.Fatalf("comment = %q", got)
	}
	// A comment that does not exist yet needs no request at all.
	f.readErr = errors.New("must not be called")
	if got, err := FetchText(f, TextTarget{Kind: TextNewComment, Issue: 1}); got != "" || err != nil {
		t.Fatalf("new comment = %q / %v", got, err)
	}
}

func TestSaveTextPushesWhenBaseMatches(t *testing.T) {
	// The buffer's trailing newline differs from the server text; that is not
	// a concurrent edit, and the push must go through with it trimmed.
	f := &stubForge{issueBody: "before"}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "before", "after\n", false)
	if msg.Err != nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if f.editedIssue != 9 || f.editedBody != "after" {
		t.Fatalf("pushed %d/%q", f.editedIssue, f.editedBody)
	}
}

func TestSaveTextReportsStaleBaseWithoutPushing(t *testing.T) {
	f := &stubForge{issueBody: "someone else's text"}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "what I opened", "mine", false)
	if !msg.Stale {
		t.Fatalf("a moved server text must be reported stale: %+v", msg)
	}
	if msg.Current != "someone else's text" {
		t.Fatalf("current = %q", msg.Current)
	}
	if f.editedBody != "" {
		t.Fatalf("nothing may be written on a stale base, wrote %q", f.editedBody)
	}
}

func TestSaveTextForceOverwritesWithoutReading(t *testing.T) {
	// force is the user's answer to the stale dialog: no second read, and the
	// push happens even though the server moved.
	f := &stubForge{issueBody: "moved", readErr: errors.New("must not be read")}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "base", "mine", true)
	if msg.Err != nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if f.editedBody != "mine" {
		t.Fatalf("pushed %q", f.editedBody)
	}
}

func TestSaveTextFailedBaseReadIsAnError(t *testing.T) {
	// A base check that cannot run must not be assumed safe.
	f := &stubForge{readErr: errors.New("offline")}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextComment, Issue: 9, ID: "3"}, "base", "mine", false)
	if msg.Err == nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if f.editedBody != "" {
		t.Fatalf("nothing may be written after a failed check, wrote %q", f.editedBody)
	}
}

func TestSaveTextNewCommentSkipsTheStaleCheck(t *testing.T) {
	f := &stubForge{readErr: errors.New("must not be read")}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextNewComment, Issue: 4}, "", "hello", false)
	if msg.Err != nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if f.createdOn != 4 || f.editedBody != "hello" {
		t.Fatalf("created %d/%q", f.createdOn, f.editedBody)
	}
}

func TestSaveTextPushErrorSurfaces(t *testing.T) {
	f := &stubForge{issueBody: "base", pushErr: errors.New("gh: 403")}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "base", "mine", false)
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "403") {
		t.Fatalf("msg = %+v", msg)
	}
	if msg.Body != "mine" {
		t.Fatalf("the message must carry the text back, got %q", msg.Body)
	}
}

func TestNumericIDRefusesNonDigits(t *testing.T) {
	if _, err := numericID("42"); err != nil {
		t.Fatalf("a plain id must pass: %v", err)
	}
	for _, bad := range []string{"", "1/../repos", "abc", "-1", "1 2"} {
		if _, err := numericID(bad); err == nil {
			t.Fatalf("%q must be refused as a comment id", bad)
		}
	}
}

func TestJSONBodyAndParseBodyField(t *testing.T) {
	// Round trip: the request document a mutation sends decodes back through
	// the same field both forges answer with.
	payload, err := jsonBody("line one\n\"quoted\"\n")
	if err != nil {
		t.Fatal(err)
	}
	body, err := parseBodyField([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if body != "line one\n\"quoted\"\n" {
		t.Fatalf("body = %q", body)
	}
	if _, err := parseBodyField([]byte("gh: not logged in")); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}
