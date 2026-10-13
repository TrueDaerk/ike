package complexitycli

import (
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/cli"
)

// diffbyfile_test.go covers `--diff --by file` (#2955): the per-file text
// table, the JSON files array beside the unchanged sections, and the plain
// diff JSON without it.

func TestDiffByFileText(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, By: "file"})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"files (3):", "FILE", "CHANGED", "EXCESS", "5→7 (+2)", "1→0 (-1)",
		"verdict: MORE COMPLEX (excess +2, Σcog +3, ΣCCN +3)"} {
		if !strings.Contains(out, want) {
			t.Errorf("--by file text lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "changed (1):") {
		t.Errorf("--by file still prints the sections:\n%s", out)
	}
	// ΔCCN descending: the two +2 files by path, the removal last.
	fresh, pkg, a := strings.Index(out, "fresh/c.go"), strings.Index(out, "pkg/b.go"), strings.Index(out, "  a.go")
	if fresh < 0 || pkg < fresh || a < pkg {
		t.Errorf("file order:\n%s", out)
	}
}

func TestDiffByFileJSON(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, By: "file", JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var got struct {
		Changed []map[string]any `json:"changed"`
		Files   []map[string]any `json:"files"`
		Verdict string           `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(got.Changed) != 1 || got.Verdict != "more_complex" || len(got.Files) != 3 {
		t.Fatalf("changed %d verdict %q files %d\n%s", len(got.Changed), got.Verdict, len(got.Files), out)
	}
	var pkg map[string]any
	for _, f := range got.Files {
		if f["file"] == "pkg/b.go" {
			pkg = f
		}
	}
	want := map[string]float64{"changed": 1, "added": 0, "removed": 0, "ccn_before": 5, "ccn_after": 7, "ccn_delta": 2}
	for k, v := range want {
		if pkg[k] != v {
			t.Errorf("pkg/b.go %s = %v, want %v (%v)", k, pkg[k], v, pkg)
		}
	}
	for _, k := range []string{"cog_before", "cog_after", "cog_delta", "nloc_before", "nloc_after", "excess_before", "excess_after", "excess_delta"} {
		if _, ok := pkg[k]; !ok {
			t.Errorf("pkg/b.go lacks %s", k)
		}
	}
	// Without --by the JSON carries no files array.
	plain, _, _ := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, JSON: true})
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plain), &top); err != nil {
		t.Fatal(err)
	}
	if _, ok := top["files"]; ok {
		t.Errorf("plain diff JSON has files:\n%s", plain)
	}
}
