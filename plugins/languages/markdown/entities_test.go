package langmarkdown

import (
	"testing"

	"ike/internal/escapes"
	"ike/internal/lang"
)

func entityTexts(lines []string, spans []lang.Span) map[int][]string {
	out := map[int][]string{}
	for _, s := range spans {
		if s.Capture == escapes.EntityCapture {
			out[s.Line] = append(out[s.Line], s.Replace)
		}
	}
	return out
}

func TestMarkdownEntities(t *testing.T) {
	lines := []string{
		`Fish &amp; chips &#x2026;`,
		"Inline `&amp;` stays, &lt; decodes",
		"```",
		`&amp; inside a fence stays literal`,
		"```",
		`&amp; after the fence decodes`,
		`    &amp; indented code stays literal`,
		`&notanentity; stays raw`,
	}
	got := entityTexts(lines, entitySpans(lines))
	if v := got[0]; len(v) != 2 || v[0] != "&" || v[1] != "…" {
		t.Errorf("line 0 decodes %v, want & and the ellipsis", v)
	}
	if v := got[1]; len(v) != 1 || v[0] != "<" {
		t.Errorf("line 1 decodes %v, want only the reference outside the code span", v)
	}
	for _, li := range []int{3, 6, 7} {
		if v := got[li]; len(v) != 0 {
			t.Errorf("line %d decodes %v, want nothing", li, v)
		}
	}
	if v := got[5]; len(v) != 1 || v[0] != "&" {
		t.Errorf("line 5 decodes %v, want the fence to have closed", v)
	}
}
