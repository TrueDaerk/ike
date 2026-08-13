package app

import (
	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
	"ike/internal/palette"
)

// classes.go is the class category of search everywhere (#1849): JetBrains
// treats classes as first-class, so typing a class name surfaces it under its
// own kind instead of drowning among the workspace symbols. It is not a second
// workspace/symbol source — a symbolView is a kind-filtered window onto the one
// symbolMode cache (`symbols.go`), so no extra request per keystroke is issued
// and the tier ranking (#377) of the shared cache carries over unchanged.

// classesPrefix selects the class mode inside the palette and scopes search
// everywhere to classes (#1417). Unique among the registered modes.
const classesPrefix = '/'

// symbolView is a kind-filtered view of a symbolMode: same cache, same live
// re-query, only the rows whose SymbolKind keep accepts. Two exist — the class
// category (class-like kinds) and the class-free symbol seat of search
// everywhere, so a class never appears twice in one composed list.
type symbolView struct {
	symbols     *symbolMode
	prefix      rune
	placeholder string
	keep        func(kind int) bool
}

// newClassMode builds the class category over the shared symbol cache.
func newClassMode(symbols *symbolMode) *symbolView {
	return &symbolView{
		symbols:     symbols,
		prefix:      classesPrefix,
		placeholder: "Go to class — type to search classes, structs, interfaces…",
		keep:        ilsp.ClassLike,
	}
}

// newNonClassSymbolMode builds the symbol seat of search everywhere: every
// workspace symbol except the class-like ones, which the class category owns.
// It keeps the symbol prefix, so '$' scoping and the row glyph stay the ones
// users know from the standalone mode.
func newNonClassSymbolMode(symbols *symbolMode) *symbolView {
	return &symbolView{
		symbols:     symbols,
		prefix:      symbolsPrefix,
		placeholder: "Go to symbol — type to search the workspace…",
		keep:        func(kind int) bool { return !ilsp.ClassLike(kind) },
	}
}

// Prefix implements palette.Mode.
func (v *symbolView) Prefix() rune { return v.prefix }

// Placeholder implements palette.Mode.
func (v *symbolView) Placeholder() string { return v.placeholder }

// Results implements palette.Mode: the shared cache's ranking restricted to
// the accepted kinds. Because the filter runs before the per-source cap of
// search everywhere (searchAllPerKind), an exactly-matched project class keeps
// its own eight seats and cannot be crowded out by unrelated symbols.
func (v *symbolView) Results(query string, cx palette.Context) []palette.Item {
	return v.symbols.results(query, v.keep)
}

// QueryChanged implements palette.LiveMode by delegating to the shared cache,
// which sends each query at most once however many views forward it.
func (v *symbolView) QueryChanged(query string, cx palette.Context) tea.Cmd {
	return v.symbols.QueryChanged(query, cx)
}
