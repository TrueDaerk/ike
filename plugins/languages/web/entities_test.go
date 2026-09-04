package langweb

import (
	"testing"

	"ike/internal/escapes"
)

func TestJSXEntitySpans(t *testing.T) {
	lines := []string{
		`return <i>Fish &amp; Chips</i>;`,
		`const s = "&amp;"; // a string, not JSX text`,
		`if (a > b && c < d) { run(); }`, // no valid entity between > and <
		`<td>{cell("&amp;")}&nbsp;</td>`, // brace island is code, tail is text
		`const f = (x) => x &amp; y;`,    // arrow, no tag close
	}
	got := map[int][]string{}
	for _, s := range jsxEntitySpans(lines) {
		if s.Capture == escapes.EntityCapture {
			got[s.Line] = append(got[s.Line], s.Replace)
		}
	}
	if v := got[0]; len(v) != 1 || v[0] != "&" {
		t.Errorf("line 0 decodes %v, want the JSX text reference", v)
	}
	for _, li := range []int{1, 2, 4} {
		if v := got[li]; len(v) != 0 {
			t.Errorf("line %d decodes %v, want nothing", li, v)
		}
	}
	if v := got[3]; len(v) != 1 || v[0] != " " {
		t.Errorf("line 3 decodes %v, want only the &nbsp; outside the braces", v)
	}
}

// TestScriptSpansWiring: the hook layers the #2345 families — masks,
// permission and cron hints, constant conceals — over the existing ones.
func TestScriptSpansWiring(t *testing.T) {
	lines := []string{
		`const apiKey = "sk_live_x";`,
		`fs.chmod(p, 0o755);`,
		`const CRON = "*/5 * * * *";`,
		`const MAX_BYTES = 0x100000;`,
	}
	want := map[int]string{0: "secret.value", 1: "perm.mode", 2: "cron.hint", 3: "number."}
	found := map[int]bool{}
	for _, s := range scriptSpans(lines) {
		for li, prefix := range want {
			if s.Line == li && len(s.Capture) >= len(prefix) && s.Capture[:len(prefix)] == prefix {
				found[li] = true
			}
		}
	}
	for li, capture := range want {
		if !found[li] {
			t.Errorf("line %d: no %q span produced", li, capture)
		}
	}
}

func TestCSSSpans(t *testing.T) {
	spans := cssSpans([]string{`content: "\00e9";`})
	if len(spans) != 1 || spans[0].Replace != "é" || spans[0].Capture != escapes.UnicodeCapture {
		t.Fatalf("got %+v, want the decoded CSS escape", spans)
	}
}
