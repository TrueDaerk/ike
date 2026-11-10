package langhttp

import "testing"

// configskip_test.go covers highlighting and completion of the
// `# @no-curlrc` / `# @no-netrc` directives (#3114).

func TestConfigSkipDirectiveHighlighted(t *testing.T) {
	lines := []string{
		`# @no-curlrc`,
		`// @no-netrc`,
		`# @no-netrc yes`,
		`GET https://example.com`,
	}
	for _, c := range []struct{ line, col int }{{0, 2}, {0, 11}, {1, 3}, {1, 11}, {2, 2}} {
		if got := spanCaptureAt(t, lines, c.line, c.col); got != "keyword" {
			t.Errorf("line %d col %d = %q, want keyword", c.line, c.col, got)
		}
	}
	if got := spanCaptureAt(t, lines, 2, 13); got == "keyword" {
		t.Errorf("a stray value must not read as the marker")
	}
}

// Both markers complete on a `# @` line, without a trailing space (they take
// no value), and the hyphen keeps filtering.
func TestConfigSkipDirectiveCompletes(t *testing.T) {
	items := completeAt(t, "### x\n# @|\nGET https://example.com\n")
	if !has(items, "@no-curlrc") || !has(items, "@no-netrc") {
		t.Fatalf("items = %v, want both opt-outs", labels(items))
	}
	if got := insertFor(items, "@no-netrc"); got != "@no-netrc" {
		t.Errorf("insert = %q, want the bare marker", got)
	}
	items = completeAt(t, "### x\n# @no-n|\nGET https://example.com\n")
	if !has(items, "@no-netrc") || has(items, "@no-curlrc") || has(items, "@assert") {
		t.Errorf("typed prefix must filter: %v", labels(items))
	}
}
