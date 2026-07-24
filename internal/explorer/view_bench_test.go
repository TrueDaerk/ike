package explorer

import (
	"fmt"
	"testing"
)

// benchTree builds a synthetic flattened tree of n rows.
func benchTree(n int) Model {
	m := New(".")
	m.SetSize(40, 50)
	rows := make([]*node, 0, n)
	rows = append(rows, m.root)
	for i := 1; i < n; i++ {
		rows = append(rows, &node{
			name:  fmt.Sprintf("file_%04d.go", i),
			path:  fmt.Sprintf("/proj/dir/file_%04d.go", i),
			depth: 1 + i%4,
		})
	}
	m.rows = rows
	m.invalidateWidth() // rows injected directly; first frame refills the cache
	return m
}

// BenchmarkExplorerView guards #1096: View over a 2000-row tree.
func BenchmarkExplorerView(b *testing.B) {
	m := benchTree(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}

// BenchmarkExplorerViewport guards #1096: the Update-path viewport cost.
func BenchmarkExplorerViewport(b *testing.B) {
	m := benchTree(2000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _, _ = m.viewport()
	}
}
