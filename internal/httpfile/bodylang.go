package httpfile

import "strings"

// bodyLanguageTags maps a media type's structured suffix or subtype to a
// language tag. The editor resolves the tag against the language registry (as
// an id first, then as a file extension, so "js" finds whichever language owns
// JavaScript files); the dispatch side only needs to know whether a body is
// JSON.
//
// "application/x-www-form-urlencoded" and "multipart/form-data" are absent:
// neither is a language a grammar could own.
var bodyLanguageTags = map[string]string{
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
	"graphql":    "graphql",
}

// BodyLanguageTag resolves a Content-Type header value to a language tag. It
// handles parameters ("; charset=utf-8"), vendor trees
// ("application/vnd.api+json") and the "+suffix" form, which is why the
// suffix is consulted before the bare subtype.
func BodyLanguageTag(contentType string) (string, bool) {
	media, _, _ := strings.Cut(contentType, ";")
	media = strings.ToLower(strings.TrimSpace(media))
	_, subtype, ok := strings.Cut(media, "/")
	if !ok {
		return "", false
	}
	if _, suffix, hasSuffix := strings.Cut(subtype, "+"); hasSuffix {
		if tag, known := bodyLanguageTags[suffix]; known {
			return tag, true
		}
	}
	// "x-" prefixed subtypes are the same thing under an older name
	// ("application/x-yaml").
	subtype = strings.TrimPrefix(subtype, "x-")
	subtype, _, _ = strings.Cut(subtype, "+")
	tag, known := bodyLanguageTags[subtype]
	return tag, known
}
