// Package epochtime detects numeric Unix epoch timestamps in a line of text
// and renders them as a human-readable UTC form (#1618). It is the detection
// half of the inline timestamp decoding: the editor's conceal layer (#1585's
// stand-in mechanic, #1594's positional reveal) shows the decoded form on
// lines the caret is not on, and the raw digits reappear under the caret.
//
// It is a leaf package — pure Go over internal/lang's span type — so every
// span producer that wants the feature (the JSON languages, the log language,
// the .http bodies) can call it without a dependency cycle.
//
// Detection is deliberately conservative: ordinary numeric literals must not
// turn into dates. Two guards do the work.
//
//   - Range: only values between 2001-01-01 and 2100-01-01 decode, as seconds
//     (9–10 digits) or milliseconds (12–13 digits). Everything outside — port
//     numbers, byte counts, ids, years — is left alone.
//   - Context: the caller picks how permissive the surrounding text must be.
//     JSONValue accepts a digit run only in a JSON value position (after ":",
//     "[" or "," and before ",", "]", "}" or the line end), so object keys and
//     digits inside prose strings never match. Loose accepts any run whose
//     neighbouring characters are plain delimiters, which is what unstructured
//     log lines need.
//
// Both contexts additionally reject a run glued to a character that makes it
// part of something larger: a letter or "_" (identifiers), "." (floats, IPs,
// versions), "-" (ISO dates, ids), ":" (clock times), "/" (paths, dates), "%"
// and "+". The one exception is a leading ":" in JSONValue, which is the
// member separator there, not a clock: {"ts":1722945600} decodes.
package epochtime

import (
	"fmt"
	"strconv"
	"time"

	"ike/internal/lang"
)

// Capture is the highlight capture name carried by the produced spans. The
// editor routes spans with this capture and a stand-in into its own conceal
// channel, gated by editor.timestamp_decoding, so the feature toggles apart
// from the markdown (#1599) and log (#1621) rendering layers.
const Capture = "timestamp"

// Context selects how strict the surrounding-text check is.
type Context int

const (
	// JSONValue accepts a digit run only where JSON puts a value: preceded by
	// ":", "[" or "," and followed by ",", "]", "}" or the line end. Quoted
	// runs ("1722945600") count too — the digits alone are the match.
	JSONValue Context = iota
	// Loose accepts any digit run delimited by plain, non-gluing characters.
	// It is the mode for unstructured text (log lines), where the range guard
	// carries the whole burden.
	Loose
)

// Bounds of the plausible range, as seconds since the epoch: 2001-01-01 to
// 2100-01-01 UTC. A value outside is not treated as a timestamp.
const (
	minSeconds = 978307200  // 2001-01-01T00:00:00Z
	maxSeconds = 4102444800 // 2100-01-01T00:00:00Z
)

// Match is one decoded timestamp on a line: the half-open rune-column range
// [Start, End) of the raw digits and the UTC form they decode to.
type Match struct {
	Start, End int
	Text       string
}

// Scan returns the epoch timestamps in line, left to right.
func Scan(line string, ctx Context) []Match {
	runes := []rune(line)
	var out []Match
	for i := 0; i < len(runes); {
		if !isDigit(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && isDigit(runes[j]) {
			j++
		}
		if text, ok := decode(string(runes[i:j])); ok && accepted(runes, i, j, ctx) {
			out = append(out, Match{Start: i, End: j, Text: text})
		}
		i = j
	}
	return out
}

// Spans produces the conceal-with-stand-in spans for lines, ready to be
// appended to a language's lang.Language.Spans output. Every span keeps
// Capture, so the raw digits still style as a timestamp while revealed.
func Spans(lines []string, ctx Context) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = appendSpans(out, li, line, ctx)
	}
	return out
}

// LineSpans is Spans for a single line at index li — for producers that scan
// only part of a buffer (a .http request body).
func LineSpans(li int, line string, ctx Context) []lang.Span {
	return appendSpans(nil, li, line, ctx)
}

