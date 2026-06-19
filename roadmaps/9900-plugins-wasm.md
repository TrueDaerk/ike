# Roadmap 9900 — Plugins: WASM (Runtime, Sandboxed, Cross-language)

Add runtime-loadable, sandboxed plugins on top of the compile-in registry
(Roadmap 0020). A plugin is a `.wasm` file dropped into a plugins directory; IKE
loads it at startup with [wazero](https://github.com/tetratelabs/wazero) (pure
Go, no CGo) and registers its capabilities through the **same** registry +
`host.API` interface defined in Roadmap 0020.

## Prerequisite

Roadmap 0020 complete. This roadmap adds a new *producer* of registry entries; it
must not change the extension-point contract. If it forces contract changes,
that's a signal Roadmap 0020's API was wrong — fix it there.

## What changes vs compile-in

|            | Compile-in (02)          | WASM (99)                                 |
|------------|--------------------------|-------------------------------------------|
| Load       | blank import, build-time | scan dir, runtime                         |
| Producer   | Go `init()` → `Register` | Wasm module exports → bridge → `Register` |
| Host calls | direct Go call           | guest → host-function → `host.API`        |
| Isolation  | none                     | sandbox (memory, no ambient FS/net)       |
| Language   | Go only                  | anything compiling to Wasm                |

## Architecture

```
internal/wasm/          wazero runtime, module load/instantiate lifecycle
internal/wasm/abi/      host↔guest ABI: function signatures + (de)serialization
internal/wasm/bridge/   adapts a loaded module into plugin.Plugin capabilities
sdk/                    guest-side SDK (Go first) so plugin authors target a typed API
```

Flow: scan plugins dir → instantiate each `.wasm` → call its exported
`register()` → bridge translates declared capabilities into `registry.Register`
calls → from then on the module is indistinguishable from a compile-in plugin to
the rest of IKE.

## ABI / bridge

- Wasm passes only numbers/pointers across the boundary. Define a compact
  serialization (length-prefixed bytes in linear memory) for capability
  descriptors, `tea.Msg` payloads, and host-call args/returns.
- Host functions exported to the guest mirror `host.API`: open file, dispatch
  msg, set status, read config — each a thin marshalling shim.
- Guest entry points: `register()` (declare capabilities), `on_command(id)`,
  `on_key(id)`, `on_hook(id, payload)`.

## Sandbox / safety

- No ambient FS or network: capabilities granted explicitly via host functions only.
- Resource limits: memory cap per module, call timeouts / fuel to stop runaway loops.
- Faulting module is isolated and unloaded; IKE stays up.
- Manifest per plugin (name, version, requested capabilities) validated on load.

## Milestones

- [ ] `internal/wasm`: wazero runtime, load/instantiate/unload lifecycle, dir scan.
- [ ] `internal/wasm/abi`: serialization format + host-function signatures.
- [ ] Host-function shims mapping guest calls onto `host.API`.
- [ ] `internal/wasm/bridge`: module capabilities → `registry.Register`.
- [ ] `sdk/` (Go): typed guest SDK + buildable example `.wasm` plugin.
- [ ] Sandbox limits: memory cap, call timeout/fuel, crash isolation.
- [ ] Plugin manifest: parse, validate, capability gating.
- [ ] Tests: load/instantiate, ABI round-trip, sandbox limits, crash isolation, parity with a compile-in equivalent.
- [ ] Wiki: plugin-author guide + ABI reference under `wiki/`.

## Out of scope (later)

Plugin registry/marketplace, signing/verification, hot-reload on file change,
non-Go guest SDKs (Rust/Zig) — though the ABI must not preclude them.
