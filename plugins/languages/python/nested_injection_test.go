//go:build cgo

package langpython

import (
	"strings"
	"testing"

	"ike/internal/highlight"

	// The nested case needs the injected languages' grammars registered:
	// html for the level-1 fragment, typescript/css for its <script>/<style>.
	_ "ike/plugins/languages/web"
)

// TestPythonNestedHTMLInjection guards #1697: HTML injected into a Python
// f-string resolves its own injections recursively, so the <style> body
// highlights as CSS and the <script> body as JavaScript — exactly as they
// would in a plain .html buffer. The f-string interpolation ({title}, #1466)
// splits the string into chunks around it without breaking the nested
// regions, and escaped braces ({{ … }}) stay inside their chunk.
func TestPythonNestedHTMLInjection(t *testing.T) {
	lines := []string{
		`text = f"""<!DOCTYPE html>`,
		`<html>`,
		`<head>`,
		`<title>{title}</title>`,
		`<style>`,
		`body {{ margin: 0; }}`,
		`</style>`,
		`</head>`,
		`<body>`,
		`<script>`,
		`const data = JSON.parse(atob(payload));`,
		`</script>`,
		`</body>`,
		`</html>"""`,
	}
	spans := highlight.Highlight("page.py", lines)
	if len(spans) == 0 {
		t.Fatal("expected spans for Python source, got none")
	}
	ix := highlight.NewIndex(spans)
	cases := []struct {
		name string
		line int
		word string
		want string
	}{
		{"level-1 html tag", 1, "html", "tag"},
		{"interpolated expression stays python", 3, "title}", "variable"},
		{"level-2 css property", 5, "margin", "property"},
		{"level-2 js keyword", 10, "const", "keyword"},
		{"level-2 js constant heuristic", 10, "JSON", "constant"},
	}
	for _, c := range cases {
		col := strings.Index(lines[c.line], c.word)
		if col < 0 {
			t.Fatalf("%s: %q not in line %d", c.name, c.word, c.line)
		}
		if got := ix.CaptureAt(c.line, col); got != c.want {
			t.Errorf("%s: CaptureAt(%d,%d) = %q, want %q", c.name, c.line, col, got, c.want)
		}
	}
}
