package example

import (
	"testing"

	"ike/internal/host"
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

// TestHookNotifiesInsteadOfStatus guards the 0130 migration: the file-opened
// hook raises an info toast and no longer writes the persistent status line
// (which used to stick forever and cover the mode/cursor segments).
func TestHookNotifiesInsteadOfStatus(t *testing.T) {
	h := host.New(nil)
	Plugin{}.Capabilities().Hooks[0].Notify(h, "/tmp/x.go")
	if h.Status() != "" {
		t.Fatalf("hook must not write the status line, got %q", h.Status())
	}
	ns := h.DrainNotifications()
	if len(ns) != 1 || ns[0].Severity != host.Info || ns[0].Text != "example saw open: /tmp/x.go" {
		t.Fatalf("expected one info notification, got %+v", ns)
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
