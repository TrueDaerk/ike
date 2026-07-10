// Package wasm embeds the wazero runtime (pure Go, no CGo) for
// runtime-loadable plugins (Roadmap 9900, #23): module load / instantiate /
// unload lifecycle plus the plugins-directory scan. This slice owns only the
// lifecycle — the host↔guest ABI (#24) and the capability bridge into the
// plugin registry (#25) build on top; until they land a loaded module simply
// sits instantiated.
//
// Safety posture (#27): modules get WASI with no preopened filesystem, no
// environment and no program arguments — no ambient FS or network. Each
// module's memory is capped, every guest call runs under a deadline that
// closes a runaway module, and a faulting module (bad bytes, missing
// imports, a trap or timeout) is isolated and unloaded while IKE stays up.
// An optional sidecar manifest (manifest.go) narrows a module's capabilities
// further.
package wasm

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	wasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// Sandbox defaults (#27). 1024 wasm pages of 64 KiB = 64 MiB — comfortable
// for Go-runtime guests, far below anything that could starve the editor.
const (
	DefaultMemoryLimitPages uint32 = 1024
	DefaultCallTimeout             = 5 * time.Second
)

// Options tune the sandbox limits; the zero value means the defaults above.
type Options struct {
	// MemoryLimitPages caps every module's linear memory, in 64 KiB pages.
	MemoryLimitPages uint32
	// CallTimeout bounds each guest call (start functions, register, and
	// every callback). A call exceeding it closes the module — runaway loops
	// cannot freeze IKE.
	CallTimeout time.Duration
}

// Module is one loaded plugin module.
type Module struct {
	Name string // file base name without .wasm — the plugin's identity
	Path string
	// Manifest is the validated sidecar manifest, nil when the module ships
	// without one (nil grants all capabilities; see manifest.go).
	Manifest *Manifest
	mod      api.Module
}

// Runtime owns the wazero engine and the loaded module set.
type Runtime struct {
	ctx     context.Context
	rt      wazero.Runtime
	timeout time.Duration

	mu      sync.Mutex
	modules map[string]*Module
	stdout  io.Writer // guest stdout/stderr sink; io.Discard by default
}

// NewRuntime builds an engine with the default sandbox limits. Guest
// stdout/stderr go to out (nil discards), so a chatty module cannot corrupt
// the TUI frame.
func NewRuntime(ctx context.Context, out io.Writer) *Runtime {
	return NewRuntimeWith(ctx, out, Options{})
}

// NewRuntimeWith is NewRuntime with explicit sandbox limits.
func NewRuntimeWith(ctx context.Context, out io.Writer, opts Options) *Runtime {
	if out == nil {
		out = io.Discard
	}
	if opts.MemoryLimitPages == 0 {
		opts.MemoryLimitPages = DefaultMemoryLimitPages
	}
	if opts.CallTimeout == 0 {
		opts.CallTimeout = DefaultCallTimeout
	}
	// CloseOnContextDone is what makes CallContext deadlines effective: an
	// in-flight guest call is aborted (and the module closed) when its
	// context expires, instead of looping forever.
	cfg := wazero.NewRuntimeConfig().
		WithMemoryLimitPages(opts.MemoryLimitPages).
		WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	// WASI without preopens: TinyGo/Go guests need the import surface, but
	// they get no filesystem, no env, no args — capabilities arrive only
	// through host functions (#24).
	wasi.MustInstantiate(ctx, rt)
	return &Runtime{ctx: ctx, rt: rt, timeout: opts.CallTimeout, modules: map[string]*Module{}, stdout: out}
}

// CallContext returns the deadline-bound context every guest call must run
// under. The caller must cancel it.
func (r *Runtime) CallContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.ctx, r.timeout)
}

// Allows reports whether the named module's manifest grants a capability.
// Unknown modules and modules without a manifest are unrestricted (the
// manifest narrows, it does not exist to widen). The abi host gate and the
// bridge's registration gating consult this.
func (r *Runtime) Allows(module, capability string) bool {
	r.mu.Lock()
	m := r.modules[module]
	r.mu.Unlock()
	if m == nil {
		return true
	}
	return m.Manifest.Allows(capability)
}

