package ui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestWinSizesPersistAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "winsize.json")
	s := LoadWinSizes(path)
	s.Adjust("palette", 4, 0)
	s.Adjust("palette", 4, 1)
	re := LoadWinSizes(path)
	if dw, dh := re.Get("palette"); dw != 8 || dh != 1 {
		t.Fatalf("persisted delta = (%d,%d), want (8,1)", dw, dh)
	}
	if dw, dh := re.Get("other"); dw != 0 || dh != 0 {
		t.Fatalf("unknown kind delta = (%d,%d), want zeros", dw, dh)
	}
	var nilS *WinSizes
	nilS.Adjust("x", 1, 1)
	if dw, dh := nilS.Get("x"); dw != 0 || dh != 0 {
		t.Fatal("nil WinSizes must stay inert")
	}
}

func TestResizeDeltaMapping(t *testing.T) {
	for key, want := range map[string][2]int{
		"ctrl+shift+left":  {-4, 0},
		"ctrl+shift+right": {4, 0},
		"ctrl+shift+up":    {0, -1},
		"ctrl+shift+down":  {0, 1},
	} {
		dw, dh, ok := ResizeDelta(key)
		if !ok || dw != want[0] || dh != want[1] {
			t.Fatalf("ResizeDelta(%q) = (%d,%d,%v)", key, dw, dh, ok)
		}
	}
	if _, _, ok := ResizeDelta("ctrl+left"); ok {
		t.Fatal("ctrl+left is not a resize chord")
	}
}

// wideContent renders a fixed-size body so the shell's clamped budget is
// observable through the rendered width.
type wideContent struct{}

func (wideContent) Title() string { return "RESIZE-TEST" }
func (wideContent) Render(w int) string {
	return strings.Repeat(strings.Repeat("x", w)+"\n", 30)
}

func TestFloatingResizeChordsShrinkAndPersist(t *testing.T) {
	s := LoadWinSizes(filepath.Join(t.TempDir(), "winsize.json"))
	f := New(Config{})
	f.SetSizeStore(s)
	f.SetContent(wideContent{})
	f.SetSize(100, 40)
	f.Open()
	before := lipgloss.Width(f.View())
	for i := 0; i < 3; i++ {
		if !f.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl | tea.ModShift}) {
			t.Fatal("resize chord must be consumed")
		}
	}
	after := lipgloss.Width(f.View())
	if after >= before {
		t.Fatalf("ctrl+shift+left must narrow the shell: before %d after %d", before, after)
	}
	if dw, _ := s.Get("RESIZE-TEST"); dw != -12 {
		t.Fatalf("stored width delta = %d, want -12", dw)
	}
	// A small terminal re-clamps a huge positive delta: the resized shell is
	// never wider than the default (delta-free) shell at the same size.
	plain := New(Config{})
	plain.SetContent(wideContent{})
	plain.SetSize(48, 12)
	plain.Open()
	base := lipgloss.Width(plain.View())
	s.Adjust("RESIZE-TEST", 500, 500)
	f.SetSize(48, 12)
	if v := f.View(); lipgloss.Width(v) > base {
		t.Fatalf("resized shell must re-clamp to the terminal budget: %d > %d", lipgloss.Width(v), base)
	}
}
