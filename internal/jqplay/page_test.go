package jqplay

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// page_test.go covers the progressive producer (#2796): pages of
// PageOutputs, a suspended goroutine between them, cancellation without a
// leak, a per-page deadline that ignores time spent suspended, and the total
// budget still reporting a cap.

// bigArray is a JSON array of n small objects.
func bigArray(n int) string {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"i":%d}`, i)
	}
	b.WriteString("]")
	return b.String()
}

// drainPages pulls every page and returns the assembled result and the
// page count.
func drainPages(t *testing.T, p *Producer) (Result, int) {
	t.Helper()
	res := p.Shape()
	n := 0
	for {
		pg, ok := p.Next(context.Background())
		if !ok {
			t.Fatalf("Next returned no page after %d pages", n)
		}
		n++
		res.Append(pg)
		if pg.Done {
			return res, n
		}
		if n > 1000 {
			t.Fatal("the producer never finished")
		}
	}
}

// TestProducerPages is the issue's acceptance case: `.[]` over 5,000
// elements comes as pages of PageOutputs, partial until the last one, and
// assembles into exactly the text Run produces.
func TestProducerPages(t *testing.T) {
	in, err := Parse(bigArray(5000))
	if err != nil {
		t.Fatal(err)
	}
	p := Start(context.Background(), ".[]", in, Options{})
	first, ok := p.Next(context.Background())
	if !ok || first.Done {
		t.Fatalf("first page ok=%v done=%v", ok, first.Done)
	}
	if len(first.Outputs) != PageOutputs {
		t.Fatalf("first page holds %d outputs, want %d", len(first.Outputs), PageOutputs)
	}
	res := p.Shape()
	res.Append(first)
	if !res.Partial() || res.Count() != PageOutputs {
		t.Fatalf("after one page: partial=%v count=%d", res.Partial(), res.Count())
	}
	if _, pages := drainPages(t, p); pages != 24 {
		t.Errorf("the rest came in %d pages, want 24", pages)
	}
	// Re-run from scratch to compare the assembled text with Run's.
	p2 := Start(context.Background(), ".[]", in, Options{})
	all, n := drainPages(t, p2)
	if n != 25 {
		t.Errorf("5,000 outputs paged into %d pages, want 25", n)
	}
	if all.Partial() || all.Truncated || all.Err != "" || all.Count() != 5000 {
		t.Fatalf("assembled: partial=%v truncated=%v err=%q count=%d", all.Partial(), all.Truncated, all.Err, all.Count())
	}
	ctx, cancel := context.WithTimeout(context.Background(), EvalTimeout)
	defer cancel()
	if want := Run(ctx, ".[]", in).Text(); all.Text() != want {
		t.Error("the paged text differs from Run's")
	}
	if !p2.Wait(time.Second) {
		t.Error("the producer goroutine is still alive after its last page")
	}
}

// TestProducerExactPageIsDone: a result of exactly one page is Done on that
// page, never followed by an empty page claiming more.
func TestProducerExactPageIsDone(t *testing.T) {
	in, _ := Parse(bigArray(PageOutputs))
	p := Start(context.Background(), ".[]", in, Options{})
	pg, ok := p.Next(context.Background())
	if !ok || !pg.Done || len(pg.Outputs) != PageOutputs {
		t.Fatalf("ok=%v done=%v n=%d", ok, pg.Done, len(pg.Outputs))
	}
	if _, ok := p.Next(context.Background()); ok {
		t.Error("Next after the last page must report the end")
	}
}

// TestProducerCancelWhileSuspended: cancelling the host context ends the
// goroutine parked between two pages, and Next reports the end.
func TestProducerCancelWhileSuspended(t *testing.T) {
	in, _ := Parse(bigArray(5000))
	ctx, cancel := context.WithCancel(context.Background())
	p := Start(ctx, ".[]", in, Options{})
	if _, ok := p.Next(ctx); !ok {
		t.Fatal("first page")
	}
	cancel()
	if !p.Wait(time.Second) {
		t.Fatal("the suspended producer did not exit on cancel")
	}
	if _, ok := p.Next(context.Background()); ok {
		t.Error("Next after cancel must report the end")
	}
}

// TestProducerStopWhileComputing: Stop reaches into a page that is still
// being computed — an infinite iterator — and ends the goroutine.
func TestProducerStopWhileComputing(t *testing.T) {
	in, _ := Parse("null")
	p := Start(context.Background(), "repeat(0) | select(. == 1)", in, Options{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		p.Stop()
	}()
	pg, ok := p.Next(context.Background())
	if ok && pg.Err == "" {
		t.Fatalf("a stopped page must fail, got ok=%v page=%+v", ok, pg)
	}
	if !p.Wait(time.Second) {
		t.Fatal("the computing producer did not exit on Stop")
	}
}

// TestProducerNextHonoursCallerContext: a Next whose own context ends while
// the page is computing returns at once with ok=false, leaving the producer
// to its host context.
func TestProducerNextHonoursCallerContext(t *testing.T) {
	in, _ := Parse("null")
	p := Start(context.Background(), "repeat(0) | select(. == 1)", in, Options{})
	defer p.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, ok := p.Next(ctx); ok {
		t.Fatal("Next must give up with its context")
	}
	if time.Since(start) > time.Second {
		t.Error("Next outlived its context")
	}
}

// TestProducerPageTimeoutIgnoresSuspension: the per-page deadline counts
// computing time only. A first page that is instant, then a suspension
// longer than the timeout, then a second page — which arrives, because the
// clock did not run while nobody asked.
func TestProducerPageTimeoutIgnoresSuspension(t *testing.T) {
	prev := evalTimeout
	evalTimeout = 100 * time.Millisecond
	t.Cleanup(func() { evalTimeout = prev })
	in, _ := Parse(bigArray(1000))
	p := Start(context.Background(), ".[]", in, Options{})
	defer p.Stop()
	if _, ok := p.Next(context.Background()); !ok {
		t.Fatal("first page")
	}
	time.Sleep(3 * evalTimeout)
	pg, ok := p.Next(context.Background())
	if !ok || pg.Err != "" || len(pg.Outputs) != PageOutputs {
		t.Fatalf("second page after a long suspension: ok=%v err=%q n=%d", ok, pg.Err, len(pg.Outputs))
	}
}

// TestProducerPageTimeout: a page that takes longer than the deadline fails
// with the timeout message, like a whole run used to.
func TestProducerPageTimeout(t *testing.T) {
	prev := evalTimeout
	evalTimeout = 50 * time.Millisecond
	t.Cleanup(func() { evalTimeout = prev })
	in, _ := Parse("null")
	p := Start(context.Background(), "def f: f; f", in, Options{})
	pg, ok := p.Next(context.Background())
	if !ok || !pg.Done || !strings.Contains(pg.Err, "did not finish") {
		t.Fatalf("ok=%v done=%v err=%q", ok, pg.Done, pg.Err)
	}
	if !p.Wait(time.Second) {
		t.Error("the timed-out producer did not exit")
	}
}

// TestProducerTotalCap: `range(infinite)` pages up to MaxOutputs and the
// last page reports the cap, exactly as Run does.
func TestProducerTotalCap(t *testing.T) {
	in, _ := Parse("null")
	p := Start(context.Background(), "range(infinite)", in, Options{})
	res, pages := drainPages(t, p)
	if !res.Truncated || res.Partial() || res.Count() != MaxOutputs {
		t.Fatalf("truncated=%v partial=%v count=%d pages=%d", res.Truncated, res.Partial(), res.Count(), pages)
	}
	if pages != MaxOutputs/PageOutputs {
		t.Errorf("%d pages, want %d", pages, MaxOutputs/PageOutputs)
	}
}

// TestProducerByteCap: few values, each enormous — the byte budget ends the
// stream with the cap, and a page never exceeds PageBytes by more than one
// value.
func TestProducerByteCap(t *testing.T) {
	in, _ := Parse("null")
	// Each value is ~1 MiB of string: nine of them cross 8 MiB.
	p := Start(context.Background(), `range(20) | "x" * 1048576`, in, Options{})
	res, pages := drainPages(t, p)
	if !res.Truncated || res.Count() >= 20 {
		t.Fatalf("truncated=%v count=%d", res.Truncated, res.Count())
	}
	if res.Size() > MaxResultBytes+(1<<20) {
		t.Errorf("result holds %d bytes, cap %d", res.Size(), MaxResultBytes)
	}
	if pages != res.Count() {
		t.Errorf("a megabyte value fills a page: %d pages for %d values", pages, res.Count())
	}
}

// TestProducerErrorAfterValues: a runtime error after some values lands on
// the page that holds them (Result.Err's contract), Done.
func TestProducerErrorAfterValues(t *testing.T) {
	in, _ := Parse(`[{"x":1},3]`)
	p := Start(context.Background(), ".[] | .x", in, Options{})
	pg, ok := p.Next(context.Background())
	if !ok || !pg.Done || len(pg.Outputs) != 1 || pg.Err == "" {
		t.Fatalf("ok=%v done=%v n=%d err=%q", ok, pg.Done, len(pg.Outputs), pg.Err)
	}
}

// TestProducerCompileError: a program that does not compile is one Done
// page carrying the error.
func TestProducerCompileError(t *testing.T) {
	in, _ := Parse("null")
	p := Start(context.Background(), ".foo[", in, Options{})
	pg, ok := p.Next(context.Background())
	if !ok || !pg.Done || pg.Err == "" || len(pg.Outputs) != 0 {
		t.Fatalf("ok=%v done=%v n=%d err=%q", ok, pg.Done, len(pg.Outputs), pg.Err)
	}
}

// TestFoldsSinceAndTextSince: the fold and text increments of appended pages
// line up with a scan of the whole result.
func TestFoldsSinceAndTextSince(t *testing.T) {
	in, _ := Parse(bigArray(450))
	p := Start(context.Background(), ".[] | {i: ., a: [1,2]}", in, Options{})
	res := p.Shape()
	var text string
	var folds []Fold
	for {
		pg, ok := p.Next(context.Background())
		if !ok {
			t.Fatal("Next")
		}
		n := len(res.Outputs)
		res.Append(pg)
		text += res.TextSince(n)
		folds = append(folds, res.FoldsSince(n)...)
		if pg.Done {
			break
		}
	}
	if text != res.Text() {
		t.Error("TextSince increments do not rebuild Text")
	}
	want := res.Folds()
	if len(folds) != len(want) {
		t.Fatalf("%d folds from increments, %d from the whole scan", len(folds), len(want))
	}
	for i := range want {
		if folds[i].HeaderLine != want[i].HeaderLine || folds[i].EndLine != want[i].EndLine || folds[i].Items != want[i].Items {
			t.Fatalf("fold %d: increment %+v, whole %+v", i, folds[i], want[i])
		}
	}
	if per := len(Evaluate(".[] | {i: ., a: [1,2]}", bigArray(1)).Folds()); len(want) != 450*per {
		t.Errorf("%d folds, want %d (%d per value)", len(want), 450*per, per)
	}
}

// TestCutLines pages a text by lines: whole lines, at least one per chunk,
// rejoined with "\n" into the original.
func TestCutLines(t *testing.T) {
	lines := []string{"a", strings.Repeat("b", 100), "c", "d"}
	var chunks []string
	rest := lines
	for len(rest) > 0 {
		var chunk []string
		chunk, rest = cutLines(rest, 50)
		chunks = append(chunks, strings.Join(chunk, "\n"))
	}
	if len(chunks) != 3 {
		t.Fatalf("chunks = %q", chunks)
	}
	if strings.Join(chunks, "\n") != strings.Join(lines, "\n") {
		t.Error("chunks do not rejoin into the text")
	}
}

// TestXMQProducerPagesByLines: an xmq run is one CLI result paged by lines
// — several chunks that count as one value and fold as one document.
func TestXMQProducerPagesByLines(t *testing.T) {
	fakeXMQ(t, `printf '[\n'
