package keymap

import (
	"fmt"
	"strings"
)

// override.go parses the keys of the `keymap.bindings` override map (0460,
// #1312). A key is either a bare chord — "ctrl+s", the spelling that has
// always existed — or a chord qualified by the context it applies in:
// "editor.ctrl+s". The qualified form is what lets a chord run one command in
// the editor and another one everywhere else ("keep both, resolve by
// context"), which the flat one-chord-one-command map cannot express.
//
// The editor qualifier can carry a language scope (#1876): "editor[http].cmd+e"
// binds the chord only in editors whose buffer is classified as the language
// id "http". A prefix that is not exactly a known context name — optionally
// bracket-qualified — is still parsed as part of the chord, so bracket chords
// ("cmd+[") and the #1312 spellings keep their meaning unchanged.

// contextNames maps the config spelling of a context to its Context value.
// "global" is spelled out so a user can bind explicitly at the least specific
// level instead of relying on the bare form's replace-every-context semantics.
var contextNames = map[string]Context{
	"global":      Global,
	"editor":      Editor,
	"explorer":    Explorer,
	"palette":     Palette,
	"diff":        Diff,
	"terminal":    Terminal,
	"preview":     Preview,
	"vcs":         VCS,
	"debug":       Debug,
	"problems":    Problems,
	"structure":   Structure,
	"usages":      Usages,
	"http":        HTTP,
	"breakpoints": Breakpoints,
	"archive":     Archive,
	"data":        Data,
	"es":          ES,
	"tests":       Tests,
	"issues":      Issues,
	"dom":         DOM,
	"xdoctor":     Doctor,
	"lspdoctor":   LSPDoctor,
	"scratch":     Scratch,
	"hex":         Hex,
	"notebook":    Notebook,
}

// ContextNames returns the config spellings a binding key may be qualified
// with, in a stable order (documentation, settings UI, diagnostics): the
// original five first (config-format stability), the #1794 pane contexts
// alphabetically after.
func ContextNames() []string {
	return []string{
		"global", "editor", "explorer", "palette", "diff",
		"archive", "breakpoints", "data", "debug", "dom", "es", "hex", "http", "issues",
		"lspdoctor", "notebook", "preview", "problems", "scratch", "structure", "terminal", "tests",
		"usages", "vcs", "xdoctor",
	}
}

// ParseContextName resolves a config context spelling ("editor",
// "editor[http]") to its Context. It reports ok=false for anything that is not
// a known context, which is how a bare chord is told apart from a qualified
// one. The bracket form is accepted on the editor context only (#1876) and
// its language qualifier must have a registered-language-id shape
// (ValidLangQualifier) — the language itself is not checked against the
// runtime registry here, so a binding for a lazily-registered language parses.
func ParseContextName(s string) (Context, bool) {
	s = strings.ToLower(s)
	if i := strings.IndexByte(s, '['); i >= 0 {
		if !strings.HasSuffix(s, "]") {
			return Global, false
		}
		base, lang := s[:i], s[i+1:len(s)-1]
		c, ok := contextNames[base]
		if !ok || c != Editor || !ValidLangQualifier(lang) {
			return Global, false
		}
		return WithLang(c, lang), true
	}
	c, ok := contextNames[s]
	return c, ok
}

// ValidLangQualifier reports whether s can serve as the language qualifier of
// an override key: non-empty, lowercase letters, digits and -_+# — the shape
// of registered language ids. Dots are excluded by construction (the
// override-key grammar splits at the first dot), brackets by the parse above.
func ValidLangQualifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && !strings.ContainsRune("-_+#", r) {
			return false
		}
	}
	return true
}

// ContextName renders a Context in its config spelling — the inverse of
// ParseContextName, with Global spelled "global".
func ContextName(c Context) string {
	if c == Global {
		return "global"
	}
	return string(c)
}

// OverrideKey is a parsed `keymap.bindings.*` map key.
//
// Qualified reports whether the key named a context explicitly. It is not the
// same as Context != Global: an unqualified key applies to every context (it
// rebinds the chord wherever the defaults put it), while "global.<chord>"
// applies to the Global context only.
type OverrideKey struct {
	Context   Context
	Qualified bool
	Chord     Chord
}

// String renders the key back in its config spelling.
func (k OverrideKey) String() string {
	if k.Qualified {
		return ContextName(k.Context) + "." + k.Chord.String()
	}
	return k.Chord.String()
}

// ParseOverrideKey parses a binding map key. A leading "<context>." qualifier
// is consumed when the prefix names a known context; everything else is parsed
// as a bare chord, so chords that contain dots ("cmd+.") keep working.
func ParseOverrideKey(raw string) (OverrideKey, error) {
	key := OverrideKey{}
	rest := raw
	if dot := strings.Index(raw, "."); dot > 0 {
		if ctx, ok := ParseContextName(raw[:dot]); ok {
			key.Context, key.Qualified = ctx, true
			rest = raw[dot+1:]
		}
	}
	chord, err := ParseChord(rest)
	if err != nil {
		return OverrideKey{}, err
	}
	key.Chord = chord
	return key, nil
}

// BindingConfigKey renders the dotted config key a binding override is written
// to: "keymap.bindings.<chord>" unqualified, "keymap.bindings.<context>.<chord>"
// when the override is scoped to one context.
func BindingConfigKey(ctx Context, chord string, qualified bool) string {
	if !qualified {
		return "keymap.bindings." + chord
	}
	return fmt.Sprintf("keymap.bindings.%s.%s", ContextName(ctx), chord)
}
