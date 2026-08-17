package app

import (
	"strings"
	"testing"

	"ike/internal/palette"
	"ike/internal/run"
)

// TestRunConfigsModeResults verifies the picker rows: stored and launch.json
// entries render with their kind/target detail, and the import names its
// source file.
func TestRunConfigsModeResults(t *testing.T) {
	mode := newRunConfigsMode()
	mode.entries = []runConfigEntry{
		{cfg: run.Config{Name: "prog", Kind: run.KindRun, Lang: "go", File: "cmd/main.go"}, stored: true},
		{cfg: run.Config{Name: "Launch server", Kind: run.KindDebug, Lang: "go", File: "cmd/srv"}},
	}
	items := mode.Results("", palette.Context{})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Title != "prog" || strings.Contains(items[0].Detail, "launch.json") {
		t.Fatalf("stored row = %+v", items[0])
	}
	if !strings.Contains(items[1].Detail, ".vscode/launch.json") || !strings.Contains(items[1].Detail, "debug") {
		t.Fatalf("imported row must name its source and kind: %+v", items[1])
	}
	picked, ok := items[1].Msg.(RunConfigPickedMsg)
	if !ok || picked.Stored || picked.Cfg.Name != "Launch server" {
		t.Fatalf("activation msg = %+v", items[1].Msg)
	}

	// Fuzzy filtering matches over the name.
	items = mode.Results("srv", palette.Context{})
	if len(items) != 1 || items[0].Title != "Launch server" {
		t.Fatalf("filtered items = %+v", items)
	}
}

// TestRunPickedConfigLaunchesRun verifies a run-kind pick rides the ordinary
// launch funnel into the Run tool, like run.file.
func TestRunPickedConfigLaunchesRun(t *testing.T) {
	m := runModel(t, "bottom")
	store := run.Load()
	cfg, _, ok := store.EnsureFor(projectRoot(), m.activeEditor().Path())
	if !ok {
		t.Fatal("no config for the fake language")
	}
	_ = run.Save(store)
	tm, _ := m.Update(RunConfigPickedMsg{Cfg: *cfg, Stored: true})
	m = tm.(Model)
	if len(m.toolLocations(runToolName)) == 0 {
		t.Fatal("a run-kind pick must open the Run tool")
	}
}
