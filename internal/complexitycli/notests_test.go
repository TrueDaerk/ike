package complexitycli

import (
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/cli"
)

// notests_test.go covers --no-tests (#2950): the test files leave the
// project table, the groups, the brief lines and the diff; the summary and
// the JSON say so.

const testGo = `package p

func helperUnderTest(a int) int {
	if a > 0 {
		return 1
	}
	return 0
}
`

// testsFixture is the fixture project plus a Go test file.
func testsFixture(t *testing.T) string {
	t.Helper()
	dir := fixture(t)
	write(t, dir, "pkg/b_test.go", testGo)
	return dir
}

func TestNoTestsProjectText(t *testing.T) {
	requireTrees(t)
	dir := testsFixture(t)
	out, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}})
	if !strings.Contains(out, "b_test.go") || !strings.Contains(out, "summary: 3 functions in 3 files") || strings.Contains(out, "tests hidden") {
		t.Fatalf("without --no-tests the test file must count:\n%s", out)
	}
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, NoTests: true})
	if code != ExitOK || strings.Contains(out, "b_test.go") {
		t.Fatalf("--no-tests kept the test file (exit %d):\n%s", code, out)
	}
	for _, want := range []string{"summary: 2 functions in 2 files", "tests hidden: 1 file (--no-tests)"} {
		if !strings.Contains(out, want) {
			t.Errorf("--no-tests output lacks %q:\n%s", want, out)
		}
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, NoTests: true, Min: 3})
	if !strings.Contains(out, "shown: 1 of 2 functions (CCN or cog ≥ 3; tests hidden)") {
		t.Errorf("shown line does not reflect --no-tests:\n%s", out)
	}
}

func TestNoTestsGroupsAndBrief(t *testing.T) {
	requireTrees(t)
	dir := testsFixture(t)
	for _, a := range []cli.ComplexityArgs{
		{Paths: []string{dir}, NoTests: true, By: "file"},
		{Paths: []string{dir}, NoTests: true, Brief: "file"},
	} {
		out, _, code := run(t, a)
		if code != ExitOK || strings.Contains(out, "b_test.go") || !strings.Contains(out, "pkg/b.go") {
			t.Errorf("%+v (exit %d):\n%s", a, code, out)
		}
	}
	out, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, NoTests: true, By: "file", JSON: true})
	var got struct {
		Summary map[string]any `json:"summary"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary["tests_hidden"] != true || got.Summary["functions"] != float64(2) {
		t.Errorf("groups JSON summary = %v", got.Summary)
	}
}

func TestNoTestsProjectJSON(t *testing.T) {
	requireTrees(t)
	dir := testsFixture(t)
	out, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	if strings.Contains(out, "tests_hidden") {
		t.Fatalf("tests_hidden must stay absent without the flag:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true, NoTests: true})
	var got struct {
		Summary   map[string]any   `json:"summary"`
		Functions []map[string]any `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Summary["tests_hidden"] != true || len(got.Functions) != 2 {
		t.Errorf("summary %v, %d functions", got.Summary, len(got.Functions))
	}
}

func TestNoTestsNamedFileKept(t *testing.T) {
	requireTrees(t)
	dir := testsFixture(t)
	out, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir + "/pkg/b_test.go"}, NoTests: true})
	if !strings.Contains(out, "helperUnderTest") {
		t.Errorf("a test file named explicitly must stay:\n%s", out)
	}
}

func TestNoTestsDiff(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	write(t, dir, "fresh/c_test.go", testGo)
	out, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true})
	if !strings.Contains(out, "helperUnderTest") {
		t.Fatalf("without --no-tests the new test file is added:\n%s", out)
	}
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, NoTests: true})
	if code != ExitOK || strings.Contains(out, "helperUnderTest") || !strings.Contains(out, "added (1):") {
		t.Fatalf("--no-tests --diff kept the test file (exit %d):\n%s", code, out)
	}
	if !strings.Contains(out, "tests hidden (--no-tests)") || strings.Contains(out, "skipped:") {
		t.Errorf("a test file is skipped like an excluded one, silently:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, NoTests: true, JSON: true})
	if !strings.Contains(out, `"tests_hidden": true`) {
		t.Errorf("diff JSON lacks tests_hidden:\n%s", out)
	}
}
