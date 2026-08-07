package consthint

import (
	"strconv"
	"strings"

	"ike/internal/epochtime"
	"ike/internal/lang"
	"ike/internal/numhint"
)

// spans.go is the per-language half of the constant conceals (#1701): which
// line of a buffer is a constant assignment, and where its right-hand side
// starts. Each language keys on the thing that marks a constant in that
// language:
//
//   - Python — a CONST_CASE name (`MAX_BYTES = …`), the convention that is all
//     Python has; an optional annotation (`RETRIES: Final = 3`) is skipped.
//     Lowercase assignments are left alone, so ordinary locals never conceal.
//   - Go — the `const` keyword: single-line declarations and the lines of a
//     `const ( … )` block, with an optional type between name and `=`.
//   - PHP — `const NAME = …` (with visibility/final modifiers and an optional
//     type) and `define('NAME', …)`.
//
// The name decides the reading exactly like a config key does (#1627, #1685):
// the user's number_hint_units mapping first, the built-in key words second,
// the value's shape third. A plain decimal literal goes through the same
// literalHint the config formats use; everything else runs the expression
// evaluator, and a right-hand side holding anything beyond literal arithmetic
// produces nothing.

// PythonSpans produces the constant conceals for a Python buffer, ready to
// append to the language's lang.Language.Spans output.
func PythonSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		if s, ok := pythonLine(li, []rune(line)); ok {
			out = append(out, s)
		}
	}
	return out
}

