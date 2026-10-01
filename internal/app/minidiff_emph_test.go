package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/diff"
	"ike/internal/theme"
	"ike/internal/unidiff"
)

// miniDiffRow returns the rendered line whose plain text starts with prefix.
func miniDiffRow(t *testing.T, lines []string, prefix string) string {
	t.Helper()
	for _, l := range lines {
		if strings.HasPrefix(ansi.Strip(l), prefix) {
			return l
		}
	}
	t.Fatalf("no line starting with %q in %q", prefix, lines)
	return ""
}

// emphRuns returns the plain text of every bold (emphasized) segment.
func emphRuns(line string) []string {
	var runs []string
	for _, seg := range strings.Split(line, "\x1b[m") {
		if strings.Contains(seg, "\x1b[1;") || strings.Contains(seg, "\x1b[1m") {
			runs = append(runs, ansi.Strip(seg))
		}
	}
	return runs
}

func TestMiniDiffEmphasizesChangedPair(t *testing.T) {
	pal := theme.DefaultPalette()
	res := diff.Compute("keep\nfoo := alpha + 1\n", "keep\nfoo := omega + 1\n")
	lines := renderMiniDiff(pal, res, 80, true)
	minus := miniDiffRow(t, lines, "- ")
	plus := miniDiffRow(t, lines, "+ ")
	if got := ansi.Strip(minus); got != "- foo := alpha + 1" {
		t.Fatalf("plain text changed: %q", got)
	}
	if runs := emphRuns(minus); len(runs) == 0 || strings.Contains(strings.Join(runs, ""), "foo") {
		t.Fatalf("removed side emphasis runs = %q, want only the changed word", runs)
	}
	if runs := emphRuns(plus); len(runs) == 0 || !strings.Contains(strings.Join(runs, ""), "ome") {
		t.Fatalf("added side emphasis runs = %q", runs)
	}
	// Every line closes its styles: the final escape is a reset.
	for _, l := range []string{minus, plus} {
		if !strings.HasSuffix(l, "\x1b[m") {
			t.Fatalf("line does not end in a style reset: %q", l)
		}
	}
}

func TestMiniDiffPureAddRemoveUnemphasized(t *testing.T) {
	pal := theme.DefaultPalette()
	res := diff.Compute("a\nb\n", "a\nb\nadded line\n")
	for _, l := range renderMiniDiff(pal, res, 80, true) {
		if runs := emphRuns(l); len(runs) > 0 {
			t.Fatalf("pure add got emphasis %q in %q", runs, l)
		}
	}
}

func TestMiniDiffEmphasisOff(t *testing.T) {
	pal := theme.DefaultPalette()
	res := diff.Compute("x := alpha\n", "x := omega\n")
	on := renderMiniDiff(pal, res, 80, true)
	off := renderMiniDiff(pal, res, 80, false)
	for _, l := range off {
		if runs := emphRuns(l); len(runs) > 0 {
			t.Fatalf("emphasis rendered with the setting off: %q", l)
		}
	}
	if len(emphRuns(miniDiffRow(t, on, "+ "))) == 0 {
		t.Fatal("no emphasis with the setting on")
	}
	// The public entry point follows editor.diff_word_highlight's runtime
	// switch, which every mini-diff surface goes through.
	defer unidiff.SetWordHighlight(unidiff.WordHighlightEnabled())
	unidiff.SetWordHighlight(false)
	for _, l := range miniDiffLines(pal, res, 80) {
		if runs := emphRuns(l); len(runs) > 0 {
			t.Fatalf("miniDiffLines ignored the off switch: %q", l)
		}
	}
	unidiff.SetWordHighlight(true)
	if len(emphRuns(miniDiffRow(t, miniDiffLines(pal, res, 80), "+ "))) == 0 {
		t.Fatal("miniDiffLines ignored the on switch")
	}
}

func TestMiniDiffEmphasisTruncatesBalanced(t *testing.T) {
	pal := theme.DefaultPalette()
	// The changed token stays under the whole-line fallback share (#2849)
	// thanks to the long unchanged tail, so the pair carries spans.
	tail := " and a long unchanged tail"
	left := "start with " + strings.Repeat("a", 20) + tail
	right := "start with " + strings.Repeat("b", 20) + tail
	res := diff.Compute(left+"\n", right+"\n")
	const width = 20
	for _, l := range renderMiniDiff(pal, res, width, true) {
		if w := ansi.StringWidth(l); w > width {
			t.Fatalf("line wider than %d: %d %q", width, w, l)
		}
	}
	plus := miniDiffRow(t, renderMiniDiff(pal, res, width, true), "+ ")
	if !strings.HasSuffix(ansi.Strip(plus), "…") {
		t.Fatalf("truncated line lacks ellipsis: %q", ansi.Strip(plus))
	}
	if !strings.HasSuffix(plus, "\x1b[m") {
		t.Fatalf("truncated line leaves a style open: %q", plus)
	}
	if runs := emphRuns(plus); len(runs) == 0 || strings.Contains(strings.Join(runs, ""), "…") {
		t.Fatalf("emphasis runs %q: want the visible changed span, ellipsis in base style", runs)
	}
}

func TestMiniDiffEmphasisIgnoreWhitespaceAndTabs(t *testing.T) {
	pal := theme.DefaultPalette()
	res := diff.ComputeWith("\tx = old  value\n", "\tx = new value\n", diff.Options{IgnoreWhitespace: true})
	lines := renderMiniDiff(pal, res, 80, true)
	minus := miniDiffRow(t, lines, "- ")
	if got := ansi.Strip(minus); got != "-     x = old  value" {
		t.Fatalf("tab expansion changed: %q", got)
	}
	for _, r := range emphRuns(minus) {
		if strings.TrimSpace(r) != r {
			t.Fatalf("trimmed span emphasizes whitespace: %q", r)
		}
	}
}
