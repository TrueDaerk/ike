package format

import "sort"

// settings.go carries the metadata the Formatters settings page (#1662) needs
// to edit a language's `[format.<languageID>]` table: which languages ship a
// **built-in** formatter — the one `builtin = false` disables — and which
// extra config keys that built-in reads (SQL's `keywords`). Language plugins
// declare both from init(), so the settings UI edits them generically instead
// of knowing that SQL exists.

// ConfigKey declares one extra `[format.<lang>]` key beyond the generic
// external-command set (command / args / range_args / temp_file / install /
// enabled / builtin).
type ConfigKey struct {
	// Key is the TOML key inside the language's table ("keywords").
	Key string
	// Values enumerates the accepted values; empty means free text.
	Values []string
	// Default is the value in effect while the key is absent.
	Default string
	// Help is the one-line description shown next to the field.
	Help string
}

// RegisterBuiltin records that langID ships a built-in formatter (toggled by
// `[format.<lang>] builtin`), together with the extra config keys it reads.
// Call from init(), next to Register.
func RegisterBuiltin(langID string, keys ...ConfigKey) {
	extMu.Lock()
	builtins[langID] = keys
	extMu.Unlock()
}

// Builtin reports a language's built-in formatter registration: its extra
// config keys, and whether it has one at all.
func Builtin(langID string) ([]ConfigKey, bool) {
	extMu.Lock()
	defer extMu.Unlock()
	keys, ok := builtins[langID]
	return keys, ok
}

// BuiltinLangs lists the languages with a registered built-in formatter,
// sorted so callers get a stable order regardless of registration order.
func BuiltinLangs() []string {
	extMu.Lock()
	out := make([]string, 0, len(builtins))
	for id := range builtins {
		out = append(out, id)
	}
	extMu.Unlock()
	sort.Strings(out)
	return out
}

// builtins holds the registrations, guarded by extMu (external.go) — the
// external-formatter side state and this one are always touched together.
var builtins = map[string][]ConfigKey{}
