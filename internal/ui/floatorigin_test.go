package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// floatorigin_test.go covers the position-offset half of the floating-window
// store (#2896): offsets ride next to the size deltas, resolve through a
// clamped centered origin, and the anchor math keeps the edge opposite a
// dragged one in place.

func TestWinSizesOffsetPersistsBesideSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "winsize.json")
	s := LoadWinSizes(path)
	s.Nudge("settings", 3, 2)
	s.SetOffset("settings", -4, 5)
	if dx, dy := LoadWinSizes(path).Offset("settings"); dx != 0 || dy != 0 {
		t.Fatal("SetOffset must not persist on its own (the drag release flushes)")
	}
	s.Flush()
	re := LoadWinSizes(path)
	if dw, dh := re.Get("settings"); dw != 3 || dh != 2 {
		t.Fatalf("size delta = (%d,%d), want (3,2)", dw, dh)
	}
	if dx, dy := re.Offset("settings"); dx != -4 || dy != 5 {
		t.Fatalf("offset = (%d,%d), want (-4,5)", dx, dy)
	}
	if OffsetKey("popupterm") != "popupterm:pos" {
		t.Fatal("the offset key must match the popup box's #1793 key")
	}
	var nilS *WinSizes
	nilS.SetOffset("x", 1, 1)
	if dx, dy := nilS.Offset("x"); dx != 0 || dy != 0 {
		t.Fatal("nil WinSizes must stay inert")
	}
}

func TestFloatOriginCentersShiftsAndClamps(t *testing.T) {
	cases := []struct {
		name         string
		w, h, ox, oy int
		wantX, wantY int
	}{
		{"centered", 40, 10, 0, 0, 20, 7},
		{"shifted", 40, 10, -5, 3, 15, 10},
		{"clamped past bottom-right", 40, 10, 500, 500, 40, 14},
		{"clamped past top-left", 40, 10, -500, -500, 0, 0},
		{"larger than the terminal stays centered", 90, 30, 7, 7, -5, -3},
	}
	for _, c := range cases {
		x, y := FloatOrigin(80, 24, c.w, c.h, c.ox, c.oy)
		if x != c.wantX || y != c.wantY {
			t.Errorf("%s: origin = (%d,%d), want (%d,%d)", c.name, x, y, c.wantX, c.wantY)
		}
	}
}

// After a resize from extent w to w2, the offset AnchorOffset returns places
// the box so the edge opposite the grabbed one keeps its cell — for both
// parities of the centering division.
func TestAnchorOffsetKeepsOppositeEdge(t *testing.T) {
	for _, tw := range []int{80, 81} {
		for _, w2 := range []int{30, 37, 44, 45} {
			x, w := 25, 40
			// Grabbed right edge: the left edge stays.
			if nx, _ := FloatOrigin(tw, 24, w2, 5, AnchorOffset(tw, x, w, w2, 1), 0); nx != x {
				t.Errorf("tw=%d w2=%d right grab: left edge %d, want %d", tw, w2, nx, x)
			}
			// Grabbed left edge: the right edge stays.
			if nx, _ := FloatOrigin(tw, 24, w2, 5, AnchorOffset(tw, x, w, w2, -1), 0); nx+w2 != x+w {
				t.Errorf("tw=%d w2=%d left grab: right edge %d, want %d", tw, w2, nx+w2, x+w)
			}
		}
	}
}

func TestAnchoredStepKeepsGrabbedEdgeOnScreen(t *testing.T) {
	// Box at x=5, w=20 in an 80-column terminal: right edge after cell 24.
	if d := AnchoredStep(80, 5, 20, -1, -9); d != -5 {
		t.Errorf("left edge may grow to cell 0 only: step %d, want -5", d)
	}
	if d := AnchoredStep(80, 5, 20, -1, 3); d != 3 {
		t.Errorf("left edge shrink passes through: step %d, want 3", d)
	}
	if d := AnchoredStep(80, 5, 20, 1, 99); d != 55 {
		t.Errorf("right edge may grow to the last cell only: step %d, want 55", d)
	}
	// A box wider than the terminal has no room to grow, but a shrink step
	// is never amplified.
	if d := AnchoredStep(80, 0, 90, 1, -2); d != -2 {
		t.Errorf("oversized box shrink: step %d, want -2", d)
	}
	if d := AnchoredStep(80, 0, 90, 1, 4); d != 0 {
		t.Errorf("oversized box grow: step %d, want 0", d)
	}
	if d := AnchoredStep(80, -5, 90, -1, -3); d != 0 {
		t.Errorf("overhanging left edge grow: step %d, want 0", d)
	}
	if d := AnchoredStep(80, -5, 90, -1, 2); d != 2 {
		t.Errorf("overhanging left edge shrink: step %d, want 2", d)
	}
	if d := AnchoredStep(80, 5, 20, 0, 7); d != 0 {
		t.Errorf("an axis not grabbed takes no step, got %d", d)
	}
}

// The stack composites each layer at its Origin, so a stored offset moves the
// drawn box and the hit-tests (which resolve through the same Origin) agree.
func TestStackCompositesAtStoredOffset(t *testing.T) {
	f := newTestShell("MOVED", "body")
	sizes := LoadWinSizes("")
	f.SetSizeStore(sizes)
	s := NewStack(f)
	v := f.View()
	w, h := lipgloss.Width(v), lipgloss.Height(v)
	cx, cy := f.Origin(80, 24, w, h)
	if cx != (80-w)/2 || cy != (24-h)/2 {
		t.Fatalf("no offset: origin (%d,%d), want centered (%d,%d)", cx, cy, (80-w)/2, (24-h)/2)
	}
	f.SetOffset(-cx, -cy) // move to the top-left corner
	if x, y := f.Origin(80, 24, w, h); x != 0 || y != 0 {
		t.Fatalf("offset origin = (%d,%d), want (0,0)", x, y)
	}
	rows := strings.Split(ansi.Strip(s.Composite(blankCanvas(), 80, 24)), "\n")
	if !strings.HasPrefix(rows[0], "╭") {
		t.Fatalf("the box must be drawn at the top-left corner, row 0 = %q", rows[0])
	}
}
