package langhttp

import (
	"strings"

	"ike/internal/httpfile"
	"ike/internal/lang"
)

// regions.go gives a .http buffer's request bodies their real language
// (#1303). A body is JSON, XML, HTML … depending on the request's
// `Content-Type` header — a *sibling* of the body, which a Tree-sitter
// injection query cannot read. The registry's Go-level region seam
// (lang.Language.Regions) exists for exactly this case: the plugin parses the
// buffer and reports each body's line range plus the language its media type
// resolves to.

// resolveTag maps a tag to a registered language id: id first, then file
// extension — the same order highlight.HighlightFenced uses.
func resolveTag(tag string) (string, bool) {
	if l, ok := lang.ByID(tag); ok {
		return l.ID, true
	}
	if l, ok := lang.ByExt(tag); ok {
		return l.ID, true
	}
	return "", false
}

// bodyLanguage resolves a Content-Type header value to a registered language
// id via httpfile.BodyLanguageTag. An unmapped type — or one whose tag
// resolves to nothing in this build — leaves the body with the host's own
// styling rather than guessing wrong. Form-encoded and multipart bodies have
// no language: the former gets the key=value&… span treatment of a query
// string (spans.go), the latter its boundary and part-header styling
// (multipart.go).
func bodyLanguage(contentType string) (string, bool) {
	tag, ok := httpfile.BodyLanguageTag(contentType)
	if !ok {
		return "", false
	}
	return resolveTag(tag)
}

// bodyRegions reports one region per request body whose Content-Type resolves
// to a known language. Requests without a body, without a Content-Type, or
// with an unmapped one contribute nothing.
func bodyRegions(lines []string) []lang.Region {
	return regionsFor(httpfile.Parse(strings.Join(lines, "\n")), lines)
}

// regionsFor is bodyRegions over a parse the caller already has — the span
// producer parses the buffer once per highlight pass and must not pay for a
// second one just to learn where the bodies are (#2598).
func regionsFor(f *httpfile.File, lines []string) []lang.Region {
	var out []lang.Region
	for _, r := range f.Requests {
		if r.GraphQL != nil && r.BodyFile == "" {
			// A GRAPHQL block is two languages, not one (#2423): the query
			// section and the JSON variables object below it. The block has no
			// Content-Type of its own to resolve — the envelope's
			// application/json describes the *wire* body, not what is written
			// here — so the two sections are claimed directly.
			out = append(out, graphQLRegions(r.GraphQL, lines)...)
			continue
		}
		if r.BodyStart == 0 || r.BodyFile != "" {
			// An external body (`< ./payload.json`, #1305) is a directive
			// line, not payload — the host grammar already styles it.
			continue
		}
		ct, ok := r.Header("Content-Type")
		if !ok {
			continue
		}
		id, ok := bodyLanguage(ct)
		if !ok {
			continue
		}
		// httpfile counts lines from 1; regions are 0-based, and the region
		// runs to the end of its last line.
		start, end := r.BodyStart-1, r.BodyEnd-1
		if start < 0 || end < start || end >= len(lines) {
			continue
		}
		reg := lang.Region{Lang: id, StartLine: start, EndLine: end, EndCol: len(lines[end])}
		if id == "json" {
			// """…""" literals (triplequote.go) reach the JSON language as
			// ordinary strings.
			if lits := literalsIn(lines, start, end); len(lits) > 0 {
				reg.Text = literalStandIn(lines, start, end, lits)
			}
		}
		out = append(out, reg)
	}
	return out
}
