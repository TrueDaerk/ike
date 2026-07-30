// Package format is the neutral formatter registry (Roadmap 0470, #1401): the
// single source of truth for "who reformats a buffer". Providers — the LSP
// bridge, external command formatters, built-in Go formatters shipped by
// language plugins — register here; the app resolves the active buffer's
// provider chain and applies whatever the winning provider returns as one
// undo-able buffer edit.
//
// It is a leaf package like internal/lang: pure Go, no bubbletea, no LSP
// import, so every layer (editor, app, plugins) can depend on it without a
// cycle. Registration happens from init() functions, mirroring lang.Register
// and registry.Register.
package format

import (
	"context"
	"sort"
	"sync"
)

// Tier orders the resolution chain (#1400): the first available provider in
// the lowest tier wins. The order is fixed by the spec — an explicit config
// override beats the ecosystem CLI, which beats the language server, which
// beats a built-in fallback formatter.
type Tier int

const (
	// TierOverride is an explicit [format.<languageID>] config entry (#1402).
	TierOverride Tier = iota
	// TierExternal is a language plugin's default external command formatter.
	TierExternal
	// TierLSP is textDocument/formatting via the attached language server.
	TierLSP
	// TierBuiltin is a built-in Go formatter shipped by a language plugin.
	TierBuiltin
)

// Options carries the effective per-buffer settings a formatter must honour,
// resolved by the editor through its usual layering (built-in defaults < IKE
// config < language default < .editorconfig, see internal/editor).
type Options struct {
	TabWidth  int
	UseSpaces bool
	// MaxLineLength is the editorconfig max_line_length; 0 means unlimited.
	MaxLineLength int
	// InsertFinalNewline reports whether saves append a final newline; text
	// formatters should produce output consistent with it.
	InsertFinalNewline bool
}

// Pos is a 0-based rune position in the buffer, mirroring buffer.Position
// without importing it (this package stays a leaf).
type Pos struct{ Line, Col int }

// Edit is one formatting rewrite in 0-based editor rune coordinates: the
// [Start, End) span becomes Text — the same convention as lsp.FormatEdit,
// which the plugin layer converts one-to-one.
type Edit struct {
	StartLine, StartCol int
	EndLine, EndCol     int
	Text                string
}

// Request is one formatting job: the buffer snapshot plus its effective
// settings. Text-based providers read Lines; the LSP provider reads only Path
// (the server holds the synced document).
type Request struct {
	Path     string
	Language string // language id (lang.Language.ID), "" when unknown
	Lines    []string
	Options  Options
}

// Result is a provider's answer: either the full formatted text or a set of
// edits. Exactly one should be set; Edits wins when both are. Use
// EditsForResult to normalize either shape into applicable edits.
type Result struct {
	Text  *string
	Edits []Edit
}

// TextResult wraps a full formatted text as a Result.
func TextResult(text string) Result { return Result{Text: &text} }

// Provider is one registered formatting source. Format is mandatory; every
// other capability is optional. Func fields (not an interface) keep
// registration declarative, like lang.Language.
type Provider struct {
	// Name identifies the provider in the status line ("gopls", "built-in",
	// "ruff format"). NameFor, when set, overrides it per path — the LSP
	// provider names the actual server binary.
	Name    string
	NameFor func(path string) string

	// Languages lists the language ids served; empty means every language
	// (the LSP provider — availability is decided per path instead).
	Languages []string

	// Tier places the provider in the resolution chain.
	Tier Tier

	// Available reports whether the provider can format path right now
	// (binary on PATH, ready server with the capability). Nil means always.
	Available func(path string) bool

	// RangeAvailable reports range-formatting support for path. Nil means
	// "ranges supported iff FormatRange is set".
	RangeAvailable func(path string) bool

	// Format formats the whole buffer.
	Format func(ctx context.Context, req Request) (Result, error)

	// FormatRange formats the [start, end) span; nil means no range support
	// (the registry falls back to the next range-capable provider, #1401).
	FormatRange func(ctx context.Context, req Request, start, end Pos) (Result, error)
}

// DisplayName is the provider's status-line name for path.
func (p Provider) DisplayName(path string) string {
	if p.NameFor != nil {
		if n := p.NameFor(path); n != "" {
			return n
		}
	}
	return p.Name
}

// serves reports whether the provider covers language id.
func (p Provider) serves(langID string) bool {
	if len(p.Languages) == 0 {
		return true
	}
	for _, l := range p.Languages {
		if l == langID {
			return true
		}
	}
	return false
}

// available reports whether the provider can run for path right now.
func (p Provider) available(path string) bool {
	return p.Format != nil && (p.Available == nil || p.Available(path))
}

// rangeAvailable reports whether the provider can range-format path.
func (p Provider) rangeAvailable(path string) bool {
	if p.FormatRange == nil {
		return false
	}
	if p.RangeAvailable != nil {
		return p.RangeAvailable(path)
	}
	return true
}

var (
	mu        sync.RWMutex
	providers []Provider
)

// Register adds a provider to the registry. Safe to call from init();
// providers in the same tier resolve in registration order.
func Register(p Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers = append(providers, p)
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].Tier < providers[j].Tier })
}

// chain returns the registered providers serving langID, in resolution order.
func chain(langID string) []Provider {
	mu.RLock()
	defer mu.RUnlock()
	var out []Provider
	for _, p := range providers {
		if p.serves(langID) {
			out = append(out, p)
		}
	}
	return out
}

// Resolve picks the whole-file formatter for a buffer: the first available
// provider in tier order. ok is false when no provider can format the buffer
// — the caller reports "no formatter for <language>" instead of a no-op.
func Resolve(langID, path string) (Provider, bool) {
	for _, p := range chain(langID) {
		if p.available(path) {
			return p, true
		}
	}
	return Provider{}, false
}

// ResolveRange picks the reformat-selection provider: the first available
// provider that supports ranges — a range-less winner falls through to the
// next range-capable one instead of silently formatting the whole file
// (#1401). wholeFileOnly reports the consolation case: providers exist for
// the language, but none of the available ones does ranges.
func ResolveRange(langID, path string) (p Provider, ok, wholeFileOnly bool) {
	any := false
	for _, c := range chain(langID) {
		if !c.available(path) {
			continue
		}
		any = true
		if c.rangeAvailable(path) {
			return c, true, false
		}
	}
	return Provider{}, false, any
}

// Has reports whether any provider can format the buffer — the save chain's
// capability gate (#1148 routed through the registry).
func Has(langID, path string) bool {
	_, ok := Resolve(langID, path)
	return ok
}

// ResetForTest clears the registry (tests only — production providers
// register once from init() and never unregister).
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	providers = nil
}
