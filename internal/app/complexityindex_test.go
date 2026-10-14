package app

// complexityindex_test.go covers the app's wiring of the project complexity
// index (#2916): built per project from the [complexity] settings, walked
// from Init, exposed through ComplexityIndex, re-measured from a buffer save
// and a watcher event, toggled live by a config reload, and reported to the
// usage recorder as complexity.scan.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/complexity"
	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/registry"
	"ike/internal/telemetry"
	"ike/internal/watch"

	_ "ike/plugins/languages/go"
)

func writeComplexityFixture(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"src/a.go":       "package a\n\nfunc A(x int) int {\n\tif x > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n",
		"src/a_test.go":  "package a\n\nfunc TestA() {}\n",
		"vendor/v/v.go":  "package v\n\nfunc V() {}\n",
		"docs/readme.md": "# nothing\n",
	}
	for name, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func waitComplexity(t *testing.T, x *complexity.Index, what string, cond func(v complexity.View) bool) {
	t.Helper()
	for start := time.Now(); ; {
		if cond(x.Snapshot()) {
			return
		}
		// The roots are temp dirs of a few files: a walk or a re-measure is
		// milliseconds of work, the bound only catches a hung index (#2997).
		if time.Since(start) > 2*time.Second {
			t.Fatalf("timed out waiting for %s: %v", what, x.Snapshot().Paths())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestComplexityIndexBuiltWalkedAndToggledLive: the model owns an index over
// the project root under the [complexity] defaults, Init starts the walk, a
// save and a watcher event re-measure the one file, a reload with the
// master switch off drops the snapshot and on rebuilds it, and the exclude
// list applies on reload.
func TestComplexityIndexBuiltWalkedAndToggledLive(t *testing.T) {
	withProjectWalks(t)
	proj := t.TempDir()
	if r, err := filepath.EvalSymlinks(proj); err == nil {
		proj = r
	}
	state := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", state)
	t.Chdir(proj)
	writeComplexityFixture(t, proj)

	cfg, _ := config.Load(config.Options{})
	config.Set(cfg)
	m := NewWith(registry.New(), host.FromConfig(cfg))
	x := m.ComplexityIndex()
	if x == nil {
		t.Fatal("the model must own a complexity index")
	}
	if !x.Available() {
		t.Skip("no Go grammar in this build (cgo off)")
	}
	if opts := x.Options(); !opts.Enabled || len(opts.Exclude) != 0 {
		t.Fatalf("index options = %+v, want the [complexity] defaults", opts)
	}
	if !x.ScanDone() || len(x.Snapshot().Files) != 0 {
		t.Fatal("the walk must not start before Init")
	}
	m.Init()
	a := filepath.Join(proj, "src", "a.go")
	aTest := filepath.Join(proj, "src", "a_test.go")
	waitComplexity(t, x, "the walk", func(v complexity.View) bool {
		return len(v.Files) == 2 && len(v.Files[a]) == 1 && len(v.Files[aTest]) == 1
	})
	if fns := x.Snapshot().Files[a]; fns[0].Name != "A" || fns[0].CCN != 2 {
		t.Fatalf("a.go = %+v", fns)
	}

	// A buffer save re-measures the saved file through the save path.
	if err := os.WriteFile(a, []byte("package a\n\nfunc A(x int) int {\n\tif x > 0 && x < 9 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm, _ := m.Update(todoSavedMsg{path: a})
	m = tm.(Model)
	waitComplexity(t, x, "the save re-measure", func(v complexity.View) bool {
		return len(v.Files[a]) == 2 && v.Files[a][0].CCN == 3
	})

	// An external change arrives through the watcher route; a removed file
	// drops out.
	if err := os.Remove(aTest); err != nil {
		t.Fatal(err)
	}
	m.routeWatchEvent(watch.EventMsg{Kind: watch.FileRemoved, Path: aTest})
	waitComplexity(t, x, "the watcher removal", func(v complexity.View) bool {
		_, ok := v.Files[aTest]
		return !ok && len(v.Files) == 1
	})

	// Master switch off: the reload drops the snapshot.
	off, _ := config.Load(config.Options{})
	off.Complexity.Enabled = false
	tm, _ = m.Update(config.ConfigReloadedMsg{Config: off})
	m = tm.(Model)
	if s := m.ComplexityIndex().Stats(); s.Enabled || s.Files != 0 {
		t.Fatalf("after switching off, stats = %+v", s)
	}
	if v := m.ComplexityIndex().Snapshot(); len(v.Files) != 0 {
		t.Fatalf("disabled index still answers %v", v.Paths())
	}

	// Back on with an exclude list: rebuilt, rescanned, filtered.
	if err := os.WriteFile(aTest, []byte("package a\n\nfunc TestA() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	on, _ := config.Load(config.Options{})
	on.Complexity.Enabled = true
	on.Complexity.Exclude = []string{"**/*_test.go"}
	tm, _ = m.Update(config.ConfigReloadedMsg{Config: on})
	m = tm.(Model)
	waitComplexity(t, m.ComplexityIndex(), "the restart", func(v complexity.View) bool {
		_, hasTest := v.Files[aTest]
		return len(v.Files) == 1 && !hasTest && len(v.Files[a]) == 2
	})
	if opts := m.ComplexityIndex().Options(); len(opts.Exclude) != 1 || opts.Exclude[0] != "**/*_test.go" {
		t.Fatalf("exclude not applied: %+v", opts)
	}

	// The change message is a plain loop wake.
	if _, cmd := m.Update(ComplexityChangedMsg{Gen: 3}); cmd != nil {
		t.Fatal("ComplexityChangedMsg must not schedule work")
	}
}

// TestComplexityOptionsFromHostConfig: the flat host config the model is
// built from carries the [complexity] keys as strings; a malformed value
// keeps the default and the list is comma-separated.
func TestComplexityOptionsFromHostConfig(t *testing.T) {
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c, _ := config.Load(config.Options{})
	config.Set(c)
	opts := complexityOptionsFrom(host.MapConfig{
		"complexity.enabled": "false",
		"complexity.exclude": "vendor/**, **/*_test.go,",
	})
	if opts.Enabled || len(opts.Exclude) != 2 || opts.Exclude[0] != "vendor/**" || opts.Exclude[1] != "**/*_test.go" {
		t.Fatalf("options = %+v", opts)
	}
	opts = complexityOptionsFrom(host.MapConfig{"complexity.enabled": "banana"})
	if !opts.Enabled || len(opts.Exclude) != 0 {
		t.Fatalf("a malformed value must keep the default: %+v", opts)
	}
	if opts := complexityOptionsFrom(nil); !opts.Enabled {
		t.Fatalf("nil host config must fall back to the defaults: %+v", opts)
	}
	// The typed config round-trips the list through the flat host config.
	c.Complexity.Exclude = []string{"a/**", "b"}
	if opts := complexityOptionsFrom(host.FromConfig(c)); len(opts.Exclude) != 2 || opts.Exclude[1] != "b" {
		t.Fatalf("flat round trip = %+v", opts)
	}
}

// TestComplexityScanRecorder: a finished walk is one complexity.scan op with
// counts and duration only (plus cache=corrupt for a damaged cache, #2967);
// a build without a tree reports the error phase.
func TestComplexityScanRecorder(t *testing.T) {
	dir := t.TempDir()
	r := telemetry.New(dir, nil)
	rec := complexityScanRecorder(r)
	rec(complexity.Stats{Enabled: true, Files: 12, Functions: 80, Cached: 9, LastScan: 1500 * time.Millisecond})
	rec(complexity.Stats{Enabled: true, Files: 3, CacheCorrupt: true})
	rec(complexity.Stats{Enabled: true, Unavailable: true})
	r.Command("editor.save", telemetry.SourceKeybind)
	r.Close()
	evs := cxTelemetryEvents(t, dir)
	var ops []telemetry.Event
	for _, ev := range evs {
		if ev.Type == telemetry.TypeOp && ev.Data["id"] == telemetry.OpComplexityScan {
			ops = append(ops, ev)
		}
	}
	if len(ops) != 3 {
		t.Fatalf("ops = %v, want three complexity.scan events", evs)
	}
	if d := ops[0].Data; d["phase"] != telemetry.OpPhaseOK || d["ms"] != "1500" || d["files"] != "12" || d["functions"] != "80" || d["cached"] != "9" || d["truncated"] != "false" || d["cache"] != "" {
		t.Fatalf("ok payload = %v", d)
	}
	if d := ops[1].Data; d["phase"] != telemetry.OpPhaseOK || d["cache"] != "corrupt" || d["cached"] != "0" {
		t.Fatalf("corrupt-cache payload = %v", d)
	}
	if d := ops[2].Data; d["phase"] != telemetry.OpPhaseError {
		t.Fatalf("error payload = %v", d)
	}
	for _, ev := range ops {
		for k := range ev.Data {
			switch k {
			case "id", "phase", "ms", "files", "functions", "cached", "cache", "truncated":
			default:
				t.Fatalf("unexpected field %q in %v", k, ev.Data)
			}
		}
	}
}

// cxTelemetryEvents reads every event written under the recorder dir.
func cxTelemetryEvents(t *testing.T, dir string) []telemetry.Event {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			if line == "" {
				continue
			}
			var ev telemetry.Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("bad JSONL line %q: %v", line, err)
			}
			out = append(out, ev)
		}
	}
	return out
}
