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
	// TraitReferences is find usages — the palette list and the Usages
	// pane alike (#2671).
	TraitReferences
	// TraitHighlight is the occurrence highlight of the symbol under the
	// cursor (#2671): the references question restricted to one file, and
	// recorded nowhere — it is a passive decoration.
	TraitHighlight
)

// TraitReference is one index-derived usage of a member (#2671): where its
// identifier sits, in editor coordinates, and whether that is the member's
// declaration rather than an access. The bridge merges these into a server
// answer, so it carries no preview — the bridge reads the line itself, the
// way it does for server locations.
type TraitReference struct {
	Path string
	Line int
	Col  int
	// Decl marks a declaration of the member (or a trait-use alias clause
	// naming it); the at-definition flow, which lists usages only, drops it.
	Decl bool
}

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
	// TraitReferencesAt lists the index-derived usages of the member under
	// the position that a references answer the server gave serverHits
	// locations for is missing (#2671). With no server hits it answers only
	// inside a trait body — the case the server cannot resolve at all — and
	// returns the member's whole scope: its declaration(s) and every
	// `$this` / `self` / `static` access in the declaring type, its traits,
	// its subclasses, and for a trait member its consumers and sibling
	// traits. With server hits it returns only the rows inside trait
	// bodies, which the server never reports for a consumer's member. The
	// caller deduplicates against the server's list. op is TraitReferences
	// or TraitHighlight; the latter restricts the answer to path.
	TraitReferencesAt(op TraitLookup, path string, lines []string, line, col, serverHits int) []TraitReference
	TraitRenamer
}

// TraitRenameSide names the rename path asking the index (#2672): the
// extension of a rename the server accepted, or the index-driven rename of
// a member the server refused to rename.
type TraitRenameSide int

const (
	// TraitRenameExtend is the consumer side: the server renames a member
	// declared on a class, and the index supplies the occurrences inside
	// the traits that class consumes — the rows the server never edits.
	TraitRenameExtend TraitRenameSide = iota
	// TraitRenameIndex is the trait side: PrepareRename failed inside a
	// trait body, and the index supplies the whole rename — the
	// declaration(s) in the consumer or sibling trait plus every access.
	TraitRenameIndex
)

// String is the telemetry spelling of the side.
func (s TraitRenameSide) String() string {
	if s == TraitRenameIndex {
		return "index"
	}
	return "extended"
}

// TraitRenameEdit is one identifier an index-driven rename rewrites
// (#2672): its range in editor coordinates (rune columns, one line), the
// text currently there (a property declaration keeps its `$`, so the bridge
// can preserve it), and where it sits.
type TraitRenameEdit struct {
	Path string
	Line int
	Col  int
	// EndCol is the exclusive end column of the identifier on Line.
	EndCol int
	// Text is the identifier as written: `abc`, or `$abc` for a property
	// declaration.
	Text string
	// Declaring is the FQN of the class-like declaration the edit lies in,
	// DeclName its short name; InTrait says that declaration is a trait.
	Declaring string
	DeclName  string
	InTrait   bool
	// Decl marks a declaration of the member rather than an access.
	Decl bool
}

// TraitRenamePlan is what an index-driven rename would touch (#2672).
type TraitRenamePlan struct {
	// OldName is the member's bare name (a property without its `$`) — the
	// prompt's placeholder on the index side.
	OldName string
	// Edits lists every identifier to rewrite, sorted by path and position,
	// without duplicates. Empty when the member is ambiguous.
	Edits []TraitRenameEdit
	// Ambiguous lists the declarations the member resolves to when they sit
	// on unrelated consumers with differing definitions — the rename is
	// then refused, naming them.
	Ambiguous []TraitMember
}

// TraitRenamer is the rename half of the index seam (#2672).
type TraitRenamer interface {
	// TraitRenameAt plans the index's share of a rename of the member under
	// the position. On the extend side it answers for a member the server
	// can rename itself — the plan then carries only the rows inside trait
	// bodies. On the index side it answers only inside a trait body, with
	// the member's whole scope. ok is false when the index has nothing to
	// say: not a PHP buffer, php.trait_index off, no member under the
	// position.
	TraitRenameAt(side TraitRenameSide, path string, lines []string, line, col int) (plan TraitRenamePlan, ok bool)
	// TraitRenameApplied records that a rename the index took part in was
	// applied: the side and how many identifiers the index rewrote — the
	// app turns it into the php.trait.rename telemetry op.
	TraitRenameApplied(side TraitRenameSide, edits int)
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
