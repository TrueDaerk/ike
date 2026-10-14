package preview

import (
	"os"
	"testing"

	"ike/internal/theme"
)

// loadLongTable reads the 5000-row pipe-table fixture (#2993): the Markdown
// export of the Complexity tool window on a large repository, the document
// shape that made an editor click take seconds with the preview open.
func loadLongTable(tb testing.TB) string {
	tb.Helper()
	src, err := os.ReadFile("testdata/longtable.md")
	if err != nil {
		tb.Fatal(err)
	}
	return string(src)
}

// longTablePreview is the fixture rendered into a 100x50 pane.
func longTablePreview(tb testing.TB) Model {
	tb.Helper()
	m := New("preview", "long.md", theme.DefaultPalette())
	m.SetSize(100, 50)
	m.SetSourceImmediate(loadLongTable(tb))
	return m
}

// TestCaretMoveNeverRenders (#2993): the editor forwards every caret move as
// a CursorMsg; scrolling the preview after it must not re-run glamour, which
// on the long-table fixture costs hundreds of milliseconds per render.
func TestCaretMoveNeverRenders(t *testing.T) {
	m := longTablePreview(t)
	renders := m.renders // the size and the source each rendered once
	lines := len(m.lines)
	for _, line := range []int{0, 10, 4999, 5007, 3, 2500} {
		m.SetCursorLine(line)
		if m.View() == "" {
			t.Fatalf("caret at %d: empty view", line)
		}
	}
	if m.renders != renders {
		t.Fatalf("caret moves rendered %d times, want 0", m.renders-renders)
	}
	if len(m.lines) != lines {
		t.Fatalf("caret moves changed the rendered document: %d lines, had %d", len(m.lines), lines)
	}
	if m.top == 0 {
		t.Fatal("the preview did not follow the caret")
	}
}

func BenchmarkRenderLongTable(b *testing.B) {
	src := loadLongTable(b)
	pal := theme.DefaultPalette()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Render(src, 100, pal); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetCursorLineLongTable(b *testing.B) {
	m := longTablePreview(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.SetCursorLine(i % 5000)
	}
}

func BenchmarkViewLongTable(b *testing.B) {
	m := longTablePreview(b)
	m.SetCursorLine(2500)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.View()
	}
}
