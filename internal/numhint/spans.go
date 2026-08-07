package numhint

import (
	"strconv"
	"strings"
	"time"

	"ike/internal/epochtime"
	"ike/internal/lang"
)

// spans.go is the context half of the number hints (#1627): which literal in a
// buffer gets which hint. Two inputs decide it — the key a value hangs off and
// the shape of the number itself:
//
//   - Key names carry the intent config formats have no types for. `*size*`,
//     `*bytes*`, `*memory*` name a byte count; `*timeout*`, `*ttl*`,
//     `*interval*` name a duration, with `*_ms`/`*_seconds`-style unit words
//     pinning the base; `*mode*`/`*mask*` name a radix. A weak word alone —
//     `limit`, `max` — is deliberately not enough: a rate limit is not a byte
//     count, and `max_body_limit_bytes` already matches on `bytes`.
//   - Shape covers the unkeyed cases: a multiple of 1024 is a byte size
//     wherever it appears, a `0x` literal always has a decimal reading, and a
//     five-digit-or-longer plain integer always reads better grouped.
//
// Scanning is line-local and format-neutral: `key: value` (JSON, YAML) and
// `key = value` (TOML, ini, dotenv) both reduce to "the token before the
// separator names the tokens after it", which also gets JSON one-liners and
// arrays right ({"size": 1024, "ttl_ms": 90000}, "sizes": [1024, 2048]).
// Tokens glue on the characters that make a digit run part of something larger
// (`.`, `-`, `/`, `%`, letters), so versions, dates, floats, paths and
// percentages never turn into hints.

// Spans produces the hints for a config-style buffer, ready to append to a
// language's lang.Language.Spans output.
func Spans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = appendLine(out, li, []rune(line))
	}
	return out
}

// LineSpans is Spans for a producer that scans only part of a buffer, or that
// has to feed a rewritten line (the log renderer's ANSI-stripped visible text,
// #1684): it produces the hints for one line, reported at line index li.
func LineSpans(li int, line string) []lang.Span {
	return appendLine(nil, li, []rune(line))
}

// Except drops every span whose columns a taken span already covers. It is the
// filter behind SpansExcept, exported for producers that build their hint list
// line by line (#1684).
func Except(spans, taken []lang.Span) []lang.Span {
	if len(taken) == 0 {
		return spans
	}
	kept := spans[:0]
	for _, s := range spans {
		if !overlapsAny(s, taken) {
			kept = append(kept, s)
		}
	}
	return kept
}

// SpansExcept is Spans for a producer that emits stand-ins of its own over the
// same digits — the JSON epoch decoding (#1618) — dropping every hint whose
// columns a taken span already covers. Two stand-ins over one literal would
// fight for the same cells, and the older family wins by construction: a
// 10-digit value in a JSON member is a timestamp far more often than it is a
// gigabyte count.
func SpansExcept(lines []string, taken []lang.Span) []lang.Span {
	return Except(Spans(lines), taken)
}

func overlapsAny(s lang.Span, taken []lang.Span) bool {
	for _, t := range taken {
		if t.Line == s.Line && s.StartCol < t.EndCol && t.StartCol < s.EndCol {
			return true
		}
	}
	return false
}

// appendLine scans one line, tracking the key the current value hangs off.
func appendLine(out []lang.Span, li int, runes []rune) []lang.Span {
	runes = runes[:contentEnd(runes)]
	if start := skipSpace(runes, 0); start >= len(runes) || isCommentStart(runes, start) {
		return out
	}
	key, last := "", ""
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case r == '"' || r == '\'':
			j := i + 1
			for j < len(runes) && runes[j] != r {
				j++
			}
			if j >= len(runes) {
				return out // unterminated quote: the rest is not structure
			}
			text := string(runes[i+1 : j])
			if !keyAhead(runes, j+1) {
				if s, ok := literalSpan(li, i+1, j, text, key); ok {
					out = append(out, s)
				}
			}
			last, i = text, j+1
		case isTokenRune(r):
			j := i
			for j < len(runes) && isTokenRune(runes[j]) {
				j++
			}
			text := string(runes[i:j])
			if !keyAhead(runes, j) {
				if s, ok := literalSpan(li, i, j, text, key); ok {
					out = append(out, s)
				}
			}
			last, i = text, j
		case r == ':' || r == '=':
			key, last = last, ""
			i++
		case isSpace(r):
			// Blanks do not end the key: `ttl = 3600` names its value too.
			i++
		default:
			last = ""
			i++
		}
	}
	return out
}

