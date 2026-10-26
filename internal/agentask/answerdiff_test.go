package agentask

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// answerdiff_test.go covers the simplify answer's diff (#3009): the three
// answer shapes, hunk splitting, and the one-retry rule of RunSimplify.

func TestParseAnswerDiffShapes(t *testing.T) {
	hunk := []string{" func f() {", "-\tif a {", "+\tif b {", " \treturn"}
	body := "--- a/x.go\n+++ b/x.go\n@@ -3,3 +3,3 @@\n func f() {\n-\tif a {\n+\tif b {\n \treturn\n"
	cases := map[string]string{
		"fenced diff":  "Here it is.\n\n```diff\n" + body + "```\n\nShorter now.",
		"fenced patch": "```patch\n" + body + "```",
		"bare":         "I flipped the guard.\n" + body + "\nThat is all.",
	}
	for name, answer := range cases {
		d, ok := ParseAnswerDiff(answer)
		if !ok || len(d.Files) != 1 || d.Files[0].Path != "x.go" || d.Hunks() != 1 {
			t.Errorf("%s: diff = %+v ok=%v", name, d, ok)
			continue
		}
		h := d.Files[0].Hunks[0]
		if !reflect.DeepEqual(h.Lines, hunk) || h.OldStart != 3 || h.OldLines != 3 || h.NewStart != 3 {
			t.Errorf("%s: hunk = %+v", name, h)
		}
	}
}

func TestParseAnswerDiffDetails(t *testing.T) {
	// Two hunks, a blank context row without its space, a no-newline note,
	// git's header lines, and a header-less fenced diff.
	answer := "```diff\ndiff --git a/x.go b/x.go\nindex 1..2 100644\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n a\n\n-b\n+B\n@@ -9 +9 @@\n-y\n+Y\n\\ No newline at end of file\n```"
	d, ok := ParseAnswerDiff(answer)
	if !ok || d.Hunks() != 2 {
		t.Fatalf("diff = %+v ok=%v", d, ok)
	}
	if got := d.Files[0].Hunks[0].Lines; !reflect.DeepEqual(got, []string{" a", " ", "-b", "+B"}) {
		t.Errorf("first hunk = %q", got)
	}
	if h := d.Files[0].Hunks[1]; h.OldStart != 9 || h.OldLines != 1 || !reflect.DeepEqual(h.Lines, []string{"-y", "+Y"}) {
		t.Errorf("second hunk = %+v", h)
	}
	d, ok = ParseAnswerDiff("```diff\n@@\n-a\n+b\n```")
	if !ok || len(d.Files) != 1 || d.Files[0].Path != "" {
		t.Errorf("a header-less diff must parse: %+v ok=%v", d, ok)
	}
	// Two files keep their sections apart.
	d, _ = ParseAnswerDiff("```diff\n--- a/x.go\n+++ b/x.go\n@@\n-a\n+b\n--- a/y.go\n+++ b/y.go\n@@\n-c\n+d\n```")
	if len(d.Files) != 2 || d.Files[1].Path != "y.go" {
		t.Errorf("files = %+v", d.Files)
	}
}

func TestParseAnswerDiffNone(t *testing.T) {
	for _, answer := range []string{
		"",
		"I would extract the loop.",
		`<invoke name="Read"><parameter name="file_path">x.go</parameter></invoke>`,
		"```go\nfunc f() {}\n```",
		"```diff\n--- a/x\n+++ b/x\n```", // headers without a hunk
	} {
		if HasDiff(answer) {
			t.Errorf("%q must hold no diff", answer)
		}
	}
}

// fakeRunner answers the queued results in order and records the argv.
type fakeRunner struct {
	answers []Result
	errs    []error
	argvs   [][]string
}

