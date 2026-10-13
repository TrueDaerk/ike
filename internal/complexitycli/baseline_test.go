package complexitycli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/cli"
	"ike/internal/complexity"
)

// baseline_test.go covers the accepted-debt baseline on the CLI (#2958):
// --write-baseline seeds the project file from the report, the project file
// applies by default, --baseline[=PATH] / --no-baseline pick or drop it,
// the JSON gains baseline per function and group plus summary.accepted,
// the text table marks accepted rows and a malformed file is an error.

// worseGo is branchyGo with one more branch: CCN 6, cog 8.
const worseGo = `package p

func branchy(a, b int) int {
	if a > 0 {
		if b > 0 {
			return 1
		}
	}
	for i := 0; i < a; i++ {
		if i == b {
			return i
		}
	}
	if b < 0 {
		return -1
	}
	return 0
}
`

func TestWriteBaselineSeedsProjectFile(t *testing.T) {
	requireTrees(t)
	dir := fixture(t) // ccn_warn = 4: branchy (CCN 5) is over, simple is not
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, WriteBaseline: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(out, "baseline: 1 function over threshold (CCN ≥ 4 or cog ≥ 15) written to .ike/complexity-baseline.json") {
		t.Fatalf("output = %q", out)
	}
	b, err := complexity.LoadBaseline(complexity.BaselinePath(dir))
	if err != nil || b == nil {
		t.Fatalf("load = %v, %v", b, err)
	}
	want := complexity.Entry{File: "pkg/b.go", Function: "branchy", Ordinal: 0, CCN: 5, Cognitive: 6}
	if b.CCNWarn != 4 || b.CognitiveWarn != 15 || b.Len() != 1 || b.Accepted[0] != want {
		t.Fatalf("baseline = %+v, want one entry %+v", b, want)
	}
	// The file is deterministic: writing again yields identical bytes.
	first, _ := os.ReadFile(complexity.BaselinePath(dir))
	run(t, cli.ComplexityArgs{Paths: []string{dir}, WriteBaseline: true})
	if second, _ := os.ReadFile(complexity.BaselinePath(dir)); string(first) != string(second) {
		t.Fatalf("second write differs:\n%s\n%s", first, second)
	}
	// --baseline=PATH redirects the write.
	alt := filepath.Join(dir, "debt.json")
	if _, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, WriteBaseline: true, Baseline: true, BaselinePath: alt}); code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if _, err := os.Stat(alt); err != nil {
		t.Fatalf("--baseline=PATH --write-baseline must write PATH: %v", err)
	}
}

func TestBaselineAppliesByDefault(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	run(t, cli.ComplexityArgs{Paths: []string{dir}, WriteBaseline: true})

	// Text: branchy is accepted — marked, out of the over count and the
	// excess, named on the baseline line.
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "branchy ✓") {
		t.Errorf("accepted row must carry ✓:\n%s", out)
	}
	if !strings.Contains(out, "0 over threshold (CCN ≥ 4 or cog ≥ 15 or cmt ≥ 40%) · excess 0") {
		t.Errorf("accepted function must leave the over count and excess:\n%s", out)
	}
	if !strings.Contains(out, "baseline: 1 accepted, 0 worsened (.ike/complexity-baseline.json)") {
		t.Errorf("baseline line missing:\n%s", out)
	}

	// JSON: baseline per function, accepted in the summary.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	var rep struct {
		Summary struct {
			OverThreshold int `json:"over_threshold"`
			Excess        int `json:"excess"`
			Accepted      int `json:"accepted"`
		} `json:"summary"`
		Functions []struct {
			Name     string `json:"name"`
			Baseline string `json:"baseline"`
		} `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Summary.OverThreshold != 0 || rep.Summary.Excess != 0 || rep.Summary.Accepted != 1 {
		t.Errorf("summary = %+v", rep.Summary)
	}
	got := map[string]string{}
	for _, f := range rep.Functions {
		got[f.Name] = f.Baseline
	}
	if got["branchy"] != "accepted" || got["simple"] != "new" {
		t.Errorf("function statuses = %v", got)
	}

	// Groups: the file with the accepted function is "accepted", the one
	// with nothing over threshold carries no status.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true, By: "file"})
	var grp struct {
		Groups []struct {
			File          string `json:"file"`
			OverThreshold int    `json:"over_threshold"`
			Baseline      string `json:"baseline"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &grp); err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, g := range grp.Groups {
		status[g.File] = g.Baseline
		if g.OverThreshold != 0 {
			t.Errorf("%s over_threshold = %d, want 0", g.File, g.OverThreshold)
		}
	}
	if status["pkg/b.go"] != "accepted" || status["a.go"] != "" {
		t.Errorf("group statuses = %v", status)
	}

	// Worsened: the function grows past its recorded metrics.
	write(t, dir, "pkg/b.go", worseGo)
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Summary.OverThreshold != 1 || rep.Summary.Accepted != 0 || rep.Summary.Excess != 2 {
		t.Errorf("worsened summary = %+v", rep.Summary)
	}
	for _, f := range rep.Functions {
		if f.Name == "branchy" && f.Baseline != "worsened" {
			t.Errorf("branchy = %q, want worsened", f.Baseline)
		}
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if strings.Contains(out, "branchy ✓") || !strings.Contains(out, "baseline: 0 accepted, 1 worsened") {
		t.Errorf("a worsened row is not marked:\n%s", out)
	}
}

