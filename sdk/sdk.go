// Package sdk is the typed guest-side SDK for IKE WASM plugins (Roadmap
// 9900, #26). It wraps the raw ABI (see internal/wasm/abi in the IKE repo):
// plugin authors declare commands, keymaps, and hooks as Go callbacks and
// call typed host functions (Notify, OpenFile, ConfigGet, …) — the SDK owns
// the wasm exports, the guest allocator, and the JSON marshalling.
//
// A minimal plugin:
//
//	package main
//
//	import "ike/sdk"
//
//	func init() {
//		sdk.SetName("hello")
//		sdk.Command("hello.greet", "Hello: Greet", func() {
//			sdk.Notify(sdk.Info, "hello from wasm")
//		})
//	}
//
//	func main() {}
//
// Build with the standard Go toolchain (1.24+):
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o hello.wasm .
//
// and drop the .wasm into IKE's plugins directory ($IKE_CONFIG_DIR/plugins,
// else ~/.ike/plugins). -buildmode=c-shared produces a WASI reactor module
// whose exports stay callable after initialization — the shape IKE loads.
// This package compiles only for wasip1 (it declares wasm imports).
package sdk

import (
	"encoding/json"
	"strings"
)

// Severity levels for Notify, mirroring the host's toast severities.
// Info and Warn toasts expire on their own; Error toasts persist.
type Severity int

const (
	Info Severity = iota
	Warn
	Error
)

// Hook events a plugin can subscribe to. The payload passed to the callback
// is event-specific JSON (e.g. the file path for FileOpened).
const (
	FileOpened   = "file_opened"
	BufferSaved  = "buffer_saved"
	BufferClosed = "buffer_closed"
	// CommandExecuted fires when a registered command is dispatched (palette,
	// keybinding, or internal invocation); the payload is the command id.
	CommandExecuted = "command_executed"
)

// capabilities mirrors the ABI's registration payload (abi.Capabilities).
type capabilities struct {
	Name     string        `json:"name,omitempty"`
	Commands []commandDesc `json:"commands,omitempty"`
	Keymaps  []keymapDesc  `json:"keymaps,omitempty"`
	Hooks    []hookDesc    `json:"hooks,omitempty"`
}

type commandDesc struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Context string `json:"context,omitempty"`
}

type keymapDesc struct {
	Keys      string `json:"keys"`
	CommandID string `json:"command_id,omitempty"`
	Context   string `json:"context,omitempty"`
}

type hookDesc struct {
	ID    string `json:"id"`
	Event string `json:"event"`
}

// plugin is the package-level registry — one wasm module is one plugin, so a
// package-level singleton keeps the author-facing API free of plumbing.
var plugin struct {
	name     string
	commands []commandDesc
	keymaps  []keymapDesc
	hooks    []hookDesc
	onCmd    map[string]func()
	onKey    map[string]func() // keyed by the id the host sends (see Keymap)
	onHook   map[string]func(payload []byte)
}

// SetName declares the plugin's id. IKE falls back to the .wasm file's base
// name when unset; setting it keeps the id stable across renames.
func SetName(name string) { plugin.name = name }

// Command declares a globally available palette command backed by fn.
func Command(id, title string, fn func()) { CommandIn(id, title, "", fn) }

// CommandIn is Command scoped to a pane context ("editor", "explorer", …);
// an empty context means global.
func CommandIn(id, title, context string, fn func()) {
	plugin.commands = append(plugin.commands, commandDesc{ID: id, Title: title, Context: context})
	if plugin.onCmd == nil {
		plugin.onCmd = map[string]func(){}
	}
	plugin.onCmd[id] = fn
}

// Keymap binds a key sequence (bubbletea syntax, e.g. "ctrl+k h") to a
// declared command: pressing it runs that command's callback, and IKE's help
// sheet shows the binding next to the command.
func Keymap(keys, commandID string) { KeymapIn(keys, commandID, "") }

// KeymapIn is Keymap scoped to a pane context.
func KeymapIn(keys, commandID, context string) {
	plugin.keymaps = append(plugin.keymaps, keymapDesc{Keys: keys, CommandID: commandID, Context: context})
}

// KeymapFunc binds a key sequence to a standalone callback not tied to any
// command (it will not appear in the palette).
func KeymapFunc(keys string, fn func()) { KeymapFuncIn(keys, "", fn) }

// KeymapFuncIn is KeymapFunc scoped to a pane context.
func KeymapFuncIn(keys, context string, fn func()) {
	plugin.keymaps = append(plugin.keymaps, keymapDesc{Keys: keys, Context: context})
	if plugin.onKey == nil {
		plugin.onKey = map[string]func(){}
	}
	// For a binding without a command id, the host identifies the keymap as
	// "<plugin>.<keys>". The plugin name may still change after this call, so
	// the id is resolved in dispatchKey, not here; keys is the stable part.
	plugin.onKey[keys] = fn
}

// Hook subscribes fn to a lifecycle event (FileOpened, BufferSaved,
// BufferClosed). id must be unique within the plugin; payload is the
// event-specific JSON the host emits.
func Hook(id, event string, fn func(payload []byte)) {
	plugin.hooks = append(plugin.hooks, hookDesc{ID: id, Event: event})
	if plugin.onHook == nil {
		plugin.onHook = map[string]func([]byte){}
	}
	plugin.onHook[id] = fn
}

// capsJSON encodes the declared capability set for the register export.
func capsJSON() []byte {
	data, err := json.Marshal(capabilities{
		Name:     plugin.name,
		Commands: plugin.commands,
		Keymaps:  plugin.keymaps,
		Hooks:    plugin.hooks,
	})
	if err != nil {
		return nil
	}
	return data
}

// dispatchCommand routes on_command/on_key command invocations.
func dispatchCommand(id string) {
	if fn := plugin.onCmd[id]; fn != nil {
		fn()
	}
}

// dispatchKey routes on_key. The host sends the binding's command id when it
// has one (the binding aliases a command), else "<plugin>.<keys>" for
// standalone KeymapFunc bindings — matched by the ".<keys>" suffix so the
// routing also works when the plugin id fell back to the .wasm file name.
func dispatchKey(id string) {
	if fn := plugin.onCmd[id]; fn != nil {
		fn()
		return
	}
	for keys, fn := range plugin.onKey {
		if strings.HasSuffix(id, "."+keys) {
			fn()
			return
		}
	}
}

// dispatchHook routes on_hook.
func dispatchHook(id string, payload []byte) {
	if fn := plugin.onHook[id]; fn != nil {
		fn(payload)
	}
}
