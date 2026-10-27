package jinja

import "testing"

// TestOverlaps (#3060): a range touching a block — inside it, across its
// edge, or a point in it — overlaps; ranges beside or between blocks do not.
func TestOverlaps(t *testing.T) {
	lines := []string{
		`items:`,
		`{% for x in xs %}`,
		`  - {{ x }}`,
		`{% endfor %}`,
		`tail: é {{ y }}`,
	}
	tpl := Scan(lines)
	for _, c := range []struct {
		name           string
		sl, sc, el, ec int
		want           bool
	}{
		{"before any block", 0, 0, 0, 5, false},
		{"whole for tag", 1, 0, 1, 17, true},
		{"inside the for tag", 1, 3, 1, 6, true},
		{"point in a tag", 1, 4, 1, 4, true},
		{"list dash beside an expression", 2, 2, 2, 3, false},
		{"ends at the expression start", 2, 2, 2, 4, false},
		{"runs into the expression", 2, 2, 2, 5, true},
		{"point right after a tag", 3, 12, 3, 12, false},
		{"across lines into a tag", 0, 0, 1, 1, true},
		{"rune column after é", 4, 6, 4, 7, false},
		{"expression after é", 4, 8, 4, 9, true},
		{"past the end", 9, 0, 9, 3, false},
	} {
		if got := tpl.Overlaps(c.sl, c.sc, c.el, c.ec); got != c.want {
			t.Errorf("%s: Overlaps(%d,%d,%d,%d) = %v, want %v", c.name, c.sl, c.sc, c.el, c.ec, got, c.want)
		}
	}
	if Scan([]string{"plain: text"}).Overlaps(0, 0, 0, 5) {
		t.Error("a text without blocks overlaps nothing")
	}
}
