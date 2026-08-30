package codepreview

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// viewport_test.go covers the read-only mini editor of #2327: the adaptive
// width, the scroll keys, the highlighted hit line and the plain-text
// fallback.

// goSource is a small Go file whose line 4 carries a match-worthy identifier.
func goSource(t *testing.T, n int) string {
	t.Helper()
	rows := []string{
		"package sample",
		"",
		"func Total(values []int) int {",
		"\tsum := 0",
		"\tfor _, v := range values {",
		"\t\tsum += v",
		"\t}",
		"\treturn sum",
		"}",
	}
	for i := len(rows); i < n; i++ {
		rows = append(rows, "// filler "+strconv.Itoa(i))
	}
	return writeFile(t, "sample.go", rows)
}

// TestNaturalAdaptsToTheCode is the width-adaptation criterion: a file of
// short lines asks for the floor, one of long lines for the cap, and a file in
// between for its widest visible line plus the gutter.
func TestNaturalAdaptsToTheCode(t *testing.T) {
	var c Cache
	short := writeLines(t, 40) // "line 20" and friends
	if got := c.Natural(Target{Path: short, Line: 20}, 11); got != MinPreviewWidth {
		t.Fatalf("short lines want %d cells, got %d", MinPreviewWidth, got)
	}
	var c2 Cache
	long := writeFile(t, "long.txt", lines(40, func(int) string { return strings.Repeat("x", 400) }))
	if got := c2.Natural(Target{Path: long, Line: 20}, 11); got != MaxPreviewWidth {
		t.Fatalf("long lines want the %d cap, got %d", MaxPreviewWidth, got)
	}
	var c3 Cache
	mid := writeFile(t, "mid.txt", lines(40, func(int) string { return strings.Repeat("y", 70) }))
	// The window around line 20 spans lines 15…25, so the gutter is two
	// digits wide plus its space.
	if got, want := c3.Natural(Target{Path: mid, Line: 20}, 11), 2+1+70; got != want {
		t.Fatalf("mid-width lines want %d cells, got %d", want, got)
	}
	// A target with no file falls back to the floor rather than failing.
	if got := c3.Natural(Target{}, 11); got != MinPreviewWidth {
		t.Fatalf("empty target wants %d, got %d", MinPreviewWidth, got)
	}
}

// TestNaturalIgnoresScroll keeps the columns still: scrolling a focused
// preview must not resize it under the cursor, so the width stays measured
// around the hit.
func TestNaturalIgnoresScroll(t *testing.T) {
	rows := lines(80, func(i int) string {
		if i > 40 {
			return strings.Repeat("w", 300) // a wide tail, far from the hit
		}
		return "narrow " + strconv.Itoa(i)
	})
	path := writeFile(t, "mixed.txt", rows)
	var c Cache
	target := Target{Path: path, Line: 10}
	before := c.Natural(target, 11)
	c.Render(target, 60, 11, nil)
	c.SetFocus(true)
	for range 40 {
		c.Key("j")
	}
	c.Render(target, 60, 11, nil)
	if after := c.Natural(target, 11); after != before {
		t.Fatalf("width changed while scrolling: %d → %d", before, after)
	}
}

