package bridge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/wasm"
	"ike/internal/wasm/abi"
)

// The #26 acceptance test: the SDK-built example plugin (sdk/example) loads
// and registers through the full production pipeline — runtime, host module,
// bridge, registry — and its callbacks drive real host effects.

// buildExample compiles sdk/example against the checked-in SDK.
func buildExample(t *testing.T) string {
	t.Helper()
	srcDir, err := filepath.Abs(filepath.Join("..", "..", "..", "sdk", "example"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "main.go")); err != nil {
		t.Fatalf("sdk example missing: %v", err)
	}
	out := filepath.Join(t.TempDir(), "wasm-example.wasm")
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", out, ".")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("example build: %v\n%s", err, outBytes)
	}
	return out
}

func TestSDKExampleThroughFullPipeline(t *testing.T) {
	h := host.New(host.MapConfig{"editor.tab_width": "4"})
	ctx := context.Background()
	rt := wasm.NewRuntime(ctx, nil)
	t.Cleanup(rt.Close)
	adapter := NewHostAdapter()
	adapter.SetAPI(h)
	if err := abi.InstantiateHost(ctx, rt.Engine(), adapter); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	data, err := os.ReadFile(buildExample(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wasm-example.wasm"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if diags := rt.ScanDir(dir).Diagnostics; len(diags) > 0 {
		t.Fatalf("scan diags: %v", diags)
	}
	reg := registry.New()
	if diags := RegisterModules(ctx, rt, reg); len(diags) > 0 {
		t.Fatalf("register diags: %v", diags)
	}

	// The SDK's declared capability set arrives intact.
	if ids := reg.PluginIDs(); len(ids) != 1 || ids[0] != "wasm-example" {
		t.Fatalf("plugins = %v", ids)
	}
	greet, ok := reg.Command("wasm-example.greet")
	if !ok || greet.Title != "Example: Greet" || !greet.Scope.Global {
		t.Fatalf("greet = %+v ok=%v", greet, ok)
	}
	shout, ok := reg.Command("wasm-example.shout")
	if !ok || shout.Scope.ContextID != "editor" || shout.Scope.Global {
		t.Fatalf("shout = %+v ok=%v", shout, ok)
	}
	if keys, ok := reg.Binding("wasm-example.greet"); !ok || keys != "ctrl+k g" {
		t.Fatalf("binding = %q ok=%v", keys, ok)
	}
	hooks := reg.Hooks(plugin.EventFileOpened)
	if len(hooks) != 1 || hooks[0].ID != "wasm-example.opened" {
		t.Fatalf("hooks = %+v", hooks)
	}

	// Command: Notify + ConfigGet + SetStatus round-trip through the SDK.
	greet.Run(h)()
	notes := h.DrainNotifications()
	if len(notes) != 1 || notes[0].Severity != host.Info || !strings.Contains(notes[0].Text, "tab_width=4") {
		t.Fatalf("greet notes = %+v", notes)
	}
	if h.Status() != "wasm example ran" {
		t.Fatalf("status = %q", h.Status())
	}

	// Keymap aliasing a command routes to the command's callback.
	km, ok := reg.ResolveKey("ctrl+k g", "")
	if !ok {
		t.Fatal("ctrl+k g unresolved")
	}
	km.Action(h)()
	if notes := h.DrainNotifications(); len(notes) != 1 || !strings.Contains(notes[0].Text, "hello from the wasm example") {
		t.Fatalf("keymap notes = %+v", notes)
	}

	// Standalone KeymapFunc binding fires its own callback.
	km2, ok := reg.ResolveKey("ctrl+k y", "")
	if !ok {
		t.Fatal("ctrl+k y unresolved")
	}
	km2.Action(h)()
	if notes := h.DrainNotifications(); len(notes) != 1 || notes[0].Text != "standalone binding fired" {
		t.Fatalf("standalone notes = %+v", notes)
	}

	// Hook: the JSON payload reaches the SDK callback decoded.
	hooks[0].Notify(h, "/tmp/opened.go")()
	if notes := h.DrainNotifications(); len(notes) != 1 || notes[0].Text != "example saw open: /tmp/opened.go" {
		t.Fatalf("hook notes = %+v", notes)
	}
}