// Load compiles and instantiates the module at path. Any failure — unreadable
// file, invalid bytes, unresolvable imports, a trap while the module's start
// function runs — leaves the runtime and every other module untouched.
func (r *Runtime) Load(path string) (*Module, error) {
	name := strings.TrimSuffix(filepath.Base(path), ".wasm")
	r.mu.Lock()
	if _, dup := r.modules[name]; dup {
		r.mu.Unlock()
		return nil, fmt.Errorf("wasm: module %q already loaded", name)
	}
	r.mu.Unlock()

	// A present-but-invalid manifest rejects the module before any bytes are
	// instantiated (#27); a missing sidecar is fine (nil = unrestricted).
	manifest, err := loadManifest(path, name)
	if err != nil {
		return nil, fmt.Errorf("wasm: %s: %w", name, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("wasm: read %s: %w", path, err)
	}
	compiled, err := r.rt.CompileModule(r.ctx, data)
	if err != nil {
		return nil, fmt.Errorf("wasm: compile %s: %w", name, err)
	}
	// Support both WASI conventions: command modules run _start (and usually
	// exit — fine for run-once tools), reactor modules (Go's wasip1
	// c-shared, TinyGo's default) initialize via _initialize and keep their
	// exports callable — the shape plugins use.
	cfg := wazero.NewModuleConfig().
		WithName(name).
		WithStdout(r.stdout).
		WithStderr(r.stdout).
		WithStartFunctions() // start functions run explicitly below
	mod, err := r.rt.InstantiateModule(r.ctx, compiled, cfg)
	if err != nil {
		if mod != nil {
			_ = mod.Close(r.ctx)
		}
		return nil, fmt.Errorf("wasm: instantiate %s: %w", name, err)
	}
	for _, start := range []string{"_initialize", "_start"} {
		fn := mod.ExportedFunction(start)
		if fn == nil {
			continue
		}
		// The start function runs under the call deadline too (#27): a module
		// looping in _initialize must not hang IKE's startup.
		callCtx, cancel := r.CallContext()
		_, err := fn.Call(callCtx)
		cancel()
		if err != nil {
			// A clean proc_exit(0) from a command module is a normal end of
			// its start function, not a fault.
			if exitErr, ok := err.(*sys.ExitError); ok && exitErr.ExitCode() == 0 {
				break
			}
			_ = mod.Close(r.ctx)
			return nil, fmt.Errorf("wasm: start %s: %w", name, err)
		}
		break
	}

	m := &Module{Name: name, Path: path, Manifest: manifest, mod: mod}
	r.mu.Lock()
	r.modules[name] = m
	r.mu.Unlock()
	return m, nil
}

// Unload closes and forgets the named module; unknown names are a no-op.
func (r *Runtime) Unload(name string) {
	r.mu.Lock()
	m := r.modules[name]
	delete(r.modules, name)
	r.mu.Unlock()
	if m != nil && m.mod != nil {
		_ = m.mod.Close(r.ctx)
	}
}

// Modules lists the loaded modules sorted by name.
func (r *Runtime) Modules() []*Module {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Module, 0, len(r.modules))
	for _, m := range r.modules {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Close unloads every module and releases the engine.
func (r *Runtime) Close() {
	r.mu.Lock()
	mods := r.modules
	r.modules = map[string]*Module{}
	r.mu.Unlock()
	for _, m := range mods {
		if m.mod != nil {
			_ = m.mod.Close(r.ctx)
		}
	}
	_ = r.rt.Close(r.ctx)
}

// ExportedFunction resolves a guest export by name — the seam the ABI (#24)
// calls through. ok is false when the module exports no such function.
func (m *Module) ExportedFunction(name string) (api.Function, bool) {
	fn := m.mod.ExportedFunction(name)
	return fn, fn != nil
}

// API exposes the underlying wazero module for the ABI helpers (#24), which
// operate on api.Module directly (exports plus linear memory).
func (m *Module) API() api.Module { return m.mod }

// Engine exposes the wazero runtime so the host import module ("ike", #24)
// can be instantiated on it before any guest module loads.
func (r *Runtime) Engine() wazero.Runtime { return r.rt }
