//go:build cgo

package langyaml

import (
	"testing"

	"ike/internal/highlight"
)

// TestJinjaTemplateHighlighting (#3059): environment.yml.j2 highlights as
// YAML with the Jinja layer on top — a {% if %} block inside the mapping is
// masked before the parse, so the YAML keys after it still resolve to
// property, and the Jinja tokens paint over the inner spans.
func TestJinjaTemplateHighlighting(t *testing.T) {
	lines := []string{
		`app:`,
		`  name: {{ app_name | upper }}`,
		`{% if prod %}`,
		`  debug: false`,
		`{% endif %}`,
		`  port: 8080 {# fixed #}`,
	}
	ix := highlight.NewIndex(highlight.Highlight("environment.yml.j2", lines))
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 0, "property"},             // app
		{1, 2, "property"},             // name
		{1, 8, "punctuation.special"},  // {{
		{1, 11, "variable"},            // app_name
		{1, 20, "operator"},            // |
		{1, 22, "function"},            // upper
		{1, 28, "punctuation.special"}, // }}
		{2, 0, "punctuation.special"},  // {%
		{2, 3, "keyword"},              // if
		{2, 6, "variable"},             // prod
		{3, 2, "property"},             // debug, inside the if block
		{4, 3, "keyword"},              // endif
		{5, 2, "property"},             // port, after the block
		{5, 8, "number"},               // 8080
		{5, 13, "comment"},             // {# fixed #}
	} {
		if got := ix.CaptureAt(c.line, c.col); got != c.want {
			t.Errorf("CaptureAt(%d,%d) = %q, want %q (line %q)", c.line, c.col, got, c.want, lines[c.line])
		}
	}
}

// TestNoJinjaWithoutTemplateSuffix (#3059): the same text in a plain .yml
// file gets no Jinja spans.
func TestNoJinjaWithoutTemplateSuffix(t *testing.T) {
	lines := []string{`name: {{ app_name | upper }}`}
	ix := highlight.NewIndex(highlight.Highlight("environment.yml", lines))
	if got := ix.CaptureAt(0, 6); got == "punctuation.special" {
		t.Errorf("plain YAML got a Jinja delimiter at {{")
	}
	if got := ix.CaptureAt(0, 20); got == "function" {
		t.Errorf("plain YAML got a Jinja filter at upper")
	}
}