func appendSpans(out []lang.Span, li int, line string, ctx Context) []lang.Span {
	for _, m := range Scan(line, ctx) {
		out = append(out, lang.Span{
			Line: li, StartCol: m.Start, EndCol: m.End,
			Capture: Capture, Replace: m.Text,
		})
	}
	return out
}

// Decode renders an isolated digit run as its UTC form, applying the same
// range guard as the inline detection. It exists for callers that already know
// a number is a timestamp and only want the rendering — the JWT registered
// claims (#1619) — so they need no second copy of the epoch heuristics.
func Decode(digits string) (string, bool) { return decode(digits) }

// decode turns a digit run into its UTC rendering. Seconds are 9–10 digits,
// milliseconds 12–13; a leading zero disqualifies the run (no timestamp is
// ever written that way, but zero-padded ids are).
func decode(digits string) (string, bool) {
	if len(digits) == 0 || digits[0] == '0' {
		return "", false
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return "", false
	}
	switch len(digits) {
	case 9, 10:
		if n < minSeconds || n >= maxSeconds {
			return "", false
		}
		return format(time.Unix(n, 0)), true
	case 12, 13:
		if n < minSeconds*1000 || n >= maxSeconds*1000 {
			return "", false
		}
		return format(time.UnixMilli(n)), true
	}
	return "", false
}

// format renders the UTC form: "2024-08-06 12:00:00Z", with a millisecond
// fraction appended when the value carries one.
func format(t time.Time) string {
	t = t.UTC()
	s := t.Format("2006-01-02 15:04:05")
	if ms := t.Nanosecond() / int(time.Millisecond); ms != 0 {
		s += fmt.Sprintf(".%03d", ms)
	}
	return s + "Z"
}

// accepted applies the surrounding-text guards to the run [i, j).
func accepted(runes []rune, i, j int, ctx Context) bool {
	prev, next := charAt(runes, i-1), charAt(runes, j)
	if prev == '"' && next == '"' {
		// Quoted run: the quotes are the delimiters, so the position check
		// looks at the characters outside them.
		if ctx == Loose {
			return true
		}
		return jsonValuePos(runes, i-1, j+1)
	}
	if ctx == JSONValue {
		// ":" glues in prose (a clock time) but is JSON's member separator,
		// so a value may sit directly against it: {"ts":1722945600}.
		if (glues(prev) && prev != ':') || glues(next) {
			return false
		}
		return jsonValuePos(runes, i, j)
	}
	if glues(prev) || glues(next) {
		return false
	}
	return true
}

// glues reports whether r, sitting directly against a digit run, makes the run
// part of a larger token (an identifier, float, date, clock time, path, …).
// The zero rune — the line edge — never glues.
func glues(r rune) bool {
	switch r {
	case 0:
		return false
	case '_', '.', '-', '+', ':', '/', '%', '"', '\'':
		return true
	}
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || isDigit(r)
}

// jsonValuePos reports whether [start, end) sits where JSON puts a value: the
// nearest non-blank character before it opens a value (":" for a member, "["
// or "," for an element) and the one after it closes one (",", "]", "}") or
// the line ends.
func jsonValuePos(runes []rune, start, end int) bool {
	prev := prevSignificant(runes, start)
	switch prev {
	case ':', '[', ',':
	default:
		return false
	}
	switch nextSignificant(runes, end) {
	case ',', ']', '}', 0:
		return true
	}
	return false
}

func prevSignificant(runes []rune, from int) rune {
	for i := from - 1; i >= 0; i-- {
		if !isBlank(runes[i]) {
			return runes[i]
		}
	}
	return 0
}

func nextSignificant(runes []rune, from int) rune {
	for i := from; i < len(runes); i++ {
		if !isBlank(runes[i]) {
			return runes[i]
		}
	}
	return 0
}

// charAt returns the rune at i, or 0 outside the line.
func charAt(runes []rune, i int) rune {
	if i < 0 || i >= len(runes) {
		return 0
	}
	return runes[i]
}

func isBlank(r rune) bool { return r == ' ' || r == '\t' }

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