i=0
while [ $i -lt 20000 ]; do printf '  {"i": %d},\n' $i; i=$((i+1)); done
printf '  null\n]'`)
	in, err := DialectXMQ.Parse("<r/>")
	if err != nil {
		t.Fatal(err)
	}
	p := Start(context.Background(), "to-json", in, Options{})
	res, pages := drainPages(t, p)
	if pages < 3 {
		t.Fatalf("a 300 KB output should page, got %d page(s)", pages)
	}
	if res.Count() != 1 || res.Ext() != "json" || res.Err != "" {
		t.Fatalf("count=%d ext=%q err=%q", res.Count(), res.Ext(), res.Err)
	}
	if starts := res.ValueStarts(); len(starts) != 1 {
		t.Errorf("value starts = %v, want one", starts)
	}
	if folds := res.Folds(); len(folds) != 1 || folds[0].Items != 20001 {
		t.Errorf("folds = %+v, want the one array of 20,001 items", folds)
	}
	if items := res.Outline(); len(items) != 20001 {
		t.Errorf("%d outline items", len(items))
	}
	if !strings.HasSuffix(res.Text(), "  null\n]") {
		t.Error("the chunks do not rejoin into the CLI output")
	}
}

// BenchmarkProducerFirstPage measures the time to the first page of `.[]`
// over 5,000 objects — what the reader waits for (#2796).
func BenchmarkProducerFirstPage(b *testing.B) {
	in, _ := Parse(bigArray(5000))
	for i := 0; i < b.N; i++ {
		p := Start(context.Background(), ".[]", in, Options{})
		if _, ok := p.Next(context.Background()); !ok {
			b.Fatal("first page")
		}
		p.Stop()
	}
}

// BenchmarkProducerPage measures one further page: compute plus Append.
func BenchmarkProducerPage(b *testing.B) {
	in, _ := Parse(bigArray(5000))
	p := Start(context.Background(), ".[]", in, Options{})
	res := p.Shape()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pg, ok := p.Next(context.Background())
		if !ok {
			b.StopTimer()
			p = Start(context.Background(), ".[]", in, Options{})
			res = p.Shape()
			b.StartTimer()
			continue
		}
		res.Append(pg)
	}
	p.Stop()
}

// BenchmarkRunWhole is the old shape for comparison: everything at once.
func BenchmarkRunWhole(b *testing.B) {
	in, _ := Parse(bigArray(5000))
	for i := 0; i < b.N; i++ {
		Run(context.Background(), ".[]", in)
	}
}
