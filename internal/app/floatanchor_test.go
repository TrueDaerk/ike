package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/palette"
	"ike/internal/registry"
	"ike/internal/ui"
)

// floatanchor_test.go covers the edge-anchored mouse resize of the centered
// floats (#2896): only the grabbed edge(s) follow the pointer, the opposite
// edge stays on its screen cell, and the position offset that makes it so
// persists with the size and re-clamps on a terminal resize.

// anchorBody is shell content that fills its width budget and overflows any
// height, so the box tracks every size delta (a content-sized body would
// swallow growth past its natural size).
type anchorBody struct{}

func (anchorBody) Title() string { return "ANCHOR TEST" }
func (anchorBody) Render(w int) string {
	rows := make([]string, 200)
	for i := range rows {
		rows[i] = strings.Repeat("x", w)
	}
	return strings.Join(rows, "\n")
}

// anchorKinds opens each centered float kind in a fresh model.
var anchorKinds = []struct {
	kind string
	open func(t *testing.T) Model
}{
	{"settings", func(t *testing.T) Model {
		return step(dismissOnboarding(sized(t, 120, 40)), OpenSettingsMsg{})
	}},
	{"palette", func(t *testing.T) Model {
		// The real commands fill the result list, so the box tracks height
		// deltas instead of shrinking to a handful of rows.
		m := dismissOnboarding(sizedWith(t, registry.Global(), 120, 40))
		m.palette.Open(palette.Context{ContextID: "editor", Root: "."})
		return m
	}},
	{"shell", openAnchorShell},
	{"popupterm", func(t *testing.T) Model {
		return openTestPopupWith(t, dismissOnboarding(sized(t, 100, 40)))
	}},
}

// openAnchorShell opens the shell on anchorBody, pre-shrunk so it has room to
// grow in every direction (its default already fills the height budget).
func openAnchorShell(t *testing.T) Model {
	m := dismissOnboarding(sized(t, 120, 40))
	m.winSizes.Put("ANCHOR TEST", -20, -10)
	m.shell.SetContent(anchorBody{})
	m.shell.Open()
	return m
}

// grabCell is the border cell that grabs the edge/corner (sx, sy) of a box.
func grabCell(x, y, w, h, sx, sy int) (int, int) {
	gx, gy := x+w/2, y+h/2
	switch sx {
	case -1:
		gx = x
	case 1:
		gx = x + w - 1
	}
	switch sy {
	case -1:
		gy = y
	case 1:
		gy = y + h - 1
	}
	return gx, gy
}

// anchorRect is floatRect for a test, failing when the kind is not open.
func anchorRect(t *testing.T, m Model, kind string) (x, y, w, h int) {
	t.Helper()
	x, y, w, h, ok := m.floatRect(kind)
	if !ok {
		t.Fatalf("%s: float must be open", kind)
	}
	return x, y, w, h
}

// dragFloat grabs the (sx, sy) edge/corner of the kind's box, moves the
// pointer by (dx, dy) in two motion steps and releases.
func dragFloat(t *testing.T, m Model, kind string, sx, sy, dx, dy int) Model {
	t.Helper()
	x, y, w, h := anchorRect(t, m, kind)
	gx, gy := grabCell(x, y, w, h, sx, sy)
	m = step(m, press(gx, gy))
	if d := m.floatDrag; d == nil || d.kind != kind || d.sx != sx || d.sy != sy {
		t.Fatalf("%s: press at (%d,%d) must grab edge (%d,%d), got %+v", kind, gx, gy, sx, sy, m.floatDrag)
	}
	m = step(m, motion(gx+dx/2, gy+dy/2))
	m = step(m, motion(gx+dx, gy+dy))
	m = step(m, release(gx+dx, gy+dy))
	if m.floatDrag != nil {
		t.Fatalf("%s: release must end the drag", kind)
	}
	return m
}

func TestFloatResizeDragAnchorsOppositeEdge(t *testing.T) {
	cases := []struct {
		name           string
		sx, sy, dx, dy int
	}{
		{"bottom grows", 0, 1, 0, 3},
		{"bottom shrinks", 0, 1, 0, -2},
		{"right grows", 1, 0, 3, 0},
		{"top grows", 0, -1, 0, -2},
		{"left grows", -1, 0, -3, 0},
		{"left shrinks", -1, 0, 2, 0},
		{"bottom-right corner", 1, 1, 2, 2},
		{"top-left corner", -1, -1, -2, -2},
	}
	for _, k := range anchorKinds {
		for _, c := range cases {
			t.Run(k.kind+"/"+c.name, func(t *testing.T) {
				m := k.open(t)
				x0, y0, w0, h0 := anchorRect(t, m, k.kind)
				m = dragFloat(t, m, k.kind, c.sx, c.sy, c.dx, c.dy)
				x1, y1, w1, h1 := anchorRect(t, m, k.kind)
				if want := w0 + c.dx*c.sx; w1 != want {
					t.Errorf("width = %d, want %d (was %d)", w1, want, w0)
				}
				if want := h0 + c.dy*c.sy; h1 != want {
					t.Errorf("height = %d, want %d (was %d)", h1, want, h0)
				}
				// The edge opposite the grabbed one stays on its cell; an
				// axis not grabbed keeps both edges.
				if c.sx < 0 {
					if x1+w1 != x0+w0 {
						t.Errorf("right edge moved: %d -> %d", x0+w0, x1+w1)
					}
				} else if x1 != x0 {
					t.Errorf("left edge moved: %d -> %d", x0, x1)
				}
				if c.sy < 0 {
					if y1+h1 != y0+h0 {
						t.Errorf("bottom edge moved: %d -> %d", y0+h0, y1+h1)
					}
				} else if y1 != y0 {
					t.Errorf("top edge moved: %d -> %d", y0, y1)
				}
			})
		}
	}
}

