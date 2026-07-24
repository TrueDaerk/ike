package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/editor/buffer"
	ilsp "ike/internal/lsp"
)

// BenchmarkEditorViewWarm measures a warm-cache View mid-file with the
// scrollbar + error stripe active (#1097).
func BenchmarkEditorViewWarm(b *testing.B) {
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = "func handler(w http.ResponseWriter, r *http.Request) error { return nil } // pad pad pad"
	}
	dir := b.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		b.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		b.Fatal(err)
	}
	m.SetSize(120, 40)
	m.cursor = buffer.Position{Line: 2500}
	var diags []ilsp.Diagnostic
	for i := 0; i < 60; i++ {
		diags = append(diags, ilsp.Diagnostic{
			Range:    buffer.Range{Start: buffer.Position{Line: i * 80}},
			Severity: 1 + i%3,
			Message:  "boom",
		})
	}
	m.setDiagnostics(diags)
	m.scroll()
	_ = m.View() // warm the line cache
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := m.View()
		if !strings.Contains(s, "■") {
			b.Fatal("error stripe missing")
		}
	}
}
