package complexitycli

import (
	"encoding/json"
	"strings"
	"testing"

	"ike/internal/cli"
	"ike/internal/complexity"
)

// hidden_test.go covers the hidden set on the CLI (#3031): a function the
// baseline file hides leaves the table, the totals and the JSON by default
// — with --no-baseline too —, --show-hidden lists it marked, and
// --write-baseline keeps the hidden section.

// hideBranchy writes a baseline file hiding pkg/b.go's branchy.
func hideBranchy(t *testing.T, dir string) {
	t.Helper()
	b := complexity.NewBaseline(4, 15)
	b.Hide(complexity.EntryKey{File: "pkg/b.go", Function: "branchy", Ordinal: 0})
	if err := b.Save(complexity.BaselinePath(dir)); err != nil {
		t.Fatal(err)
	}
}

func TestHiddenFunctionLeavesReportByDefault(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	hideBranchy(t, dir)
	for _, a := range []cli.ComplexityArgs{{Paths: []string{dir}}, {Paths: []string{dir}, NoBaseline: true}} {
		out, stderr, code := run(t, a)
		if code != ExitOK {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if strings.Contains(out, "branchy") {
			t.Fatalf("a hidden function must not be listed (no-baseline=%v):\n%s", a.NoBaseline, out)
		}
		if !strings.Contains(out, "summary: 1 functions in 2 files") {
			t.Fatalf("the totals leave it out:\n%s", out)
		}
		if !strings.Contains(out, "hidden: 1 function left out (.ike/complexity-baseline.json)") {
			t.Fatalf("the summary names the hidden count:\n%s", out)
		}
	}
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, JSON: true})
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var doc struct {
		Summary   map[string]any   `json:"summary"`
		Functions []map[string]any `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Summary["hidden"] != float64(1) || doc.Summary["functions"] != float64(1) || len(doc.Functions) != 1 || doc.Functions[0]["name"] != "simple" {
		t.Fatalf("json = %v / %v", doc.Summary, doc.Functions)
	}
	// Markdown and the group tables follow the same rows.
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, MD: true})
	if strings.Contains(out, "branchy") {
		t.Fatalf("markdown lists a hidden function:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, By: "file"})
	if strings.Contains(out, "pkg/b.go") {
		t.Fatalf("a file whose only function is hidden has no group row:\n%s", out)
	}
}

func TestShowHiddenListsMarkedRows(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	hideBranchy(t, dir)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, ShowHidden: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(out, "branchy"+hiddenMark) {
		t.Fatalf("--show-hidden marks the row:\n%s", out)
	}
	if !strings.Contains(out, "hidden: 1 function shown (--show-hidden)") {
		t.Fatalf("the summary says the rows are shown:\n%s", out)
	}
	out, _, _ = run(t, cli.ComplexityArgs{Paths: []string{dir}, ShowHidden: true, JSON: true})
	var doc struct {
		Summary   map[string]any   `json:"summary"`
		Functions []map[string]any `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	hidden := 0
	for _, f := range doc.Functions {
		if f["hidden"] == true {
			hidden++
			if f["name"] != "branchy" {
				t.Fatalf("hidden flag on %v", f["name"])
			}
		}
	}
	if hidden != 1 || len(doc.Functions) != 2 || doc.Summary["hidden"] != float64(1) {
		t.Fatalf("json = %v / %v", doc.Summary, doc.Functions)
	}
	// --write-baseline keeps the hidden section.
	if _, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, WriteBaseline: true}); code != ExitOK {
		t.Fatalf("write-baseline exit %d", code)
	}
	b, err := complexity.LoadBaseline(complexity.BaselinePath(dir))
	if err != nil || !b.HiddenOf().Has(complexity.EntryKey{File: "pkg/b.go", Function: "branchy", Ordinal: 0}) {
		t.Fatalf("the reseeded file lost the hidden set: %+v, %v", b, err)
	}
	// The flag parses.
	a, err := cli.Parse([]string{"complexity", "--show-hidden"})
	if err != nil || !a.Complexity.ShowHidden {
		t.Fatalf("parse = %+v, %v", a, err)
	}
}
