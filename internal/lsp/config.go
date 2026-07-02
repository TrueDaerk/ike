// Package lsp wires the LSP subsystem together: config resolution here, plus the
// jsonrpc/transport/protocol/client/manager/sync/feature sub-packages. The loader
// and precedence live in internal/config (Roadmap 0040); this file only defines
// the *content* of the [lsp] section — turning the free-form server table into a
// typed ServerSpec keyed by language.
package lsp

import "encoding/json"

// ServerSpec is one language server's launch + behaviour configuration, parsed
// from a `[lsp.servers.<lang>]` table.
type ServerSpec struct {
	Language    string
	Command     string
	Args        []string
	Env         []string
	RootMarkers []string
	Settings    map[string]any // forwarded as initializationOptions
}

// SettingsJSON encodes Settings as initializationOptions, or nil when empty.
func (s ServerSpec) SettingsJSON() json.RawMessage {
	if len(s.Settings) == 0 {
		return nil
	}
	b, err := json.Marshal(s.Settings)
	if err != nil {
		return nil
	}
	return b
}

// SpecFor resolves the server spec for a language from the [lsp.servers] table.
// It returns ok=false when the language has no entry or no command.
func SpecFor(servers map[string]map[string]any, lang string) (ServerSpec, bool) {
	raw, ok := servers[lang]
	if !ok {
		return ServerSpec{}, false
	}
	spec := ServerSpec{
		Language:    lang,
		Command:     asString(raw["command"]),
		Args:        asStringSlice(raw["args"]),
		Env:         asStringSlice(raw["env"]),
		RootMarkers: asStringSlice(raw["root_markers"]),
		Settings:    asMap(raw["settings"]),
	}
	if spec.Command == "" {
		return ServerSpec{}, false
	}
	return spec, true
}

// asString coerces a TOML scalar to a string.
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asStringSlice coerces a TOML array (decoded as []any) to []string, dropping
// non-string elements.
func asStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// asMap coerces a TOML table to map[string]any.
func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}
