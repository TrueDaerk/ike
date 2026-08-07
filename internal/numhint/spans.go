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

// Hint is one literal's outcome. Span carries the stand-in when a family
// applies; Claims marks the literals whose reading the field name decided
// (#1685) — a mapped field or a built-in key word — and those claim their
// columns whether or not a hint was produced: no other family may draw a
// stand-in where the field already said what the number means.
type Hint struct {
	Span   lang.Span
	Claims bool
}

// hinted reports whether the hint produced a stand-in of its own.
func (h Hint) hinted() bool { return h.Span.Capture != "" }

// Spans produces the hints for a config-style buffer, ready to append to a
// language's lang.Language.Spans output.
func Spans(lines []string) []lang.Span { return spansOf(Hints(lines)) }

// LineSpans is Spans for a producer that scans only part of a buffer, or that
// has to feed a rewritten line (the log renderer's ANSI-stripped visible text,
// #1684): it produces the hints for one line, reported at line index li.
func LineSpans(li int, line string) []lang.Span { return spansOf(LineHints(li, line)) }

// Hints is Spans down one level, for producers that also have to honour the
// field-name precedence (#1685): every literal is reported, including the ones
// a mapped field silenced, so Allowed can drop the other families' stand-ins
// over them.
func Hints(lines []string) []Hint {
	var out []Hint
	for li, line := range lines {
		out = appendLine(out, li, []rune(line))
	}
	return out
}

// LineHints is Hints for a single line, reported at line index li.
func LineHints(li int, line string) []Hint { return appendLine(nil, li, []rune(line)) }

// HintSpans keeps the hints that produced a stand-in, for producers that scan
// through Hints and emit the spans themselves (#1685).
func HintSpans(hints []Hint) []lang.Span { return spansOf(hints) }

// spansOf keeps the hints that produced a stand-in.
func spansOf(hints []Hint) []lang.Span {
	var out []lang.Span
	for _, h := range hints {
		if h.hinted() {
			out = append(out, h.Span)
		}
	}
	return out
}

// Allowed drops the spans of other families whose columns a field name claimed
// (#1685). The field says what the number means, so a value in a `bytes` field
// draws as a byte size even when its digits also read as a Unix timestamp, and
// a field mapped to `none` shows the raw digits rather than any family's
// stand-in.
func Allowed(other []lang.Span, hints []Hint) []lang.Span {
	if len(other) == 0 || len(hints) == 0 {
		return other
	}
	var claims []lang.Span
	for _, h := range hints {
		if h.Claims {
			claims = append(claims, h.Span)
		}
	}
	if len(claims) == 0 {
		return other
	}
	return Except(other, claims)
}

