package abi

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// host.go exports the "ike" import module: thin marshalling shims mirroring
// host.API. The Host interface is deliberately narrow and free of IKE
// imports, so the ABI tests stand alone; the bridge (#25) adapts the real
// host.API onto it.

// Host is what the shims call into on the host side.
type Host interface {
	// OpenFile asks the IDE to open path (host.API.OpenFile).
	OpenFile(path string)
	// Dispatch delivers a named message envelope (host.API.Dispatch after
	// the bridge maps the type).
	Dispatch(env DispatchEnvelope)
	// Notify raises a toast (host.API.Notify).
	Notify(n Notification)
	// SetStatus replaces the plugin's status-line segment (host.API.SetStatus).
	SetStatus(text string)
	// ConfigGet reads one dotted config key (host.API.Config().Get).
	ConfigGet(key string) (string, bool)
}

// Gate decides per call whether a module may use a host capability
// (capability names mirror the manifest's: "open_file", "dispatch",
// "notify", "set_status", "config_get"). A denied call is dropped — the
// guest observes a no-op (config_get reports the key absent). nil allows
// everything.
type Gate func(module, capability string) bool

// InstantiateHost registers the "ike" module on rt with no gating. Call it
// before any guest module loads, so guest imports resolve. Malformed guest
// payloads are dropped (a plugin cannot crash the host with garbage bytes).
func InstantiateHost(ctx context.Context, rt wazero.Runtime, host Host) error {
	return InstantiateHostGated(ctx, rt, host, nil)
}

// InstantiateHostGated is InstantiateHost with per-module capability gating
// (#27): every shim consults allow(moduleName, capability) first.
func InstantiateHostGated(ctx context.Context, rt wazero.Runtime, host Host, allow Gate) error {
	builder := rt.NewHostModuleBuilder(HostModule)

	permitted := func(mod api.Module, capability string) bool {
		return allow == nil || allow(mod.Name(), capability)
	}
	readBytes := func(mod api.Module, ptr, length uint32) ([]byte, bool) {
		if length == 0 {
			return nil, true
		}
		return mod.Memory().Read(ptr, length)
	}

	builder.NewFunctionBuilder().
		WithFunc(func(_ context.Context, mod api.Module, ptr, length uint32) {
			if !permitted(mod, "open_file") {
				return
			}
			if b, ok := readBytes(mod, ptr, length); ok && len(b) > 0 {
				host.OpenFile(string(b))
			}
		}).Export("open_file")

	builder.NewFunctionBuilder().
		WithFunc(func(_ context.Context, mod api.Module, ptr, length uint32) {
			if !permitted(mod, "dispatch") {
				return
			}
			b, ok := readBytes(mod, ptr, length)
			if !ok {
				return
			}
			if env, err := DecodeDispatch(b); err == nil {
				host.Dispatch(env)
			}
		}).Export("dispatch")

	builder.NewFunctionBuilder().
		WithFunc(func(_ context.Context, mod api.Module, ptr, length uint32) {
			if !permitted(mod, "notify") {
				return
			}
			b, ok := readBytes(mod, ptr, length)
			if !ok {
				return
			}
			if n, err := DecodeNotification(b); err == nil {
				host.Notify(n)
			}
		}).Export("notify")

	builder.NewFunctionBuilder().
		WithFunc(func(_ context.Context, mod api.Module, ptr, length uint32) {
			if !permitted(mod, "set_status") {
				return
			}
			if b, ok := readBytes(mod, ptr, length); ok {
				host.SetStatus(string(b))
			}
		}).Export("set_status")

	// config_get writes the value into a guest buffer obtained through
	// ike_alloc and returns the packed (ptr, len); 0 means key absent.
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr, length uint32) uint64 {
			if !permitted(mod, "config_get") {
				return 0
			}
			b, ok := readBytes(mod, ptr, length)
			if !ok {
				return 0
			}
			value, found := host.ConfigGet(string(b))
			if !found {
				return 0
			}
			region, err := AllocAndWrite(ctx, mod, []byte(value))
			if err != nil {
				return 0
			}
			return region
		}).Export("config_get")

	_, err := builder.Instantiate(ctx)
	return err
}