func TestBaselineFlags(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	// Without a file: no baseline field, no baseline line; a bare
	// --baseline asks for the missing project file and fails.
	out, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	if strings.Contains(out, `"baseline"`) {
		t.Errorf("no baseline file, yet the JSON carries a status:\n%s", out)
	}
	if !strings.Contains(out, `"accepted": 0`) {
		t.Errorf("summary.accepted must be present without a baseline:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if strings.Contains(out, "baseline:") {
		t.Errorf("no baseline file, yet a baseline line:\n%s", out)
	}
	if _, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Baseline: true}); code != ExitError ||
		!strings.Contains(stderr, "--baseline: no baseline file at .ike/complexity-baseline.json") {
		t.Errorf("bare --baseline without a file: exit %d, stderr %q", code, stderr)
	}

	run(t, cli.ComplexityArgs{Paths: []string{dir}, WriteBaseline: true})
	// --no-baseline ignores the file.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, NoBaseline: true})
	if strings.Contains(out, "✓") || !strings.Contains(out, "1 over threshold") || strings.Contains(out, "baseline:") {
		t.Errorf("--no-baseline must ignore the file:\n%s", out)
	}
	// --baseline=PATH names another file, relative to the working
	// directory.
	alt := filepath.Join(dir, "debt.json")
	if err := os.WriteFile(alt, []byte(`{"version":1,"accepted":[{"file":"a.go","function":"simple","ordinal":0,"ccn":1,"cognitive":0}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Baseline: true, BaselinePath: alt})
	if code != ExitOK || !strings.Contains(out, "simple ✓") || strings.Contains(out, "branchy ✓") {
		t.Errorf("--baseline=PATH must apply that file (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "baseline: 0 accepted, 0 worsened (debt.json)") {
		t.Errorf("simple is under threshold, so nothing counts as accepted:\n%s", out)
	}
	if _, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Baseline: true, BaselinePath: filepath.Join(dir, "none.json")}); code != ExitError ||
		!strings.Contains(stderr, "no baseline file at none.json") {
		t.Errorf("--baseline=missing: exit %d, stderr %q", code, stderr)
	}
	// A malformed project file is an error, not silently ignored.
	if err := os.WriteFile(complexity.BaselinePath(dir), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}}); code != ExitError || !strings.Contains(stderr, "complexity-baseline.json") {
		t.Errorf("malformed baseline: exit %d, stderr %q", code, stderr)
	}
	// …unless --no-baseline leaves it unread.
	if _, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, NoBaseline: true}); code != ExitOK {
		t.Errorf("--no-baseline must not read the malformed file: exit %d", code)
	}
	// --diff is unaffected by the baseline flags; the exclusive
	// combinations are refused by the parser and by Run alike.
	for _, a := range []cli.ComplexityArgs{
		{Paths: []string{dir}, Diff: true, WriteBaseline: true},
		{Paths: []string{dir}, Baseline: true, NoBaseline: true},
	} {
		if _, _, code := run(t, a); code != ExitError {
			t.Errorf("%+v must be refused, exit %d", a, code)
		}
	}
}
