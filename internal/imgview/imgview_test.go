package imgview

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi/kitty"

	"ike/internal/theme"
)

func writePNG(t *testing.T, w, h int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "img.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, testImage(w, h)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMetadataFallback(t *testing.T) {
	m := New("image", writePNG(t, 64, 32), theme.DefaultPalette())
	m.SetSize(60, 10)
	v := m.View()
	for _, want := range []string{"img.png", "PNG", "64×32 px", "no Kitty graphics support"} {
		if !strings.Contains(v, want) {
			t.Errorf("fallback lacks %q:\n%s", want, v)
		}
	}
	if strings.ContainsRune(v, kitty.Placeholder) {
		t.Error("fallback must not draw placeholder cells")
	}
}

func TestGraphicsViewDrawsPlaceholders(t *testing.T) {
	m := New("image", writePNG(t, 64, 32), theme.DefaultPalette())
	m.SetSize(60, 10)
	m.SetGraphics(true)
	v := m.View()
	cols, rows := m.Grid()
	if got := strings.Count(v, string(kitty.Placeholder)); got != cols*rows {
		t.Errorf("placeholder cells: got %d, want %d (%d×%d)", got, cols*rows, cols, rows)
	}
	if lines := strings.Count(v, "\n") + 1; lines != 10 {
		t.Errorf("view must fill the pane height: %d lines", lines)
	}
}

func TestSyncSeqsLifecycle(t *testing.T) {
	m := New("image", writePNG(t, 64, 32), theme.DefaultPalette())
	m.SetSize(60, 10)
	first := m.SyncSeqs()
	if len(first) != 1 || !strings.Contains(first[0], "a=T") {
		t.Fatalf("first sync must transmit once, got %d seqs", len(first))
	}
	if again := m.SyncSeqs(); again != nil {
		t.Fatalf("unchanged geometry must be idempotent, got %d seqs", len(again))
	}
	// Resize: delete the old placement, transmit the new grid.
	m.SetSize(30, 5)
	resized := m.SyncSeqs()
	if len(resized) != 2 || !strings.Contains(resized[0], "a=d") || !strings.Contains(resized[1], "a=T") {
		t.Fatalf("resize must delete + retransmit, got %v seq kinds", len(resized))
	}
}

func TestDecodeErrorKeepsPaneAlive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.png")
	os.WriteFile(path, []byte("not a png"), 0o644)
	m := New("image", path, theme.DefaultPalette())
	m.SetSize(60, 10)
	m.SetGraphics(true)
	if v := m.View(); !strings.Contains(v, "cannot decode") {
		t.Errorf("decode error must surface in the view:\n%s", v)
	}
	if seqs := m.SyncSeqs(); seqs != nil {
		t.Error("an undecoded image must not transmit")
	}
}
