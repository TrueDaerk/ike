// Package lsp wires the LSP subsystem together: config resolution here, plus the
// jsonrpc/transport/protocol/client/manager/sync/feature sub-packages. The loader
// and precedence live in internal/config (Roadmap 0040); this file only defines
// the *content* of the [lsp] section — turning the free-form server table into a
// typed ServerSpec keyed by language.
package lsp

import "ike/internal/lang"

// ServerSpec is re-exported from internal/lang: the language registry owns the
// server description (each language plugin supplies its baseline), and this file
// only parses the user's `[lsp.servers.<id>]` config *overlay* onto it.
type ServerSpec = lang.ServerSpec

// Companion is re-exported from internal/lang alongside ServerSpec: an optional
// tool a server delegates work to, probed on PATH when the server starts (#1067).
type Companion = lang.Companion

// Overlay parses a `[lsp.servers.<id>]` config entry into a ServerSpec. Unlike a
// full spec it does not require a command — config only overrides the fields the
// user actually sets, on top of the language plugin's baseline. ok=false means the
// language has no config entry at all, so the baseline stands unchanged.
func Overlay(servers map[string]map[string]any, langID string) (ServerSpec, bool) {
	raw, ok := servers[langID]
	if !ok {
		return ServerSpec{}, false
	}
	return ServerSpec{
		Language:    langID,
		Command:     asString(raw["command"]),
		Args:        asStringSlice(raw["args"]),
		Env:         asStringSlice(raw["env"]),
		RootMarkers: asStringSlice(raw["root_markers"]),
		Settings:    asMap(raw["settings"]),
	}, true
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
