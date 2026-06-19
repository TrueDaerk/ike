package example

import (
	"testing"

	"ike/internal/plugin"
)

// TestExerciseEveryExtensionPoint asserts the reference plugin contributes one
// of every capability, so it stays a complete contract example.
func TestExerciseEveryExtensionPoint(t *testing.T) {
	c := Plugin{}.Capabilities()
	if len(c.Commands) == 0 {
		t.Error("missing Command")
	}
	if len(c.Keymaps) == 0 {
		t.Error("missing Keymap")
	}
	if len(c.Panes) == 0 {
		t.Error("missing Pane")
	}
	if len(c.FileHandlers) == 0 {
		t.Error("missing FileHandler")
	}
	if len(c.Hooks) == 0 {
		t.Error("missing Hook")
	}
}

// TestPaneAdvertisesContext checks the example pane implements ContextProvider.
func TestPaneAdvertisesContext(t *testing.T) {
	p := Plugin{}.Capabilities().Panes[0].New(nil)
	cp, ok := p.(plugin.ContextProvider)
	if !ok {
		t.Fatal("example pane should advertise a context id")
	}
	if cp.ContextID() != "example.panel" {
		t.Fatalf("unexpected context id %q", cp.ContextID())
	}
}
