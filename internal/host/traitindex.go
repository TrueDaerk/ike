package host

// traitindex.go is the host seam the LSP bridge reaches the PHP declaration
// index through (Epic 0520, #2670). The bridge is a plugin: it may not import
// internal/phpindex, and the epic's navigation fallback must not become a
// package-level global either, so the app registers a narrow read-only view
// on the host and the bridge asks for it the way it asks for configuration.
//
// The seam is deliberately one question — "which members does the trait's
// consumer scope declare under the cursor?" — so every piece of PHP
// knowledge (is this a PHP buffer, is the position inside a trait body,
// which member does the syntax tree name there, is php.trait_index on) stays
// on the app side of it. An unregistered index, a disabled one and a build
// without the PHP grammar all answer the same way: nothing.
//
// It is a *fallback*: the bridge consults it only after the server answered
// empty or could not be asked at all, so a server answer is never replaced.

// TraitMember is one member declaration the PHP trait index resolved in a
// trait's consumer scope: enough to jump to it, peek it and render a hover
// card without the consumer ever seeing the index's own types.
type TraitMember struct {
	// Name is the member as declared (a property keeps its `$`), Signature
	// the one-line declaration text a hover card shows.
	Name      string
	Signature string
	// Doc is the declaration's docblock summary, "" when it has none.
	Doc string
	// Declaring is the fully qualified name of the declaring type and
	// DeclKind how PHP spells that type ("class", "trait", "enum",
	// "interface"); DeclName is its short name, for the hover footer.
	Declaring string
	DeclKind  string
	DeclName  string
	// Path, Line and Col locate the member's name in editor coordinates —
	// the jump target.
	Path string
	Line int
	Col  int
}

// TraitLookup names the feature consulting the index, so the app can record
// the per-feature telemetry op (#2670) without the bridge knowing about
// telemetry at all.
type TraitLookup int

const (
	// TraitDefinition is go-to-definition and peek definition.
	TraitDefinition TraitLookup = iota
	// TraitHover is the hover card.
	TraitHover
)

// TraitIndex is the read-only view of the PHP declaration index the bridge
// consults. Implementations must be safe to call from the bridge's request
// goroutines.
type TraitIndex interface {
	// TraitMembersAt resolves the member access under the position to its
	// declarations in the enclosing trait's consumer scope, nearest first.
	// It answers nothing unless the buffer is PHP, the position sits inside
	// a trait body and the access names a member of `$this` / `self` /
	// `static`. lines is the document as the caller sees it (the LSP
	// bridge's synced copy, or disk).
	TraitMembersAt(op TraitLookup, path string, lines []string, line, col int) []TraitMember
}

// SetTraitIndex registers (or, with nil, removes) the PHP trait index the
// bridge consults. The app calls it once per model; a project switch
// re-registers the new project's index on the live host.
func (h *Host) SetTraitIndex(idx TraitIndex) {
	h.mu.Lock()
	h.traitIndex = idx
	h.mu.Unlock()
}

// TraitIndex implements API: the registered index, or nil when the app
// registered none (a build or a test that wires no PHP index).
func (h *Host) TraitIndex() TraitIndex {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.traitIndex
}
