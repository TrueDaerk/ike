package lang

import "encoding/json"

// ServerSpec is one language server's launch + behaviour configuration. A
// language plugin supplies it as the baseline (Language.Server); the user's
// [lsp.servers.<id>] config overlays it (see internal/lsp SpecFor / resolveSpec).
// It lives here, not in internal/lsp, so the language registry is the one place a
// server is described; internal/lsp aliases this type.
type ServerSpec struct {
	Language    string
	Command     string
	Args        []string
	Env         []string
	RootMarkers []string
	Settings    map[string]any // forwarded as initializationOptions
	// Install is the recipe that installs the server binary when it is
	// missing (0180, #131): a plain argv shelled out as-is, e.g.
	// ["go", "install", "golang.org/x/tools/gopls@latest"]. Empty means the
	// plugin ships no recipe and installation stays manual.
	Install []string
	// Companions are optional tools the server delegates work to (#1067):
	// the server starts fine without them but silently loses a capability
	// (bash-language-server → shellcheck diagnostics). The LSP manager probes
	// PATH for each when the server first becomes ready and surfaces a
	// one-time hint per language for every missing one.
	Companions []Companion
}

// Companion is one optional tool a language server uses when present on PATH
// (#1067). Binary is the executable to probe for, Purpose names what it
// enables ("shell diagnostics"), and Install is a human-readable install hint
// ("brew install shellcheck") folded into the missing-tool message.
type Companion struct {
	Binary  string
	Purpose string
	Install string
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

// MergeSettings deep-merges two settings maps with over winning per key. Nested
// map[string]any sections merge recursively (so a detected toolchain path can sit
// beside, and be overridden by, an explicit user setting in the same section). A
// nil operand returns the other unchanged.
func MergeSettings(base, over map[string]any) map[string]any {
	if base == nil {
		return over
	}
	if over == nil {
		return base
	}
	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if bm, ok := out[k].(map[string]any); ok {
			if om, ok := v.(map[string]any); ok {
				out[k] = MergeSettings(bm, om)
				continue
			}
		}
		out[k] = v
	}
	return out
}