func (f *fakeRunner) run(_ context.Context, _ string, argv []string) (Result, error) {
	i := len(f.argvs)
	f.argvs = append(f.argvs, argv)
	var err error
	if i < len(f.errs) {
		err = f.errs[i]
	}
	if i >= len(f.answers) {
		return Result{}, errors.New("unexpected run")
	}
	return f.answers[i], err
}

const withDiff = "```diff\n@@\n-a\n+b\n```\nWhy."

func TestRunSimplifyDiffFirstTime(t *testing.T) {
	f := &fakeRunner{answers: []Result{{Answer: withDiff, ForkID: "s1"}}}
	res, err := RunSimplify(context.Background(), f.run, "/p", Options{Model: "sonnet"}, "PROMPT")
	if err != nil || !res.HasDiff || res.Retried || len(f.argvs) != 1 || !reflect.DeepEqual(res.Sessions, []string{"s1"}) {
		t.Fatalf("res = %+v err=%v runs=%d", res, err, len(f.argvs))
	}
	argv := f.argvs[0]
	if argv[len(argv)-1] != "PROMPT" || !strings.Contains(strings.Join(argv, "\x00"), SimplifySystemPrompt) {
		t.Errorf("argv = %q", argv)
	}
	for _, a := range argv {
		if a == "--resume" {
			t.Fatalf("the first run is a fresh session: %q", argv)
		}
	}
}

func TestRunSimplifyRetriesOnceInSameSession(t *testing.T) {
	f := &fakeRunner{answers: []Result{{Answer: "<invoke name=\"Read\">", ForkID: "s1"}, {Answer: withDiff, ForkID: "s1"}}}
	res, err := RunSimplify(context.Background(), f.run, "/p", Options{}, "PROMPT")
	if err != nil || !res.HasDiff || !res.Retried || len(f.argvs) != 2 || res.Answer != withDiff {
		t.Fatalf("res = %+v err=%v runs=%d", res, err, len(f.argvs))
	}
	follow := f.argvs[1]
	if sessionOf(follow) != "s1" || !strings.HasSuffix(follow[len(follow)-1], NoDiffFollowUp) {
		t.Errorf("follow-up = %q", follow)
	}
	for _, a := range follow {
		if a == "--fork-session" {
			t.Fatalf("the follow-up resumes the run's own session: %q", follow)
		}
	}
}

func TestRunSimplifyGivesUpAfterOneRetry(t *testing.T) {
	f := &fakeRunner{answers: []Result{{Answer: "nothing", ForkID: "s1"}, {Answer: "still nothing", ForkID: "s2"}, {Answer: withDiff}}}
	res, err := RunSimplify(context.Background(), f.run, "/p", Options{}, "PROMPT")
	if err != nil || res.HasDiff || !res.Retried || len(f.argvs) != 2 || res.Answer != "still nothing" {
		t.Fatalf("res = %+v err=%v runs=%d", res, err, len(f.argvs))
	}
	if !reflect.DeepEqual(res.Sessions, []string{"s1", "s2"}) {
		t.Errorf("sessions = %v", res.Sessions)
	}
}

func TestRunSimplifyFailureSkipsRetry(t *testing.T) {
	f := &fakeRunner{answers: []Result{{ForkID: "s1"}}, errs: []error{errors.New("boom")}}
	if _, err := RunSimplify(context.Background(), f.run, "/p", Options{}, "PROMPT"); err == nil || len(f.argvs) != 1 {
		t.Fatalf("err=%v runs=%d", err, len(f.argvs))
	}
}

