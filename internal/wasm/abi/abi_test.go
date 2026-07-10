package abi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	wasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// --- pure round-trips: every payload shape ---

func TestCapabilitiesRoundTrip(t *testing.T) {
	in := Capabilities{
		Name: "demo",
		Commands: []CommandDesc{
			{ID: "demo.hello", Title: "Demo: Hello"},
			{ID: "demo.ed", Title: "Editor thing", Context: "editor"},
		},
		Keymaps: []KeymapDesc{{Keys: "ctrl+k h", CommandID: "demo.hello"}},
		Hooks:   []HookDesc{{ID: "demo.opened", Event: "file_opened"}},
	}
	data, err := EncodeCapabilities(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeCapabilities(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || len(out.Commands) != 2 || out.Commands[1].Context != "editor" ||
		out.Keymaps[0].Keys != "ctrl+k h" || out.Hooks[0].Event != "file_opened" {
		t.Fatalf("round trip lost data: %+v", out)
	}
	// Unknown fields are tolerated (forward compatibility across ABI versions).
	if _, err := DecodeCapabilities([]byte(`{"name":"x","future_field":[1,2]}`)); err != nil {
		t.Fatal(err)
	}
	if c, err := DecodeCapabilities(nil); err != nil || c.Name != "" {
		t.Fatalf("empty payload should decode to zero caps: %+v %v", c, err)
	}
}

func TestNotificationAndDispatchRoundTrip(t *testing.T) {
	nd, err := EncodeNotification(Notification{Severity: 2, Text: "boom"})
	if err != nil {
		t.Fatal(err)
	}
	n, err := DecodeNotification(nd)
	if err != nil || n.Severity != 2 || n.Text != "boom" {
		t.Fatalf("notification = %+v err=%v", n, err)
	}

	dd, err := EncodeDispatch(DispatchEnvelope{Type: "open_settings", Payload: json.RawMessage(`{"page":"Plugins"}`)})
	if err != nil {
		t.Fatal(err)
	}
	d, err := DecodeDispatch(dd)
	if err != nil || d.Type != "open_settings" || !strings.Contains(string(d.Payload), "Plugins") {
		t.Fatalf("dispatch = %+v err=%v", d, err)
	}
	if _, err := DecodeDispatch([]byte(`{"payload":{}}`)); err == nil {
		t.Fatal("a dispatch without a type must be rejected")
	}
}

func TestPackUnpack(t *testing.T) {
	for _, c := range [][2]uint32{{0, 0}, {1, 1}, {0xFFFF_FFFF, 0xFFFF_FFFF}, {1 << 20, 42}} {
		ptr, l := UnpackPtrLen(PackPtrLen(c[0], c[1]))
		if ptr != c[0] || l != c[1] {
			t.Fatalf("pack/unpack lost (%d,%d) -> (%d,%d)", c[0], c[1], ptr, l)
		}
	}
}

// --- full contract against a real Go guest ---

// recorder implements Host and records every shim call.
type recorder struct {
	opened   []string
	notes    []Notification
	statuses []string
	dispatch []DispatchEnvelope
	config   map[string]string
}

func (r *recorder) OpenFile(p string)           { r.opened = append(r.opened, p) }
func (r *recorder) Dispatch(d DispatchEnvelope) { r.dispatch = append(r.dispatch, d) }
func (r *recorder) Notify(n Notification)       { r.notes = append(r.notes, n) }
func (r *recorder) SetStatus(s string)          { r.statuses = append(r.statuses, s) }
func (r *recorder) ConfigGet(k string) (string, bool) {
	v, ok := r.config[k]
	return v, ok
}

var (
	guestOnce sync.Once
	guestPath string
	guestErr  string
)

// guestModule compiles the reference guest once: it registers a command and
// a hook, and answers on_command/on_hook by calling back through every host
// shim — the whole contract in one module.
func guestModule(t *testing.T) string {
	t.Helper()
	guestOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ike-abi-guest")
		if err != nil {
			guestErr = err.Error()
			return
		}
		src := `package main

import "unsafe"

//go:wasmimport ike notify
func hostNotify(ptr, len uint32)

//go:wasmimport ike set_status
func hostSetStatus(ptr, len uint32)

//go:wasmimport ike open_file
func hostOpenFile(ptr, len uint32)

//go:wasmimport ike dispatch
func hostDispatch(ptr, len uint32)

//go:wasmimport ike config_get
func hostConfigGet(ptr, len uint32) uint64

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

var capsJSON = []byte(` + "`" + `{"name":"guest","commands":[{"id":"guest.hello","title":"Guest: Hello"}],"hooks":[{"id":"guest.opened","event":"file_opened"}]}` + "`" + `)

//go:wasmexport register
func register() uint64 {
	ptr, n := regionOf(capsJSON)
	return uint64(ptr)<<32 | uint64(n)
}

func readGuest(ptr, n uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n)
}

//go:wasmexport on_command
func onCommand(ptr, n uint32) {
	id := string(readGuest(ptr, n))
	// Answer through every shim: a note, a status, an open, a dispatch, and
	// a config read echoed back as a second note.
	note := []byte(` + "`" + `{"severity":0,"text":"ran ` + "`" + ` + id + ` + "`" + `"}` + "`" + `)
	p, l := regionOf(note)
	hostNotify(p, l)
	status := []byte("guest ok")
	p, l = regionOf(status)
	hostSetStatus(p, l)
	file := []byte("/tmp/from-guest.txt")
	p, l = regionOf(file)
	hostOpenFile(p, l)
	env := []byte(` + "`" + `{"type":"ping","payload":{"n":1}}` + "`" + `)
	p, l = regionOf(env)
	hostDispatch(p, l)
	key := []byte("editor.tab_width")
	p, l = regionOf(key)
	packed := hostConfigGet(p, l)
	vptr, vlen := uint32(packed>>32), uint32(packed)
	echo := []byte(` + "`" + `{"severity":1,"text":"tab_width=` + "`" + ` + string(readGuest(vptr, vlen)) + ` + "`" + `"}` + "`" + `)
	p, l = regionOf(echo)
	hostNotify(p, l)
}

//go:wasmexport on_key
func onKey(ptr, n uint32) { onCommand(ptr, n) }

//go:wasmexport on_hook
func onHook(ptr, n, pptr, plen uint32) {
	// The payload is raw JSON; embedding it verbatim inside a JSON string
	// would be malformed (the host drops garbage by design), so report its
	// presence structurally instead.
	payload := readGuest(pptr, plen)
	msg := []byte(` + "`" + `{"severity":0,"text":"hook ` + "`" + ` + string(readGuest(ptr, n)) + ` + "`" + ` payload_bytes=` + "`" + ` + itoa(len(payload)) + ` + "`" + `"}` + "`" + `)
	p, l := regionOf(msg)
	hostNotify(p, l)
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
		guestPath = filepath.Join(dir, "guest.wasm")
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

// instantiateGuest wires a fresh runtime: host module first, then the guest.
func instantiateGuest(t *testing.T, host Host) api.Module {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = rt.Close(ctx) })
	wasi.MustInstantiate(ctx, rt)
	if err := InstantiateHost(ctx, rt, host); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(guestModule(t))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.InstantiateWithConfig(ctx, data,
		wazero.NewModuleConfig().WithName("guest").WithStartFunctions("_initialize"))
	if err != nil {
		t.Fatal(err)
	}
	return mod
}

func TestFullContractAgainstRealGuest(t *testing.T) {
	rec := &recorder{config: map[string]string{"editor.tab_width": "4"}}
	mod := instantiateGuest(t, rec)
	ctx := context.Background()

	// register(): the guest declares its capabilities.
	caps, err := Register(ctx, mod)
	if err != nil {
		t.Fatal(err)
	}
	if caps == nil || caps.Name != "guest" || len(caps.Commands) != 1 || caps.Commands[0].ID != "guest.hello" {
		t.Fatalf("caps = %+v", caps)
	}
	if len(caps.Hooks) != 1 || caps.Hooks[0].Event != "file_opened" {
		t.Fatalf("hooks = %+v", caps.Hooks)
	}

	// on_command: the guest answers through every host shim.
	if err := CallCommand(ctx, mod, "guest.hello"); err != nil {
		t.Fatal(err)
	}
	if len(rec.notes) != 2 || rec.notes[0].Text != "ran guest.hello" {
		t.Fatalf("notes = %+v", rec.notes)
	}
	if rec.notes[1].Text != "tab_width=4" || rec.notes[1].Severity != 1 {
		t.Fatalf("config echo = %+v", rec.notes[1])
	}
	if len(rec.statuses) != 1 || rec.statuses[0] != "guest ok" {
		t.Fatalf("statuses = %+v", rec.statuses)
	}
	if len(rec.opened) != 1 || rec.opened[0] != "/tmp/from-guest.txt" {
		t.Fatalf("opened = %+v", rec.opened)
	}
	if len(rec.dispatch) != 1 || rec.dispatch[0].Type != "ping" {
		t.Fatalf("dispatch = %+v", rec.dispatch)
	}

	// on_key shares the path.
	if err := CallKey(ctx, mod, "guest.hello"); err != nil {
		t.Fatal(err)
	}

	// on_hook carries id + payload.
	if err := CallHook(ctx, mod, "guest.opened", []byte(`{"path":"x.go"}`)); err != nil {
		t.Fatal(err)
	}
	last := rec.notes[len(rec.notes)-1]
	if !strings.Contains(last.Text, "hook guest.opened") || !strings.Contains(last.Text, "payload_bytes=15") {
		t.Fatalf("hook note = %+v", last)
	}
}

func TestRegisterWithoutExportIsNil(t *testing.T) {
	ctx := context.Background()
	rt := wazero.NewRuntime(ctx)
	t.Cleanup(func() { _ = rt.Close(ctx) })
	// Smallest valid module: no exports at all.
	mod, err := rt.Instantiate(ctx, []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	if err != nil {
		t.Fatal(err)
	}
	caps, err := Register(ctx, mod)
	if err != nil || caps != nil {
		t.Fatalf("caps=%v err=%v", caps, err)
	}
	if err := CallCommand(ctx, mod, "x"); err == nil {
		t.Fatal("calling a missing entry point must error")
	}
}
