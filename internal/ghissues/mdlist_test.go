package ghissues

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// mdlist_test.go covers the hanging indent wrapped list items keep in the
// pane's markdown rendering (#2105).

// renderedLines runs a source through the pane's glamour pipeline at a fixed
// width and returns the plain, right-trimmed lines.
func renderedLines(t *testing.T, src string, width int) []string {
	t.Helper()
	m := &Model{width: width}
	out, err := m.renderMarkdown(src)
	if err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if p := strings.TrimRight(ansi.Strip(l), " "); p != "" {
			lines = append(lines, p)
		}
	}
	return lines
}

func TestRenderMarkdownHangingIndent(t *testing.T) {
	// 30 cells of pane, so glamour wraps at 28.
	got := renderedLines(t, "- abc def ghi jkl mno pqr stu vwx\n\n1. one long ordered item that wraps\n", 30)
	want := []string{
		"  • abc def ghi jkl mno",
		"    pqr stu vwx",
		"  1. one long ordered item",
		"     that wraps",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q,\nwant %q", got, want)
	}
}

func TestRenderMarkdownNestedListAlignment(t *testing.T) {
	src := "- parent item\n  - nested item that also wraps here\n"
	got := renderedLines(t, src, 30)
	want := []string{
		"  • parent item",
		"    • nested item that",
		"      also wraps here",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q,\nwant %q", got, want)
	}
}

func TestRenderMarkdownNarrowPaneKeepsText(t *testing.T) {
	src := "- alpha beta gamma\n\n1. delta epsilon\n"
	for _, w := range []int{1, 4, 8, 12} {
		got := renderedLines(t, src, w)
		joined := strings.Join(strings.Fields(strings.Join(got, " ")), "")
		for _, want := range []string{"alpha", "beta", "gamma", "delta", "epsilon"} {
			if !strings.Contains(joined, want) {
				t.Errorf("width %d: %q lost %q", w, got, want)
			}
		}
	}
}
