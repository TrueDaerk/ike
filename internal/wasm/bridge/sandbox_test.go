package bridge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/wasm"
	"ike/internal/wasm/abi"
)

// sandbox_test.go covers #27 at the bridge level: manifest capability gating
// through the full pipeline (registration kinds and host calls) and crash
// isolation of a runaway callback (timeout → module unloaded, IKE fine).

// gatedPipeline loads the parity guest plus the given sidecar manifest
// through the production path with the gated host module.
func gatedPipeline(t *testing.T, h host.API, manifest string) (*wasm.Runtime, *registry.Registry, []string) {
	t.Helper()
	ctx := context.Background()
	rt := wasm.NewRuntime(ctx, nil)
	t.Cleanup(rt.Close)
	adapter := NewHostAdapter()
	adapter.SetAPI(h)
	if err := abi.InstantiateHostGated(ctx, rt.Engine(), adapter, rt.Allows); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	data, err := os.ReadFile(guestModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parity.wasm"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "parity.manifest.json"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if diags := rt.ScanDir(dir).Diagnostics; len(diags) > 0 {
		t.Fatalf("scan diags: %v", diags)
	}
	reg := registry.New()
	return rt, reg, RegisterModules(ctx, rt, reg)
}

func TestManifestGatesRegistrationKinds(t *testing.T) {
	h := host.New(host.MapConfig{})
	// The parity guest declares commands, keymaps, and hooks; the manifest
	// requests only commands (+notify so the callbacks stay observable).
	_, reg, diags := gatedPipeline(t, h,
		`{"name":"parity","version":"1.0.0","capabilities":["commands","notify"]}`)
	if len(reg.Commands()) != 2 {
		t.Fatalf("commands = %+v", reg.Commands())
	}
	if got := reg.Keymaps(); len(got) != 0 {
		t.Fatalf("keymaps must be gated: %+v", got)
	}
	if got := reg.Hooks(plugin.EventFileOpened); len(got) != 0 {
		t.Fatalf("hooks must be gated: %+v", got)
	}
	joined := strings.Join(diags, "\n")
	if !strings.Contains(joined, "keymaps") || !strings.Contains(joined, "hooks") {
		t.Fatalf("dropped kinds must be diagnosed: %v", diags)
	}
	// The granted kind still works end to end.
	c, ok := reg.Command("parity.hello")
	if !ok {
		t.Fatal("granted command missing")
	}
	c.Run(h)()
	if notes := h.DrainNotifications(); len(notes) != 1 || notes[0].Text != "ran parity.hello" {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestManifestGatesHostCalls(t *testing.T) {
	h := host.New(host.MapConfig{})
	// Everything registered, but notify is NOT requested: the guest's
	// callback runs, its notify host call is silently dropped.
	_, reg, diags := gatedPipeline(t, h,
		`{"name":"parity","version":"1.0.0","capabilities":["commands","keymaps","hooks"]}`)
	if len(diags) != 0 {
		t.Fatalf("diags = %v", diags)
	}
	c, ok := reg.Command("parity.hello")
	if !ok {
		t.Fatal("command missing")
	}
	c.Run(h)()
	if notes := h.DrainNotifications(); len(notes) != 0 {
		t.Fatalf("ungranted notify must be dropped: %+v", notes)
	}
}

// --- crash isolation: runaway callback ---

var (
	runawayOnce sync.Once
	runawayPath string
	runawayErr  string
)

// runawayModule compiles a guest whose command loops forever.
func runawayModule(t *testing.T) string {
	t.Helper()
	runawayOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ike-bridge-runaway")
		if err != nil {
			runawayErr = err.Error()
			return
		}
		src := `package main

import "unsafe"

var buffers [][]byte

//go:wasmexport ike_alloc
func ikeAlloc(size uint32) uint32 {
	buf := make([]byte, size)
	buffers = append(buffers, buf)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

var capsJSON = []byte(` + "`" + `{"name":"runaway","commands":[{"id":"runaway.spin","title":"Spin"}]}` + "`" + `)

//go:wasmexport register
func register() uint64 {
	return uint64(uintptr(unsafe.Pointer(&capsJSON[0])))<<32 | uint64(len(capsJSON))
}

//go:wasmexport on_command
func onCommand(ptr, n uint32) {
	for {
	}
}

//go:wasmexport on_key
func onKey(ptr, n uint32) {}

//go:wasmexport on_hook
func onHook(ptr, n, pptr, plen uint32) {}

func main() {}
`
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
			runawayErr = err.Error()
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module runaway\n\ngo 1.24\n"), 0o644); err != nil {
			runawayErr = err.Error()
			return
		}
		runawayPath = filepath.Join(dir, "runaway.wasm")
		cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", runawayPath, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if out, err := cmd.CombinedOutput(); err != nil {
			runawayErr = err.Error() + "\n" + string(out)
		}
	})
	if runawayErr != "" {
		t.Fatalf("runaway build: %s", runawayErr)
	}
	return runawayPath
}

func TestRunawayCallbackUnloadsModule(t *testing.T) {
	h := host.New(host.MapConfig{})
	ctx := context.Background()
	rt := wasm.NewRuntimeWith(ctx, nil, wasm.Options{CallTimeout: 200 * time.Millisecond})
	t.Cleanup(rt.Close)
	adapter := NewHostAdapter()
	adapter.SetAPI(h)
	if err := abi.InstantiateHostGated(ctx, rt.Engine(), adapter, rt.Allows); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	data, err := os.ReadFile(runawayModule(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runaway.wasm"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if diags := rt.ScanDir(dir).Diagnostics; len(diags) > 0 {
		t.Fatalf("scan diags: %v", diags)
	}
	reg := registry.New()
	if diags := RegisterModules(ctx, rt, reg); len(diags) > 0 {
		t.Fatalf("register diags: %v", diags)
	}
	c, ok := reg.Command("runaway.spin")
	if !ok {
		t.Fatal("command missing")
	}
	start := time.Now()
	c.Run(h)() // spins in the guest until the deadline closes the module
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("runaway call returned after %v, deadline was 200ms", elapsed)
	}
	notes := h.DrainNotifications()
	if len(notes) != 1 || notes[0].Severity != host.Error || !strings.Contains(notes[0].Text, "unloaded") {
		t.Fatalf("notes = %+v", notes)
	}
	if len(rt.Modules()) != 0 {
		t.Fatal("runaway module must be unloaded")
	}
}
