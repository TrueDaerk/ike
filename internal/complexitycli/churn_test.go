package complexitycli

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"ike/internal/cli"
	"ike/internal/complexity"
)

// churn_test.go covers `--churn[=DAYS]` (#2957): the COMMITS / HOT columns,
// the JSON fields, the unchanged output without the flag and the error
// outside a repository.

// churnFixture is gitFixture with its edits committed and pkg/b.go edited
// once more: pkg/b.go has 3 commits, fresh/c.go 1 (a.go 2, but no function
// left).
func churnFixture(t *testing.T) string {
	t.Helper()
	dir := gitFixture(t)
	g := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	g("add", "-A")
	g("commit", "-qm", "edits")
	write(t, dir, "pkg/b.go", branchyGo+"\n// touched\n")
	g("commit", "-qam", "touch b")
	return dir
}

func TestChurnGroupText(t *testing.T) {
	requireTrees(t)
	dir := churnFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, By: "file", Churn: true, ChurnDays: 30})
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	lines := strings.Split(out, "\n")
	if !strings.HasSuffix(strings.TrimSpace(lines[0]), "EXCESS  COMMITS  HOT") {
		t.Fatalf("header = %q, want COMMITS and HOT last", lines[0])
	}
	// pkg/b.go: CCN 5 at ccn_warn 4 → excess 1, 3 commits → hot 3.
	var bRow string
	for _, l := range lines {
		if strings.HasPrefix(l, "pkg/b.go") {
			bRow = l
		}
	}
	if f := strings.Fields(bRow); len(f) < 2 || f[len(f)-2] != "3" || f[len(f)-1] != "3" {
		t.Errorf("pkg/b.go row = %q, want commits 3 hot 3", bRow)
	}
	if !strings.Contains(out, "churn: commits in the last 30 days") {
		t.Errorf("no churn line:\n%s", out)
	}
}

func TestChurnFunctionTextAndBrief(t *testing.T) {
	requireTrees(t)
	dir := churnFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Churn: true})
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if !strings.HasSuffix(strings.TrimSpace(strings.SplitN(out, "\n", 2)[0]), "CMT%  COMMITS") {
		t.Errorf("function header lacks COMMITS:\n%s", out)
	}
	if !strings.Contains(out, "churn: commits in the last 90 days") {
		t.Errorf("a bare --churn must use the 90-day default:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, Brief: "file", Churn: true})
	if !strings.Contains(out, "commits=3\thot=3") && !strings.Contains(out, "commits=3  hot=3") {
		t.Errorf("--brief --churn lacks commits / hot:\n%s", out)
	}
}

func TestChurnJSON(t *testing.T) {
	requireTrees(t)
	dir := churnFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, By: "file", Churn: true, ChurnDays: 30, JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var groups struct {
		Summary struct {
			ChurnDays int `json:"churn_days"`
		} `json:"summary"`
		Groups []struct {
			File    string `json:"file"`
			Commits *int   `json:"commits"`
			Hot     *int   `json:"hot"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &groups); err != nil {
		t.Fatal(err)
	}
	if groups.Summary.ChurnDays != 30 || len(groups.Groups) == 0 {
		t.Fatalf("summary / groups = %+v", groups)
	}
	for _, g := range groups.Groups {
		if g.Commits == nil || g.Hot == nil {
			t.Errorf("group %s lacks commits / hot", g.File)
		}
		if g.File == "pkg/b.go" && (*g.Commits != 3 || *g.Hot != 3) {
			t.Errorf("pkg/b.go commits/hot = %d/%d, want 3/3", *g.Commits, *g.Hot)
		}
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, Churn: true, JSON: true})
	var fns struct {
		Functions []struct {
			File    string `json:"file"`
			Commits *int   `json:"commits"`
		} `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &fns); err != nil {
		t.Fatal(err)
	}
	for _, f := range fns.Functions {
		want := map[string]int{"pkg/b.go": 3, "fresh/c.go": 1}[f.File]
		if f.Commits == nil || *f.Commits != want {
			t.Errorf("function in %s commits = %v, want %d", f.File, f.Commits, want)
		}
	}
}

// TestChurnOffUnchanged: without the flag neither rendering names churn.
func TestChurnOffUnchanged(t *testing.T) {
	requireTrees(t)
	dir := churnFixture(t)
	for _, a := range []cli.ComplexityArgs{
		{Paths: []string{dir}}, {Paths: []string{dir}, By: "file"}, {Paths: []string{dir}, Brief: "file"},
		{Paths: []string{dir}, JSON: true}, {Paths: []string{dir}, By: "file", JSON: true},
	} {
		out, _, _ := run(t, a)
		for _, bad := range []string{"COMMITS", "HOT", "commits", "\"hot\"", "churn"} {
			if strings.Contains(out, bad) {
				t.Errorf("%+v output names %q without --churn:\n%s", a, bad, out)
			}
		}
	}
}

// TestChurnOutsideRepo: --churn outside a repository exits 2 with a
// message; --churn with --diff is a usage error.
func TestChurnOutsideRepo(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Churn: true})
	if code != ExitError || out != "" || !strings.Contains(stderr, "--churn needs a git repository") {
		t.Fatalf("outside git: exit %d stdout %q stderr %q", code, out, stderr)
	}
	if _, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Churn: true, Diff: true}); code != ExitError || !strings.Contains(stderr, cli.ErrChurnWithDiff.Error()) {
		t.Fatalf("--churn --diff: exit %d stderr %q", code, stderr)
	}
}

// TestSettingsHot: a group without excess falls back to its CCN sum.
func TestSettingsHot(t *testing.T) {
	s := settings{churn: map[string]int{"a.go": 4}}
	if c, h := s.hot(complexity.Group{File: "a.go", Summary: complexity.Summary{CCNSum: 6}}); c != 4 || h != 24 {
		t.Errorf("hot = %d/%d, want 4/24", c, h)
	}
	if c, h := s.hot(complexity.Group{File: "b.go", Summary: complexity.Summary{Excess: 3}}); c != 0 || h != 0 {
		t.Errorf("an untouched file = %d/%d, want 0/0", c, h)
	}
}
