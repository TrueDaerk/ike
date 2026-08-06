// Package langenv registers the dotenv language (#1619): the `KEY=value`
// files (`.env`, `.env.local`, `foo.env`) that hold a project's secrets, and
// therefore hold most of the JWTs anyone ever stares at. Like ini (#1595) and
// csv (#1589) there is no Tree-sitter grammar — the whole structure is
// Go-computed through the lang.Language.Spans seam (#1585): keys style as
// property, the `=` as punctuation, values as string, `#` lines as comment,
// and any JWT in a value gets its signature segment dimmed by internal/jwt.
// Self-registers via init(); blank-imported in cmd/ike/main.go.
package langenv

import (
	"ike/internal/jwt"
	"ike/internal/lang"
	"ike/plugins/languages/register"
)

func init() {
	register.Language(lang.Language{
		ID:         "dotenv",
		Extensions: []string{"env"},
		// The common dotted variants: filepath.Ext(".env.local") is ".local",
		// so the extension index alone would miss them.
		Filenames: []string{
			".env", ".env.local", ".env.example", ".env.sample", ".env.template",
			".env.development", ".env.production", ".env.test", ".env.staging",
		},
		Spans:       envSpans,
		LineComment: "#",
	})
}

// envSpans emits the highlight spans for a dotenv buffer, line by line. Only
// full-line comments are recognised: a `#` inside a value stays part of the
// value, since a URL fragment or a password may contain one.
func envSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		start, end := trimIndex(runes)
		if start >= end {
			continue
		}
		if runes[start] == '#' {
			out = append(out, lang.Span{Line: li, StartCol: start, EndCol: end, Capture: "comment"})
			continue
		}
		out = append(out, pairSpans(li, runes, start, end)...)
		// JWTs anywhere on the line (#1619): the signature segment dims. The
		// scan is structural — three base64url segments, the first two decoding
		// to JSON — so a plain value can never be mistaken for a token.
		out = append(out, jwt.LineSpans(li, line)...)
	}
	return out
}

// pairSpans styles one `KEY=value` line: an optional `export` prefix as a
// keyword, the key as property, the `=` as punctuation, the value as string.
// A line without `=` styles whole as property.
func pairSpans(li int, runes []rune, start, end int) []lang.Span {
	var out []lang.Span
	if kw := exportEnd(runes, start, end); kw > start {
		out = append(out, lang.Span{Line: li, StartCol: start, EndCol: kw, Capture: "keyword"})
		for kw < end && isSpace(runes[kw]) {
			kw++
		}
		start = kw
	}
	sep := -1
	for i := start; i < end; i++ {
		if runes[i] == '=' {
			sep = i
			break
		}
	}
	if sep < 0 {
		return append(out, lang.Span{Line: li, StartCol: start, EndCol: end, Capture: "property"})
	}
	if ke := trimEnd(runes, start, sep); ke > start {
		out = append(out, lang.Span{Line: li, StartCol: start, EndCol: ke, Capture: "property"})
	}
	out = append(out, lang.Span{Line: li, StartCol: sep, EndCol: sep + 1, Capture: "punctuation"})
	vs := sep + 1
	for vs < end && isSpace(runes[vs]) {
		vs++
	}
	if vs < end {
		out = append(out, lang.Span{Line: li, StartCol: vs, EndCol: end, Capture: "string"})
	}
	return out
}

// exportEnd returns the end column of a leading `export ` keyword, or start
// when the line has none.
func exportEnd(runes []rune, start, end int) int {
	const kw = "export"
	if end-start <= len(kw) || string(runes[start:start+len(kw)]) != kw {
		return start
	}
	if !isSpace(runes[start+len(kw)]) {
		return start
	}
	return start + len(kw)
}

// trimIndex returns the rune-column range [start, end) of the line with
// surrounding whitespace stripped; start >= end means a blank line.
func trimIndex(runes []rune) (start, end int) {
	end = len(runes)
	for start < end && isSpace(runes[start]) {
		start++
	}
	for end > start && isSpace(runes[end-1]) {
		end--
	}
	return start, end
}

// trimEnd shrinks end back over trailing whitespace within [start, end).
func trimEnd(runes []rune, start, end int) int {
	for end > start && isSpace(runes[end-1]) {
		end--
	}
	return end
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }
