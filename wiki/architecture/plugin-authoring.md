---
type: guide
title: Writing WASM Plugins
description: Plugin-author guide — the Go guest SDK, building a .wasm plugin, installing it, and the raw ABI reference for other languages.
resource: sdk/sdk.go
tags: [plugins, wasm, sdk, guide]
timestamp: 2026-07-17T00:00:00Z
---

# Writing WASM Plugins

Roadmap 9900, #26. IKE loads WebAssembly plugins at startup from
`$IKE_CONFIG_DIR/plugins` (else `~/.ike/plugins`). A loaded plugin is
indistinguishable from a compile-in one: its commands appear in the palette,
its keymaps in the help sheet, it shows on the Settings → Plugins page, and
`plugins.<name>.enabled` toggles it. This guide covers the Go SDK (`sdk/`)
and the raw ABI for other languages.

## The Go SDK

Declare capabilities in `init()`, keep `main()` empty:

```go
package main

import "ike/sdk"

func init() {
	sdk.SetName("hello")
	sdk.Command("hello.greet", "Hello: Greet", func() {
		sdk.Notify(sdk.Info, "hello from wasm")
	})
	sdk.Keymap("ctrl+k g", "hello.greet")
	sdk.Hook("hello.opened", sdk.FileOpened, func(payload []byte) { /* JSON */ })
}

func main() {}
```

Declaration API: `SetName`, `Command`/`CommandIn` (context `"editor"`,
`"explorer"`, … scopes to a pane kind; empty = global), `Keymap`/`KeymapIn`
(alias a declared command), `KeymapFunc`/`KeymapFuncIn` (standalone binding),
`Hook` (events `FileOpened`, `BufferSaved`, `BufferClosed`, `CommandExecuted`
— payload: the dispatched command id).

Host calls from any callback: `Notify(sev, text)`, `SetStatus(text)`,
`OpenFile(path)`, `Dispatch(msgType, payload)`, `ConfigGet(key)`.

## Building

Standard Go toolchain, 1.24+ (no TinyGo needed):

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o hello.wasm .
```

`-buildmode=c-shared` produces a WASI **reactor** module whose exports stay
callable after `_initialize` — the shape IKE requires. A default build is a
command module whose `_start` runs and exits, leaving nothing to call.

Install: copy the `.wasm` into the plugins directory and restart IKE — or,
for catalog-published plugins, install from Settings → Marketplace, which
downloads the artifact checksum-verified and writes the manifest sidecar
pinning the catalog's capability list (see
[Plugin Marketplace](./marketplace.md)).
Registration diagnostics (a module that fails to load or whose `register()`
traps) print to stderr at startup; the broken module is skipped, IKE stays up.

`sdk/example/` is a buildable reference plugin exercising the full surface;
`sdk/README.md` carries the same instructions next to the code.

## Sandbox

WASI with no preopened filesystem, no environment, no args — no ambient FS
or network access. Every effect goes through the host calls; guest
stdout/stderr are discarded. Callbacks run off the UI loop: a slow or
faulting guest never freezes the editor. Linear memory is capped (64 MiB
default) and every call runs under a 5 s deadline — a runaway loop closes
the module and IKE unloads it with an error toast; a recoverable trap only
warns.

## Manifest

Optionally ship a sidecar `<plugin>.manifest.json` next to the `.wasm`:

```json
{
  "name": "hello",
  "version": "0.1.0",
  "capabilities": ["commands", "notify"]
}
```

`name` must equal the `.wasm` base name. A present manifest is the
capability ceiling: registration kinds not listed (`commands`, `keymaps`,
`hooks`) are dropped with a startup diagnostic; host calls not listed
(`open_file`, `dispatch`, `notify`, `set_status`, `config_get`) become
no-ops (`ConfigGet` reports the key absent). An invalid manifest —
malformed JSON, missing name/version, unknown or duplicate capability, name
mismatch — rejects the module. Without a manifest the plugin is
unrestricted at the capability level; the resource sandbox above always
applies. Request only what you use.

## ABI reference (other languages)

The contract is JSON over `(ptr, len)` regions of guest linear memory —
nothing assumes Go. A guest must export:

| Export | Signature | Purpose |
|---|---|---|
| `ike_alloc` | `(size u32) → u32` | allocate a buffer the host writes into |
| `register` | `() → u64` | capability JSON, packed `(ptr<<32)\|len` |
| `on_command` | `(ptr, len u32)` | a declared command was invoked (id bytes) |
| `on_key` | `(ptr, len u32)` | a binding fired (command id, else `<plugin>.<keys>`) |
| `on_hook` | `(ptr, len, pptr, plen u32)` | a hook fired (id, JSON payload) |

Host imports live under module `"ike"`: `open_file(ptr,len)`,
`dispatch(ptr,len)` (envelope `{"type","payload"}`; unknown types warn),
`notify(ptr,len)` (`{"severity":0|1|2,"text"}`), `set_status(ptr,len)`,
`config_get(ptr,len) → u64` (packed value region, `0` = absent).

The `register()` capability payload:

```json
{
  "name": "hello",
  "commands": [{"id": "hello.greet", "title": "Hello: Greet", "context": ""}],
  "keymaps":  [{"keys": "ctrl+k g", "command_id": "hello.greet", "context": ""}],
  "hooks":    [{"id": "hello.opened", "event": "file_opened"}]
}
```

Unknown JSON fields are tolerated on both sides (forward compatibility).
Malformed payloads are dropped by the host — garbage bytes cannot crash IKE.
See [Plugin Extension Contract](/architecture/plugins.md) for the host-side
runtime, ABI, and bridge internals.
