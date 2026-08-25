package forge

// mutate_test.go covers the write side (#2088): the argument and payload
// construction of both bindings on fixtures (no CLI, no network involved) and
// the order applyMutation runs one Mutation's parts in.

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestGHLabelArgs(t *testing.T) {
	got := ghLabelArgs(2088, "--add-label", []string{"bug", "size:1d"})
	want := []string{"issue", "edit", "2088", "--add-label", "bug,size:1d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("add args = %v, want %v", got, want)
	}
	got = ghLabelArgs(7, "--remove-label", []string{"stale"})
	want = []string{"issue", "edit", "7", "--remove-label", "stale"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("remove args = %v, want %v", got, want)
	}
}

func TestGHStateAndCommentArgs(t *testing.T) {
	if got := ghStateArgs(12, "close"); !reflect.DeepEqual(got, []string{"issue", "close", "12"}) {
		t.Fatalf("close args = %v", got)
	}
	if got := ghStateArgs(12, "reopen"); !reflect.DeepEqual(got, []string{"issue", "reopen", "12"}) {
		t.Fatalf("reopen args = %v", got)
	}
	// The body is piped in (#2087), so it is not part of the argv.
	want := []string{"issue", "comment", "12", "--body-file", "-"}
	if got := ghCommentArgs(12); !reflect.DeepEqual(got, want) {
		t.Fatalf("comment args = %v, want %v", got, want)
	}
}

func TestGHAssigneesRequest(t *testing.T) {
	args, body := ghAssigneesRequest(5, []string{"ada", "bo"})
	want := []string{"api", "--method", "PATCH", "repos/{owner}/{repo}/issues/5", "--input", "-"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %v, want %v", args, want)
	}
	if string(body) != `{"assignees":["ada","bo"]}` {
		t.Fatalf("body = %s", body)
	}
	// A cleared picker must send the empty array — "nobody", not "unchanged".
	if _, body = ghAssigneesRequest(5, nil); string(body) != `{"assignees":[]}` {
		t.Fatalf("cleared body = %s", body)
	}
}

const ghLabelsFixture = `[
  {"name": "bug", "color": "d73a4a"},
  {"name": "model:opus", "color": "#1d76db"},
  {"name": "", "color": "ffffff"}
]`

