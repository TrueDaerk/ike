package app

import (
	"testing"

	"ike/internal/layout"
	"ike/internal/pane"
)

// findSplitWith returns the innermost split whose subtree contains key.
func findSplitWith(n layout.Node, key string) *layout.Split {
	s, ok := n.(*layout.Split)
	if !ok {
		return nil
	}
	for _, child := range []layout.Node{s.A, s.B} {
		if inner := findSplitWith(child, key); inner != nil {
			return inner
		}
		if leaf, ok := child.(*layout.Leaf); ok && leaf.Pane == key {
			return s
		}
	}
	return nil
}

// TestAuxZoneAdaptive guards #1588: a wide, landscape-shaped host pane sends
// auxiliary panes to the right; a narrow one keeps the bottom split; a
// missing key falls back to bottom.
func TestAuxZoneAdaptive(t *testing.T) {
	narrow := sized(t, 100, 40)
	if z := narrow.auxZone("editor"); z != layout.ZoneBottom {
		t.Fatalf("narrow host zone = %v, want ZoneBottom", z)
	}
	if z := narrow.auxZone("no-such-pane"); z != layout.ZoneBottom {
		t.Fatalf("missing key zone = %v, want ZoneBottom", z)
	}
	wide := sized(t, 300, 40)
	if r := wide.lay.Panes["editor"]; r.W <= auxSplitRightMinW || r.W <= r.H {
		t.Fatalf("test setup: editor rect %+v not wide landscape", r)
	}
	if z := wide.auxZone("editor"); z != layout.ZoneRight {
		t.Fatalf("wide host zone = %v, want ZoneRight", z)
	}
}

// TestOpenTerminalAdaptiveSplit pins the end-to-end behavior (#1588): the
// terminal pane opens below on a narrow layout and to the right on a wide
// landscape editor.
func TestOpenTerminalAdaptiveSplit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		width  int
		orient layout.Orient
	}{
		{"narrow-bottom", 100, layout.Vertical},
		{"wide-right", 300, layout.Horizontal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sized(t, tc.width, 40)
			m.openTerminal()
			key := m.activeWS().Panes.Focused()
			inst := m.activeWS().Panes.Get(key)
			if inst == nil || inst.Kind() != pane.KindTerminal {
				t.Fatalf("openTerminal should focus a terminal pane, got %q", key)
			}
			t.Cleanup(inst.Terminal().Close)
			s := findSplitWith(m.activeWS().Tree, key)
			if s == nil {
				t.Fatal("terminal leaf not found in the tree")
			}
			if s.Orient != tc.orient {
				t.Fatalf("terminal split orient = %v, want %v", s.Orient, tc.orient)
			}
		})
	}
}
