package editor

// hostfold.go lets a host install fold ranges — and the placeholder text they
// collapse to — for a synthetic read-only buffer (#2029). The jq playground's
// result window (#1970) is the case it exists for: its content is not a file,
// its shape is known to the host exactly (a pretty-printed jq result), and the
// placeholder wants to say "3 keys" rather than the generic "3 lines".
//
// Everything else about folding stays where it is (fold.go): the collapsed
// set, the z-commands, the fold-aware motions, scrolling and mouse mapping all
// read the merged range list through foldRanges, so a host fold behaves
// exactly like a Tree-sitter or LSP one.

import "ike/internal/highlight"

// SetHostFolds installs fold ranges the host computed for this view's
// content. They merge over the Tree-sitter ranges and win on a shared header
// line — the host knows the document's structure first-hand, where the parse
// depends on a registered grammar. Passing nil drops them.
//
// ShowReadOnly resets the fold state with the rest of the document, so a host
// reinstalling content calls this again afterwards; that is what keeps a
// re-rendered result from carrying folds of the previous one.
func (m *Model) SetHostFolds(folds []highlight.Fold) {
	m.hostFolds = folds
	m.reconcileFolds()
}

// SetFoldSummary installs the text a collapsed fold header renders as, given
// the fold's header and end line; returning "" falls back to the default
// "⋯ N lines" tag. It is a property of the view, not of the document, so it
// survives ShowReadOnly — a host sets it once when it builds the editor.
func (m *Model) SetFoldSummary(fn func(header, end int) string) { m.foldSummary = fn }
