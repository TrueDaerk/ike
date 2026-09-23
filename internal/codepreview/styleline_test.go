package codepreview

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"ike/internal/highlight"
	"ike/internal/theme"
)

// themed returns a Cache whose capture→style table is built, so styleLine can
// be exercised without going through Render.
func themed(t *testing.T) *Cache {
	t.Helper()
	c := &Cache{}
	c.ensureTheme(theme.DefaultPalette())
	return c
}

// TestStyleLineOverlapFirstCoveringWins: overlapping spans keep CaptureAt's
// rule after the per-rune lookup became a filtered window walk (#2691) — the
// first span covering a cell, in iterator order, decides its color.
func TestStyleLineOverlapFirstCoveringWins(t *testing.T) {
	c := themed(t)
	kw, okKW := c.hl.Style("keyword")
	cm, okCM := c.hl.Style("comment")
	if !okKW || !okCM {
		t.Skip("palette styles neither keyword nor comment")
	}
	ix := highlight.NewIndex([]highlight.Span{
		{Line: 0, StartCol: 0, EndCol: 3, Capture: "keyword"},
		{Line: 0, StartCol: 0, EndCol: 8, Capture: "comment"},
	})
	got := c.styleLine(ix, 0, "abcdefgh", nil, nil, 0, 8)
	want := kw.Render("abc") + cm.Render("defgh")
	if got != want {
		t.Errorf("styleLine() = %q, want %q (first covering span wins)", got, want)
	}
}

// TestStyleLineStylesOnlyTheWindow: a line is styled at the columns shown, so
// a hit far out on a long line keeps its span color when panned into view and
// costs nothing while it is off screen (#2691).
func TestStyleLineStylesOnlyTheWindow(t *testing.T) {
	c := themed(t)
	kw, ok := c.hl.Style("keyword")
	if !ok {
		t.Skip("palette does not style keyword")
	}
	text := strings.Repeat("x", 100) + "func" + strings.Repeat("x", 100)
	ix := highlight.NewIndex([]highlight.Span{
		{Line: 0, StartCol: 100, EndCol: 104, Capture: "keyword"},
	})
	got := c.styleLine(ix, 0, text, nil, nil, 100, 110)
	want := kw.Render("func") + "xxxxxx"
	if got != want {
		t.Errorf("styleLine(window 100..110) = %q, want %q", got, want)
	}
	// The same window before the span shows plain text only.
	if got := c.styleLine(ix, 0, text, nil, nil, 0, 10); got != "xxxxxxxxxx" {
		t.Errorf("styleLine(window 0..10) = %q, want plain runes", got)
	}
}

// longLine builds a single line of n runes.
func longLine(n int) string { return strings.Repeat("a", n) }

// TestStyleLineLongLineCapDropsCaptures: past maxStyleRunes the syntax colors
// drop out — the editor's long-line rule (#2386) — but the match emphasis
// survives, so the hit stays findable in the excerpt (#2691).
func TestStyleLineLongLineCapDropsCaptures(t *testing.T) {
	c := themed(t)
	kw, ok := c.hl.Style("keyword")
	if !ok {
		t.Skip("palette does not style keyword")
	}
	ix := highlight.NewIndex([]highlight.Span{
		{Line: 0, StartCol: 0, EndCol: 10, Capture: "keyword"},
	})
	ranges := []Range{{Start: 2, End: 5}}
	mark := lipgloss.NewStyle().Bold(true).Underline(true)

	// Under the cap the capture colors and the match emphasis combine.
	short := longLine(maxStyleRunes)
	want := kw.Render("aa") + kw.Bold(true).Underline(true).Render("aaa") + kw.Render("aaaaa")
	if got := c.styleLine(ix, 0, short, ranges, nil, 0, 10); got != want {
		t.Fatalf("styleLine(under cap) = %q, want %q", got, want)
	}

	// One rune past it the same line renders plain, match emphasis intact.
	long := longLine(maxStyleRunes + 1)
	want = "aa" + mark.Render("aaa") + "aaaaa"
	if got := c.styleLine(ix, 0, long, ranges, nil, 0, 10); got != want {
		t.Errorf("styleLine(over cap) = %q, want %q (plain, match still marked)", got, want)
	}
}

