package lsp

// traitfallback.go is the PHP trait-scope navigation fallback (Epic 0520,
// #2670). Intelephense resolves `$this` inside a trait body as the trait
// itself, so go-to-definition, peek and hover on `$this->abc()` come back
// empty whenever `abc` is declared on one of the trait's consumers — the
// bridge could only say "no definition found".
//
// The PHP declaration index knows those members. The bridge reaches it
// through the host seam (host.TraitIndex, registered by the app) and
// consults it strictly *after* the server: on an empty definition answer,
// on an empty hover, and when no server could be asked at all. A server
// answer is therefore never replaced — unlike the pre-server local
// providers (internal/lsp/localdef.go), whose first claim short-circuits the
// request.
//
// Everything PHP-specific lives behind the seam: without a registered index,
// with php.trait_index off, outside a trait body and in a build without the
// PHP grammar it answers nothing and these functions report "not handled",
// so the existing empty-answer status message appears unchanged.

import (
	"strings"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// traitFooterPrefix opens the hover card's footer line, which names the type
// the member was actually found on.
const traitFooterPrefix = "resolved via trait consumer "

// traitMembers asks the host's trait index for the declarations of the
// member under the position. Nothing when no index is registered.
func (b *bridge) traitMembers(h host.API, op host.TraitLookup, path string, line, col int) []host.TraitMember {
	idx := h.TraitIndex()
	if idx == nil {
		return nil
	}
	return idx.TraitMembersAt(op, path, b.docLines(path), line, col)
}

// traitDefinition delivers the index's answer for a definition or peek
// request the way a server location would be delivered — one hit navigates
// (or peeks), several open the same multi-location picker a multi-location
// server answer opens, so the jump is recorded in the navigation history by
// the app's own funnel. It reports whether it handled the request.
func (b *bridge) traitDefinition(h host.API, path string, line, col int, peek bool) bool {
	members := b.traitMembers(h, host.TraitDefinition, path, line, col)
	if len(members) == 0 {
		return false
	}
	if len(members) > 1 {
		// The member is declared independently on several consumers: pick,
		// don't guess (#279) — and a peek keeps its intent through the
		// picker (#1154).
		refs := make([]ilsp.Reference, 0, len(members))
		for _, m := range members {
			refs = append(refs, ilsp.Reference{Path: m.Path, Line: m.Line, Col: m.Col, Preview: traitPreview(m)})
		}
		h.Send(ilsp.DefinitionCandidatesMsg{Refs: refs, Peek: peek})
		return true
	}
	m := members[0]
	if peek {
		h.Send(ilsp.PeekDefinitionMsg{Path: m.Path, Line: m.Line, Col: m.Col})
	} else {
		h.Send(ilsp.DefinitionMsg{Path: m.Path, Line: m.Line, Col: m.Col})
	}
	return true
}

// traitHover delivers the index's answer as a hover card and reports whether
// it had one.
func (b *bridge) traitHover(h host.API, path string, line, col int, mouse bool) bool {
	members := b.traitMembers(h, host.TraitHover, path, line, col)
	if len(members) == 0 {
		return false
	}
	h.Send(ilsp.HoverMsg{Path: path, Contents: traitHoverCard(members), Mouse: mouse, Line: line, Col: col})
	return true
}

// traitPreview is one picker row's preview: the signature and the type that
// declares it — the whole point of the pick being *which* consumer.
func traitPreview(m host.TraitMember) string {
	if decl := traitDeclLabel(m); decl != "" {
		return m.Signature + "  —  " + decl
	}
	return m.Signature
}

// traitDeclLabel names the declaring type the way the card and the picker
// show it: "class B", or just the name when the kind is unknown.
func traitDeclLabel(m host.TraitMember) string {
	name := m.DeclName
	if name == "" {
		name = m.Declaring
	}
	if name == "" {
		return ""
	}
	if m.DeclKind != "" {
		return m.DeclKind + " " + name
	}
	return name
}

// traitHoverCard renders the markdown the hover popup shows: per declaration
// the signature in a php code fence, the declaring type (kind, short name and
// fully qualified name), the docblock summary, and a footer naming the type
// the member was resolved on. Several declarations are listed, rule-separated
// — the same shape a multi-consumer definition offers as a picker.
func traitHoverCard(members []host.TraitMember) string {
	var sb strings.Builder
	for i, m := range members {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}
		sb.WriteString("```php\n")
		sb.WriteString(m.Signature)
		sb.WriteString("\n```\n\n")
		if decl := traitDeclLabel(m); decl != "" {
			sb.WriteString(decl)
			if m.Declaring != "" && m.Declaring != m.DeclName {
				sb.WriteString(" — `")
				sb.WriteString(m.Declaring)
				sb.WriteString("`")
			}
			sb.WriteString("\n\n")
		}
		if m.Doc != "" {
			sb.WriteString(m.Doc)
			sb.WriteString("\n\n")
		}
		sb.WriteString("*")
		sb.WriteString(traitFooterPrefix)
		name := m.DeclName
		if name == "" {
			name = m.Declaring
		}
		sb.WriteString(name)
		sb.WriteString("*\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}
