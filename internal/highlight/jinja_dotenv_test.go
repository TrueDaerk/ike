package highlight_test

import (
	"testing"

	"ike/internal/highlight"
	"ike/internal/jinja"
	"ike/internal/lang"
	"ike/internal/secret"
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

// TestJinjaDotenvSecretMaskSkipsBlocks (#3101): in a Jinja dotenv template a
// secret-suspect value that is (or contains) a Jinja block is a placeholder,
// not a credential — no secret mask may overlap a block, so the Jinja tokens
// render. A literal secret on another line still masks, and so does the same
// line in a plain .env file.
func TestJinjaDotenvSecretMaskSkipsBlocks(t *testing.T) {
	lines := []string{
		`DB_PASSWORD={{ vault_db_password }}`,
		`API_TOKEN=abc{{ suffix }}`,
		`OTHER_SECRET=hunter2`,
	}
	const path = "roles/app/templates/.env.j2"
	tpl := jinja.Scan(lines)
	spans, _, _ := highlight.HighlightScoped(path, lines)
	masked := map[int]bool{}
	for _, s := range spans {
		if s.Capture != secret.Capture {
			continue
		}
		masked[s.Line] = true
		if tpl.Overlaps(s.Line, s.StartCol, s.Line, s.EndCol) {
			t.Errorf("secret span %+v overlaps a Jinja block (line %q)", s, lines[s.Line])
		}
	}
	if !masked[2] {
		t.Errorf("literal secret on line 2 lost its mask: %q", lines[2])
	}
	ix := highlight.NewIndex(spans)
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 12, "punctuation.special"}, // {{
		{0, 15, "variable"},            // vault_db_password
		{0, 33, "punctuation.special"}, // }}
	} {
		if got := ix.CaptureAt(c.line, c.col); got != c.want {
			t.Errorf("CaptureAt(%d,%d) = %q, want %q", c.line, c.col, got, c.want)
		}
	}

	plain, _, _ := highlight.HighlightScoped("app/.env", []string{`DB_PASSWORD=hunter2`})
	var masks int
	for _, s := range plain {
		if s.Capture == secret.Capture {
			masks++
		}
	}
	if masks == 0 {
		t.Error("plain .env secret no longer masked")
	}
}