// TestScrollsVerticallyThroughTheFile is the vertical-scroll criterion: the
// focused preview walks the whole file, clamps at both ends, and z returns to
// the hit the list selected.
func TestScrollsVerticallyThroughTheFile(t *testing.T) {
	path := writeLines(t, 200)
	var c Cache
	target := Target{Path: path, Line: 100}
	const w, h = 40, 11
	first := plain(c.Render(target, w, h, nil))
	if !strings.HasSuffix(first[0], "line 95") {
		t.Fatalf("row 0 = %q, want the centered window", first[0])
	}
	// Blurred, the motions belong to the host's list.
	if c.Key("j") {
		t.Fatal("a blurred preview must not consume scroll keys")
	}
	c.SetFocus(true)
	if !c.Key("j") || !c.Key("j") {
		t.Fatal("j must scroll a focused preview")
	}
	if rows := plain(c.Render(target, w, h, nil)); !strings.HasSuffix(rows[0], "line 97") {
		t.Fatalf("row 0 = %q, want line 97 after two j", rows[0])
	}
	c.Key("ctrl+d")
	if rows := plain(c.Render(target, w, h, nil)); !strings.HasSuffix(rows[0], "line 102") {
		t.Fatalf("row 0 = %q, want line 102 after ctrl+d", rows[0])
	}
	c.Key("g")
	if rows := plain(c.Render(target, w, h, nil)); !strings.HasSuffix(rows[0], "line 1") {
		t.Fatalf("row 0 = %q, want the file head", rows[0])
	}
	// Scrolling up from the head clamps instead of running off the file.
	for range 5 {
		c.Key("k")
	}
	if rows := plain(c.Render(target, w, h, nil)); !strings.HasSuffix(rows[0], "line 1") {
		t.Fatalf("row 0 = %q, want the head to hold", rows[0])
	}
	c.Key("G")
	tail := plain(c.Render(target, w, h, nil))
	if !strings.HasSuffix(tail[h-1], "line 200") {
		t.Fatalf("last row = %q, want the file tail", tail[h-1])
	}
	for range 5 {
		c.Key("j")
	}
	if rows := plain(c.Render(target, w, h, nil)); !strings.HasSuffix(rows[h-1], "line 200") {
		t.Fatalf("last row = %q, want the tail to hold", rows[h-1])
	}
	c.Key("z")
	if rows := plain(c.Render(target, w, h, nil)); !strings.HasSuffix(rows[h/2], "line 100") {
		t.Fatalf("row %d = %q, want the hit re-centered", h/2, rows[h/2])
	}
}

// TestSelectionMoveRecenters is the acceptance criterion that moving the
// result cursor repositions the preview even after it was scrolled away.
func TestSelectionMoveRecenters(t *testing.T) {
	path := writeLines(t, 200)
	var c Cache
	c.Render(Target{Path: path, Line: 20}, 40, 11, nil)
	c.SetFocus(true)
	for range 30 {
		c.Key("j")
	}
	c.Key("l")
	rows := plain(c.Render(Target{Path: path, Line: 150}, 40, 11, nil))
	if !strings.HasSuffix(rows[5], "line 150") {
		t.Fatalf("row 5 = %q, want the new hit centered", rows[5])
	}
	if !strings.HasSuffix(rows[0], "line 145") {
		t.Fatalf("row 0 = %q, want the horizontal offset dropped too", rows[0])
	}
}

// TestScrollsHorizontally is the long-line criterion: h/l walk the excerpt
// sideways, 0 returns to the first column, and no row ever outgrows the
// column while doing so.
func TestScrollsHorizontally(t *testing.T) {
	body := strings.Repeat("abcdefghij", 30) // 300 cells
	path := writeFile(t, "wide.txt", lines(20, func(int) string { return body }))
	var c Cache
	target := Target{Path: path, Line: 10}
	const w, h = 40, 11
	head := plain(c.Render(target, w, h, nil))
	if !strings.Contains(head[0], "abcdefghij") {
		t.Fatalf("row 0 = %q, want the line's head", head[0])
	}
	c.SetFocus(true)
	for range 3 { // 3 × hStep = 12 cells
		c.Key("l")
	}
	rows := c.Render(target, w, h, nil)
	shifted := plain(rows)
	if strings.HasPrefix(strings.TrimLeft(shifted[0], " 0123456789"), "abcdefghij") {
		t.Fatalf("row 0 = %q, want the text scrolled right", shifted[0])
	}
	if !strings.Contains(shifted[0], "cdefghijab") {
		t.Fatalf("row 0 = %q, want the window twelve cells in", shifted[0])
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got > w {
			t.Fatalf("scrolled row %d is %d cells wide, past the %d column", i, got, w)
		}
	}
	c.Key("h")
	c.Key("0")
	if back := plain(c.Render(target, w, h, nil)); back[0] != head[0] {
		t.Fatalf("0 gave %q, want the unscrolled %q", back[0], head[0])
	}
}

