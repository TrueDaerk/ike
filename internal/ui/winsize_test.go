package ui

import (
	"os"
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

// TestResizeDeltaDeliveredChords guards #774: macOS terminals never see
// ctrl+shift+arrows (Mission Control owns them), so the cmd (super/meta) and
// alt spellings must resize too.
func TestResizeDeltaDeliveredChords(t *testing.T) {
	for _, key := range []string{"shift+super+left", "shift+meta+left", "alt+shift+left"} {
		ddw, ddh, ok := ResizeDelta(key)
		if !ok || ddw != -4 || ddh != 0 {
			t.Errorf("%s = (%d,%d,%v), want (-4,0,true)", key, ddw, ddh, ok)
		}
	}
	if ddw, ddh, ok := ResizeDelta("shift+super+down"); !ok || ddw != 0 || ddh != 1 {
		t.Errorf("shift+super+down = (%d,%d,%v)", ddw, ddh, ok)
	}
	if _, _, ok := ResizeDelta("super+left"); ok {
		t.Error("bare super+left must not resize (line-nav chords stay free)")
	}
}

// TestResizeZone covers the border-ring hit-test for mouse resizes (#933):
// edges set one axis, corners both, anything one cell inside is content.
func TestResizeZone(t *testing.T) {
	const w, h = 20, 10
	cases := []struct {
		name   string
		x, y   int
		sx, sy int
		ok     bool
	}{
		{"left edge", 0, 5, -1, 0, true},
		{"right edge", w - 1, 5, 1, 0, true},
		{"top edge", 10, 0, 0, -1, true},
		{"bottom edge", 10, h - 1, 0, 1, true},
		{"top-left corner", 0, 0, -1, -1, true},
		{"top-right corner", w - 1, 0, 1, -1, true},
		{"bottom-left corner", 0, h - 1, -1, 1, true},
		{"bottom-right corner", w - 1, h - 1, 1, 1, true},
		{"just inside left", 1, 5, 0, 0, false},
		{"just inside bottom", 10, h - 2, 0, 0, false},
		{"interior", 10, 5, 0, 0, false},
		{"outside", w, 5, 0, 0, false},
		{"negative", -1, 5, 0, 0, false},
	}
	for _, c := range cases {
		sx, sy, ok := ResizeZone(c.x, c.y, w, h)
		if sx != c.sx || sy != c.sy || ok != c.ok {
			t.Errorf("%s: ResizeZone(%d,%d) = (%d,%d,%v), want (%d,%d,%v)",
				c.name, c.x, c.y, sx, sy, ok, c.sx, c.sy, c.ok)
		}
	}
	// Degenerate boxes have no resize ring.
	if _, _, ok := ResizeZone(0, 0, 2, 2); ok {
		t.Error("a 2x2 box must not offer a resize ring")
	}
}

// TestNudgeFlush (#933): Nudge accumulates without touching disk; Flush
// persists the accumulated deltas.
func TestNudgeFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "winsize.json")
	s := LoadWinSizes(path)
	s.Nudge("k", 3, 1)
	s.Nudge("k", 2, 0)
	if _, err := os.Stat(path); err == nil {
		t.Fatal("Nudge must not persist")
	}
	if dw, dh := s.Get("k"); dw != 5 || dh != 1 {
		t.Fatalf("Get = (%d,%d), want (5,1)", dw, dh)
	}
	s.Flush()
	if dw, dh := LoadWinSizes(path).Get("k"); dw != 5 || dh != 1 {
		t.Fatalf("reload after Flush = (%d,%d), want (5,1)", dw, dh)
	}
}

// TestFloatingMaxWidthCap (#932): the shell's outer width stops growing at
// the configured cap on a large terminal; 0 disables the cap.
func TestFloatingMaxWidthCap(t *testing.T) {
	f := New(Config{})
	f.SetContent(wideContent{})
	f.SetMaxWidth(60)
	f.SetSize(200, 30)
	f.Open()
	if w := lipgloss.Width(f.View()); w > 60 {
		t.Fatalf("capped shell width = %d, want <= 60", w)
	}
	f.SetMaxWidth(0)
	if w := lipgloss.Width(f.View()); w <= 60 {
		t.Fatalf("uncapped shell width = %d, want the terminal-bound width", w)
	}
}
