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

// InstantiateHost registers the "ike" module on rt. Call it before any guest
// module loads, so guest imports resolve. Malformed guest payloads are
// dropped (a plugin cannot crash the host with garbage bytes).
func InstantiateHost(ctx context.Context, rt wazero.Runtime, host Host) error {
	builder := rt.NewHostModuleBuilder(HostModule)

	readBytes := func(mod api.Module, ptr, length uint32) ([]byte, bool) {
		if length == 0 {
			return nil, true
		}
		return mod.Memory().Read(ptr, length)
	}

	builder.NewFunctionBuilder().
		WithFunc(func(_ context.Context, mod api.Module, ptr, length uint32) {
			if b, ok := readBytes(mod, ptr, length); ok && len(b) > 0 {
				host.OpenFile(string(b))
			}
		}).Export("open_file")

	builder.NewFunctionBuilder().
		WithFunc(func(_ context.Context, mod api.Module, ptr, length uint32) {
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
			if b, ok := readBytes(mod, ptr, length); ok {
				host.SetStatus(string(b))
			}
		}).Export("set_status")

	// config_get writes the value into a guest buffer obtained through
	// ike_alloc and returns the packed (ptr, len); 0 means key absent.
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr, length uint32) uint64 {
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
