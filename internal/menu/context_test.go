package menu

import "testing"

func testContextItems() []Item {
	return []Item{
		{Title: "Save", Command: "editor.write"},
		{Title: "Future", Command: "blocked.future"},
		{Title: "Close", Command: "editor.closeTab"},
	}
}

func openedContext() *Context {
	c := NewContext(testInfo)
	c.Open(testContextItems(), 10, 5, 100, 40)
	return c
}

func TestContextOpenAnchorsAndSelectsFirstRunnable(t *testing.T) {
	c := openedContext()
	if !c.IsOpen() {
		t.Fatal("Open must open the menu")
	}
	if x, y := c.Pos(); x != 10 || y != 5 {
		t.Fatalf("pos=(%d,%d) want (10,5)", x, y)
	}
	if c.sel != 0 {
		t.Fatalf("sel=%d want 0 (first runnable)", c.sel)
	}
}

func TestContextOpenClampsToTerminal(t *testing.T) {
	c := NewContext(testInfo)
	c.Open(testContextItems(), 99, 39, 100, 40)
	x, y := c.Pos()
	w := listWidth(testContextItems(), testInfo) + 2
	h := len(testContextItems()) + 2
	if x != 100-w || y != 40-h {
		t.Fatalf("pos=(%d,%d) want clamped (%d,%d)", x, y, 100-w, 40-h)
	}
	// A terminal smaller than the box clamps to 0, not negative.
	c.Open(testContextItems(), 0, 0, 5, 3)
	if x, y := c.Pos(); x != 0 || y != 0 {
		t.Fatalf("small-terminal pos=(%d,%d) want (0,0)", x, y)
	}
}

func TestContextOpenEmptyIsNoop(t *testing.T) {
	c := NewContext(testInfo)
	c.Open(nil, 0, 0, 100, 40)
	if c.IsOpen() {
		t.Fatal("empty item list must not open")
	}
}

func TestContextItemAtHitTestsEntries(t *testing.T) {
	c := openedContext()
	if idx, ok := c.ItemAt(11, 6); !ok || idx != 0 {
		t.Fatalf("ItemAt(11,6)=%d,%v want 0,true", idx, ok)
	}
	if idx, ok := c.ItemAt(11, 8); !ok || idx != 2 {
		t.Fatalf("ItemAt(11,8)=%d,%v want 2,true", idx, ok)
	}
	// Border row and cells outside the box miss.
	if _, ok := c.ItemAt(11, 5); ok {
		t.Fatal("top border row must not hit an entry")
	}
	if _, ok := c.ItemAt(9, 6); ok {
		t.Fatal("cell left of the box must miss")
	}
}

func TestContextHoverSkipsDisabled(t *testing.T) {
	c := openedContext()
	c.Hover(2)
	if c.sel != 2 {
		t.Fatalf("sel=%d want 2", c.sel)
	}
	c.Hover(1) // blocked.future is disabled
	if c.sel != 2 {
		t.Fatalf("hover on a disabled entry moved sel to %d", c.sel)
	}
}

func TestContextInvokeDispatchesRunMsg(t *testing.T) {
	c := openedContext()
	cmd := c.Invoke(0)
	if cmd == nil {
		t.Fatal("Invoke on a runnable entry must return a command")
	}
	if msg, ok := cmd().(RunMsg); !ok || msg.Command != "editor.write" {
		t.Fatalf("msg=%#v want RunMsg{editor.write}", cmd())
	}
	if c.IsOpen() {
		t.Fatal("invoking must close the menu")
	}
}

func TestContextInvokeDisabledIsNoop(t *testing.T) {
	c := openedContext()
	if cmd := c.Invoke(1); cmd != nil {
		t.Fatal("Invoke on a disabled entry must be a no-op")
	}
	if !c.IsOpen() {
		t.Fatal("a failed invoke must keep the menu open")
	}
}

func TestContextKeysNavigateInvokeDismiss(t *testing.T) {
	c := openedContext()
	c.Update(key("down")) // skips the disabled entry
	if c.sel != 2 {
		t.Fatalf("sel=%d want 2", c.sel)
	}
	cmd := c.Update(key("enter"))
	if cmd == nil {
		t.Fatal("enter must invoke the selection")
	}
	if msg := cmd().(RunMsg); msg.Command != "editor.closeTab" {
		t.Fatalf("msg=%v want editor.closeTab", msg)
	}

	c = openedContext()
	c.Update(key("esc"))
	if c.IsOpen() {
		t.Fatal("esc must dismiss the menu")
	}
}

func TestContextIsOpenNilSafe(t *testing.T) {
	var c *Context
	if c.IsOpen() {
		t.Fatal("nil Context must report closed")
	}
}
