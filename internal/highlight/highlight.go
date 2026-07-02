package highlight

import (
	"path/filepath"
	"strings"
)

// langByExt maps a file extension (no dot, lower-case) to a grammar language id.
var langByExt = map[string]string{
	"go":    "go",
	"py":    "python",
	"pyi":   "python",
	"php":   "php",
	"phtml": "php",
}

// Lang returns the grammar language id for a path ("go", "python", "php"), or ""
// when the extension is not supported.
func Lang(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	return langByExt[ext]
}

// Supported reports whether a path has a grammar (and therefore whether parsing
// is worth scheduling).
func Supported(path string) bool { return Lang(path) != "" }

// Highlight parses lines for the grammar matching path and returns the spans.
// It returns nil when the language is unsupported or when the build has CGo
// disabled (the stub). The actual parse lives in parse_cgo.go / parse_stub.go.
func Highlight(path string, lines []string) []Span {
	lang := Lang(path)
	if lang == "" {
		return nil
	}
	return parse(lang, lines)
}
