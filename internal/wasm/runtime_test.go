package wasm

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// emptyModule is the smallest valid wasm binary: magic + version, no
// sections. It compiles and instantiates with no imports or exports.
var emptyModule = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

// buildFixture compiles a Go program to wasip1/wasm once per test run — a
// real module with a WASI _start, exactly what plugin authors will ship.
var (
	fixtureOnce sync.Once
	fixtureDir  string
	fixtureErr  error
)

func fixture(t *testing.T, name, src string) string {
	t.Helper()
	fixtureOnce.Do(func() {
		fixtureDir, fixtureErr = os.MkdirTemp("", "ike-wasm-fixtures")
	})
	if fixtureErr != nil {
		t.Fatal(fixtureErr)
	}
	srcDir := filepath.Join(fixtureDir, name+"-src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "go.mod"), []byte("module fixture\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(fixtureDir, name+".wasm")
	args := []string{"build", "-o", out}
	if strings.Contains(src, "wasmexport") {
		// Reactor-style module: exports stay callable after _initialize.
		args = append(args, "-buildmode=c-shared")
	}
	args = append(args, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v\n%s", err, b)
	}
	return out
}

func newTestRuntime(t *testing.T, out *bytes.Buffer) *Runtime {
	t.Helper()
	var sink *bytes.Buffer
	if out != nil {
		sink = out
	}
	var r *Runtime
	if sink != nil {
		r = NewRuntime(context.Background(), sink)
	} else {
		r = NewRuntime(context.Background(), nil)
	}
	t.Cleanup(r.Close)
	return r
}

func TestLoadInstantiateUnload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.wasm")
	if err := os.WriteFile(path, emptyModule, 0o644); err != nil {
		t.Fatal(err)
	}
	r := newTestRuntime(t, nil)
	m, err := r.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "hello" || len(r.Modules()) != 1 {
		t.Fatalf("module = %+v, loaded = %d", m, len(r.Modules()))
	}
	// Duplicate names are rejected.
	if _, err := r.Load(path); err == nil {
		t.Fatal("duplicate load should fail")
	}
	r.Unload("hello")
	if len(r.Modules()) != 0 {
		t.Fatal("unload should forget the module")
	}
	r.Unload("hello") // idempotent
	// Reload after unload works.
	if _, err := r.Load(path); err != nil {
		t.Fatal(err)
	}
}

func TestRealGoModuleRunsStart(t *testing.T) {
	path := fixture(t, "greeter", `package main

import "fmt"

func main() { fmt.Println("greetings from wasm") }
`)
	var out bytes.Buffer
	r := newTestRuntime(t, &out)
	if _, err := r.Load(path); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "greetings from wasm") {
		t.Fatalf("guest stdout = %q", out.String())
	}
}

func TestFaultingModuleIsIsolated(t *testing.T) {
	trap := fixture(t, "trapper", `package main

func main() { panic("boom") }
`)
	good := t.TempDir() + "/good.wasm"
	if err := os.WriteFile(good, emptyModule, 0o644); err != nil {
		t.Fatal(err)
	}
	r := newTestRuntime(t, nil)
	if _, err := r.Load(trap); err == nil {
		t.Fatal("a trapping start function must fail the load")
	}
	if len(r.Modules()) != 0 {
		t.Fatal("the faulting module must not linger")
	}
	// The runtime survives: another module loads fine afterwards.
	if _, err := r.Load(good); err != nil {
		t.Fatalf("runtime should stay usable after a fault: %v", err)
	}
}

func TestBadBytesRejected(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "garbage.wasm")
	if err := os.WriteFile(bad, []byte("not wasm at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newTestRuntime(t, nil)
	if _, err := r.Load(bad); err == nil {
		t.Fatal("garbage bytes must be rejected")
	}
	if _, err := r.Load(filepath.Join(dir, "missing.wasm")); err == nil {
		t.Fatal("a missing file must error")
	}
}

func TestScanDirLoadsGoodSkipsBad(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.wasm"), emptyModule, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.wasm"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.wasm"), emptyModule, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.wasm"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := newTestRuntime(t, nil)
	res := r.ScanDir(dir)
	if len(res.Loaded) != 2 || res.Loaded[0].Name != "alpha" || res.Loaded[1].Name != "beta" {
		t.Fatalf("loaded = %+v", res.Loaded)
	}
	if len(res.Diagnostics) != 1 || !strings.Contains(res.Diagnostics[0], "broken") {
		t.Fatalf("diagnostics = %v", res.Diagnostics)
	}
	// A missing dir is normal and silent.
	if res := r.ScanDir(filepath.Join(dir, "nope")); len(res.Loaded) != 0 || len(res.Diagnostics) != 0 {
		t.Fatalf("missing dir should be quiet, got %+v", res)
	}
}

func TestDefaultDirHonorsOverride(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "/custom")
	if got := DefaultDir(); got != filepath.Join("/custom", "plugins") {
		t.Fatalf("DefaultDir = %q", got)
	}
	t.Setenv("IKE_CONFIG_DIR", "")
	t.Setenv("HOME", "/home/x")
	if got := DefaultDir(); got != filepath.Join("/home/x", ".ike", "plugins") {
		t.Fatalf("DefaultDir = %q", got)
	}
}

func TestExportedFunctionSeam(t *testing.T) {
	path := fixture(t, "exporter", `package main

//go:wasmexport add
func add(a, b int32) int32 { return a + b }

func main() {}
`)
	r := newTestRuntime(t, nil)
	m, err := r.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := m.ExportedFunction("add")
	if !ok {
		t.Fatal("exported function should resolve")
	}
	res, err := fn.Call(context.Background(), 2, 40)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || int32(res[0]) != 42 {
		t.Fatalf("add = %v", res)
	}
	if _, ok := m.ExportedFunction("nope"); ok {
		t.Fatal("unknown export should report ok=false")
	}
}
