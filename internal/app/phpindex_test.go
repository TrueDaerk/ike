package app

// phpindex_test.go covers the app's wiring of the PHP declaration index
// (#2667): built per project from the [php] settings, exposed through
// PHPIndex, fed by the completion engine's observer path, and toggled live
// by a config reload of php.trait_index.

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/phpindex"
	"ike/internal/registry"

	_ "ike/plugins/languages/php"
)

func writePHPFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"src/A.php": "<?php\nnamespace App;\ntrait A { public function run() { $this->abc(); } }\n",
		"src/B.php": "<?php\nnamespace App;\nclass B { use A; public function abc() {} }\n",
	}
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func waitPHPScan(t *testing.T, x *phpindex.Index) {
	t.Helper()
	for start := time.Now(); !x.ScanDone(); {
		if time.Since(start) > 10*time.Second {
			t.Fatal("PHP index scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestPHPIndexBuiltAndToggledLive: the model owns an index over the project
// root scanned from the [php] defaults; a reload with php.trait_index off
// drops it at once, on rebuilds it, and the engine forwards buffer edits to
// it without a disk write.
func TestPHPIndexBuiltAndToggledLive(t *testing.T) {
	proj := t.TempDir()
	state := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", state)
	t.Chdir(proj)
	writePHPFixture(t, proj)

	cfg, _ := config.Load(config.Options{})
	config.Set(cfg)
	m := NewWith(registry.New(), host.FromConfig(cfg))
	x := m.PHPIndex()
	if x == nil {
		t.Fatal("the model must own a PHP index")
	}
	if !x.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	if opts := x.Options(); !opts.Enabled || opts.ParentDepth != 3 || opts.MaxFiles != 20000 || opts.IncludeVendor {
		t.Fatalf("index options = %+v, want the [php] defaults", opts)
	}
	waitPHPScan(t, x)
	if got := x.ConsumersOf(`App\A`); len(got) != 1 || got[0] != `App\B` {
		t.Fatalf("ConsumersOf(A) = %v, want [App\\B]", got)
	}

	// The engine's observer path: an edited consumer buffer reaches the
	// scope without touching the disk.
	bPath := filepath.Join(proj, "src", "B.php")
	m.completeEngine.Emit(host.EditorEvent{Kind: host.EditorChange, Path: bPath,
		Text: "<?php\nnamespace App;\nclass B { use A; public function abc() {} public function fresh() {} }\n"})
	x.Flush()
	found := false
	for _, mem := range x.VisibleMembers(`App\A`) {
		if mem.Name == "fresh" {
			found = true
		}
	}
	if !found {
		t.Fatal("a buffer edit did not reach the index through the engine")
	}

	// Master switch off: the reload drops the index.
	off, _ := config.Load(config.Options{})
	off.PHP.TraitIndex = false
	tm, _ := m.Update(config.ConfigReloadedMsg{Config: off})
	m = tm.(Model)
	if s := m.PHPIndex().Stats(); s.Enabled || s.Files != 0 {
		t.Fatalf("after switching off, stats = %+v", s)
	}
	if got := m.PHPIndex().ConsumersOf(`App\A`); len(got) != 0 {
		t.Fatalf("disabled index still answers %v", got)
	}

	// Back on: rebuilt and rescanned.
	on, _ := config.Load(config.Options{})
	on.PHP.TraitIndex = true
	on.PHP.Index.ParentDepth = 1
	tm, _ = m.Update(config.ConfigReloadedMsg{Config: on})
	m = tm.(Model)
	waitPHPScan(t, m.PHPIndex())
	if got := m.PHPIndex().ConsumersOf(`App\A`); len(got) != 1 || got[0] != `App\B` {
		t.Fatalf("after switching on, ConsumersOf(A) = %v", got)
	}
	if opts := m.PHPIndex().Options(); opts.ParentDepth != 1 {
		t.Fatalf("parent depth not applied: %+v", opts)
	}
}

// TestPHPOptionsFromHostConfig: the flat host config the model is built
// from carries the [php] keys as strings; a malformed value keeps the
// default.
func TestPHPOptionsFromHostConfig(t *testing.T) {
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c, _ := config.Load(config.Options{})
	config.Set(c)
	opts := phpOptionsFrom(host.MapConfig{
		"php.trait_index":          "false",
		"php.index.parent_depth":   "5",
		"php.index.include_vendor": "true",
		"php.index.max_files":      "banana",
	})
	if opts.Enabled || opts.ParentDepth != 5 || !opts.IncludeVendor || opts.MaxFiles != 20000 {
		t.Fatalf("options = %+v", opts)
	}
	if opts := phpOptionsFrom(nil); !opts.Enabled {
		t.Fatalf("nil host config must fall back to the defaults: %+v", opts)
	}
}
