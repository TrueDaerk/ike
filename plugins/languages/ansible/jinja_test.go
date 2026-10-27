package langansible

import (
	"fmt"
	"slices"
	"testing"

	"ike/internal/lang"
)

// at renders the span covering line:col as "capture" ("" when none).
func at(spans []lang.Span, line, col int) string {
	for _, s := range spans {
		if s.Line == line && s.StartCol <= col && col < s.EndCol {
			return s.Capture
		}
	}
	return ""
}

// TestJinjaSpansBlocks (#3061): blocks in values paint, blocks in comments
// do not, and an unterminated "{{" neither paints nor hides later blocks.
func TestJinjaSpansBlocks(t *testing.T) {
	lines := []string{
		`msg: "{{ a }}"`,               // 0
		`x: 1 # {{ b }}`,               // 1 comment
		`bad: "{{ oops`,                // 2 unterminated
		`ok: "{% if c %}y{% endif %}"`, // 3
		`text: "a # {{ d }}"`,          // 4 '#' inside quotes
		`raw: it's {{ e }}`,            // 5 apostrophe in a plain scalar
	}
	spans := jinjaSpans(lines)
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 9, "variable"},
		{1, 10, ""},
		{2, 9, ""},
		{3, 9, "keyword"},
		{3, 11, "variable"},
		{3, 19, "keyword"},
		{4, 14, "variable"},
		{5, 13, "variable"},
	} {
		if got := at(spans, c.line, c.col); got != c.want {
			t.Errorf("%d:%d %q: got %q, want %q", c.line, c.col, lines[c.line], got, c.want)
		}
	}
}

// TestJinjaSpansBareKeys (#3061): the bare-expression keys tokenize their
// inline value (quoted or plain, comment cut off), a list below them, and a
// block scalar; other keys and {{ }}-written values are not doubled.
func TestJinjaSpansBareKeys(t *testing.T) {
	lines := []string{
		`- when: x == 1 # note`,       // 0 list-item key
		`  changed_when: "r.rc != 0"`, // 1 quoted
		`  failed_when:`,              // 2
		`    - a is failed`,           // 3
		`    - b`,                     // 4
		`  until:`,                    // 5
		`  - c`,                       // 6 compact list
		`  when: >-`,                  // 7
		`    d and`,                   // 8
		`    e`,                       // 9
		`  name: f is defined`,        // 10 not an expression key
		`  when: "{{ g }}"`,           // 11 handled by blocks
		`  whenever: h`,               // 12 other key
	}
	spans := jinjaSpans(lines)
	for _, c := range []struct {
		line, col int
		want      string
	}{
		{0, 8, "variable"},
		{0, 10, "operator"},
		{0, 13, "number"},
		{0, 17, ""}, // the comment
		{1, 17, "variable"},
		{1, 19, "property"},
		{1, 23, "operator"},
		{3, 6, "variable"},
		{3, 8, "keyword"},
		{3, 11, "function"},
		{4, 6, "variable"},
		{6, 4, "variable"},
		{8, 4, "variable"},
		{8, 6, "keyword"},
		{9, 4, "variable"},
		{10, 8, ""},
		{12, 12, ""},
	} {
		if got := at(spans, c.line, c.col); got != c.want {
			t.Errorf("%d:%d %q: got %q, want %q", c.line, c.col, lines[c.line], got, c.want)
		}
	}
	var g []string
	for _, s := range spans {
		if s.Line == 11 {
			g = append(g, fmt.Sprintf("%d-%d", s.StartCol, s.EndCol))
		}
	}
	if want := []string{"9-11", "12-13", "14-16"}; !slices.Equal(g, want) {
		t.Errorf("line 11 spans %v, want %v (once, from the block scan)", g, want)
	}
}
