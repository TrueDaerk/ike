package diff

// bench_test.go guards the diff viewer's interactive cost (#2495). Mouse
// selection is the sensitive path: a drag emits one motion event per pointer
// cell, and every one of them used to rebuild every styled line of the whole
// diff. BenchmarkDragMotion measures exactly what one motion event during a
// drag costs — the drag plus the View frame it causes — so a regression that
// puts full-document work back on the drag shows up as an order of magnitude.

import (
	"fmt"
	"strings"
	"testing"

	"ike/internal/highlight"
	"ike/internal/theme"
)

// benchRows is the document size the drag benchmarks run over: a few thousand
// rows, the size the user report was filed at.
const benchRows = 3000

// benchTexts builds a Go-ish left/right pair of n lines with a change every
// tenth line, so the diff has real hunks and real intra-line refinement spans.
func benchTexts(n int) (left, right string) {
	var l, r strings.Builder
	for i := 0; i < n; i++ {
		line := fmt.Sprintf("\tif value%d := compute(%d, \"label %d\"); value%d != nil {", i, i, i, i)
		l.WriteString(line)
		l.WriteByte('\n')
		if i%10 == 0 {
			r.WriteString(strings.Replace(line, "compute", "recompute", 1))
		} else {
			r.WriteString(line)
		}
		r.WriteByte('\n')
	}
	return l.String(), r.String()
}

// benchSpans fakes what a Tree-sitter parse would return for those lines: a
// keyword, a function call and a string per line. The package's own test
// binary registers no language plugins, so highlight.Highlight finds no
// grammar — seeding the indexes directly is what keeps the benchmark on the
// syntax-highlighted render path the issue is about.
func benchSpans(lines int) highlight.Index {
	spans := make([]highlight.Span, 0, lines*3)
	for i := 0; i < lines; i++ {
		spans = append(spans,
			highlight.Span{Line: i, StartCol: 1, EndCol: 3, Capture: "keyword"},
			highlight.Span{Line: i, StartCol: 15, EndCol: 22, Capture: "function"},
			highlight.Span{Line: i, StartCol: 26, EndCol: 40, Capture: "string"},
		)
	}
	return highlight.NewIndex(spans)
}

// benchModel is a themed, syntax-highlighted, fully expanded model over an
// n-row diff at a realistic pane size.
func benchModel(tb testing.TB, n int, unified bool) *Model {
	tb.Helper()
	m := NewFiles("bench", "/tmp/left.go", "/tmp/right.go", theme.DefaultPalette())
	m.SetSize(200, 50)
	m.SetContext(-1) // no collapsing: every row is a rendered line
	m.SetUnified(unified)
	left, right := benchTexts(n)
	m.SetContents(left, right)
	m.leftIx, m.rightIx = benchSpans(n), benchSpans(n)
	m.render()
	return &m
}

// BenchmarkDragMotion is the issue's headline number: one motion event during
// an active drag, plus the View pass the event causes. Anything that scales
// with the document size here is a bug — the drag must only move the anchors,
// and View must re-style no more than the visible window.
func BenchmarkDragMotion(b *testing.B) {
	m := benchModel(b, benchRows, false)
	m.MousePress(20, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.MouseDrag(20+i%40, 1+i%40)
		_ = m.View()
	}
}

// BenchmarkDragMotionUnified is the same drag in unified layout, where a
// changed row renders as two visual lines.
func BenchmarkDragMotionUnified(b *testing.B) {
	m := benchModel(b, benchRows, true)
	m.MousePress(20, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.MouseDrag(20+i%40, 1+i%40)
		_ = m.View()
	}
}

// BenchmarkDragMotionOnly isolates the model mutation the motion event does,
// without the frame: it must be constant in the document size.
func BenchmarkDragMotionOnly(b *testing.B) {
	m := benchModel(b, benchRows, false)
	m.MousePress(20, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.MouseDrag(20+i%40, 1+i%40)
	}
}

// BenchmarkView is a frame with no selection at all — the floor the drag
// frame is measured against.
func BenchmarkView(b *testing.B) {
	m := benchModel(b, benchRows, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkRender is the full re-render a resize or a re-diff pays, for scale:
// the drag benchmark should be orders of magnitude below it.
func BenchmarkRender(b *testing.B) {
	m := benchModel(b, benchRows, false)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.render()
	}
}
