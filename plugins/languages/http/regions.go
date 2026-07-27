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

// bodyLanguages maps a media type's structured suffix or subtype to a language
// tag, resolved against the registry as an id first and then as a file
// extension (so "js" finds whichever language owns JavaScript files). An
// unmapped type — or one whose tag resolves to nothing in this build — leaves
// the body with the host's own styling rather than guessing wrong.
var bodyLanguages = map[string]string{
	"json":       "json",
	"ndjson":     "ndjson",
	"xml":        "xml",
	"html":       "html",
	"xhtml":      "html",
	"css":        "css",
	"javascript": "js",
	"ecmascript": "js",
	"yaml":       "yaml",
	"toml":       "toml",
	"markdown":   "markdown",
	"sql":        "sql",
}

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

// bodyLanguage resolves a Content-Type header value to a language id. It
// handles parameters ("; charset=utf-8"), vendor trees
// ("application/vnd.api+json") and the "+suffix" form, which is why the
// suffix is consulted before the bare subtype.
func bodyLanguage(contentType string) (string, bool) {
	media, _, _ := strings.Cut(contentType, ";")
	media = strings.ToLower(strings.TrimSpace(media))
	_, subtype, ok := strings.Cut(media, "/")
	if !ok {
		return "", false
	}
	if _, suffix, hasSuffix := strings.Cut(subtype, "+"); hasSuffix {
		if tag, known := bodyLanguages[suffix]; known {
			return resolveTag(tag)
		}
	}
	// "x-" prefixed subtypes are the same thing under an older name
	// ("application/x-yaml").
	subtype = strings.TrimPrefix(subtype, "x-")
	if _, _, hasSuffix := strings.Cut(subtype, "+"); hasSuffix {
		subtype, _, _ = strings.Cut(subtype, "+")
	}
	tag, known := bodyLanguages[subtype]
	if !known {
		return "", false
	}
	return resolveTag(tag)
}

// bodyRegions reports one region per request body whose Content-Type resolves
// to a known language. Requests without a body, without a Content-Type, or
// with an unmapped one contribute nothing.
func bodyRegions(lines []string) []lang.Region {
	f := httpfile.Parse(strings.Join(lines, "\n"))
	var out []lang.Region
	for _, r := range f.Requests {
		if r.BodyStart == 0 {
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
		out = append(out, lang.Region{Lang: id, StartLine: start, EndLine: end, EndCol: len(lines[end])})
	}
	return out
}
