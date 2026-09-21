package lsp

// traitrefs.go is the PHP trait-scope find-usages complement (Epic 0520,
// #2671). Find usages is blind in both directions around traits: inside
// trait A the server cannot resolve `$this->abc()` and reports nothing, and
// on `abc()` declared in class B it never reports the calls inside the
// traits B consumes. The declaration index knows both, and the bridge merges
// its rows into the server's answer *after* the server answered — like the
// navigation fallback (traitfallback.go), never in front of the server.
//
// The merge is one function every references flow runs through: the palette
// list (findReferences), the at-definition usages (findReferences without
// the declaration), the Usages pane (findUsages) and the no-server case. An
// index row carries the `trait` badge so the pane shows where it came from,
// and is deduplicated by (path, line, col) against the server's list. The
// occurrence highlight (requestDocumentHighlight) asks the same question
// restricted to the current file.

import (
	"strings"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// traitBadge marks a references row the PHP trait index added.
const traitBadge = "trait"

// mergeTraitReferences appends the index-derived usages the server answer is
// missing (#2671) and returns the merged list; the server's rows keep their
// order and come first. includeDecl mirrors the request flag: the
// at-definition flow lists usages only, so declaration rows are dropped.
func (b *bridge) mergeTraitReferences(h host.API, path string, line, col int, includeDecl bool, refs []ilsp.Reference) []ilsp.Reference {
	idx := h.TraitIndex()
	if idx == nil {
		return refs
	}
	rows := idx.TraitReferencesAt(host.TraitReferences, path, b.docLines(path), line, col, len(refs))
	if len(rows) == 0 {
		return refs
	}
	type refKey struct {
		path      string
		line, col int
	}
	seen := make(map[refKey]bool, len(refs))
	for _, r := range refs {
		seen[refKey{r.Path, r.Line, r.Col}] = true
	}
	files := map[string][]string{}
	for _, r := range rows {
		if r.Decl && !includeDecl {
			continue
		}
		k := refKey{r.Path, r.Line, r.Col}
		if seen[k] {
			continue
		}
		seen[k] = true
		lines, ok := files[r.Path]
		if !ok {
			// The synced document when the file is open, else disk — the
			// same text the index scanned.
			lines = b.docLines(r.Path)
			files[r.Path] = lines
		}
		ref := ilsp.Reference{Path: r.Path, Line: r.Line, Col: r.Col, Badge: traitBadge}
		if r.Line >= 0 && r.Line < len(lines) {
			ref.Preview = strings.TrimSpace(lines[r.Line])
		}
		refs = append(refs, ref)
	}
	return refs
}

// traitHighlights is the occurrence-highlight counterpart (#2671): with the
// server reporting no occurrences, the index's file-local usages of the
// member under the cursor inside a trait body light up instead. The member
// identifier's width comes from the line text, so `$this->abc()` marks
// `abc` exactly like a server highlight would.
func (b *bridge) traitHighlights(h host.API, path string, line, col int) []ilsp.DocumentHighlight {
	idx := h.TraitIndex()
	if idx == nil {
		return nil
	}
	lines := b.docLines(path)
	rows := idx.TraitReferencesAt(host.TraitHighlight, path, lines, line, col, 0)
	if len(rows) == 0 {
		return nil
	}
	out := make([]ilsp.DocumentHighlight, 0, len(rows))
	for _, r := range rows {
		if r.Path != path {
			continue
		}
		end := r.Col + identWidth(lines, r.Line, r.Col)
		out = append(out, ilsp.DocumentHighlight{
			Range: buffer.Range{Start: buffer.Position{Line: r.Line, Col: r.Col}, End: buffer.Position{Line: r.Line, Col: end}},
			Kind:  protocol.HighlightText,
		})
	}
	return out
}

// identWidth is the rune width of the identifier starting at col in the
// line (at least one, so an empty highlight never appears).
func identWidth(lines []string, line, col int) int {
	if line < 0 || line >= len(lines) {
		return 1
	}
	runes := []rune(lines[line])
	n := 0
	for i := col; i < len(runes) && (runes[i] == '_' || runes[i] == '$' || isIdentRune(runes[i])); i++ {
		n++
	}
	if n == 0 {
		return 1
	}
	return n
}

func isIdentRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r > 0x7f
}
