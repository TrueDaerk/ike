package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// sbEditor loads a real file with n numbered lines into a pane sized w×h.
func sbEditor(t *testing.T, n, w, h int) Model {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	path := filepath.Join(t.TempDir(), "sb.txt")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.Configure(host.MapConfig{"editor.line_numbers": "false"})
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetSize(w, h)
	return m
}

func diag(line, severity int) ilsp.Diagnostic {
	return ilsp.Diagnostic{
		Range:    buffer.Range{Start: buffer.Position{Line: line}, End: buffer.Position{Line: line, Col: 1}},
		Severity: severity,
		Message:  "m",
	}
}

// TestScrollbarGeometry checks visibility and proportional thumb placement.
func TestScrollbarGeometry(t *testing.T) {
	// Buffer fits: no scrollbar.
	m := sbEditor(t, 5, 20, 10)
	if _, _, _, _, ok := m.scrollbarGeometry(); ok {
		t.Fatal("scrollbar visible although the buffer fits the viewport")
	}
	// 40 lines in a 10-row viewport: thumb covers a quarter of the track.
	m = sbEditor(t, 40, 20, 10)
	track, total, start, length, ok := m.scrollbarGeometry()
	if !ok || track != 10 || total != 40 {
		t.Fatalf("geometry = track %d total %d ok %v, want 10/40/true", track, total, ok)
	}
	if start != 0 || length != 10*10/40 {
		t.Fatalf("thumb at top = %d/%d, want 0/%d", start, length, 10*10/40)
	}
	// Scrolled to the bottom the thumb hugs the track end.
	m.SetScroll(30, 0)
	_, _, start, length, _ = m.scrollbarGeometry()
	if start+length != 10 {
		t.Fatalf("thumb at bottom = %d+%d, want to end at 10", start, length)
	}
}

// TestScrollThumbClamps covers the shared thumb math edge cases.
func TestScrollThumbClamps(t *testing.T) {
	if s, l := scrollThumb(10, 5, 10, 0); s != 0 || l != 10 {
		t.Fatalf("no overflow: thumb = %d/%d, want full track 0/10", s, l)
	}
	if _, l := scrollThumb(10, 1000, 10, 0); l != 1 {
		t.Fatalf("huge buffer: thumb length = %d, want minimum 1", l)
	}
	if s, l := scrollThumb(10, 1000, 10, 990); s+l != 10 {
		t.Fatalf("max offset: thumb = %d+%d, want flush with track end", s, l)
	}
}

// TestScrollbarHit verifies only the rightmost column within the track hits,
// and never while the buffer fits.
func TestScrollbarHit(t *testing.T) {
	m := sbEditor(t, 40, 20, 10)
	if !m.ScrollbarHit(19, 0) || !m.ScrollbarHit(19, 9) {
		t.Fatal("press on the scrollbar column did not hit")
	}
	if m.ScrollbarHit(18, 0) || m.ScrollbarHit(19, 10) || m.ScrollbarHit(19, -1) {
		t.Fatal("press off the scrollbar column hit")
	}
	small := sbEditor(t, 5, 20, 10)
	if small.ScrollbarHit(19, 0) {
		t.Fatal("hit reported although no scrollbar renders")
	}
}

// TestScrollbarTrackClickJumps maps a track press to a proportional offset.
func TestScrollbarTrackClickJumps(t *testing.T) {
	m := sbEditor(t, 100, 20, 10) // total 100, maxOff 90, thumb at 0
	if drag := m.ScrollbarPress(9); drag {
		t.Fatal("track press reported a thumb drag")
	}
	if want := 9 * 90 / 9; m.view.Top != want {
		t.Fatalf("Top after bottom track click = %d, want %d", m.view.Top, want)
	}
	if m.ScrollbarPress(4) {
		t.Fatal("track press reported a thumb drag")
	}
	if want := 4 * 90 / 9; m.view.Top != want {
		t.Fatalf("Top after mid track click = %d, want %d", m.view.Top, want)
	}
}

