package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/wasm"
	"ike/internal/wasm/abi"
)

// The parity suite (#25 acceptance): the same capability set registered once
// as a compile-in plugin and once via a real WASM guest must be
// indistinguishable through the registry, and invoking either side must drive
// the same observable host effects.

var (
	guestOnce sync.Once
	guestPath string
	guestErr  string
)

// guestModule compiles the parity guest once: it declares the same
// capabilities as the compile-in twin below and answers every entry point
// with a notify carrying the entry point's identity.
func guestModule(t *testing.T) string {
	t.Helper()
	guestOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ike-bridge-guest")
		if err != nil {
			guestErr = err.Error()
			return
		}
		src := `package main

import "unsafe"

//go:wasmimport ike notify
func hostNotify(ptr, len uint32)

var buffers [][]byte

//go:wasmexport ike_alloc
func ikeAlloc(size uint32) uint32 {
	buf := make([]byte, size)
	buffers = append(buffers, buf)
	return uint32(uintptr(unsafe.Pointer(&buf[0])))
}

func regionOf(b []byte) (uint32, uint32) {
	if len(b) == 0 {
		return 0, 0
	}
	return uint32(uintptr(unsafe.Pointer(&b[0]))), uint32(len(b))
}

var capsJSON = []byte(` + "`" + `{"name":"parity","commands":[{"id":"parity.hello","title":"Parity: Hello"},{"id":"parity.ed","title":"Parity: Editor","context":"editor"}],"keymaps":[{"keys":"ctrl+k p","command_id":"parity.hello"}],"hooks":[{"id":"parity.opened","event":"file_opened"}]}` + "`" + `)

//go:wasmexport register
func register() uint64 {
	ptr, n := regionOf(capsJSON)
	return uint64(ptr)<<32 | uint64(n)
}

func readGuest(ptr, n uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n)
}

func note(text string) {
	msg := []byte(` + "`" + `{"severity":0,"text":"` + "`" + ` + text + ` + "`" + `"}` + "`" + `)
	p, l := regionOf(msg)
	hostNotify(p, l)
}

//go:wasmexport on_command
func onCommand(ptr, n uint32) { note("ran " + string(readGuest(ptr, n))) }

//go:wasmexport on_key
func onKey(ptr, n uint32) { note("key " + string(readGuest(ptr, n))) }

//go:wasmexport on_hook
func onHook(ptr, n, pptr, plen uint32) {
	note("hook " + string(readGuest(ptr, n)) + " payload_bytes=" + itoa(int(plen)))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func main() {}
`
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
			guestErr = err.Error()
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module guest\n\ngo 1.24\n"), 0o644); err != nil {
			guestErr = err.Error()
			return
		}
		guestPath = filepath.Join(dir, "parity.wasm")
		cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", guestPath, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if out, err := cmd.CombinedOutput(); err != nil {
			guestErr = err.Error() + "\n" + string(out)
		}
	})
	if guestErr != "" {
		t.Fatalf("guest build: %s", guestErr)
	}
	return guestPath
}

// compileInTwin is the compile-in equivalent of the WASM parity guest: the
// exact same capability set, with callbacks producing the same notifications.
type compileInTwin struct{}

func (compileInTwin) ID() string { return "parity" }

func (compileInTwin) Capabilities() plugin.Capabilities {
	note := func(text string) func(h host.API) tea.Cmd {
		return func(h host.API) tea.Cmd {
			return func() tea.Msg { h.Notify(host.Info, text); return nil }
		}
	}
	return plugin.Capabilities{
		Commands: []plugin.Command{
			{ID: "parity.hello", Title: "Parity: Hello", Scope: plugin.GlobalScope(), Run: note("ran parity.hello")},
			{ID: "parity.ed", Title: "Parity: Editor", Scope: plugin.PaneScope("editor"), Run: note("ran parity.ed")},
		},
		Keymaps: []plugin.Keymap{
			{Keys: "ctrl+k p", Scope: plugin.GlobalScope(), CommandID: "parity.hello", Priority: plugin.CorePriority, Action: note("key parity.hello")},
		},
		Hooks: []plugin.Hook{
			{ID: "parity.opened", Event: plugin.EventFileOpened, Notify: func(h host.API, payload any) tea.Cmd {
				return func() tea.Msg {
					// Mirror what the guest observes: the bridge hands hooks
					// their payload as JSON bytes.
					data, _ := json.Marshal(payload)
					h.Notify(host.Info, "hook parity.opened payload_bytes="+strconv.Itoa(len(data)))
					return nil
				}
			}},
		},
	}
}

