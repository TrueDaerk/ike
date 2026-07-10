package wasm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sandbox_test.go covers the #27 acceptance list: manifest parsing/rejection,
// capability lookups, memory-cap enforcement, call-timeout enforcement, and
// crash isolation of the runtime around them.

// --- manifest parsing & validation ---

func TestParseManifest(t *testing.T) {
	m, err := ParseManifest([]byte(`{"name":"demo","version":"1.2.0","capabilities":["commands","notify"],"future":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "demo" || m.Version != "1.2.0" || len(m.Capabilities) != 2 {
		t.Fatalf("manifest = %+v", m)
	}

	rejects := map[string]string{
		"malformed json":       `{"name":`,
		"missing name":         `{"version":"1.0.0"}`,
		"blank name":           `{"name":"  ","version":"1.0.0"}`,
		"missing version":      `{"name":"demo"}`,
		"unknown capability":   `{"name":"demo","version":"1.0.0","capabilities":["filesystem"]}`,
		"duplicate capability": `{"name":"demo","version":"1.0.0","capabilities":["notify","notify"]}`,
	}
	for label, src := range rejects {
		if _, err := ParseManifest([]byte(src)); err == nil {
			t.Errorf("%s: expected rejection", label)
		}
	}
}

func TestManifestAllows(t *testing.T) {
	var nilManifest *Manifest
	if !nilManifest.Allows(CapNotify) || !nilManifest.Allows(CapCommands) {
		t.Fatal("nil manifest must grant everything")
	}
	m := &Manifest{Name: "demo", Version: "1.0.0", Capabilities: []string{CapCommands, CapNotify}}
	if !m.Allows(CapCommands) || !m.Allows(CapNotify) {
		t.Fatal("requested capabilities must be granted")
	}
	if m.Allows(CapOpenFile) || m.Allows(CapConfigGet) {
		t.Fatal("unrequested capabilities must be denied")
	}
}

// --- manifest at load time ---

func writeModuleWithManifest(t *testing.T, dir, name, manifest string) string {
	t.Helper()
	path := filepath.Join(dir, name+".wasm")
	if err := os.WriteFile(path, emptyModule, 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".manifest.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestLoadWithValidManifest(t *testing.T) {
	r := newTestRuntime(t, nil)
	dir := t.TempDir()
	path := writeModuleWithManifest(t, dir, "demo",
		`{"name":"demo","version":"0.1.0","capabilities":["commands","notify"]}`)
	m, err := r.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Manifest == nil || m.Manifest.Version != "0.1.0" {
		t.Fatalf("manifest = %+v", m.Manifest)
	}
	if !r.Allows("demo", CapNotify) || r.Allows("demo", CapOpenFile) {
		t.Fatal("runtime capability lookup must follow the manifest")
	}
	// Unknown modules and modules without manifests stay unrestricted.
	if !r.Allows("unknown", CapOpenFile) {
		t.Fatal("unknown module must be unrestricted")
	}
}

func TestLoadRejectsBadManifests(t *testing.T) {
	r := newTestRuntime(t, nil)
	dir := t.TempDir()
	cases := map[string]string{
		"mismatch":  `{"name":"other","version":"0.1.0"}`,
		"malformed": `{"name":`,
		"unknowncp": `{"name":"unknowncp","version":"0.1.0","capabilities":["network"]}`,
	}
	for name, manifest := range cases {
		path := writeModuleWithManifest(t, dir, name, manifest)
		if _, err := r.Load(path); err == nil {
			t.Errorf("%s: module with invalid manifest must be rejected", name)
		}
	}
	if len(r.Modules()) != 0 {
		t.Fatalf("nothing should have loaded: %v", r.Modules())
	}
}

func TestScanDirSkipsInvalidManifest(t *testing.T) {
	r := newTestRuntime(t, nil)
	dir := t.TempDir()
	writeModuleWithManifest(t, dir, "good", `{"name":"good","version":"1.0.0"}`)
	writeModuleWithManifest(t, dir, "bad", `{"name":"bad"}`) // missing version
	res := r.ScanDir(dir)
	if len(res.Loaded) != 1 || res.Loaded[0].Name != "good" {
		t.Fatalf("modules = %+v", res.Loaded)
	}
	if len(res.Diagnostics) != 1 || !strings.Contains(res.Diagnostics[0], "missing version") {
		t.Fatalf("diags = %v", res.Diagnostics)
	}
}

// --- memory cap ---

func TestMemoryCapRejectsHungryModule(t *testing.T) {
	// The Go runtime guest needs well over 16 pages (1 MiB) of linear memory;
	// under that cap instantiation must fail — and leave the runtime usable.
	r := NewRuntimeWith(context.Background(), nil, Options{MemoryLimitPages: 16})
	t.Cleanup(r.Close)
	path := fixture(t, "hello", `package main

func main() { println("hello") }
`)
	if _, err := r.Load(path); err == nil {
		t.Fatal("a module needing more memory than the cap must fail to load")
	}
	// Crash isolation: the runtime survives and loads small modules fine.
	small := filepath.Join(t.TempDir(), "small.wasm")
	if err := os.WriteFile(small, emptyModule, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Load(small); err != nil {
		t.Fatalf("runtime unusable after rejected module: %v", err)
	}
}

// --- call timeout ---

func TestCallTimeoutStopsRunawayLoop(t *testing.T) {
	r := NewRuntimeWith(context.Background(), nil, Options{CallTimeout: 200 * time.Millisecond})
	t.Cleanup(r.Close)
	path := fixture(t, "spinner", `package main

//go:wasmexport spin
func spin() {
	for {
	}
}

func main() {}
`)
	m, err := r.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := m.ExportedFunction("spin")
	if !ok {
		t.Fatal("spin export missing")
	}
	ctx, cancel := r.CallContext()
	defer cancel()
	start := time.Now()
	_, err = fn.Call(ctx)
	if err == nil {
		t.Fatal("runaway loop must be aborted")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("abort took %v, deadline was 200ms", elapsed)
	}
}

func TestStartFunctionTimeoutRejectsModule(t *testing.T) {
	r := NewRuntimeWith(context.Background(), nil, Options{CallTimeout: 200 * time.Millisecond})
	t.Cleanup(r.Close)
	path := fixture(t, "hangstart", `package main

func init() {
	for {
	}
}

//go:wasmexport noop
func noop() {}

func main() {}
`)
	start := time.Now()
	if _, err := r.Load(path); err == nil {
		t.Fatal("a module hanging in _initialize must be rejected")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("load abort took %v, deadline was 200ms", elapsed)
	}
	if len(r.Modules()) != 0 {
		t.Fatal("hanging module must not stay loaded")
	}
}