// literalSpan builds the hint span for the token at rune columns [start, end)
// of line li, given the key it hangs off. It reports false when the token is
// not a numeric literal or no family applies. The family order is fixed —
// radix, byte size, duration, grouping — so a literal never carries two hints
// and rendering is deterministic.
func literalSpan(li, start, end int, text, key string) (lang.Span, bool) {
	span := func(capture, replace string) (lang.Span, bool) {
		return lang.Span{
			Line: li, StartCol: start, EndCol: end,
			Capture: capture, Replace: replace,
		}, true
	}
	if hexDigits, ok := hexLiteral(text); ok {
		dec, ok := DecimalOf(hexDigits)
		if !ok {
			return lang.Span{}, false
		}
		return span(RadixCapture, text+Gap+"= "+dec)
	}
	if !isDecimal(text) {
		return lang.Span{}, false
	}
	// A run that decodes as a plausible Unix timestamp (#1618) is one: a
	// duration or a bare count in that range reads as nonsense ("19941d" for
	// an expiry date), so those two families step aside. Byte sizes and radix
	// readings still apply — a gigabyte count lands in the same range by
	// arithmetic, not by meaning — and SpansExcept resolves the collision in
	// the one producer that decodes epochs too.
	_, isEpoch := epochtime.Decode(text)
	v, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		// Past an uint64 the run is an identifier, but grouping still reads.
		if g, ok := Group(text); ok {
			return span(GroupCapture, g)
		}
		return lang.Span{}, false
	}
	if isEpoch {
		if sizeKey(key) {
			if s, ok := FormatBytes(v); ok {
				return span(SizeCapture, s)
			}
		}
		if r := radixOf(key); r != radixNone {
			return radixSpan(span, r, text, v)
		}
		return lang.Span{}, false
	}
	if r := radixOf(key); r != radixNone {
		if s, ok := radixSpan(span, r, text, v); ok {
			return s, true
		}
	}
	if sizeKey(key) {
		if s, ok := FormatBytes(v); ok {
			return span(SizeCapture, s)
		}
	}
	if base, ok := durationBase(key); ok {
		if s, ok := FormatDuration(v, base); ok {
			return span(DurationCapture, s)
		}
	}
	// The shape trigger comes after both key families: a duration in
	// milliseconds is a multiple of 1024 often enough (86400000 is) that the
	// key has to win when it names one.
	if v >= sizeStepBytes && v%sizeStepBytes == 0 {
		if s, ok := FormatBytes(v); ok {
			return span(SizeCapture, s)
		}
	}
	if g, ok := Group(text); ok {
		return span(GroupCapture, g)
	}
	return lang.Span{}, false
}

// radixSpan renders a decimal in the base its key context is read in. Values
// whose reading is the same digits in both bases get no hint.
func radixSpan(span func(capture, replace string) (lang.Span, bool), r radix, text string, v uint64) (lang.Span, bool) {
	switch {
	case r == radixOctal && v >= 8:
		return span(RadixCapture, text+Gap+"= "+OctalOf(v))
	case r == radixHex && v >= 10:
		return span(RadixCapture, text+Gap+"= "+HexOf(v))
	}
	return lang.Span{}, false
}

// sizeWords name a byte count on their own. Weak quantifiers (limit, max,
// quota) are absent by design: they say how much of something, not of bytes.
var sizeWords = []string{"size", "byte", "memory", "capacity", "buffer", "payload", "storage"}

// durationWords name a duration whose base unit is conventional rather than
// spelled out: the timeout family is written in milliseconds (Java, JS and
// most broker configs), the TTL family in seconds (HTTP, DNS).
var (
	durationMillisWords = []string{"timeout", "interval", "delay", "duration", "backoff", "period", "latency", "elapsed"}
	durationSecondWords = []string{"ttl", "expires", "expiry", "expiration", "lifetime", "lease", "max_age", "maxage"}
)

// unitWords pin the base unit explicitly. They are matched against the key's
// last word, so `params` is not a millisecond key and `flush_ms` is.
var unitWords = map[string]time.Duration{
	"ns": time.Nanosecond, "nanos": time.Nanosecond, "nanoseconds": time.Nanosecond,
	"us": time.Microsecond, "micros": time.Microsecond, "microseconds": time.Microsecond,
	"ms": time.Millisecond, "msec": time.Millisecond, "msecs": time.Millisecond,
	"millis": time.Millisecond, "milliseconds": time.Millisecond,
	"s": time.Second, "sec": time.Second, "secs": time.Second, "seconds": time.Second,
	"min": time.Minute, "mins": time.Minute, "minutes": time.Minute,
	"h": time.Hour, "hr": time.Hour, "hrs": time.Hour, "hours": time.Hour,
	"d": 24 * time.Hour, "days": 24 * time.Hour,
}

