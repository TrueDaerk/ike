package project

import (
	"testing"

	"ike/internal/palette"
	"ike/internal/registry"
)

// peek_test.go covers the quick-peek surface (#2136): the three commands, the
// picker's alternate activation, and the peek picker flavour's inversion.

func TestPeekCommandsRegistered(t *testing.T) {
	r := registry.New()
	r.Add(commands{})

	for _, id := range []string{"project.peek", "project.peek.return", "project.peek.keep"} {
		c, ok := r.Command(id)
		if !ok {
			t.Fatalf("%s should be registered", id)
		}
		if c.Owner != "project" || c.Title == "" || c.Run == nil || !c.Scope.Global {
			t.Errorf("%s wrong: %+v", id, c)
		}
	}
	if keys, ok := r.Binding("project.peek.return"); !ok || keys != "cmd+shift+b" {
		t.Errorf("project.peek.return default chord missing, got %q, %v", keys, ok)
	}
	if len(r.Conflicts()) != 0 {
		t.Errorf("registration should be conflict-free: %v", r.Conflicts())
	}
}

// TestPickerRowsCarryPeekAlt (#2136): every history row of the normal picker
// peeks via alt+enter, and the raw open-path fallback does too.
func TestPickerRowsCarryPeekAlt(t *testing.T) {
	m, _ := newPicker(t, fixedHistory)
	items := m.Results("", palette.Context{})
	if len(items) == 0 {
		t.Fatal("no items")
	}
	first := items[0]
	if _, ok := first.Msg.(PickedMsg); !ok {
		t.Fatalf("primary activation should switch, got %#v", first.Msg)
	}
	alt, ok := first.Alt.(PeekPickedMsg)
	if !ok {
		t.Fatalf("alt activation should peek, got %#v", first.Alt)
	}
	if alt.Path != first.Msg.(PickedMsg).Path {
		t.Errorf("alt path %q != primary path %q", alt.Path, first.Msg.(PickedMsg).Path)
	}

	items = m.Results("somewhere", palette.Context{})
	last := items[len(items)-1]
	if _, ok := last.Alt.(PeekPickedMsg); !ok {
		t.Errorf("open-path fallback should carry the peek alt, got %#v", last.Alt)
	}
}

// TestPeekPickerInvertsActivation (#2136): the project.peek flavour peeks on
// plain activation and switches on alt+enter.
func TestPeekPickerInvertsActivation(t *testing.T) {
	base := t.TempDir()
	m := NewPeekPickerMode(fixedHistory)
	m.projectsDir = func() (string, error) { return base, nil }
	if m.Prefix() == PickerPrefix {
		t.Fatal("peek flavour must register its own prefix")
	}
	items := m.Results("", palette.Context{})
	if len(items) == 0 {
		t.Fatal("no items")
	}
	if _, ok := items[0].Msg.(PeekPickedMsg); !ok {
		t.Fatalf("primary activation should peek, got %#v", items[0].Msg)
	}
	if _, ok := items[0].Alt.(PickedMsg); !ok {
		t.Fatalf("alt activation should switch, got %#v", items[0].Alt)
	}
}

// TestPeekToValidates (#2136): PeekTo mirrors SwitchTo — a valid root yields
// PeekProjectMsg, an invalid one the shared SwitchFailedMsg.
func TestPeekToValidates(t *testing.T) {
	dir := t.TempDir()
	msg := PeekTo(dir)()
	pm, ok := msg.(PeekProjectMsg)
	if !ok {
		t.Fatalf("expected PeekProjectMsg, got %#v", msg)
	}
	want, _ := Validate(dir)
	if pm.Root != want {
		t.Errorf("root %q, want %q", pm.Root, want)
	}
	if _, ok := PeekTo(dir + "/missing")().(SwitchFailedMsg); !ok {
		t.Error("invalid path should fail like SwitchTo")
	}
}
