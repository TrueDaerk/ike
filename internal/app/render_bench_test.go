package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/registry"
)

// BenchmarkAppRender measures one full-frame composition at 200x60 with a
// file open — the per-keystroke cost (#1095). Guard rail for the wash fix:
// the frame must not re-run lipgloss Wrap/align over the composed screen.
func BenchmarkAppRender(b *testing.B) {
	t := &testing.T{}
	m := sizedWith(t, registry.New(), 200, 60)
	path := filepath.Join(b.TempDir(), "main.go")
	content := "package main\n\n" + strings.Repeat("func f() { /* line */ }\n", 200)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		b.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	out, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: 60})
	m = out.(Model)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.render()
	}
}
