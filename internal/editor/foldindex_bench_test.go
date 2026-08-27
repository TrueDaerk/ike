package editor

import (
	"strings"
	"testing"

	"ike/internal/editor/buffer"
)

// BenchmarkLineHiddenManyFolds guards the fold interval index (#2187): with
// fold-all on a large file, lineHidden runs per rendered row per frame — the
// View body loop and wrap.DisplayRow both ask — and used to scan the whole
// collapsed map each time.
func BenchmarkLineHiddenManyFolds(b *testing.B) {
	const lines, folds = 20000, 2000
	src := make([]string, lines)
	for i := range src {
		src[i] = "line"
	}
	m := New()
	m.buf = buffer.FromString(strings.Join(src, "\n"))
	m.SetSize(80, 40)
	m.folded = make(map[int]int, folds)
	for i := 0; i < folds; i++ {
		h := i * (lines / folds)
		m.folded[h] = h + 5
	}
	m.bumpFolds()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// One frame's worth of rows.
		for row := 0; row < 40; row++ {
			if m.lineHidden(row * 7) {
				continue
			}
		}
	}
}
