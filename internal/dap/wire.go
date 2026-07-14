// Package dap is a minimal Debug Adapter Protocol client (0350, #578): the
// LSP base-protocol framing (Content-Length headers, reused from
// internal/lsp/jsonrpc) carrying DAP's seq/type envelope. It provides the
// request/response/event plumbing (conn.go), a typed session API over the
// requests IKE needs (session.go), and the protocol structs (protocol.go).
// Adapters are spawned like language servers through internal/lsp/transport;
// per-language adapter commands come from the language registry
// (lang.DebugAdapterProvider).
package dap

import "encoding/json"

// envelope is the DAP wire message: every message carries seq and type;
// requests add command/arguments, responses request_seq/success/body (and a
// human message on failure), events event/body.
type envelope struct {
	Seq  int    `json:"seq"`
	Type string `json:"type"`

	// request
	Command   string          `json:"command,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`

	// response
	RequestSeq int    `json:"request_seq,omitempty"`
	Success    bool   `json:"success,omitempty"`
	Message    string `json:"message,omitempty"`

	// event
	Event string `json:"event,omitempty"`

	// response / event payload
	Body json.RawMessage `json:"body,omitempty"`
}

const (
	typeRequest  = "request"
	typeResponse = "response"
	typeEvent    = "event"
)