// GoSpans produces the constant conceals for a Go buffer: single-line `const`
// declarations plus the lines of `const ( … )` blocks.
func GoSpans(lines []string) []lang.Span {
	var out []lang.Span
	inBlock := false
	for li, line := range lines {
		runes := []rune(line)
		t := strings.TrimSpace(line)
		switch {
		case inBlock && strings.HasPrefix(t, ")"):
			inBlock = false
		case inBlock:
			if s, ok := goConstLine(li, runes, 0); ok {
				out = append(out, s)
			}
		case strings.HasPrefix(t, "const ("):
			inBlock = true
		case strings.HasPrefix(t, "const "):
			from := skipSpace(runes, 0) + len("const ")
			if s, ok := goConstLine(li, runes, from); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// PHPSpans produces the constant conceals for a PHP buffer: `const` and
// `define()` declarations.
func PHPSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		if s, ok := phpConstLine(li, runes); ok {
			out = append(out, s)
		} else if s, ok := phpDefineLine(li, runes); ok {
			out = append(out, s)
		}
	}
	return out
}

// --- Python ------------------------------------------------------------------

// pythonLine reads one `NAME = expr` / `NAME: annotation = expr` line. The
// name must be CONST_CASE — Python's only constant marker — so ordinary
// assignments never produce a span.
func pythonLine(li int, runes []rune) (lang.Span, bool) {
	i := skipSpace(runes, 0)
	name, j := identAt(runes, i)
	if !pythonConstName(name) {
		return lang.Span{}, false
	}
	j = skipSpace(runes, j)
	if j < len(runes) && runes[j] == ':' {
		// An annotation: skipped, not parsed. The rune set is wide enough for
		// `Final`, `Final[int]` and a quoted forward reference; anything else
		// means the line is not the assignment it looked like.
		for j++; j < len(runes) && runes[j] != '='; j++ {
			if !annotationRune(runes[j]) {
				return lang.Span{}, false
			}
		}
	}
	if j >= len(runes) || runes[j] != '=' || (j+1 < len(runes) && runes[j+1] == '=') {
		return lang.Span{}, false
	}
	start := skipSpace(runes, j+1)
	end := trimEnd(runes, start, cutAtHash(runes, start))
	if start >= end {
		return lang.Span{}, false
	}
	return hintSpan(li, start, end, string(runes[start:end]), name, FlavorPython)
}

// pythonConstName reports whether name is CONST_CASE: leading underscores
// allowed (a module-private constant), then an uppercase run of two or more
// characters. Single letters stay raw — `N = 8` is a loop bound, not a config.
func pythonConstName(name string) bool {
	name = strings.TrimLeft(name, "_")
	if len(name) < 2 || !isUpper(rune(name[0])) {
		return false
	}
	for _, r := range name {
		if !isUpper(r) && !isDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

// annotationRune is the rune set a skipped Python annotation may hold.
func annotationRune(r rune) bool {
	switch r {
	case '_', '.', '[', ']', ',', ' ', '\t', '"', '\'':
		return true
	}
	return isLetter(r) || isDigit(r)
}

// --- Go ----------------------------------------------------------------------

// goConstLine reads one `Name [Type] = expr` declaration starting at rune
// index from — the body of a single-line `const` or one line of a block. The
// `const` keyword is the constant marker, so any identifier case qualifies;
// an `iota` or any other identifier on the right-hand side fails the
// evaluator and produces nothing.
func goConstLine(li int, runes []rune, from int) (lang.Span, bool) {
	end := cutAtSlashes(runes, from)
	i := skipSpace(runes, from)
	name, j := identAt(runes, i)
	if name == "" || name == "_" {
		return lang.Span{}, false
	}
	j = skipSpace(runes, j)
	if j < end && runes[j] != '=' {
		// The optional type, one token up to the `=`.
		k := j
		for k < end && runes[k] != '=' && !isSpace(runes[k]) {
			k++
		}
		if k == j {
			return lang.Span{}, false
		}
		j = skipSpace(runes, k)
	}
	if j >= end || runes[j] != '=' || (j+1 < end && runes[j+1] == '=') {
		return lang.Span{}, false
	}
	start := skipSpace(runes, j+1)
	e := trimEnd(runes, start, end)
	if start >= e {
		return lang.Span{}, false
	}
	return hintSpan(li, start, e, string(runes[start:e]), name, FlavorGo)
}

// --- PHP ---------------------------------------------------------------------

// phpModifiers are the keywords that may precede `const` in a class body.
var phpModifiers = map[string]bool{
	"public": true, "protected": true, "private": true, "final": true,
}

// phpConstLine reads one `const [type] NAME = expr;` declaration, with any
// visibility/final modifiers in front. The identifier directly before the `=`
// is the name; the words between `const` and it are the optional type.
func phpConstLine(li int, runes []rune) (lang.Span, bool) {
	i := skipSpace(runes, 0)
	for {
		w, j := identAt(runes, i)
		if w == "" {
			return lang.Span{}, false
		}
		i = skipSpace(runes, j)
		if strings.ToLower(w) == "const" {
			break
		}
		if !phpModifiers[strings.ToLower(w)] {
			return lang.Span{}, false
		}
	}
	name := ""
	for {
		if i < len(runes) && runes[i] == '?' {
			i++ // a nullable type: `?int`
		}
		w, j := identAt(runes, i)
		if w == "" {
			return lang.Span{}, false
		}
		j = skipSpace(runes, j)
		if j < len(runes) && runes[j] == '=' && !(j+1 < len(runes) && runes[j+1] == '=') {
			name, i = w, j
			break
		}
		i = j // w was a type word; the name is still ahead
	}
	start := skipSpace(runes, i+1)
	end := trimEnd(runes, start, phpExprEnd(runes, start))
	if start >= end {
		return lang.Span{}, false
	}
	return hintSpan(li, start, end, string(runes[start:end]), name, FlavorPHP)
}

// phpDefineLine reads one `define('NAME', expr)` call at the start of a line.
func phpDefineLine(li int, runes []rune) (lang.Span, bool) {
	i := skipSpace(runes, 0)
	if i < len(runes) && runes[i] == '\\' {
		i++ // a fully-qualified \define()
	}
	w, j := identAt(runes, i)
	if strings.ToLower(w) != "define" {
		return lang.Span{}, false
	}
	j = skipSpace(runes, j)
	if j >= len(runes) || runes[j] != '(' {
		return lang.Span{}, false
	}
	j = skipSpace(runes, j+1)
	if j >= len(runes) || (runes[j] != '\'' && runes[j] != '"') {
		return lang.Span{}, false
	}
	q := runes[j]
	k := j + 1
	for k < len(runes) && runes[k] != q {
		k++
	}
	if k >= len(runes) {
		return lang.Span{}, false
	}
	name := string(runes[j+1 : k])
	k = skipSpace(runes, k+1)
	if k >= len(runes) || runes[k] != ',' {
		return lang.Span{}, false
	}
	start := skipSpace(runes, k+1)
	depth, end := 1, -1
	for m := start; m < len(runes); m++ {
		switch runes[m] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				end = m
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return lang.Span{}, false
	}
	end = trimEnd(runes, start, end)
	if start >= end {
		return lang.Span{}, false
	}
	return hintSpan(li, start, end, string(runes[start:end]), name, FlavorPHP)
}

// phpExprEnd returns the end of a `const` right-hand side: the terminating
// `;` or a trailing comment, whichever comes first.
func phpExprEnd(runes []rune, from int) int {
	for i := from; i < len(runes); i++ {
		switch runes[i] {
		case ';', '#':
			return i
		case '/':
			if i+1 < len(runes) && (runes[i+1] == '/' || runes[i+1] == '*') {
				return i
			}
		}
	}
	return len(runes)
}

// --- rendering ---------------------------------------------------------------

// hintSpan builds the conceal span for the right-hand side raw at rune
// columns [start, end) of line li, read under the constant's name. A plain
// decimal literal goes through the shared literalHint — the exact reading a
// config value with that key gets, user mapping included (#1685). Everything
// else is evaluated; the computed value then renders in the name's unit, or
// by shape (a 1024-multiple as bytes, anything else with its digits grouped).
func hintSpan(li, start, end int, raw, name string, f Flavor) (lang.Span, bool) {
	if isPlainDecimal(raw) {
		h, ok := numhint.LiteralHint(li, start, end, raw, name)
		if !ok || h.Span.Capture == "" {
			return lang.Span{}, false
		}
		return h.Span, true
	}
	res, ok := Eval(raw, f)
	if !ok {
		return lang.Span{}, false
	}
	span := func(capture, replace string) (lang.Span, bool) {
		if replace == raw {
			return lang.Span{}, false
		}
		return lang.Span{
			Line: li, StartCol: start, EndCol: end,
			Capture: capture, Replace: replace,
		}, true
	}
	dec := strconv.FormatUint(res.Value, 10)
	if u, ok := numhint.FieldUnit(name); ok {
		return mappedValue(span, raw, dec, res, u)
	}
	if res.Single && !res.Decimal {
		// A prefixed literal (0x, 0o, 0b, legacy octal): its decimal reading
		// is the one thing worth appending, whatever the name says — the
		// same rule the config scan applies to hex literals (#1627).
		return radixReading(span, raw, dec, res.Value)
	}
	if u, ok := numhint.KeyUnit(name); ok {
		if s, ok2 := unitValue(span, raw, dec, res.Value, u); ok2 {
			return s, true
		}
		// The unit renders nothing for this value (bytes below 1 KiB, a
		// duration of its own base) — fall through to the shape families,
		// mirroring the config scan.
	}
	if res.Value >= 1024 && res.Value%1024 == 0 {
		if s, ok2 := numhint.FormatBytes(res.Value); ok2 {
			return span(numhint.SizeCapture, s)
		}
	}
	if g, ok2 := numhint.Group(dec); ok2 {
		return span(numhint.GroupCapture, g)
	}
	if !res.Single {
		// A computed expression is worth concealing even when the result is
		// small: `6 * 7` reads as nothing until it reads as 42.
		return span(numhint.GroupCapture, dec)
	}
	return lang.Span{}, false
}

type spanFn func(capture, replace string) (lang.Span, bool)

// mappedValue renders a value whose constant name the user mapped to a unit
// (#1685). The mapping is final either way: when the unit renders nothing for
// this value — or is `none` — the expression stays raw rather than falling
// through to another family.
func mappedValue(span spanFn, raw, dec string, res Result, u numhint.Unit) (lang.Span, bool) {
	if u.Kind == numhint.UnitOff {
		return lang.Span{}, false
	}
	if res.Single && !res.Decimal {
		return radixReading(span, raw, dec, res.Value)
	}
	switch u.Kind {
	case numhint.UnitTimestampSeconds:
		if s, ok := epochtime.DecodeSeconds(dec); ok {
			return span(epochtime.Capture, s)
		}
	case numhint.UnitTimestampMillis:
		if s, ok := epochtime.DecodeMillis(dec); ok {
			return span(epochtime.Capture, s)
		}
	default:
		return unitValue(span, raw, dec, res.Value, u)
	}
	return lang.Span{}, false
}

// unitValue renders a computed value in one unit: bytes and durations replace
// the expression, the radix units append the conventional reading, grouping
// regroups the digits.
func unitValue(span spanFn, raw, dec string, v uint64, u numhint.Unit) (lang.Span, bool) {
	switch u.Kind {
	case numhint.UnitBytes:
		if s, ok := numhint.FormatBytes(v); ok {
			return span(numhint.SizeCapture, s)
		}
	case numhint.UnitDuration:
		if s, ok := numhint.FormatDuration(v, u.Base); ok {
			return span(numhint.DurationCapture, s)
		}
	case numhint.UnitOctal:
		if v >= 8 {
			return span(numhint.RadixCapture, raw+numhint.Gap+"= "+numhint.OctalOf(v))
		}
	case numhint.UnitHex:
		if v >= 10 {
			return span(numhint.RadixCapture, raw+numhint.Gap+"= "+numhint.HexOf(v))
		}
	case numhint.UnitGroup:
		if g, ok := numhint.Group(dec); ok {
			return span(numhint.GroupCapture, g)
		}
	}
	return lang.Span{}, false
}

// radixReading appends the decimal reading of a prefixed literal, grouped when
// long enough. Values below 10 read the same in every base and get nothing.
func radixReading(span spanFn, raw, dec string, v uint64) (lang.Span, bool) {
	if v < 10 {
		return lang.Span{}, false
	}
	if g, ok := numhint.Group(dec); ok {
		dec = g
	}
	return span(numhint.RadixCapture, raw+numhint.Gap+"= "+dec)
}

// isPlainDecimal reports whether text is bare decimal digits without a
// leading zero — the literal shape literalHint reads directly. A leading-zero
// run is octal in Go and PHP and a syntax error in Python, so it goes through
// the evaluator instead.
func isPlainDecimal(text string) bool {
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

// --- shared scanning helpers -------------------------------------------------

// identAt reads the identifier starting at rune index i, returning it and the
// index past it — "" when i does not start one.
func identAt(runes []rune, i int) (string, int) {
	if i >= len(runes) || (!isLetter(runes[i]) && runes[i] != '_') {
		return "", i
	}
	j := i
	for j < len(runes) && (isLetter(runes[j]) || isDigit(runes[j]) || runes[j] == '_') {
		j++
	}
	return string(runes[i:j]), j
}

// cutAtHash returns the index of the first `#` from i, or the line end.
func cutAtHash(runes []rune, i int) int {
	for ; i < len(runes); i++ {
		if runes[i] == '#' {
			return i
		}
	}
	return len(runes)
}

// cutAtSlashes returns the index of the first `//` from i, or the line end.
func cutAtSlashes(runes []rune, i int) int {
	for ; i+1 < len(runes); i++ {
		if runes[i] == '/' && runes[i+1] == '/' {
			return i
		}
	}
	return len(runes)
}

func skipSpace(runes []rune, i int) int {
	for i < len(runes) && isSpace(runes[i]) {
		i++
	}
	return i
}

func trimEnd(runes []rune, start, end int) int {
	if end > len(runes) {
		end = len(runes)
	}
	for end > start && isSpace(runes[end-1]) {
		end--
	}
	return end
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }
