package langhttp

import "testing"

// bodylimit_test.go covers highlighting and completion of the `# @stream`
// and `# @max-body` directives (#3117). The spans are Go-computed, so they
// assert without the grammar (and therefore without cgo).

func TestBodyLimitDirectivesHighlighted(t *testing.T) {
	lines := []string{
		// 0123456789012345
		`# @stream`,
		`// @max-body 5MB`,
		`# @max-body lots`,
		`GET https://example.com/export`,
	}
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 2, "keyword"},  // @stream
		{0, 8, "keyword"},  // …m
		{1, 3, "keyword"},  // @max-body
		{1, 11, "keyword"}, // …y
		{1, 13, "number"},  // 5MB
		{1, 15, "number"},
		{2, 2, "keyword"}, // a rejected size keeps its marker
	} {
		if got := spanCaptureAt(t, lines, c.line, c.col); got != c.want {
			t.Errorf("line %d col %d = %q, want %q", c.line, c.col, got, c.want)
		}
	}
	if got := spanCaptureAt(t, lines, 2, 13); got == "number" {
		t.Error("a rejected size must not read as a number")
	}
}

func TestBodyLimitDirectivesComplete(t *testing.T) {
	items := completeAt(t, "### x\n# @|\nGET https://example.com\n")
	if !has(items, "@stream") || !has(items, "@max-body") {
		t.Fatalf("items = %v, want @stream and @max-body", labels(items))
	}
	if got := insertFor(items, "@stream"); got != "@stream" {
		t.Errorf("@stream insert = %q, want the bare marker (it takes no value)", got)
	}
	if got := insertFor(items, "@max-body"); got != "@max-body " {
		t.Errorf("@max-body insert = %q, want the marker plus a space for the size", got)
	}
	items = completeAt(t, "### x\n# @max|\nGET https://example.com\n")
	if !has(items, "@max-body") || has(items, "@stream") {
		t.Errorf("typed prefix must filter: %v", labels(items))
	}
}
