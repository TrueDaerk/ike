package langhttp

import "testing"

// nolog_test.go covers highlighting and completion of the `# @no-log`
// directive (#3115).

func TestNoLogDirectiveHighlighted(t *testing.T) {
	lines := []string{
		`# @no-log`,
		`// @no-log`,
		`# @no-log something`,
		`### @no-log`,
		`GET https://example.com`,
	}
	for _, c := range []struct{ line, col int }{{0, 2}, {0, 8}, {1, 3}, {1, 9}, {2, 2}} {
		if got := spanCaptureAt(t, lines, c.line, c.col); got != "keyword" {
			t.Errorf("line %d col %d = %q, want keyword", c.line, c.col, got)
		}
	}
	if got := spanCaptureAt(t, lines, 2, 12); got == "keyword" {
		t.Errorf("a stray value must not read as the marker")
	}
	if got := spanCaptureAt(t, lines, 3, 5); got == "keyword" {
		t.Errorf("`### @no-log` is a block separator, not the directive")
	}
}

// The marker completes on a `# @` line without a trailing space (it takes no
// value), and the typed prefix filters.
func TestNoLogDirectiveCompletes(t *testing.T) {
	items := completeAt(t, "### x\n# @|\nGET https://example.com\n")
	if !has(items, "@no-log") {
		t.Fatalf("items = %v, want @no-log", labels(items))
	}
	if got := insertFor(items, "@no-log"); got != "@no-log" {
		t.Errorf("insert = %q, want the bare marker", got)
	}
	items = completeAt(t, "### x\n# @no-l|\nGET https://example.com\n")
	if !has(items, "@no-log") || has(items, "@no-netrc") || has(items, "@assert") {
		t.Errorf("typed prefix must filter: %v", labels(items))
	}
}
