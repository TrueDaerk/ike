package complexitycli

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"ike/internal/cli"
)

// trend_test.go covers `--trend[=N]` (#2962): the text table and the JSON
// on the fixture repository, the window, the path selection and the
// test-file rule, the conflicting flags (exit 2) and a directory outside
// git.

// trendFixture commits the gitFixture's working tree twice more, so the
// history reads base → work → more.
func trendFixture(t *testing.T) string {
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
	g("commit", "-qm", "work")
	write(t, dir, "pkg/b_test.go", "package p\n\nfunc TestX(x int) int {\n\tif x > 0 && x < 9 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n")
	g("add", "-A")
	g("commit", "-qm", "more")
	return dir
}

func TestTrendText(t *testing.T) {
	requireTrees(t)
	dir := trendFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Trend: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"complexity trend: last 3 commits (first parent)", "COMMIT", "DATE", "SUBJECT", "FUNCS", "CCN", "COG", "EXCESS", "OVER",
		"base", "work", "more", "totals: ", "ΣCCN "} {
		if !strings.Contains(out, want) {
			t.Errorf("text lacks %q:\n%s", want, out)
		}
	}
	// Oldest first: base above work above more; the oldest row has no Δ,
	// the later ones carry one.
	base, work, more := strings.Index(out, "  base"), strings.Index(out, "  work"), strings.Index(out, "  more")
	if base < 0 || work < base || more < work {
		t.Errorf("row order:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		switch {
		case strings.Contains(l, "  base  "):
			if strings.Contains(l, "(") {
				t.Errorf("the oldest row carries a Δ: %s", l)
			}
		case strings.Contains(l, "  work  "):
			if !strings.Contains(l, "(+") && !strings.Contains(l, "(-") && !strings.Contains(l, "(+0)") {
				t.Errorf("the work row lacks its Δ: %s", l)
			}
		}
	}
	// --trend=2 narrows the window; --no-tests drops the test file of the
	// last commit so its function count does not grow.
	out2, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Trend: true, TrendCommits: 2})
	if code != ExitOK || !strings.Contains(out2, "last 2 commits") || strings.Contains(out2, "  base  ") {
		t.Errorf("--trend=2:\n%s", out2)
	}
	noTests, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, Trend: true, TrendCommits: 2, NoTests: true, JSON: true})
	withTests, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, Trend: true, TrendCommits: 2, JSON: true})
	fnsOf := func(doc string) []float64 {
		var got struct {
			Points []struct {
				Summary map[string]any `json:"summary"`
			} `json:"points"`
		}
		if err := json.Unmarshal([]byte(doc), &got); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, doc)
		}
		var out []float64
		for _, p := range got.Points {
			out = append(out, p.Summary["functions"].(float64))
		}
		return out
	}
	a, b := fnsOf(withTests), fnsOf(noTests)
	if len(a) != 2 || len(b) != 2 || a[1] != a[0]+1 || b[1] != b[0] {
		t.Errorf("functions with tests %v, without %v", a, b)
	}
}

func TestTrendJSON(t *testing.T) {
	requireTrees(t)
	dir := trendFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Trend: true, TrendCommits: 5, JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var got struct {
		Commits int `json:"commits"`
		Points  []struct {
			Sha     string             `json:"sha"`
			Short   string             `json:"short"`
			Time    string             `json:"time"`
			Subject string             `json:"subject"`
			Summary map[string]any     `json:"summary"`
			Delta   map[string]float64 `json:"delta"`
		} `json:"points"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if got.Commits != 5 || len(got.Points) != 3 || got.Points[0].Subject != "base" || got.Points[2].Subject != "more" {
		t.Fatalf("doc = %+v", got)
	}
	p := got.Points[1]
	if len(p.Sha) != 40 || p.Short == "" || !strings.HasPrefix(p.Sha, p.Short) || !strings.Contains(p.Time, "T") {
		t.Errorf("identity = %+v", p)
	}
	for _, k := range []string{"files", "functions", "ccn_sum", "cog_sum", "excess", "over_threshold", "ccn_warn", "histogram"} {
		if _, ok := p.Summary[k]; !ok {
			t.Errorf("summary lacks %s: %v", k, p.Summary)
		}
	}
	for _, k := range []string{"functions", "ccn", "cognitive", "excess", "over_threshold"} {
		if _, ok := p.Delta[k]; !ok {
			t.Errorf("delta lacks %s: %v", k, p.Delta)
		}
	}
	// The oldest point's delta is all zero; the work commit grew branchy
	// and removed simple (ccn +2 -1 = +1 across the two files).
	for k, v := range got.Points[0].Delta {
		if v != 0 {
			t.Errorf("oldest delta %s = %v", k, v)
		}
	}
	if p.Delta["ccn"] == 0 || got.Points[1].Summary["ccn_sum"].(float64)-got.Points[0].Summary["ccn_sum"].(float64) != p.Delta["ccn"] {
		t.Errorf("work delta = %v (sums %v → %v)", p.Delta, got.Points[0].Summary["ccn_sum"], p.Summary["ccn_sum"])
	}
	// The ccn_warn of the fixture's project config (4) rides every point.
	if p.Summary["ccn_warn"] != 4.0 {
		t.Errorf("ccn_warn = %v", p.Summary["ccn_warn"])
	}
}

func TestTrendErrorsAndConflicts(t *testing.T) {
	requireTrees(t)
	// Outside a repository: exit 2 with git's message.
	_, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{fixture(t)}, Trend: true})
	if code != ExitError || stderr == "" {
		t.Errorf("non-repository: exit %d, stderr %q", code, stderr)
	}
	dir := trendFixture(t)
	for _, a := range []cli.ComplexityArgs{
		{Paths: []string{dir}, Trend: true, Diff: true},
		{Paths: []string{dir}, Trend: true, By: "file"},
		{Paths: []string{dir}, Trend: true, Brief: "class"},
	} {
		_, stderr, code := run(t, a)
		if code != ExitError || !strings.Contains(stderr, cli.ErrTrendWithDiff.Error()) {
			t.Errorf("%+v: exit %d, stderr %q", a, code, stderr)
		}
	}
	// A path selection narrows the walk to that subtree.
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir + "/pkg"}, Trend: true, JSON: true})
	if code != ExitOK {
		t.Fatalf("pkg: exit %d", code)
	}
	var got struct {
		Points []struct {
			Summary map[string]any `json:"summary"`
		} `json:"points"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Points) != 3 || got.Points[0].Summary["files"] != 1.0 || got.Points[2].Summary["files"] != 2.0 {
		t.Errorf("pkg selection files per point: %v", got.Points)
	}
}