// SpansWith is the pairing every config-format producer needs: it returns the
// number hints for lines together with the spans of the other family scanned
// over the same buffer — the epoch stand-ins (#1618) — that survive them. A
// field-pinned hint wins over the incoming span (#1685), the incoming span
// wins over a hint the value's shape alone produced (#1627), and both are
// dropped where a field is mapped to `none`.
func SpansWith(lines []string, other []lang.Span) (hints, kept []lang.Span) {
	hs := Hints(lines)
	kept = Allowed(other, hs)
	return Except(spansOf(hs), kept), kept
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
func appendLine(out []Hint, li int, runes []rune) []Hint {
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
				if h, ok := literalHint(li, i+1, j, text, key); ok {
					out = append(out, h)
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
				if h, ok := literalHint(li, i, j, text, key); ok {
					out = append(out, h)
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

// literalHint builds the hint for the token at rune columns [start, end) of
// line li, given the key it hangs off. It reports false when the token is not
// a numeric literal or nothing about it is worth reporting. The family order
// is fixed — radix, byte size, duration, grouping — so a literal never carries
// two hints and rendering is deterministic.
//
// A key the user mapped to a unit (#1685) short-circuits all of it: that unit
// is applied and the literal claims its columns either way.
func literalHint(li, start, end int, text, key string) (Hint, bool) {
	bare := lang.Span{Line: li, StartCol: start, EndCol: end}
	span := func(capture, replace string) (lang.Span, bool) {
		s := bare
		s.Capture, s.Replace = capture, replace
		return s, true
	}
	shape := func(s lang.Span, ok bool) (Hint, bool) { return Hint{Span: s}, ok }
	claim := func(s lang.Span, ok bool) (Hint, bool) {
		if !ok {
			return Hint{}, false
		}
		return Hint{Span: s, Claims: true}, true
	}
	if u, ok := FieldUnit(key); ok {
		if !isDecimal(text) {
			if _, hex := hexLiteral(text); !hex {
				return Hint{}, false
			}
		}
		s, ok := mappedSpan(span, text, u)
		if !ok {
			// The unit renders nothing for this value — a `bytes` field
			// holding 512. The field still claimed the digits: no other
			// family may read them as something else.
			s = bare
		}
		return Hint{Span: s, Claims: true}, true
	}
	if hexDigits, ok := hexLiteral(text); ok {
		dec, ok := DecimalOf(hexDigits)
		if !ok {
			return Hint{}, false
		}
		return shape(span(RadixCapture, text+Gap+"= "+dec))
	}
	if !isDecimal(text) {
		return Hint{}, false
	}
	// A run that decodes as a plausible Unix timestamp (#1618) is one: a
	// duration or a bare count in that range reads as nonsense ("19941d" for
	// an expiry date), so those two families step aside. Byte sizes and radix
	// readings still apply — a gigabyte count lands in the same range by
	// arithmetic, not by meaning — and they claim the digits (#1685), so the
	// timestamp of the producer that decodes epochs too gives way.
	_, isEpoch := epochtime.Decode(text)
	v, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		// Past an uint64 the run is an identifier, but grouping still reads.
		if g, ok := Group(text); ok {
			return shape(span(GroupCapture, g))
		}
		return Hint{}, false
	}
	if isEpoch {
		if sizeKey(key) {
			if s, ok := FormatBytes(v); ok {
				return claim(span(SizeCapture, s))
			}
		}
		if r := radixOf(key); r != radixNone {
			return claim(radixSpan(span, r, text, v))
		}
		return Hint{}, false
	}
	if r := radixOf(key); r != radixNone {
		if s, ok := radixSpan(span, r, text, v); ok {
			return claim(s, true)
		}
	}
	if sizeKey(key) {
		if s, ok := FormatBytes(v); ok {
			return claim(span(SizeCapture, s))
		}
	}
	if base, ok := durationBase(key); ok {
		if s, ok := FormatDuration(v, base); ok {
			return claim(span(DurationCapture, s))
		}
	}
	// The shape trigger comes after both key families: a duration in
	// milliseconds is a multiple of 1024 often enough (86400000 is) that the
	// key has to win when it names one.
	if v >= sizeStepBytes && v%sizeStepBytes == 0 {
		if s, ok := FormatBytes(v); ok {
			return shape(span(SizeCapture, s))
		}
	}
	if g, ok := Group(text); ok {
		return shape(span(GroupCapture, g))
	}
	return Hint{}, false
}

// LiteralHint is literalHint exported for the code-constant producer (#1701):
// a constant assignment's single-literal right-hand side reads exactly like a
// keyed config value — user mapping first, built-in key words second, the
// value's shape third.
func LiteralHint(li, start, end int, text, key string) (Hint, bool) {
	return literalHint(li, start, end, text, key)
}

// KeyUnit returns the unit the built-in key heuristics read off a field name —
// the reading literalHint applies when no user mapping covers the key —
// exported for the code-constant producer (#1701), which renders computed
// expression values and has no literal to hand literalHint. The order mirrors
// literalHint: radix, byte size, duration.
func KeyUnit(key string) (Unit, bool) {
	switch radixOf(key) {
	case radixOctal:
		return Unit{Kind: UnitOctal}, true
	case radixHex:
		return Unit{Kind: UnitHex}, true
	}
	if sizeKey(key) {
		return Unit{Kind: UnitBytes}, true
	}
	if base, ok := durationBase(key); ok {
		return Unit{Kind: UnitDuration, Base: base}, true
	}
	return Unit{}, false
}

// mappedSpan renders a literal in the unit its field name is mapped to
// (#1685). The mapping is the user's word on the field, so it is never
// second-guessed: only that family is tried, and a value the unit says nothing
// about — 512 in a `bytes` field, a run past uint64 — gets no stand-in rather
// than falling through to another family.
func mappedSpan(span func(capture, replace string) (lang.Span, bool), text string, u Unit) (lang.Span, bool) {
	if u.Kind == UnitOff {
		return lang.Span{}, false
	}
	if hexDigits, ok := hexLiteral(text); ok {
		// A hex literal has one reading whatever the field is mapped to: its
		// decimal value. Rendering a hex literal as bytes or a date would hide
		// the digits the mapping cannot be about.
		if dec, ok := DecimalOf(hexDigits); ok {
			return span(RadixCapture, text+Gap+"= "+dec)
		}
		return lang.Span{}, false
	}
	if u.Kind == UnitTimestampSeconds || u.Kind == UnitTimestampMillis {
		decode := epochtime.DecodeSeconds
		if u.Kind == UnitTimestampMillis {
			decode = epochtime.DecodeMillis
		}
		if s, ok := decode(text); ok {
			return span(epochtime.Capture, s)
		}
		return lang.Span{}, false
	}
	v, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		if u.Kind == UnitGroup {
			if g, ok := Group(text); ok {
				return span(GroupCapture, g)
			}
		}
		return lang.Span{}, false
	}
	switch u.Kind {
	case UnitBytes:
		if s, ok := FormatBytes(v); ok {
			return span(SizeCapture, s)
		}
	case UnitDuration:
		if s, ok := FormatDuration(v, u.Base); ok {
			return span(DurationCapture, s)
		}
	case UnitOctal:
		return radixSpan(span, radixOctal, text, v)
	case UnitHex:
		return radixSpan(span, radixHex, text, v)
	case UnitGroup:
		if g, ok := Group(text); ok {
			return span(GroupCapture, g)
		}
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