// TestScrollbarThumbDrag drags the thumb and expects the viewport to follow,
// keeping the grab point and clamping at both ends.
func TestScrollbarThumbDrag(t *testing.T) {
	m := sbEditor(t, 100, 20, 10) // track 10, thumb length 10*10/100 = 1
	_, _, start, length, _ := m.scrollbarGeometry()
	if start != 0 {
		t.Fatalf("initial thumb start = %d, want 0", start)
	}
	if !m.ScrollbarPress(start) {
		t.Fatal("press on the thumb did not start a drag")
	}
	maxOff := 100 - 10
	den := 10 - length
	m.ScrollbarDrag(5)
	if want := 5 * maxOff / den; m.view.Top != want {
		t.Fatalf("Top after drag to 5 = %d, want %d", m.view.Top, want)
	}
	m.ScrollbarDrag(50) // way past the end: clamps to max offset
	if m.view.Top != maxOff {
		t.Fatalf("Top after overshoot = %d, want %d", m.view.Top, maxOff)
	}
	m.ScrollbarDrag(-5)
	if m.view.Top != 0 {
		t.Fatalf("Top after undershoot = %d, want 0", m.view.Top)
	}
	// Grab offset: pressing mid-thumb keeps the grab row inside the thumb.
	m2 := sbEditor(t, 30, 20, 15) // total 30, track 15, thumb length 7
	_, _, _, l2, _ := m2.scrollbarGeometry()
	if !m2.ScrollbarPress(3) { // 3 rows into the thumb
		t.Fatal("press inside the thumb did not start a drag")
	}
	m2.ScrollbarDrag(7) // pointer at 7, grab 3 -> thumb start 4
	if want := 4 * (30 - 15) / (15 - l2); m2.view.Top != want {
		t.Fatalf("Top after grab-offset drag = %d, want %d", m2.view.Top, want)
	}
}

// TestScrollbarStripePlacement puts diagnostics on proportional track rows
// with the worst severity winning a shared cell.
func TestScrollbarStripePlacement(t *testing.T) {
	m := sbEditor(t, 100, 20, 10) // total 101
	m.setDiagnostics([]ilsp.Diagnostic{
		diag(0, 2),   // row 0
		diag(0, 1),   // same row: error outranks the warning
		diag(50, 3),  // 50*10/100 = 5
		diag(99, 4),  // last line: row 9
		diag(500, 1), // out of range: dropped
	})
	stripe := m.scrollbarStripe(10, 100)
	want := map[int]int{0: 1, 5: 3, 9: 4}
	if len(stripe) != len(want) {
		t.Fatalf("stripe = %v, want %v", stripe, want)
	}
	for y, sev := range want {
		if stripe[y] != sev {
			t.Fatalf("stripe[%d] = %d, want %d (full: %v)", y, stripe[y], sev, stripe)
		}
	}
}

// TestScrollbarRenders checks the overlaid View: every row ends in a bar cell,
// thumb rows carry the heavy glyph, diagnostic rows the marker.
func TestScrollbarRenders(t *testing.T) {
	m := sbEditor(t, 40, 20, 10)
	m.SetFocused(true)
	m.setDiagnostics([]ilsp.Diagnostic{diag(20, 1)}) // 20*10/40 = row 5
	rows := strings.Split(m.View(), "\n")
	if len(rows) != 10 {
		t.Fatalf("view rows = %d, want 10", len(rows))
	}
	_, _, start, length, ok := m.scrollbarGeometry()
	if !ok {
		t.Fatal("no scrollbar geometry")
	}
	for y, row := range rows {
		plain := ansi.Strip(row)
		if w := ansi.StringWidth(row); w != 20 {
			t.Fatalf("row %d width = %d, want 20 (%q)", y, w, plain)
		}
		last := []rune(plain)[len([]rune(plain))-1]
		var want rune
		switch {
		case y == 5:
			want = '■'
		case y >= start && y < start+length:
			want = '┃'
		default:
			want = '│'
		}
		if last != want {
			t.Fatalf("row %d ends in %q, want %q", y, last, want)
		}
	}
	// A fitting buffer renders no bar column.
	small := sbEditor(t, 3, 20, 10)
	for _, row := range strings.Split(small.View(), "\n") {
		if strings.ContainsAny(ansi.Strip(row), "│┃■") {
			t.Fatalf("bar glyph in a non-overflowing view: %q", row)
		}
	}
}
