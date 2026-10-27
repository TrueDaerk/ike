package langenv

import (
	"testing"

	"ike/internal/highlight"
)

// TestJinjaTemplateHighlighting (#3059): an Ansible .env.j2 highlights as
// dotenv with the Jinja layer on top; the statement lines are masked, so the
// dotenv producer sees blank lines and the keys around them keep styling.
func TestJinjaTemplateHighlighting(t *testing.T) {
	lines := []string{
		`API_HOST={{ api_host }}`,
		`{% if debug %}`,
		`DEBUG=1`,
		`{% endif %}`,
	}
	ix := highlight.NewIndex(highlight.Highlight(".env.j2", lines))
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 0, "property"},            // API_HOST
		{0, 9, "punctuation.special"}, // {{
		{0, 12, "variable"},           // api_host
		{1, 3, "keyword"},             // if
		{2, 0, "property"},            // DEBUG
		{3, 3, "keyword"},             // endif
	} {
		if got := ix.CaptureAt(c.line, c.col); got != c.want {
			t.Errorf("CaptureAt(%d,%d) = %q, want %q (line %q)", c.line, c.col, got, c.want, lines[c.line])
		}
	}
	// The statement lines are not dotenv pairs: no key style on them.
	if got := ix.CaptureAt(1, 0); got == "property" {
		t.Error("a {% if %} line styled as a dotenv key")
	}
}