// radix selects the reading a decimal literal gains in a key context that is
// conventionally written in another base.
type radix int

const (
	radixNone radix = iota
	radixHex
	radixOctal
)

// radixOf classifies a key as a permission (octal) or bit-flag (hex) context.
// Permissions are checked first: `umask` carries both words.
func radixOf(key string) radix {
	k := strings.ToLower(key)
	for _, w := range []string{"umask", "mode", "perm"} {
		if strings.Contains(k, w) {
			return radixOctal
		}
	}
	for _, w := range []string{"mask", "flag"} {
		if strings.Contains(k, w) {
			return radixHex
		}
	}
	return radixNone
}

// sizeKey reports whether the key names a byte count.
func sizeKey(key string) bool {
	k := strings.ToLower(key)
	for _, w := range sizeWords {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// durationBase returns the unit a key's value is counted in: the unit word the
// key ends with when it spells one out, the conventional base of the duration
// family it names otherwise.
func durationBase(key string) (time.Duration, bool) {
	words := keyWords(key)
	if len(words) > 1 {
		if d, ok := unitWords[words[len(words)-1]]; ok {
			return d, true
		}
	}
	k := strings.ToLower(key)
	for _, w := range durationSecondWords {
		if strings.Contains(k, w) {
			return time.Second, true
		}
	}
	for _, w := range durationMillisWords {
		if strings.Contains(k, w) {
			return time.Millisecond, true
		}
	}
	return 0, false
}

// keyWords splits a key into lowercase words on both separators and camel-case
// boundaries, so `TIMEOUT_MS`, `timeout-ms` and `timeoutMs` all end in "ms".
func keyWords(key string) []string {
	var out []string
	runes := []rune(key)
	start := 0
	flush := func(end int) {
		if end > start {
			out = append(out, strings.ToLower(string(runes[start:end])))
		}
		start = end
	}
	for i, r := range runes {
		switch {
		case !isLetter(r) && !isDigit(r):
			flush(i)
			start = i + 1
		case i > 0 && isUpper(r) && !isUpper(runes[i-1]):
			flush(i)
		}
	}
	flush(len(runes))
	return out
}

// hexLiteral reports whether text is a `0x` literal, returning its digits.
func hexLiteral(text string) (string, bool) {
	if len(text) < 3 || text[0] != '0' || (text[1] != 'x' && text[1] != 'X') {
		return "", false
	}
	for _, r := range text[2:] {
		if !isHexDigit(r) {
			return "", false
		}
	}
	return text[2:], true
}

// isDecimal reports whether text is a plain unsigned digit run without the
// leading zero that marks an id or a zero-padded field rather than a quantity.
func isDecimal(text string) bool {
	if len(text) == 0 || (len(text) > 1 && text[0] == '0') {
		return false
	}
	for _, r := range text {
		if !isDigit(r) {
			return false
		}
	}
	return true
}

// keyAhead reports whether the next significant rune from i is a key/value
// separator — the token before it names a value, it is not one.
func keyAhead(runes []rune, i int) bool {
	switch nextSignificant(runes, i) {
	case ':', '=':
		return true
	}
	return false
}

// contentEnd cuts a trailing ` #` or ` //` comment off a line, so numbers in
// prose after the value are left alone.
func contentEnd(runes []rune) int {
	for i := 1; i < len(runes); i++ {
		if !isSpace(runes[i-1]) {
			continue
		}
		if runes[i] == '#' || (runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '/') {
			return i
		}
	}
	return len(runes)
}

// isCommentStart reports whether a line's first non-blank rune opens a comment
// in any of the covered formats.
func isCommentStart(runes []rune, i int) bool {
	switch runes[i] {
	case '#', ';':
		return true
	case '/':
		return i+1 < len(runes) && runes[i+1] == '/'
	}
	return false
}

// isTokenRune reports whether r continues a token. The set is deliberately
// wide: gluing `.`, `-`, `+`, `/` and `%` into the token is what keeps floats,
// dates, versions, paths and percentages from parsing as bare integers.
func isTokenRune(r rune) bool {
	switch r {
	case '_', '.', '-', '+', '/', '%', '\\', '@', '$':
		return true
	}
	return isLetter(r) || isDigit(r)
}

func nextSignificant(runes []rune, from int) rune {
	for i := from; i < len(runes); i++ {
		if !isSpace(runes[i]) {
			return runes[i]
		}
	}
	return 0
}

func skipSpace(runes []rune, i int) int {
	for i < len(runes) && isSpace(runes[i]) {
		i++
	}
	return i
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isHexDigit(r rune) bool {
	return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
