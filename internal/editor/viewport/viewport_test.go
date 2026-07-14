package viewport

import "testing"

func TestScrollKeepsCursorVisible(t *testing.T) {
	v := &Viewport{}
	v.SetSize(80, 10)
	v.Scroll(20, 0, 100)
	if v.Top != 11 { // cursor 20, height 10 -> top = 20-10+1
		t.Fatalf("Top=%d want 11", v.Top)
	}
	v.Scroll(5, 0, 100)
	if v.Top != 5 {
		t.Fatalf("scroll up Top=%d want 5", v.Top)
	}
}

func TestScrollOffMargin(t *testing.T) {
	v := &Viewport{ScrollOff: 3}
	v.SetSize(80, 10)
	v.Scroll(0, 0, 100)
	// cursor at 0 keeps top at 0.
	if v.Top != 0 {
		t.Fatalf("Top=%d want 0", v.Top)
	}
	v.Scroll(8, 0, 100) // needs 3 lines below visible -> top moves
	if v.Top != 2 {
		t.Fatalf("scrolloff Top=%d want 2", v.Top)
	}
}

func TestHorizontalScroll(t *testing.T) {
	v := &Viewport{}
	v.SetSize(20, 10) // no gutter -> text width 20
	v.Scroll(0, 30, 5)
	if v.Left != 11 { // 30 - 20 + 1
		t.Fatalf("Left=%d want 11", v.Left)
	}
}

func TestGutterWidthAndAbsolute(t *testing.T) {
	v := &Viewport{LineNumbers: true}
	if w := v.GutterWidth(5); w != 5 { // sign + min 3 digits + 1
		t.Fatalf("gutter width=%d want 5", w)
	}
	if w := v.GutterWidth(1000); w != 6 { // sign + 4 digits + 1
		t.Fatalf("gutter width=%d want 6", w)
	}
	if g := v.Gutter(0, 3, 10); g != "   1 " {
		t.Fatalf("abs gutter=%q want '   1 '", g)
	}
}

func TestGutterRelative(t *testing.T) {
	v := &Viewport{LineNumbers: true, RelativeNumbers: true}
	// current line shows absolute, left-aligned.
	if g := v.Gutter(3, 3, 10); g != " 4   " {
		t.Fatalf("rel current=%q want ' 4   '", g)
	}
	// other line shows distance, right-aligned.
	if g := v.Gutter(0, 3, 10); g != "   3 " {
		t.Fatalf("rel other=%q want '   3 '", g)
	}
}

func TestGutterDisabled(t *testing.T) {
	v := &Viewport{}
	if g := v.Gutter(0, 0, 10); g != "" {
		t.Fatalf("disabled gutter=%q want empty", g)
	}
}
