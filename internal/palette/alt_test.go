package palette

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// alt_test.go covers the alternate activation (#2136): alt+enter emits the
// focused row's Alt msg and closes the palette; rows without one fall back to
// the primary activation.

type altMsg struct{ id string }
type primaryMsg struct{ id string }

func altPalette(items []Item) *Palette {
	cmd := NewCommandMode(fakeSource{}, nil, false)
	p := New(Config{DefaultPrefix: ':'}, cmd, stubMode{prefix: '&', items: items})
	p.SetSize(100, 40)
	p.OpenLocked(Context{}, '&')
	return p
}

func TestAltEnterEmitsAltMsg(t *testing.T) {
	p := altPalette([]Item{{Title: "one", Msg: primaryMsg{"one"}, Alt: altMsg{"one"}}})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("alt+enter should activate")
	}
	got, ok := cmd().(altMsg)
	if !ok || got.id != "one" {
		t.Fatalf("expected the Alt msg, got %#v", cmd())
	}
	if p.IsOpen() {
		t.Error("alt activation should close the palette")
	}
}

func TestAltEnterFallsBackToPrimary(t *testing.T) {
	p := altPalette([]Item{{Title: "one", Msg: primaryMsg{"one"}}})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if cmd == nil {
		t.Fatal("alt+enter should fall back to the primary activation")
	}
	if got, ok := cmd().(primaryMsg); !ok || got.id != "one" {
		t.Fatalf("expected the primary msg, got %#v", cmd())
	}
}

func TestPlainEnterStillPrimary(t *testing.T) {
	p := altPalette([]Item{{Title: "one", Msg: primaryMsg{"one"}, Alt: altMsg{"one"}}})
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should activate")
	}
	if _, ok := cmd().(primaryMsg); !ok {
		t.Fatalf("plain enter must keep the primary activation, got %#v", cmd())
	}
}