// TestHitLineHighlighted is the matched-line criterion: the hit carries a
// background across the whole column — the other rows do not — and its match
// ranges render differently from the same line without them.
func TestHitLineHighlighted(t *testing.T) {
	path := goSource(t, 20)
	var c Cache
	const w, h = 60, 9
	// Line 5 with a nine-row window centres exactly (the window starts at
	// line 1, so the hit lands on the middle row).
	rows := c.Render(Target{Path: path, Line: 5}, w, h, nil)
	hit := rows[h/2]
	if !strings.Contains(ansi.Strip(hit), "for _, v := range values") {
		t.Fatalf("centre row = %q, want the hit line", ansi.Strip(hit))
	}
	if got := ansi.StringWidth(hit); got != w {
		t.Fatalf("hit line is %d cells wide, want the full %d", got, w)
	}
	for i, r := range rows {
		if i == h/2 || strings.TrimSpace(ansi.Strip(r)) == "" {
			continue
		}
		if got := ansi.StringWidth(r); got == w {
			t.Fatalf("row %d fills the column too — only the hit line may", i)
		}
	}
	// The match range adds its own emphasis on top of whatever the syntax
	// colours already did.
	marked := c.Render(Target{Path: path, Line: 5, Ranges: []Range{{Start: 5, End: 10}}}, w, h, nil)
	if marked[h/2] == hit {
		t.Fatal("a match range must change how the hit line renders")
	}
	if ansi.Strip(marked[h/2]) != ansi.Strip(hit) {
		t.Fatal("the match emphasis must not change the text, only its styling")
	}
}

// TestMatchRangeFollowsTabExpansion keeps an indented hit's emphasis over the
// right columns: the raw range is measured in runes, the rendered line in
// expanded cells.
func TestMatchRangeFollowsTabExpansion(t *testing.T) {
	path := writeFile(t, "tabs.txt", []string{"\t\tsum += v", "second", "third"})
	var c Cache
	// "sum" starts at rune 2 of the raw line, cell 8 of the rendered one.
	marked := c.Render(Target{Path: path, Line: 1, Ranges: []Range{{Start: 2, End: 5}}}, 40, 3, nil)
	off := c.Render(Target{Path: path, Line: 1, Ranges: []Range{{Start: 8, End: 11}}}, 40, 3, nil)
	if marked[0] == off[0] {
		t.Fatal("the range must be mapped through the tab expansion, not used raw")
	}
	if !strings.Contains(ansi.Strip(marked[0]), "        sum += v") {
		t.Fatalf("row 0 = %q, want the tabs expanded", ansi.Strip(marked[0]))
	}
}

// TestPlainFallbackForUnsupportedFiletype is the fallback criterion: a file
// whose extension no grammar backs renders as plain text, unstyled and
// unbroken. The other half — a real grammar colouring the excerpt — needs a
// language plugin linked in and lives in ./hltest.
func TestPlainFallbackForUnsupportedFiletype(t *testing.T) {
	var c Cache
	unknown := writeFile(t, "notes.zzz", []string{"alpha beta", "gamma delta", "epsilon"})
	rows := c.Render(Target{Path: unknown, Line: 2}, 40, 3, nil)
	if got := afterGutter(rows[0]); got != "alpha beta" {
		t.Fatalf("unsupported filetype row 0 = %q, want plain text", got)
	}
	if got := afterGutter(rows[2]); got != "epsilon" {
		t.Fatalf("unsupported filetype row 2 = %q, want plain text", got)
	}
	// The hit line keeps its highlight even with nothing to colour.
	if got := ansi.StringWidth(rows[1]); got != 40 {
		t.Fatalf("hit line is %d cells wide, want the full 40", got)
	}
}

// sgrRE matches one SGR escape sequence.
var sgrRE = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// afterGutter returns a rendered row without its styled line-number gutter —
// the styled run that opens the row and the reset that closes it.
func afterGutter(row string) string {
	if at := sgrRE.FindAllStringIndex(row, 2); len(at) == 2 {
		return row[at[1][1]:]
	}
	return row
}