// The grabbed edge stops at the screen edge: dragging the top edge far past
// row 0 grows the box only up to the top row, and the bottom stays put.
func TestFloatResizeDragStopsAtScreenEdge(t *testing.T) {
	for _, k := range anchorKinds {
		t.Run(k.kind, func(t *testing.T) {
			m := k.open(t)
			_, y0, _, h0 := anchorRect(t, m, k.kind)
			m = dragFloat(t, m, k.kind, 0, -1, 0, -500)
			_, y1, _, h1 := anchorRect(t, m, k.kind)
			if y1 < 0 || y1+h1 != y0+h0 {
				t.Fatalf("top drag past the screen: rect y=%d h=%d, want y>=0 and bottom %d", y1, h1, y0+h0)
			}
		})
	}
}

// The frame composites the box where the hit-tests resolve it: after a
// top-edge drag the box's top border row is drawn on the new top row.
func TestFloatResizeDragRendersAtAnchoredRect(t *testing.T) {
	m := step(dismissOnboarding(sized(t, 120, 40)), OpenSettingsMsg{})
	m = dragFloat(t, m, "settings", 0, -1, 0, -2)
	x, y, w, _ := m.settingsRect()
	rows := strings.Split(plainView(m), "\n")
	if got := []rune(rows[y]); x+w > len(got) || got[x] != '╭' {
		t.Fatalf("settings top-left corner must be drawn at (%d,%d), row = %q", x, y, rows[y])
	}
}

// Size and offset persist together per window kind: the release writes both
// to the store on disk, and the reopened window comes back where and as large
// as the user left it.
func TestFloatResizeDragPersistsSizeAndOffset(t *testing.T) {
	m := step(dismissOnboarding(sized(t, 120, 40)), OpenSettingsMsg{})
	m = dragFloat(t, m, "settings", -1, -1, -3, -2)
	x, y, w, h := m.settingsRect()
	disk := ui.LoadWinSizes(winSizeFile())
	if dw, dh := disk.Get("settings"); dw != 3 || dh != 2 {
		t.Fatalf("persisted size delta = (%d,%d), want (3,2)", dw, dh)
	}
	ox, oy := m.winSizes.Offset("settings")
	if ox == 0 && oy == 0 {
		t.Fatal("a top-left drag must store a position offset")
	}
	if dx, dy := disk.Offset("settings"); dx != ox || dy != oy {
		t.Fatalf("persisted offset = (%d,%d), want (%d,%d)", dx, dy, ox, oy)
	}
	m.settings.Close()
	m = step(m, OpenSettingsMsg{})
	if x2, y2, w2, h2 := m.settingsRect(); x2 != x || y2 != y || w2 != w || h2 != h {
		t.Fatalf("reopened rect = (%d,%d %dx%d), want (%d,%d %dx%d)", x2, y2, w2, h2, x, y, w, h)
	}
}

// The shell layers persist per content title; the popup box mirrors its
// offset into the user-scoped fallback store like the move drag (#1714).
func TestFloatResizeDragPersistsShellAndPopupOffsets(t *testing.T) {
	m := openAnchorShell(t)
	m = dragFloat(t, m, "shell", 0, 1, 0, 3)
	ox, oy := m.winSizes.Offset("ANCHOR TEST")
	if dx, dy := ui.LoadWinSizes(winSizeFile()).Offset("ANCHOR TEST"); dx != ox || dy != oy || oy == 0 {
		t.Fatalf("shell offset on disk = (%d,%d), in memory (%d,%d); a bottom drag must store a vertical offset", dx, dy, ox, oy)
	}

	p := openTestPopupWith(t, dismissOnboarding(sized(t, 100, 40)))
	p = dragFloat(t, p, "popupterm", 1, 0, 3, 0)
	px, py := p.winSizes.Get(popupTermPosKey)
	if px == 0 {
		t.Fatal("a right-edge drag must store a horizontal popup offset")
	}
	if gx, gy := ui.LoadWinSizes(globalWinSizeFile()).Get(popupTermPosKey); gx != px || gy != py {
		t.Fatalf("global popup offset = (%d,%d), want (%d,%d)", gx, gy, px, py)
	}
}

// A terminal resize re-clamps every stored offset: a box dragged toward the
// bottom-right corner, with an offset far past the screen, stays fully on
// screen — before and after the terminal shrinks.
func TestFloatOffsetReclampsOnTerminalResize(t *testing.T) {
	for _, k := range anchorKinds {
		t.Run(k.kind, func(t *testing.T) {
			m := k.open(t)
			m = dragFloat(t, m, k.kind, -1, -1, 6, 3) // shrink from the top-left
			m.setFloatOffset(k.kind, 500, 500)
			x0, y0, w0, h0 := anchorRect(t, m, k.kind)
			if x0+w0 != m.width || y0+h0 != m.height {
				t.Fatalf("an oversized offset must clamp into the bottom-right corner, rect (%d,%d %dx%d) in %dx%d", x0, y0, w0, h0, m.width, m.height)
			}
			m = step(m, tea.WindowSizeMsg{Width: 70, Height: 24})
			x, y, w, h := anchorRect(t, m, k.kind)
			if x < 0 || y < 0 || x+w > m.width || y+h > m.height {
				t.Fatalf("rect (%d,%d %dx%d) must stay inside %dx%d after a terminal resize", x, y, w, h, m.width, m.height)
			}
		})
	}
}
