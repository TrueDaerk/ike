package complexitycli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"ike/internal/cli"
)

// `go test ./internal/complexitycli -update` rewrites testdata/*.md.
var update = flag.Bool("update", false, "rewrite the Markdown golden files")

// golden compares got with testdata/name.
func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// TestMarkdownFoldAndEscape: rows past MarkdownFold fold into <details>,
// and `|`, backticks, `<` and newlines in a cell are escaped.
func TestMarkdownFoldAndEscape(t *testing.T) {
	md := Markdown{Headline: "35 functions · a|b", Notes: []string{"note with `tick`"}}
	sec := MarkdownSection{Title: "Changed", Header: []string{"File", "Function", "CCN"}, Numeric: []bool{false, false, true}}
	sec.Rows = append(sec.Rows, []string{"pkg/a|b.go", "op`|`*", "1→3 (+2)"}, []string{"x.go", "List<T>\nnext", "-1"})
	for i := 2; i < 35; i++ {
		sec.Rows = append(sec.Rows, []string{"f.go", fmt.Sprintf("fn%02d", i), fmt.Sprint(i)})
	}
	md.Sections = []MarkdownSection{sec, {Header: []string{"Only"}, Rows: [][]string{{"one"}}}}
	got := RenderMarkdown(md)
	golden(t, "fold.md", got)
	for _, want := range []string{
		"**35 functions · a\\|b**",
		"| pkg/a\\|b.go | op\\`\\|\\`\\* | 1→3 (+2) |",
		"| x.go | List&lt;T> next | -1 |",
		"| --- | --- | ---: |",
		"<summary>5 more rows</summary>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Markdown lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "| fn"); n != 33 {
		t.Errorf("%d function rows, want 33", n)
	}
	head, _, _ := strings.Cut(got, "<details>")
	if strings.Contains(head, "fn30") || !strings.Contains(head, "fn29") {
		t.Errorf("the first table must hold exactly %d rows:\n%s", MarkdownFold, head)
	}
}

// TestProjectMarkdown is the golden project table of the fixture.
func TestProjectMarkdown(t *testing.T) {
	requireTrees(t)
	dir := fixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, MD: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	golden(t, "project.md", out)
}

// TestGroupClassMarkdown is the golden --by class table.
func TestGroupClassMarkdown(t *testing.T) {
	requireTrees(t)
	dir := groupFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, By: "class", MD: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	golden(t, "group_class.md", out)
}

// TestGroupFileAndBriefMarkdown: --by file and --brief render a table.
func TestGroupFileAndBriefMarkdown(t *testing.T) {
	requireTrees(t)
	dir := groupFixture(t)
	out, _, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, By: "file", MD: true})
	if code != ExitOK || !strings.Contains(out, "| File | Funcs |") || !strings.Contains(out, "| client.go | 2 |") {
		t.Errorf("--by file --md (exit %d):\n%s", code, out)
	}
	out, _, code = run(t, cli.ComplexityArgs{Paths: []string{dir}, Brief: "file", MD: true})
	if code != ExitOK || !strings.HasPrefix(out, "| File | Funcs | NLOC | ΣCCN | Σcog | Max CCN/cog | Cmt% |") ||
		!strings.Contains(out, "| client.go | 2 |") {
		t.Errorf("--brief --md (exit %d):\n%s", code, out)
	}
}

// shaRE matches the short shas a diff header names.
var shaRE = regexp.MustCompile(`\([0-9a-f]{7}\)`)

// TestDiffMarkdown is the golden PR-comment payload: the verdict, the
// totals and the changed / added / removed tables.
func TestDiffMarkdown(t *testing.T) {
	requireTrees(t)
	dir := gitFixture(t)
	out, stderr, code := run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, MD: true})
	if code != ExitOK {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	golden(t, "diff.md", shaRE.ReplaceAllString(out, "(SHA)"))
	out, _, code = run(t, cli.ComplexityArgs{Paths: []string{dir}, Diff: true, By: "file", MD: true})
	if code != ExitOK || !strings.Contains(out, "### Files (3)") || !strings.Contains(out, "| pkg/b.go | 1 | 0 | 0 | 5→7 (+2) |") {
		t.Errorf("--diff --by file --md (exit %d):\n%s", code, out)
	}
}

// TestMarkdownWithJSONIsUsageError: --md and --json exclude each other.
func TestMarkdownWithJSONIsUsageError(t *testing.T) {
	_, stderr, code := run(t, cli.ComplexityArgs{MD: true, JSON: true})
	if code != ExitError || !strings.Contains(stderr, cli.ErrMarkdownWithJSON.Error()) {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}