// wasmRegistry loads the parity guest through the full production path —
// runtime, host module, bridge — into a fresh registry backed by h.
func wasmRegistry(t *testing.T, h host.API) (*registry.Registry, *HostAdapter) {
	t.Helper()
	ctx := context.Background()
	rt := wasm.NewRuntime(ctx, nil)
	t.Cleanup(rt.Close)
	adapter := NewHostAdapter()
	adapter.SetAPI(h)
	if err := abi.InstantiateHost(ctx, rt.Engine(), adapter); err != nil {
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
	if diags := rt.ScanDir(dir).Diagnostics; len(diags) > 0 {
		t.Fatalf("scan diags: %v", diags)
	}
	reg := registry.New()
	if diags := RegisterModules(ctx, rt, reg); len(diags) > 0 {
		t.Fatalf("register diags: %v", diags)
	}
	return reg, adapter
}

// runCmd executes a capability callback synchronously and drains the host's
// notification queue: guest callbacks run inside the returned tea.Cmd.
func runCmd(t *testing.T, h *host.Host, cmd tea.Cmd) []host.Notification {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	cmd()
	return h.DrainNotifications()
}

func TestParityThroughRegistry(t *testing.T) {
	h := host.New(host.MapConfig{})
	wasmReg, _ := wasmRegistry(t, h)
	nativeReg := registry.New()
	nativeReg.Add(compileInTwin{})

	// The registry views must be structurally identical.
	wc, nc := wasmReg.Commands(), nativeReg.Commands()
	if len(wc) != len(nc) {
		t.Fatalf("command count: wasm %d native %d", len(wc), len(nc))
	}
	for i := range wc {
		if wc[i].Owner != nc[i].Owner || wc[i].ID != nc[i].ID || wc[i].Title != nc[i].Title || wc[i].Scope != nc[i].Scope {
			t.Fatalf("command %d differs: wasm %+v native %+v", i, wc[i], nc[i])
		}
	}
	wk, nk := wasmReg.Keymaps(), nativeReg.Keymaps()
	if len(wk) != 1 || len(nk) != 1 ||
		wk[0].Keys != nk[0].Keys || wk[0].CommandID != nk[0].CommandID ||
		wk[0].Scope != nk[0].Scope || wk[0].Priority != nk[0].Priority {
		t.Fatalf("keymaps differ: wasm %+v native %+v", wk, nk)
	}
	// The binding lookup the help sheet uses resolves identically.
	wb, wok := wasmReg.Binding("parity.hello")
	nb, nok := nativeReg.Binding("parity.hello")
	if !wok || !nok || wb != nb {
		t.Fatalf("binding: wasm %q(%v) native %q(%v)", wb, wok, nb, nok)
	}
	wh, nh := wasmReg.Hooks(plugin.EventFileOpened), nativeReg.Hooks(plugin.EventFileOpened)
	if len(wh) != 1 || len(nh) != 1 || wh[0].Owner != nh[0].Owner || wh[0].ID != nh[0].ID {
		t.Fatalf("hooks differ: wasm %+v native %+v", wh, nh)
	}
	// Context scoping resolves identically.
	if len(wasmReg.CommandsForContext("editor")) != len(nativeReg.CommandsForContext("editor")) {
		t.Fatal("context-scoped command sets differ")
	}
}

func TestParityOfBehavior(t *testing.T) {
	h := host.New(host.MapConfig{})
	wasmReg, _ := wasmRegistry(t, h)
	nativeReg := registry.New()
	nativeReg.Add(compileInTwin{})

	observe := func(reg *registry.Registry) []string {
		var out []string
		for _, id := range []string{"parity.hello", "parity.ed"} {
			c, ok := reg.Command(id)
			if !ok {
				t.Fatalf("command %s missing", id)
			}
			for _, n := range runCmd(t, h, c.Run(h)) {
				out = append(out, n.Text)
			}
		}
		km := reg.Keymaps()[0]
		for _, n := range runCmd(t, h, km.Action(h)) {
			out = append(out, n.Text)
		}
		hook := reg.Hooks(plugin.EventFileOpened)[0]
		for _, n := range runCmd(t, h, hook.Notify(h, "somefile.go")) {
			out = append(out, n.Text)
		}
		return out
	}

	wasmSeen := observe(wasmReg)
	nativeSeen := observe(nativeReg)
	if strings.Join(wasmSeen, "|") != strings.Join(nativeSeen, "|") {
		t.Fatalf("behavior differs:\nwasm:   %v\nnative: %v", wasmSeen, nativeSeen)
	}
	want := []string{"ran parity.hello", "ran parity.ed", "key parity.hello", "hook parity.opened payload_bytes=13"}
	if strings.Join(wasmSeen, "|") != strings.Join(want, "|") {
		t.Fatalf("observed %v want %v", wasmSeen, want)
	}
}

func TestRegisterModulesSkipsBareAndFaultyModules(t *testing.T) {
	ctx := context.Background()
	rt := wasm.NewRuntime(ctx, nil)
	t.Cleanup(rt.Close)
	dir := t.TempDir()
	// A bare module (no register export) contributes nothing but stays loaded.
	empty := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	if err := os.WriteFile(filepath.Join(dir, "bare.wasm"), empty, 0o644); err != nil {
		t.Fatal(err)
	}
	if diags := rt.ScanDir(dir).Diagnostics; len(diags) > 0 {
		t.Fatalf("scan diags: %v", diags)
	}
	reg := registry.New()
	if diags := RegisterModules(ctx, rt, reg); len(diags) > 0 {
		t.Fatalf("diags: %v", diags)
	}
	if got := reg.PluginIDs(); len(got) != 0 {
		t.Fatalf("bare module registered plugins: %v", got)
	}
	if len(rt.Modules()) != 1 {
		t.Fatal("bare module should remain loaded")
	}
}
