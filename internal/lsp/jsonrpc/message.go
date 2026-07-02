// Package jsonrpc implements the JSON-RPC 2.0 wire protocol the LSP client speaks
// over a language server's stdio (Roadmap 0100). It is pure Go — no CGo, no
// process spawning (that is internal/lsp/transport) — so it stays trivially
// cross-compilable and unit-testable over an in-memory pipe.
package jsonrpc

import "encoding/json"

// version is the only JSON-RPC version LSP uses.
const version = "2.0"

// Standard JSON-RPC error codes (a subset; LSP adds its own in the reserved
// range, decoded transparently as plain ints).
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// ID is a JSON-RPC request id, which may be a number or a string. Outgoing
// requests always use numbers; incoming server→client requests may use either,
// so both round-trip.
type ID struct {
	Num   int64
	Str   string
	IsStr bool
}

// NumID builds a numeric id.
func NumID(n int64) ID { return ID{Num: n} }

// MarshalJSON encodes the id as a number or a string.
func (id ID) MarshalJSON() ([]byte, error) {
	if id.IsStr {
		return json.Marshal(id.Str)
	}
	return json.Marshal(id.Num)
}

// UnmarshalJSON decodes a number-or-string id.
func (id *ID) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		id.IsStr = true
		return json.Unmarshal(b, &id.Str)
	}
	id.IsStr = false
	return json.Unmarshal(b, &id.Num)
}

// Error is a JSON-RPC error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface so a response error can be returned
// directly from Call.
func (e *Error) Error() string {
	if e == nil {
		return "<nil jsonrpc error>"
	}
	return e.Message
}

// message is the unified on-the-wire envelope. The shape (which fields are set)
// distinguishes the three kinds:
//   - response:     ID set, Method empty (Result or Error set)
//   - request:      ID set, Method set
//   - notification: ID nil, Method set
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *ID             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func (m *message) isResponse() bool { return m.ID != nil && m.Method == "" }
func (m *message) isRequest() bool  { return m.ID != nil && m.Method != "" }
