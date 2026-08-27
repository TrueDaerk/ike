package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/keydoctor"
	"ike/internal/keymap"
	"ike/internal/registry"
)

// deadBindingsApp builds a sized app model with a private config dir, so a
// rebind writes into the test's own user config.
func deadBindingsApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

// TestDeadBindingsReportOpens (#2161): keymap.deadBindings audits the live
// binding table — the JetBrains default set always has cmd chords, which are
// at best at risk in a plain terminal.
func TestDeadBindingsReportOpens(t *testing.T) {
	m := deadBindingsApp(t)
	out, _ := m.Update(KeymapDeadBindingsMsg{})
	m = out.(Model)
	if !m.keyDoctor.ReportOpen() {
		t.Fatal("keymap.deadBindings must open the report")
	}
	if len(m.keyDoctor.Findings()) == 0 {
		t.Fatal("the default keymap must yield findings in a plain terminal")
	}
	for _, f := range m.keyDoctor.Findings() {
		if f.Reason == "" {
			t.Fatalf("finding %s carries no reason", f.Binding.Chord)
		}
	}
}

// TestDeadBindingsRebindPersists (#2161): applying an offer writes the
// override at user scope — the new chord binds the command, the old one
// unbinds — and the reload it triggers re-audits the open report.
func TestDeadBindingsRebindPersists(t *testing.T) {
	m := deadBindingsApp(t)
	out, _ := m.Update(KeymapDeadBindingsMsg{})
	m = out.(Model)
	var target keydoctor.Finding
	for _, f := range m.keyDoctor.Findings() {
		if len(f.Suggestions) > 0 {
			target = f
			break
		}
	}
	if target.Binding.Command == "" {
		t.Fatal("no finding offered a rebind")
	}
	old, fresh := target.Binding.Chord, target.Suggestions[0]

	out, cmd := m.Update(keydoctor.RebindMsg{Command: target.Binding.Command, Old: old, New: fresh})
	m = out.(Model)
	reload := runForReload(t, cmd)
	if got := reload.Config.Keymap.Bindings[fresh.String()]; got != target.Binding.Command {
		t.Fatalf("keymap.bindings[%q] = %q, want %q", fresh, got, target.Binding.Command)
	}
	if got, ok := reload.Config.Keymap.Bindings[old.String()]; !ok || got != "" {
		t.Fatalf("old chord %q = %q (present=%v), want an empty unbind", old, got, ok)
	}

	// The reload lands: the report re-audits, and the repaired chord is gone.
	orig := config.Get()
	t.Cleanup(func() { config.Set(orig) })
	out, _ = m.Update(reload)
	m = out.(Model)
	if !m.keyDoctor.ReportOpen() {
		t.Fatal("the report must stay open across the reload")
	}
	for _, f := range m.keyDoctor.Findings() {
		if f.Binding.Chord.Equal(old) && f.Binding.Command == target.Binding.Command {
			t.Fatalf("rebound binding %s still reported dead", old)
		}
	}
	// The rebind is effective, not just written: the command resolves on the
	// new chord.
	if b, ok := m.bindings.Table().Lookup(fresh, keymap.Global); !ok || b.Command != target.Binding.Command {
		t.Fatalf("lookup(%s) = %+v (ok=%v), want %s", fresh, b, ok, target.Binding.Command)
	}
}