// spanRun builds n spans of width runes each, alternating two captures — the
// shape a minified source hands the preview.
func spanRun(n, width int) []highlight.Span {
	captures := []string{"keyword", "string"}
	out := make([]highlight.Span, 0, n)
	for i := range n {
		out = append(out, highlight.Span{
			Line:     0,
			StartCol: i * width,
			EndCol:   i*width + width,
			Capture:  captures[i%len(captures)],
		})
	}
	return out
}

// TestStyleLineLinearOnMinifiedLine (#2691): a 200 kB single line carrying
// ~20 000 spans used to cost O(runes × spans) per frame and froze the update
// loop for tens of seconds. Styling it now pays for the rendered window.
func TestStyleLineLinearOnMinifiedLine(t *testing.T) {
	c := themed(t)
	text := longLine(200_000)
	ix := highlight.NewIndex(spanRun(20_000, 10))
	start := time.Now()
	for range 100 {
		c.styleLine(ix, 0, text, []Range{{Start: 120_000, End: 120_010}}, nil, 0, MaxPreviewWidth)
	}
	if d := time.Since(start) / 100; d > 50*time.Millisecond {
		t.Errorf("styleLine over a 200 kB line with 20 000 spans took %v, want well under 50ms", d)
	}
}

// TestRenderMinifiedFileIsFast (#2691): the whole frame — read, parse, style,
// clip — over a one-line 200 kB file stays inside a frame budget.
func TestRenderMinifiedFileIsFast(t *testing.T) {
	path := writeFile(t, "bundle.js", []string{longLine(200_000)})
	var c Cache
	start := time.Now()
	rows := c.Render(Target{Path: path, Line: 1, Ranges: []Range{{Start: 120_000, End: 120_010}}}, 80, 20, nil)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("Render of a 200 kB single-line file took %v", d)
	}
	if len(rows) != 20 {
		t.Fatalf("Render() = %d rows, want 20", len(rows))
	}
	if got := plain(rows)[0]; got != " 1 "+longLine(77) {
		t.Errorf("row 1 = %q, want the gutter and 77 runes of the line", got)
	}
}

// BenchmarkStyleLineMinified pins the minified-line path (#2691).
func BenchmarkStyleLineMinified(b *testing.B) {
	c := &Cache{}
	c.ensureTheme(theme.DefaultPalette())
	text := longLine(200_000)
	ix := highlight.NewIndex(spanRun(20_000, 10))
	ranges := []Range{{Start: 120_000, End: 120_010}}
	b.ReportAllocs()
	for b.Loop() {
		c.styleLine(ix, 0, text, ranges, nil, 0, MaxPreviewWidth)
	}
}

// TestStyleKeyTracksTheColumnWindow: the styled rows are clipped to the shown
// columns now, so the memo has to notice a pan (#2691).
func TestStyleKeyTracksTheColumnWindow(t *testing.T) {
	tg := Target{Path: "/tmp/x.go", Line: 3}
	a := styleKey(tg, 1, 10, 0, 80, theme.DefaultPalette())
	if b := styleKey(tg, 1, 10, 4, 80, theme.DefaultPalette()); a == b {
		t.Error("styleKey() ignores the horizontal offset")
	}
	if b := styleKey(tg, 1, 10, 0, 81, theme.DefaultPalette()); a == b {
		t.Error("styleKey() ignores the column width")
	}
	if b := styleKey(tg, 1, 10, 0, 80, theme.DefaultPalette()); a != b {
		t.Error("styleKey() is not stable for one window: " + strconv.Quote(a) + " vs " + strconv.Quote(b))
	}
}
