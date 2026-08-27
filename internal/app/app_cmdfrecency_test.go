package app

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/palette"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// TestDispatchRecordsCommandFrecency proves the execution-history boost (#2153)
// is fed by the single dispatch funnel and persisted per project, so the next
// palette open — in this or a later session — ranks the command higher.
func TestDispatchRecordsCommandFrecency(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)

	reg := registry.New()
	reg.Add(fakePlugin{id: "p", caps: plugin.Capabilities{Commands: []plugin.Command{{
		ID: "p.go", Scope: plugin.GlobalScope(),
		Run: func(h host.API) tea.Cmd { h.SetStatus("ran"); return nil },
	}}}})
	m := NewWith(reg, host.MapConfig{})
	m.RunCommand("p.go")
	m.RunCommand("p.go")

	// Persisted where the store seam says, and readable by a fresh load.
	re := palette.LoadFrecency(filepath.Join(dir, "cmdfrecency.json"))
	if got := re.Score("p.go"); got < 1.9 {
		t.Fatalf("persisted frecency score = %v, want ~2", got)
	}
	if got := re.Score("p.never"); got != 0 {
		t.Fatalf("unexecuted command score = %v, want 0", got)
	}
}
