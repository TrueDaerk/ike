// Package wasm embeds the wazero runtime (pure Go, no CGo) for
// runtime-loadable plugins (Roadmap 9900, #23): module load / instantiate /
// unload lifecycle plus the plugins-directory scan. This slice owns only the
// lifecycle — the host↔guest ABI (#24) and the capability bridge into the
// plugin registry (#25) build on top; until they land a loaded module simply
// sits instantiated.
//
// Safety posture (full sandbox rules are #27): modules get WASI with no
// preopened filesystem, no environment and no program arguments — no ambient
// FS or network — and a faulting module (bad bytes, missing imports, a trap
// during instantiation) is isolated and unloaded while IKE stays up.
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

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	wasi "github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// Module is one loaded plugin module.
type Module struct {
	Name string // file base name without .wasm — the plugin's identity
	Path string
	mod  api.Module
}

// Runtime owns the wazero engine and the loaded module set.
type Runtime struct {
	ctx context.Context
	rt  wazero.Runtime

	mu      sync.Mutex
	modules map[string]*Module
	stdout  io.Writer // guest stdout/stderr sink; io.Discard by default
}

// NewRuntime builds an engine. Guest stdout/stderr go to out (nil discards),
// so a chatty module cannot corrupt the TUI frame.
func NewRuntime(ctx context.Context, out io.Writer) *Runtime {
	if out == nil {
		out = io.Discard
	}
	rt := wazero.NewRuntime(ctx)
	// WASI without preopens: TinyGo/Go guests need the import surface, but
	// they get no filesystem, no env, no args — capabilities arrive only
	// through host functions (#24).
	wasi.MustInstantiate(ctx, rt)
	return &Runtime{ctx: ctx, rt: rt, modules: map[string]*Module{}, stdout: out}
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
		if _, err := fn.Call(r.ctx); err != nil {
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

	m := &Module{Name: name, Path: path, mod: mod}
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
