package highlight_test

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/lang"
	_ "ike/plugins/languages/env"
)

// TestJinjaLayerOverDotenvPrefix (#3100): a dotenv file with a middle word
// (.env.stats.j2) resolves to dotenv through the ".env." name prefix behind
// the template-suffix fallback, so the dotenv spans render under the Jinja
// layer instead of plain text.
func TestJinjaLayerOverDotenvPrefix(t *testing.T) {
	const path = "roles/app/templates/.env.stats.j2"
	if l, ok := lang.ByPath(path); !ok || l.ID != "dotenv" {
		t.Fatalf("ByPath(%q) = %q, %v, want dotenv", path, l.ID, ok)
	}
	lines := []string{
		`# comment`,
		`APP_ENV=production`,
		`HOST={{ inventory_hostname }}`,
		`{% if debug %}`,
		`DEBUG=1`,
		`{% endif %}`,
	}
	spans, _, _ := highlight.HighlightScoped(path, lines)
	ix := highlight.NewIndex(spans)
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 2, "comment"},
		{1, 0, "property"},    // APP_ENV
		{1, 7, "punctuation"}, // =
		{1, 8, "string"},      // production
		{2, 0, "property"},    // HOST
		{2, 5, "punctuation.special"},
		{2, 8, "variable"},
		{3, 3, "keyword"},
		{4, 0, "property"},
		{4, 6, "string"},
		{5, 3, "keyword"},
	} {
		if got := ix.CaptureAt(c.line, c.col); got != c.want {
			t.Errorf("CaptureAt(%d,%d) = %q, want %q (line %q)", c.line, c.col, got, c.want, lines[c.line])
		}
	}
}
