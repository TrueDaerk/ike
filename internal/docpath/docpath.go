// Package docpath derives the structural path to a caret position in a JSON or
// YAML buffer (#1660): the answer to "where am I?" in a deep Kubernetes
// manifest, CI config or lockfile, as `spec.template.containers[2].env[0].name`
// — cursor-first and copyable, where the structure panel answers top-down.
//
// It is a leaf package: pure Go, no CGo, no Tree-sitter, no registry import,
// like internal/bracket. The scanners are deliberately *structural*, not
// document parsers — nothing is loaded, nothing is resolved, no value is
// unmarshalled:
//
//   - JSON runs a container stack over the buffer up to the caret: `{`/`[`
//     push, `}`/`]` pop, a string in key position names its object frame, a
//     comma at array level advances its index. Strings and JSONC comments are
//     skipped, so a brace inside `"a } b"` never closes anything.
//   - YAML is indentation-driven: every line contributes its `- ` dashes and
//     its `key:` at their columns, and a column pops the frames that are no
//     longer enclosing. Block scalars, `#` comments and `---` document
//     boundaries are honoured; a flow value (`{a: 1}`, `[1, 2]`) hands off to
//     the JSON scanner, which is the same grammar.
//
// Both degrade gracefully by construction: they only ever read *up to* the
// caret, so a buffer that is broken below it — or is still being typed — still
// yields the nearest enclosing path, and an unbalanced closer simply finds an
// empty stack. Anchors and aliases are reported as written (`<<`, `&a`); #1629
// owns resolution, and a path that silently followed an alias would name a
// location the file does not contain.
package docpath

import (
	"strconv"
	"strings"
)

// Step is one element of a path: a mapping key, or — when Seq is set — a
// sequence element at Index (0-based, as jq and yq count).
type Step struct {
	Key   string
	Index int
	Seq   bool
}

// Source is the buffer view a scan reads. The editor passes its buffer
// directly, so a scan never copies the document.
type Source interface {
	Line(i int) string
	LineCount() int
}

// Lines adapts a plain slice to Source, for callers that already hold the
// whole document (and for tests).
type Lines []string

// Line returns line i, or "" when it is out of range.
func (l Lines) Line(i int) string {
	if i < 0 || i >= len(l) {
		return ""
	}
	return l[i]
}

// LineCount returns the number of lines.
func (l Lines) LineCount() int { return len(l) }

// kind is the scanner a language id selects.
type kind int

const (
	kindNone kind = iota
	kindJSON
	kindYAML
)

// kindOf maps a registered language id to its scanner. The jsonc extension
// resolves to the "json" language, so JSONC buffers are covered by kindJSON
// (whose scanner skips `//` and `/* */`); ansible shares YAML's syntax.
func kindOf(langID string) kind {
	switch langID {
	case "json", "ndjson":
		return kindJSON
	case "yaml", "ansible":
		return kindYAML
	}
	return kindNone
}

// IsLang reports whether a language id has a path scanner.
func IsLang(langID string) bool { return kindOf(langID) != kindNone }

// IsYAML reports whether a language id is scanned as YAML — the yq copy form
// is the YAML flavour of the same path.
func IsYAML(langID string) bool { return kindOf(langID) == kindYAML }

// At returns the path to the caret at (line, col) — 0-based editor coordinates
// — in a buffer of language langID, outermost step first. It is empty at the
// document root, for a language without a scanner, and wherever the buffer
// gives no enclosing container.
func At(langID string, src Source, line, col int) []Step {
	switch kindOf(langID) {
	case kindJSON:
		return scanJSON(src, line, col)
	case kindYAML:
		return scanYAML(src, line, col)
	}
	return nil
}

// Dotted renders the human-readable form shown in the status line:
// `spec.template.containers[2].env[0].name`. Keys are written verbatim — this
// is the reading form; JQ and YQ are the machine-precise ones.
func Dotted(steps []Step) string {
	var b strings.Builder
	for _, s := range steps {
		if s.Seq {
			b.WriteString("[" + strconv.Itoa(s.Index) + "]")
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s.Key)
	}
	return b.String()
}

// JQ renders the path as a jq expression: `.spec.containers[2].name`. A key
// that is not a bare identifier is bracket-quoted (`.["my-key"]`), which jq
// accepts in every position a dotted key works. The empty path is `.`, jq's
// identity — the whole document, which is exactly where the caret is.
func JQ(steps []Step) string {
	var b strings.Builder
	for _, s := range steps {
		switch {
		case s.Seq:
			if b.Len() == 0 {
				b.WriteByte('.')
			}
			b.WriteString("[" + strconv.Itoa(s.Index) + "]")
		case plainKey(s.Key):
			b.WriteByte('.')
			b.WriteString(s.Key)
		default:
			if b.Len() == 0 {
				b.WriteByte('.')
			}
			b.WriteString("[" + strconv.Quote(s.Key) + "]")
		}
	}
	if b.Len() == 0 {
		return "."
	}
	return b.String()
}

// YQ renders the path as a yq (v4) expression. The syntax is jq's, except that
// a key needing quotes is written as `."my-key"` rather than bracketed — yq's
// own documented spelling.
func YQ(steps []Step) string {
	var b strings.Builder
	for _, s := range steps {
		switch {
		case s.Seq:
			if b.Len() == 0 {
				b.WriteByte('.')
			}
			b.WriteString("[" + strconv.Itoa(s.Index) + "]")
		case plainKey(s.Key):
			b.WriteByte('.')
			b.WriteString(s.Key)
		default:
			b.WriteByte('.')
			b.WriteString(strconv.Quote(s.Key))
		}
	}
	if b.Len() == 0 {
		return "."
	}
	return b.String()
}

// plainKey reports whether a key can be written bare after a dot in jq and yq:
// an identifier, the common case for config keys.
func plainKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
