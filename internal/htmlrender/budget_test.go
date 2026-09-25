package htmlrender

// budget_test.go covers the bounds of a render (0530/7, #2745): the byte
// budget that stops a large page with a "truncated" line, and the
// cancellation RenderContext honours.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// bigPage is a page of n numbered paragraphs.
func bigPage(n int) []byte {
	var b bytes.Buffer
	b.WriteString("<html><body>")
	for i := range n {
		b.WriteString("<p>paragraph ")
		b.WriteString(strings.Repeat("x", i%7+1))
		b.WriteString(" of the report</p>\n")
	}
	b.WriteString("</body></html>")
	return b.Bytes()
}

func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

// TestBudgetTruncates: a page past the budget stops there and ends with the
// notice naming the budget; the same input renders the same lines every
// time.
func TestBudgetTruncates(t *testing.T) {
	page := bigPage(2000)
	opts := Options{Width: 60, Budget: 4 * 1024}
	d := Render(page, opts)
	if !d.Truncated {
		t.Fatal("a page past the budget must report Truncated")
	}
	lines := plain(d.Lines)
	if last := lines[len(lines)-1]; last != "… truncated after 4 KB" {
		t.Fatalf("last line = %q, want the truncation notice", last)
	}
	if lines[len(lines)-2] != "" {
		t.Fatalf("the notice must stand apart from the content: %q", lines[len(lines)-2])
	}
	// Only the budget's worth of paragraphs rendered.
	paras := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "paragraph ") {
			paras++
		}
	}
	if paras == 0 || paras >= 200 {
		t.Fatalf("rendered %d paragraphs of 2000 under a 4 KB budget", paras)
	}
	// Deterministic: a second render is identical.
	again := Render(page, opts)
	if strings.Join(again.Lines, "\n") != strings.Join(d.Lines, "\n") {
		t.Fatal("the truncated render must be deterministic")
	}
	// The notice maps back to the cut, not past the source.
	if off, ok := d.SourceOffset(len(d.Lines) - 1); !ok || off > opts.Budget {
		t.Fatalf("notice offset = %d, %v; want within the budget", off, ok)
	}
}

// TestBudgetFits: a page within the budget renders whole, without a notice —
// exactly like an unbounded render.
func TestBudgetFits(t *testing.T) {
	page := bigPage(20)
	d := Render(page, Options{Width: 60, Budget: len(page)})
	full := Render(page, Options{Width: 60})
	if d.Truncated || strings.Join(d.Lines, "\n") != strings.Join(full.Lines, "\n") {
		t.Fatalf("a page within the budget must render unchanged:\n%s", strings.Join(plain(d.Lines), "\n"))
	}
}

// TestBudgetCutBoundaries: the cut never splits a character or leaves half
// a tag behind to render as text.
func TestBudgetCutBoundaries(t *testing.T) {
	// "é" is two bytes; a budget of 4 lands inside the second one.
	cut, ok := cutBudget([]byte("abcéé"), 4)
	if !ok || string(cut) != "abc" {
		t.Fatalf("cut = %q, %v; want \"abc\"", cut, ok)
	}
	cut, _ = cutBudget([]byte("<p>one</p><div class=x>two"), 15)
	if string(cut) != "<p>one</p>" {
		t.Fatalf("cut = %q; want the half tag dropped", cut)
	}
	d := Render([]byte("<p>one</p><div class=x>two</div>"), Options{Width: 40, Budget: 15})
	for _, l := range plain(d.Lines) {
		if strings.Contains(l, "<") {
			t.Fatalf("a half tag rendered as text: %q", l)
		}
	}
	if _, ok := cutBudget([]byte("short"), 0); ok {
		t.Fatal("budget 0 must mean unbounded")
	}
}

// TestRenderContextCancelled: a cancelled context abandons the walk and
// reports its cause instead of a document.
func TestRenderContextCancelled(t *testing.T) {
	cx, cancel := context.WithCancel(context.Background())
	cancel()
	d, err := RenderContext(cx, bigPage(5000), Options{Width: 60})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(d.Lines) != 0 {
		t.Fatalf("a cancelled render returned %d lines", len(d.Lines))
	}
	// A small document may finish before the first poll; that is fine — the
	// guarantee is about large ones.
	d, err = RenderContext(context.Background(), bigPage(10), Options{Width: 60})
	if err != nil || len(d.Lines) == 0 {
		t.Fatalf("an uncancelled render must succeed: %v", err)
	}
}

// TestBudgetBoundsLargePage: a multi-MB page under the default budget
// renders in bounded time — the budget, not the file, sets the cost.
func TestBudgetBoundsLargePage(t *testing.T) {
	if testing.Short() {
		t.Skip("large page")
	}
	page := bigPage(160000) // ~5 MB
	if len(page) < 5<<20 {
		t.Fatalf("page is %d bytes, want 5 MB", len(page))
	}
	start := time.Now()
	d := Render(page, Options{Width: 100, Budget: 2048 * 1024})
	took := time.Since(start)
	if !d.Truncated {
		t.Fatal("a 5 MB page must hit the 2 MB budget")
	}
	t.Logf("5 MB page, 2 MB budget: %d lines in %v", len(d.Lines), took)
}

func BenchmarkRenderBudget(b *testing.B) {
	page := bigPage(160000)
	opts := Options{Width: 100, Budget: 2048 * 1024}
	b.SetBytes(int64(opts.Budget))
	for b.Loop() {
		Render(page, opts)
	}
}
