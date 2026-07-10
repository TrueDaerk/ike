# IKE WASM Plugin SDK (Go)

Typed guest-side SDK for writing IKE plugins as WebAssembly modules. Plugin
authors declare commands, keymaps, and hooks as Go callbacks; the SDK owns
the wasm exports, the guest allocator, and the JSON marshalling of the raw
ABI (`internal/wasm/abi` in the IKE repository).

## Quick start

```go
package main

import "ike/sdk"

func init() {
	sdk.SetName("hello")
	sdk.Command("hello.greet", "Hello: Greet", func() {
		sdk.Notify(sdk.Info, "hello from wasm")
	})
}

func main() {}
```

Build with the standard Go toolchain (Go 1.24+; no TinyGo required):

```sh
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o hello.wasm .
```

`-buildmode=c-shared` matters: it produces a WASI **reactor** module whose
exports stay callable after initialization — the shape IKE loads. A plain
build produces a command module whose `_start` exits immediately.

Install by copying the `.wasm` into IKE's plugins directory —
`$IKE_CONFIG_DIR/plugins` if set, else `~/.ike/plugins` — and restarting IKE.
Your commands appear in the command palette; the plugin shows up on the
Settings → Plugins page and can be disabled with `plugins.<name>.enabled`.

## API surface

Declaration (call from `init()`):

- `SetName(name)` — stable plugin id (falls back to the `.wasm` file name)
- `Command(id, title, fn)` / `CommandIn(id, title, context, fn)` — palette
  commands; context `"editor"`, `"explorer"`, … scopes to a focused pane kind
- `Keymap(keys, commandID)` / `KeymapIn(...)` — bind a key sequence
  (bubbletea syntax, e.g. `"ctrl+k g"`) to a declared command
- `KeymapFunc(keys, fn)` / `KeymapFuncIn(...)` — standalone binding
- `Hook(id, event, fn)` — lifecycle subscription; events `sdk.FileOpened`,
  `sdk.BufferSaved`, `sdk.BufferClosed`; `fn` receives event-specific JSON

Host calls (from any callback):

- `Notify(sev, text)` — toast (`sdk.Info` / `sdk.Warn` / `sdk.Error`)
- `SetStatus(text)` — persistent status-line segment
- `OpenFile(path)` — open a file in the editor
- `Dispatch(msgType, payload)` — typed message envelope (`"open_file"` today)
- `ConfigGet(key)` — read a dotted config key, e.g. `"editor.tab_width"`

## Sandbox

Plugins run under WASI with **no** preopened filesystem, environment, or
arguments — no ambient FS or network. Everything a plugin can do goes
through the host calls above. Guest stdout/stderr are discarded. Linear
memory is capped (64 MiB by default) and every callback runs under a
deadline (5 s): a runaway loop closes the module and IKE unloads it with an
error toast — the editor never freezes. A trapping callback surfaces as a
warning toast, never a crash.

## Manifest

Ship an optional sidecar `<plugin>.manifest.json` next to the `.wasm` to
declare your plugin's identity and narrow its capabilities:

```json
{
  "name": "hello",
  "version": "0.1.0",
  "capabilities": ["commands", "notify"]
}
```

`name` must equal the `.wasm` file's base name. When a manifest is present,
anything not listed in `capabilities` is denied: undeclared registration
kinds (`commands`, `keymaps`, `hooks`) are dropped with a startup
diagnostic, undeclared host calls (`open_file`, `dispatch`, `notify`,
`set_status`, `config_get`) become no-ops. An invalid manifest (malformed
JSON, missing name/version, unknown capability, name mismatch) rejects the
module entirely. Without a manifest the plugin keeps full capabilities —
the manifest narrows the sandbox, the sandbox itself always applies.

## Example

`example/` is a buildable plugin exercising the full surface. From this
directory:

```sh
cd example
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasm-example.wasm .
cp wasm-example.wasm wasm-example.manifest.json ~/.ike/plugins/
```

## Other languages

The ABI is JSON over `(ptr, len)` regions — nothing assumes Go. Rust or Zig
guests need: exports `ike_alloc` / `register` / `on_command` / `on_key` /
`on_hook`, imports under module `"ike"`, and any JSON library. See the ABI
reference in the IKE wiki (`wiki/architecture/plugins.md`).
