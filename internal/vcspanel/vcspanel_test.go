package vcspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"ike/internal/vcs"
)

func key(s string) tea.KeyPressMsg {
	if s == "tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestViewShowsHeaderAndPlaceholder(t *testing.T) {
	m := New(nil)
	m.SetSize(80, 10)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Changes") {
		t.Fatalf("header missing:\n%s", v)
	}
	if !strings.Contains(v, "not a git repository") {
		t.Fatalf("nil snapshot must show the placeholder:\n%s", v)
	}
	m.SetVCS(&vcs.Snapshot{Root: "/r", Branch: "main"})
	v = ansi.Strip(m.View())
	if !strings.Contains(v, "⎇ main") {
		t.Fatalf("branch missing from header:\n%s", v)
	}
	if strings.Contains(v, "not a git repository") {
		t.Fatal("placeholder must vanish with a snapshot")
	}
}

// TestNoStagingOrLogSurfaces pins the #750 slimming: the panel is a read-only
// changes list — no staging checkboxes, no commit message, no Log tab.
func TestNoStagingOrLogSurfaces(t *testing.T) {
	m := changesPanel()
	v := ansi.Strip(m.View())
	for _, gone := range []string{"[x]", "[ ]", "[~]", "Message:", "commit", "Log", "stage"} {
		if strings.Contains(v, gone) {
			t.Fatalf("slimmed panel must not render %q:\n%s", gone, v)
		}
	}
	// Former staging/commit/tab keys are inert: no command, no state change.
	for _, k := range []string{" ", "c", "m", "1", "2", "tab"} {
		if cmd := m.Update(key(k)); cmd != nil {
			t.Fatalf("key %q must be inert, emitted %#v", k, cmd())
		}
	}
	if cmd := m.Update(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}); cmd != nil {
		t.Fatalf("ctrl+s must be inert, emitted %#v", cmd())
	}
}
