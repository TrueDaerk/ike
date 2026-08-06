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
	"ike/internal/numhint"
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
		Spans: envSpans,
		// Duplicate keys (#1623): the earlier assignment loses silently in
		// most loaders, so it is marked. See lint.go.
		Lint:        envLint,
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
		// Masking spans come first: overlapping spans resolve first-covering-
		// wins, so the mask must precede the value's own string span (#1623).
		out = append(out, maskSpans(li, runes, start, end)...)
		out = append(out, pairSpans(li, runes, start, end)...)
		// JWTs anywhere on the line (#1619): the signature segment dims. The
		// scan is structural — three base64url segments, the first two decoding
		// to JSON — so a plain value can never be mistaken for a token.
		out = append(out, jwt.LineSpans(li, line)...)
	}
	// Number-readability hints (#1627): byte sizes, durations, digit grouping
	// and radix readings over the `KEY=value` pairs.
	return append(out, numhint.Spans(lines)...)
}

// entry is one parsed assignment line, in rune columns. It is the single
// structural read of a line: the span producer, the secret masker (#1623) and
// the duplicate-key linter all work from it, so what counts as "the key" can
// never drift between them.
type entry struct {
	exportStart, exportEnd int    // the `export` prefix; equal when absent
	keyStart, keyEnd       int    // the key, whitespace-trimmed
	sep                    int    // column of `=`, or -1 on a line without one
	valStart, valEnd       int    // the value, leading whitespace skipped; equal when empty
	key                    string // the key text, for the secret and duplicate tables
}

// parseEntry reads the assignment structure out of the content range
// [start, end) of a line. A line without `=` yields sep == -1 and the whole
// range as the key.
func parseEntry(runes []rune, start, end int) entry {
	e := entry{exportStart: start, exportEnd: start, sep: -1}
	if kw := exportEnd(runes, start, end); kw > start {
		e.exportEnd = kw
		for kw < end && isSpace(runes[kw]) {
			kw++
		}
		start = kw
	}
	for i := start; i < end; i++ {
		if runes[i] == '=' {
			e.sep = i
			break
		}
	}
	keyEnd := end
	if e.sep >= 0 {
		keyEnd = trimEnd(runes, start, e.sep)
	}
	e.keyStart, e.keyEnd = start, keyEnd
	e.key = string(runes[start:keyEnd])
	if e.sep < 0 {
		e.valStart, e.valEnd = end, end
		return e
	}
	vs := e.sep + 1
	for vs < end && isSpace(runes[vs]) {
		vs++
	}
	e.valStart, e.valEnd = vs, end
	return e
}

// pairSpans styles one `KEY=value` line: an optional `export` prefix as a
// keyword, the key as property, the `=` as punctuation, the value as string.
// A line without `=` styles whole as property.
func pairSpans(li int, runes []rune, start, end int) []lang.Span {
	var out []lang.Span
	e := parseEntry(runes, start, end)
	if e.exportEnd > e.exportStart {
		out = append(out, lang.Span{Line: li, StartCol: e.exportStart, EndCol: e.exportEnd, Capture: "keyword"})
	}
	if e.keyEnd > e.keyStart {
		out = append(out, lang.Span{Line: li, StartCol: e.keyStart, EndCol: e.keyEnd, Capture: "property"})
	}
	if e.sep < 0 {
		return out
	}
	out = append(out, lang.Span{Line: li, StartCol: e.sep, EndCol: e.sep + 1, Capture: "punctuation"})
	if e.valEnd > e.valStart {
		out = append(out, lang.Span{Line: li, StartCol: e.valStart, EndCol: e.valEnd, Capture: "string"})
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
