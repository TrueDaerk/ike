package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// newTestShell builds an open shell showing body under heading, sized to a
// standard test terminal.
func newTestShell(heading, body string) *Floating {
	f := New(Config{})
	f.SetContent(&stubContent{heading: heading, body: body})
	f.SetSize(80, 24)
	f.Open()
	return f
}

// blankCanvas is an 80x24 canvas of spaces for compositing assertions.
func blankCanvas() string {
	row := strings.Repeat(" ", 80)
	rows := make([]string, 24)
	for i := range rows {
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

func keyMsg(s string) tea.KeyMsg {
	if s == "esc" {
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func TestStackOfOneBehavesLikeFloating(t *testing.T) {
	base := newTestShell("BASE", "base body")
	s := NewStack(base)

	if !s.IsOpen() || s.Top() != base || s.Depth() != 1 {
		t.Fatal("stack of one open base layer should report it as top")
	}
	out := ansi.Strip(s.Composite(blankCanvas(), 80, 24))
	if !strings.Contains(out, "base body") {
		t.Fatalf("composite missing base body: %q", out)
	}
	// esc dismisses, exactly like a bare shell; the base layer stays in the
	// stack for reuse.
	if !s.Update(keyMsg("esc")) {
		t.Fatal("esc should be consumed")
	}
	if base.IsOpen() || s.IsOpen() {
		t.Fatal("esc should close the base layer")
	}
	base.Open()
	if s.Top() != base {
		t.Fatal("reopened base layer should be back on top")
	}
}

func TestStackCompositesTopmostLast(t *testing.T) {
	// The lower body is much wider than the top box, so its edges must stay
	// visible around the overdrawn middle.
	wide := strings.Repeat("<", 30) + strings.Repeat(">", 30)
	base := newTestShell("BASE", wide+"\n"+wide+"\n"+wide+"\n"+wide+"\n"+wide)
	top := newTestShell("TOP", "UP")
	s := NewStack(base)
	s.layers = append(s.layers, stackLayer{f: top, transient: true})

	out := ansi.Strip(s.Composite(blankCanvas(), 80, 24))
	if !strings.Contains(out, "UP") {
		t.Fatalf("topmost body not drawn: %q", out)
	}
	if !strings.Contains(out, "<<<<<") {
		t.Fatalf("lower layer fully hidden: %q", out)
	}
	// Topmost drawn last: the small top box splices into the middle of a wide
	// lower row — that row shows the lower body's left edge, then the top box.
	spliced := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "<") && strings.Contains(line, "TOP") {
			spliced = true
			if strings.Contains(line, wide) {
				t.Fatalf("top box did not overdraw the lower row: %q", line)
			}
		}
	}
	if !spliced {
		t.Fatalf("top box not layered over the lower body: %q", out)
	}
}

func TestStackRoutesInputToTopAndPopsOneLayerAtATime(t *testing.T) {
	base := newTestShell("BASE", "base body")
	top := newTestShell("TOP", "top body")
	s := NewStack(base)
	s.Push(top)

	if s.Top() != top || s.Depth() != 2 {
		t.Fatal("pushed layer should be topmost")
	}
	// A scroll key goes to the top layer only and is swallowed.
	if !s.Update(keyMsg("j")) {
		t.Fatal("key should be consumed by the top layer")
	}
	if !base.IsOpen() || !top.IsOpen() {
		t.Fatal("non-dismiss key must not close any layer")
	}
	// First esc closes only the topmost; the transient layer leaves the stack.
	s.Update(keyMsg("esc"))
	if top.IsOpen() {
		t.Fatal("esc should close the top layer")
	}
	if !base.IsOpen() || s.Top() != base {
		t.Fatal("base layer should survive and become top again")
	}
	if len(s.layers) != 1 {
		t.Fatalf("closed transient layer should leave the stack, have %d layers", len(s.layers))
	}
	// Second esc closes the base.
	s.Update(keyMsg("esc"))
	if base.IsOpen() || s.IsOpen() {
		t.Fatal("second esc should close the base layer")
	}
}

func TestStackPushThreadsSharedState(t *testing.T) {
	s := NewStack()
	s.SetSize(100, 40)
	s.SetMaxWidth(60)
	s.SetSizeStore(&WinSizes{})

	f := New(Config{})
	f.SetContent(&stubContent{heading: "T", body: "b"})
	s.Push(f)

	if !f.IsOpen() {
		t.Fatal("Push should open the layer")
	}
	if f.width != 100 || f.height != 40 {
		t.Fatalf("Push should size the layer to the stack size, got %dx%d", f.width, f.height)
	}
	if f.maxWidth != 60 || f.sizes == nil {
		t.Fatal("Push should thread max width and size store into the layer")
	}
	if s.Top() != f {
		t.Fatal("pushed layer should be top")
	}
}

func TestStackPopAndWindowSize(t *testing.T) {
	base := newTestShell("BASE", "b")
	top := newTestShell("TOP", "t")
	s := NewStack(base)
	s.Push(top)

	if !s.Pop() || top.IsOpen() || s.Top() != base {
		t.Fatal("Pop should close only the topmost layer")
	}
	// A window size message resizes every layer and is consumed while open.
	if !s.Update(tea.WindowSizeMsg{Width: 90, Height: 30}) {
		t.Fatal("size message should be consumed while a layer is open")
	}
	if base.width != 90 || base.height != 30 {
		t.Fatalf("size message should reach the base layer, got %dx%d", base.width, base.height)
	}
	if !s.Pop() || s.Pop() {
		t.Fatal("Pop should close the base then report nothing left to pop")
	}
	if s.Update(keyMsg("j")) {
		t.Fatal("keys must not be consumed with no open layer")
	}
}