func TestParseGHLabels(t *testing.T) {
	labels, err := parseGHLabels([]byte(ghLabelsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 2 {
		t.Fatalf("labels = %+v, want the two named ones", labels)
	}
	if labels[1].Name != "model:opus" || labels[1].Color != "1d76db" {
		t.Fatalf("a leading # must be stripped: %+v", labels[1])
	}
}

func TestParseGHLogins(t *testing.T) {
	logins, err := parseGHLogins([]byte(`[{"login":"ada"},{"login":""},{"login":"bo"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(logins, []string{"ada", "bo"}) {
		t.Fatalf("logins = %v", logins)
	}
	if _, err := parseGHLogins([]byte("gh: not logged in")); err == nil {
		t.Fatal("non-JSON must error")
	}
}

const giteaLabelsFixture = `[
  {"id": 3, "name": "bug", "color": "#d73a4a"},
  {"id": 9, "name": "size:1d", "color": "8250df"}
]`

func TestResolveLabelIDs(t *testing.T) {
	labels, err := parseGiteaLabels([]byte(giteaLabelsFixture))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := resolveLabelIDs(labels, []string{"size:1d", "bug"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int64{9, 3}) {
		t.Fatalf("ids = %v, want [9 3]", ids)
	}
	if _, err := resolveLabelIDs(labels, []string{"nope"}); err == nil {
		t.Fatal("an unknown label must error rather than be dropped silently")
	}
}

func TestGiteaLoginsAndOwner(t *testing.T) {
	logins, err := parseGiteaLogins([]byte(`[{"login":"dev"},{"login":"ops"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got := withOwner(logins, "TrueDaerk"); !reflect.DeepEqual(got, []string{"TrueDaerk", "dev", "ops"}) {
		t.Fatalf("owner must lead the collaborators: %v", got)
	}
	if got := withOwner([]string{"dev"}, "dev"); !reflect.DeepEqual(got, []string{"dev"}) {
		t.Fatalf("an owner already listed must not repeat: %v", got)
	}
}

// fakeForge records the calls applyMutation (#2088) and saveText (#2087)
// make, in order, and answers the text reads from its two body fields.
type fakeForge struct {
	calls []string
	fail  string // the call that returns an error

	issueBody   string
	commentBody string
	readErr     error
}

func (f *fakeForge) record(name string) error {
	f.calls = append(f.calls, name)
	if f.fail == name {
		return errors.New("boom")
	}
	return nil
}

func (f *fakeForge) Issues(IssueState) ([]Issue, error) { return nil, nil }
func (f *fakeForge) PRs() ([]PR, error)                 { return nil, nil }
func (f *fakeForge) Timeline(int, int) ([]TimelineEntry, bool, error) {
	return nil, false, nil
}
func (f *fakeForge) CreateComment(_ int, body string) error { return f.record("comment:" + body) }
func (f *fakeForge) EditComment(id, body string) error {
	return f.record("editcomment:" + id + ":" + body)
}
func (f *fakeForge) EditIssueBody(_ int, body string) error { return f.record("editbody:" + body) }
func (f *fakeForge) IssueBody(int) (string, error)          { return f.issueBody, f.readErr }
func (f *fakeForge) CommentBody(string) (string, error)     { return f.commentBody, f.readErr }
func (f *fakeForge) AddLabels(_ int, labels []string) error {
	return f.record("add:" + strings.Join(labels, ","))
}
func (f *fakeForge) RemoveLabels(_ int, labels []string) error {
	return f.record("remove:" + strings.Join(labels, ","))
}
func (f *fakeForge) SetAssignees(_ int, a []string) error {
	return f.record("assign:" + strings.Join(a, ","))
}
func (f *fakeForge) CloseIssue(int) error             { return f.record("close") }
func (f *fakeForge) ReopenIssue(int) error            { return f.record("reopen") }
func (f *fakeForge) RepoLabels() ([]Label, error)     { return nil, nil }
func (f *fakeForge) Collaborators() ([]string, error) { return nil, nil }
func (f *fakeForge) PRDetail(int) (PRDetail, error) { return PRDetail{}, nil }
func (f *fakeForge) CommentPR(_ int, body string) error {
	return f.record("prcomment:" + body)
}
func (f *fakeForge) MergePR(_ int, method string) error { return f.record("merge:" + method) }
func (f *fakeForge) ClosePR(int) error                  { return f.record("closepr") }
func (f *fakeForge) Capabilities() (Capabilities, error) {
	return Capabilities{Triage: true, Push: true}, nil
}

func TestApplyMutationOrder(t *testing.T) {
	f := &fakeForge{}
	err := applyMutation(f, Mutation{
		Issue: 1, Kind: MutateState,
		Comment: "closing", AddLabels: []string{"done"}, RemoveLabels: []string{"wip"},
		Assignees: []string{"ada"}, SetAssignees: true, State: "closed",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"comment:closing", "remove:wip", "add:done", "assign:ada", "close"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Fatalf("order = %v, want %v", f.calls, want)
	}
}

func TestApplyMutationSkipsAndStops(t *testing.T) {
	f := &fakeForge{}
	if err := applyMutation(f, Mutation{Issue: 1, State: "open"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"reopen"}) {
		t.Fatalf("empty fields must be skipped: %v", f.calls)
	}
	// An empty assignee set is only sent when SetAssignees says so.
	f = &fakeForge{}
	if err := applyMutation(f, Mutation{Issue: 1, SetAssignees: true}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.calls, []string{"assign:"}) {
		t.Fatalf("clearing the assignees must reach the forge: %v", f.calls)
	}
	// A failing part stops the rest, so the caller's rollback covers it.
	f = &fakeForge{fail: "remove:wip"}
	if err := applyMutation(f, Mutation{
		Issue: 1, RemoveLabels: []string{"wip"}, AddLabels: []string{"done"}, State: "closed",
	}); err == nil {
		t.Fatal("the failure must travel")
	}
	if !reflect.DeepEqual(f.calls, []string{"remove:wip"}) {
		t.Fatalf("nothing may run after a failure: %v", f.calls)
	}
}
