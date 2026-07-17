// Package abi defines the host↔guest contract for WASM plugins (Roadmap
// 9900, #24). Wasm passes only numbers across the boundary, so every richer
// value crosses as a byte region in the guest's linear memory:
//
//   - Arguments travel as (ptr, len) u32 pairs. Buffers the host must place
//     in guest memory are allocated through the guest's exported
//     `ike_alloc(size) ptr`.
//   - Results returned by the guest travel as one packed u64:
//     (ptr << 32) | len — a single scalar every wasm-targeting language can
//     return without multi-value support.
//   - Payload bytes are JSON. Compact enough for capability descriptors and
//     event payloads, and language-neutral by construction: Rust/Zig guests
//     serialize with any JSON library; nothing here assumes Go on the guest
//     side (the Go SDK, #26, is merely the first client).
//
// Guest entry points (exports):
//
//	ike_alloc(size u32) u32              buffer allocator for host→guest data
//	register() u64                       capability descriptors (JSON, packed ptr/len)
//	on_command(ptr, len u32)             a declared command was invoked (id bytes)
//	on_key(ptr, len u32)                 a declared keymap fired (id bytes)
//	on_hook(ptr, len, pptr, plen u32)    a subscribed hook fired (id, payload)
//
// Host functions (imports under module "ike") mirror host.API as thin
// marshalling shims: open_file, dispatch, notify, set_status, config_get.
package abi

import (
	"encoding/json"
	"fmt"
)

// HostModule is the wasm import-module name the host functions live under.
const HostModule = "ike"

// Guest export names.
const (
	ExportAlloc     = "ike_alloc"
	ExportRegister  = "register"
	ExportOnCommand = "on_command"
	ExportOnKey     = "on_key"
	ExportOnHook    = "on_hook"
)

// Capabilities is the guest's registration payload: the JSON mirror of the
// plugin.Capabilities slice a wasm module may contribute. Kept deliberately
// narrow; later slices extend it additively (unknown fields are ignored on
// both sides).
type Capabilities struct {
	// Name is the plugin's self-declared id; the bridge (#25) namespaces or
	// falls back to the file name when empty.
	Name     string        `json:"name,omitempty"`
	Commands []CommandDesc `json:"commands,omitempty"`
	Keymaps  []KeymapDesc  `json:"keymaps,omitempty"`
	Hooks    []HookDesc    `json:"hooks,omitempty"`
}

// CommandDesc declares one palette-reachable command.
type CommandDesc struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Context scopes the command ("" = global; "editor"/"explorer"/… mirror
	// the pane context ids).
	Context string `json:"context,omitempty"`
}

// KeymapDesc declares one key binding routed back via on_key.
type KeymapDesc struct {
	Keys      string `json:"keys"`
	CommandID string `json:"command_id,omitempty"`
	Context   string `json:"context,omitempty"`
}

// HookDesc subscribes to a lifecycle event routed back via on_hook.
type HookDesc struct {
	ID string `json:"id"`
	// Event names the lifecycle moment: "file_opened", "buffer_saved",
	// "buffer_closed", "command_executed" (mirrors plugin.Event).
	Event string `json:"event"`
}

// Notification is the payload of the notify host call.
type Notification struct {
	// Severity: 0 info, 1 warn, 2 error (mirrors host.Severity).
	Severity int    `json:"severity"`
	Text     string `json:"text"`
}

// DispatchEnvelope is the payload of the dispatch host call: a named message
// with an opaque JSON body. The bridge (#25) maps well-known Type values
// onto concrete tea.Msg shapes; unknown types are rejected, not guessed.
type DispatchEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// EncodeCapabilities / DecodeCapabilities round-trip the registration payload.
func EncodeCapabilities(c Capabilities) ([]byte, error) { return json.Marshal(c) }

func DecodeCapabilities(data []byte) (Capabilities, error) {
	var c Capabilities
	if len(data) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("abi: capabilities: %w", err)
	}
	return c, nil
}

// EncodeNotification / DecodeNotification round-trip a notify payload.
func EncodeNotification(n Notification) ([]byte, error) { return json.Marshal(n) }

func DecodeNotification(data []byte) (Notification, error) {
	var n Notification
	if err := json.Unmarshal(data, &n); err != nil {
		return n, fmt.Errorf("abi: notification: %w", err)
	}
	return n, nil
}

// EncodeDispatch / DecodeDispatch round-trip a dispatch envelope.
func EncodeDispatch(d DispatchEnvelope) ([]byte, error) { return json.Marshal(d) }

func DecodeDispatch(data []byte) (DispatchEnvelope, error) {
	var d DispatchEnvelope
	if err := json.Unmarshal(data, &d); err != nil {
		return d, fmt.Errorf("abi: dispatch: %w", err)
	}
	if d.Type == "" {
		return d, fmt.Errorf("abi: dispatch: missing type")
	}
	return d, nil
}

// PackPtrLen packs a guest memory region into the single-scalar return form.
func PackPtrLen(ptr, length uint32) uint64 { return uint64(ptr)<<32 | uint64(length) }

// UnpackPtrLen splits a packed region scalar.
func UnpackPtrLen(v uint64) (ptr, length uint32) { return uint32(v >> 32), uint32(v) }
