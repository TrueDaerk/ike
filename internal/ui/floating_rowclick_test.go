package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// rowClickContent is a stubContent that also implements RowClickable, so the
// shell's hit-test seam (#2275) can be driven end to end.
type rowClickContent struct {
	stubContent
	clicked []int
}

func (c *rowClickContent) ClickRow(row int) tea.Cmd {
	c.clicked = append(c.clicked, row)
	return func() tea.Msg { return row }
}

// openShell returns an open 80×24 shell over body.
func openShell(body string) *Floating {
	f := New(Config{})
	f.SetContent(&stubContent{heading: "T", body: body})
	f.SetSize(80, 24)
	f.Open()
	return f
}

// TestFloatingBodyPointMapsChromeOrigin: the shell subtracts its own border,
// padding and heading rows, so the first body line is row 0.
func TestFloatingBodyPointMapsChromeOrigin(t *testing.T) {
	f := openShell("a\nb\nc")
	ox, oy := f.ContentOrigin()
	x, y, ok := f.BodyPoint(ox+3, oy+2)
	if !ok {
		t.Fatal("a press on the third body line must map")
	}
	if x != 3 || y != 2 {
		t.Fatalf("BodyPoint = (%d, %d), want (3, 2)", x, y)
	}
	if row, ok := f.BodyRow(ox, oy); !ok || row != 0 {
		t.Fatalf("BodyRow at the origin = (%d, %v), want (0, true)", row, ok)
	}
}

// TestFloatingBodyPointRejectsChrome: the border, the padding and the heading
// rows above the body carry no row.
func TestFloatingBodyPointRejectsChrome(t *testing.T) {
	f := openShell("a\nb\nc")
	ox, oy := f.ContentOrigin()
	if _, _, ok := f.BodyPoint(ox-1, oy); ok {
		t.Fatal("a press left of the content must not map")
	}
	if _, _, ok := f.BodyPoint(ox, oy-1); ok {
		t.Fatal("a press on the heading rows must not map")
	}
}

// TestFloatingBodyPointAddsScrollOffset: the row the pointer sits on is the
// scrolled-to content row, not the on-screen line.
func TestFloatingBodyPointAddsScrollOffset(t *testing.T) {
	f := openShell(strings.TrimRight(strings.Repeat("line\n", 200), "\n"))
	f.Wheel(7)
	ox, oy := f.ContentOrigin()
	row, ok := f.BodyRow(ox, oy+2)
	if !ok {
		t.Fatal("a press inside the body must map")
	}
	if row != 9 {
		t.Fatalf("row = %d, want 9 (2 visible + 7 scrolled off)", row)
	}
}

// TestFloatingBodyPointClosedShell: a stray press on a closed shell is inert.
func TestFloatingBodyPointClosedShell(t *testing.T) {
	f := New(Config{})
	f.SetContent(&stubContent{heading: "T", body: "a\nb"})
	f.SetSize(80, 24)
	ox, oy := f.ContentOrigin()
	if _, _, ok := f.BodyPoint(ox, oy); ok {
		t.Fatal("a closed shell must map no press")
	}
}

// TestFloatingClickRowRoutesToContent: RowClickable content sees the body row
// and its command comes back to the host.
func TestFloatingClickRowRoutesToContent(t *testing.T) {
	c := &rowClickContent{stubContent: stubContent{heading: "T", body: "a\nb\nc\nd"}}
	f := New(Config{})
	f.SetContent(c)
	f.SetSize(80, 24)
	f.Open()
	ox, oy := f.ContentOrigin()
	cmd, handled := f.ClickRow(ox+1, oy+2)
	if !handled {
		t.Fatal("a press on the body must reach RowClickable content")
	}
	if len(c.clicked) != 1 || c.clicked[0] != 2 {
		t.Fatalf("ClickRow saw %v, want [2]", c.clicked)
	}
	if cmd == nil || cmd() != 2 {
		t.Fatal("the content's command must come back to the host")
	}
}

// TestFloatingClickRowUnhandled: content without the seam, and presses on the
// chrome, report unhandled so the host can route them itself.
func TestFloatingClickRowUnhandled(t *testing.T) {
	f := openShell("a\nb")
	ox, oy := f.ContentOrigin()
	if _, handled := f.ClickRow(ox, oy); handled {
		t.Fatal("plain Content must not claim the press")
	}
	c := &rowClickContent{stubContent: stubContent{heading: "T", body: "a\nb"}}
	f.SetContent(c)
	if _, handled := f.ClickRow(ox, oy-1); handled {
		t.Fatal("a press on the heading must not reach the content")
	}
	if len(c.clicked) != 0 {
		t.Fatalf("ClickRow saw %v, want none", c.clicked)
	}
}