// TestParseAnswerDiffElided pins the elision check (#3050): a placeholder
// row or a hunk clearly shorter than its header counts names the hunk, a
// complete diff, a spread literal and a header miscounted by a line or two
// do not.
func TestParseAnswerDiffElided(t *testing.T) {
	cases := []struct {
		name, diff, want string
	}{
		{"complete", "```diff\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n```", ""},
		{"no counts", "```diff\n@@\n-a\n+b\n```", ""},
		{"placeholder paren", "```diff\n@@ -1,3 +1,3 @@\n a\n-\t\t... (whole kind switch, lines 494–639, moved verbatim into helpers below)\n+c\n```", "hunk 1 stands a placeholder in for lines (\"... (whole kind switch, lines 494–639, moved verbatim into helpers below)\")"},
		{"placeholder alone", "```diff\n@@\n a\n ...\n-b\n+c\n```", "hunk 1 stands a placeholder in for lines (\"...\")"},
		{"placeholder comment", "```diff\n@@\n a\n // ... unchanged ...\n-b\n+c\n```", "hunk 1 stands a placeholder in for lines (\"// ... unchanged ...\")"},
		{"placeholder ellipsis bracket", "```diff\n@@\n a\n \t[…]\n-b\n+c\n```", "hunk 1 stands a placeholder in for lines (\"[…]\")"},
		{"spread is code", "```diff\n@@ -1,2 +1,2 @@\n ...rest,\n-b\n+c\n```", ""},
		{"miscount by one", "```diff\n@@ -1,4 +1,4 @@\n a\n-b\n+c\n x\n```", ""},
		{"small hunk miscounted", "```diff\n@@ -3,4 +3,4 @@\n-\tif a > 0 {\n+\tif a > 0 { // simpler\n```", ""},
		{"short by nine", "```diff\n@@ -1,11 +1,11 @@\n a\n-b\n+c\n```", ""},
		{"short before", "```diff\n@@ -453,220 +453,236 @@\n a\n b\n-c\n+d\n e\n```", "hunk 1 holds 4 of the 220 before lines its header counts"},
		{"short after", "```diff\n@@ -1,3 +1,40 @@\n a\n-b\n+c\n e\n```", "hunk 1 holds 3 of the 40 after lines its header counts"},
		{"second hunk", "```diff\n@@ -1,2 +1,2 @@\n a\n-b\n+c\n@@ -9,30 +9,30 @@\n x\n-y\n+z\n```", "hunk 2 holds 2 of the 30 before lines its header counts"},
	}
	for _, c := range cases {
		d, ok := ParseAnswerDiff(c.diff)
		if !ok {
			t.Errorf("%s: no diff parsed", c.name)
			continue
		}
		if d.Elided != c.want {
			t.Errorf("%s: Elided = %q, want %q", c.name, d.Elided, c.want)
		}
	}
}

const elidedDiff = "```diff\n@@ -1,50 +1,50 @@\n a\n ... (rest unchanged)\n-b\n+c\n```\nWhy."

func TestRunSimplifyRetriesOnceOnElidedDiff(t *testing.T) {
	f := &fakeRunner{answers: []Result{{Answer: elidedDiff, ForkID: "s1"}, {Answer: withDiff, ForkID: "s1"}}}
	res, err := RunSimplify(context.Background(), f.run, "/p", Options{}, "PROMPT")
	if err != nil || !res.HasDiff || res.Diff.Elided != "" || !res.Retried || len(f.argvs) != 2 || res.Answer != withDiff {
		t.Fatalf("res = %+v err=%v runs=%d", res, err, len(f.argvs))
	}
	follow := f.argvs[1]
	if sessionOf(follow) != "s1" || !strings.HasSuffix(follow[len(follow)-1], ElidedDiffFollowUp) {
		t.Errorf("follow-up = %q", follow)
	}
}

func TestRunSimplifyKeepsSecondElidedDiff(t *testing.T) {
	f := &fakeRunner{answers: []Result{{Answer: elidedDiff, ForkID: "s1"}, {Answer: elidedDiff, ForkID: "s1"}, {Answer: withDiff}}}
	res, err := RunSimplify(context.Background(), f.run, "/p", Options{}, "PROMPT")
	if err != nil || !res.HasDiff || res.Diff.Elided == "" || !res.Retried || len(f.argvs) != 2 {
		t.Fatalf("res = %+v err=%v runs=%d", res, err, len(f.argvs))
	}
}
