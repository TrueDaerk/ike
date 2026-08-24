package forge

import (
	"errors"
	"strings"
	"testing"
)

// text_test.go covers the editable-text layer (#2087): the target vocabulary,
// the normalization the stale check and the push agree on, the dispatch onto
// the three mutations, the check-then-push order including the stale verdict,
// and the request/parse helpers both bindings decode a forge text with.
//
// The stub is mutate_test.go's fakeForge: it already implements the whole
// interface for the mutation tests, and recording the text calls in the same
// place keeps one fake in the package.

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
	f := &fakeForge{}
	for _, target := range []TextTarget{
		{Kind: TextIssueBody, Issue: 7},
		{Kind: TextComment, Issue: 7, ID: "42"},
		{Kind: TextNewComment, Issue: 7},
	} {
		if err := PushText(f, target, "text"); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{"editbody:text", "editcomment:42:text", "comment:text"}
	if len(f.calls) != 3 || f.calls[0] != want[0] || f.calls[1] != want[1] || f.calls[2] != want[2] {
		t.Fatalf("dispatch = %v, want %v", f.calls, want)
	}
}

func TestFetchTextDispatch(t *testing.T) {
	f := &fakeForge{issueBody: "the body", commentBody: "the comment"}
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
	f := &fakeForge{issueBody: "before"}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "before", "after\n", false)
	if msg.Err != nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if len(f.calls) != 1 || f.calls[0] != "editbody:after" {
		t.Fatalf("pushed %v", f.calls)
	}
}

func TestSaveTextReportsStaleBaseWithoutPushing(t *testing.T) {
	f := &fakeForge{issueBody: "someone else's text"}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "what I opened", "mine", false)
	if !msg.Stale {
		t.Fatalf("a moved server text must be reported stale: %+v", msg)
	}
	if msg.Current != "someone else's text" {
		t.Fatalf("current = %q", msg.Current)
	}
	if len(f.calls) != 0 {
		t.Fatalf("nothing may be written on a stale base, wrote %v", f.calls)
	}
}

func TestSaveTextForceOverwritesWithoutReading(t *testing.T) {
	// force is the user's answer to the stale dialog: no second read, and the
	// push happens even though the server moved.
	f := &fakeForge{issueBody: "moved", readErr: errors.New("must not be read")}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "base", "mine", true)
	if msg.Err != nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if len(f.calls) != 1 || f.calls[0] != "editbody:mine" {
		t.Fatalf("pushed %v", f.calls)
	}
}

func TestSaveTextFailedBaseReadIsAnError(t *testing.T) {
	// A base check that cannot run must not be assumed safe.
	f := &fakeForge{readErr: errors.New("offline")}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextComment, Issue: 9, ID: "3"}, "base", "mine", false)
	if msg.Err == nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if len(f.calls) != 0 {
		t.Fatalf("nothing may be written after a failed check, wrote %v", f.calls)
	}
}

func TestSaveTextNewCommentSkipsTheStaleCheck(t *testing.T) {
	f := &fakeForge{readErr: errors.New("must not be read")}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextNewComment, Issue: 4}, "", "hello", false)
	if msg.Err != nil || msg.Stale {
		t.Fatalf("msg = %+v", msg)
	}
	if len(f.calls) != 1 || f.calls[0] != "comment:hello" {
		t.Fatalf("created %v", f.calls)
	}
}

func TestSaveTextPushErrorSurfaces(t *testing.T) {
	f := &fakeForge{issueBody: "base", fail: "editbody:mine"}
	msg := saveText(f, "/tmp/b.md", TextTarget{Kind: TextIssueBody, Issue: 9}, "base", "mine", false)
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "boom") {
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

func TestCommentEditRequestRoundTrips(t *testing.T) {
	// The PATCH document gh sends decodes back through the same field both
	// forges answer with, arbitrary markdown included.
	args, payload := ghCommentEditRequest("771122", "line one\n\"quoted\"\n")
	want := []string{"api", "--method", "PATCH",
		"repos/{owner}/{repo}/issues/comments/771122", "--input", "-"}
	for i := range want {
		if i >= len(args) || args[i] != want[i] {
			t.Fatalf("args = %v, want %v", args, want)
		}
	}
	body, err := parseBodyField(payload)
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

func TestGHBodyArgs(t *testing.T) {
	want := []string{"issue", "edit", "2087", "--body-file", "-"}
	got := ghBodyArgs(2087)
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("body args = %v, want %v", got, want)
		}
	}
}
