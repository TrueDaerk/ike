package editor

import (
	"strconv"
	"strings"
	"unicode"

	"ike/internal/editor/buffer"
	"ike/internal/editor/history"
)

// increment.go implements the vim ctrl+a / ctrl+x number increment/decrement
// and the JetBrains-flavored value toggle (#1658). Both work on the token
// under the cursor, or — when the cursor sits before one — on the first
// candidate to its right on the same line, and both record a dot so "."
// replays them and a single history entry so undo reverts one step.

// numberSpan is a number literal found on a line, in rune indices [start,end).
type numberSpan struct {
	start, end int
	hex        bool
}

// findNumber returns the number ctrl+a acts on for a cursor at col: the one
// under the cursor, else the first one starting after it. Hex literals are
// recognised whole ("0x1f"), decimals take a directly preceding "-" as their
// sign, like vim.
func findNumber(runes []rune, col int) (numberSpan, bool) {
	for i := 0; i < len(runes); {
		if !isDigit(runes[i]) {
			i++
			continue
		}
		span := numberAt(runes, i)
		if span.end > col {
			return span, true
		}
		i = span.end
	}
	return numberSpan{}, false
}

// numberAt reads the number literal starting at the digit position i.
func numberAt(runes []rune, i int) numberSpan {
	if runes[i] == '0' && i+2 < len(runes) && (runes[i+1] == 'x' || runes[i+1] == 'X') && isHexDigit(runes[i+2]) {
		end := i + 2
		for end < len(runes) && isHexDigit(runes[end]) {
			end++
		}
		return numberSpan{start: i, end: end, hex: true}
	}
	end := i
	for end < len(runes) && isDigit(runes[end]) {
		end++
	}
	start := i
	if start > 0 && runes[start-1] == '-' {
		start--
	}
	return numberSpan{start: start, end: end}
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func isHexDigit(r rune) bool {
	return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// addToNumber returns the literal text with delta added, preserving the shape
// of the original: a decimal's leading-zero width ("007" + 1 = "008"), a hex
// literal's prefix casing, digit width and letter case ("0x1f" + 1 = "0x20").
// ok is false for literals too wide to evaluate, which are left alone.
func addToNumber(text string, hex bool, delta int64) (string, bool) {
	if hex {
		return addToHex(text, delta)
	}
	v, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return "", false
	}
	out := strconv.FormatInt(saturatingAdd(v, delta), 10)
	// Only a literal written with leading zeros keeps a fixed width; "99" + 1
	// is "100", but "007" + 1 stays three digits wide.
	digits := strings.TrimPrefix(text, "-")
	if len(digits) > 1 && digits[0] == '0' {
		out = padDigits(out, len(digits))
	}
	return out, true
}

// addToHex adds delta to a "0x…" literal. Hex arithmetic wraps in 64 bits
// (vim's behavior: 0x0 decremented is 0xffffffffffffffff) rather than
// saturating, because a hex literal is a bit pattern, not a quantity.
func addToHex(text string, delta int64) (string, bool) {
	prefix, digits := text[:2], text[2:]
	v, err := strconv.ParseUint(digits, 16, 64)
	if err != nil {
		return "", false
	}
	out := strconv.FormatUint(v+uint64(delta), 16)
	if strings.ContainsAny(digits, "ABCDEF") {
		out = strings.ToUpper(out)
	}
	return prefix + padDigits(out, len(digits)), true
}

// padDigits left-pads s with zeros to width, keeping a leading "-" outermost.
func padDigits(s string, width int) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	if len(s) < width {
		s = strings.Repeat("0", width-len(s)) + s
	}
	return sign + s
}

// saturatingAdd adds delta to v, clamping at the int64 bounds instead of
// wrapping — a decimal counter that overflows is far more likely a mistake
// than an intent.
func saturatingAdd(v, delta int64) int64 {
	sum := v + delta
	if delta > 0 && sum < v {
		return 1<<63 - 1
	}
	if delta < 0 && sum > v {
		return -1 << 63
	}
	return sum
}

// adjustNumber applies delta to the number at every caret and records the dot.
func (m *Model) adjustNumber(delta int64) {
	m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
		runes := []rune(m.buf.Line(pos.Line))
		span, ok := findNumber(runes, pos.Col)
		if !ok {
			return pos
		}
		out, ok := addToNumber(string(runes[span.start:span.end]), span.hex, delta)
		if !ok {
			return pos
		}
		rec.Apply(buffer.Edit{
			Range: buffer.Range{
				Start: buffer.Position{Line: pos.Line, Col: span.start},
				End:   buffer.Position{Line: pos.Line, Col: span.end},
			},
			Text: out,
		})
		// Vim leaves the cursor on the last character of the new number.
		return buffer.Position{Line: pos.Line, Col: span.start + len([]rune(out)) - 1}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.adjustNumber(delta) }}
}

// togglePairs are the value pairs the toggle action cycles. Matching is
// case-insensitive and the replacement copies the original's capitalization,
// so one lowercase entry covers "true", "True" and "TRUE".
var defaultTogglePairs = [][2]string{
	{"true", "false"},
	{"on", "off"},
	{"yes", "no"},
	{"enabled", "disabled"},
	{"==", "!="},
	{"&&", "||"},
	{"<", ">"},
}

// parseTogglePairs reads the comma-separated editor.toggle_pairs value, whose
// entries are "a=b". Malformed entries are skipped rather than failing the
// whole list, so one typo doesn't disable toggling.
func parseTogglePairs(raw string) [][2]string {
	var out [][2]string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		a, b, ok := strings.Cut(entry, "=")
		a, b = strings.TrimSpace(a), strings.TrimSpace(b)
		if !ok || a == "" || b == "" {
			continue
		}
		out = append(out, [2]string{a, b})
	}
	return out
}

// togglePairs returns the pairs in match order: configured ones first, so a
// user entry can redefine a built-in member ("on=off" → "on=disabled").
func (m *Model) togglePairs() [][2]string {
	if m.cfg == nil {
		return defaultTogglePairs
	}
	raw, ok := m.cfg.Get("editor.toggle_pairs")
	if !ok || strings.TrimSpace(raw) == "" {
		return defaultTogglePairs
	}
	return append(parseTogglePairs(raw), defaultTogglePairs...)
}

// toggleTokenAt returns the span of the token toggling would act on and its
// replacement: the token under the cursor when it matches a pair, else the
// first matching token to its right on the line.
func toggleTokenAt(runes []rune, col int, pairs [][2]string) (int, int, string, bool) {
	for i := 0; i < len(runes); {
		cls := tokenClass(runes[i])
		if cls == classOther {
			i++
			continue
		}
		j := i
		for j < len(runes) && tokenClass(runes[j]) == cls {
			j++
		}
		if j > col {
			if repl, ok := toggleCounterpart(string(runes[i:j]), pairs); ok {
				return i, j, repl, true
			}
		}
		i = j
	}
	return 0, 0, "", false
}

// token classes: toggling matches whole words or whole operator runs, so "<="
// is one token and never toggles as "<".
const (
	classOther = iota
	classWord
	classSymbol
)

func tokenClass(r rune) int {
	switch {
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return classWord
	case unicode.IsSpace(r):
		return classOther
	case unicode.IsPunct(r) || unicode.IsSymbol(r):
		return classSymbol
	default:
		return classOther
	}
}

// toggleCounterpart returns the other member of the first pair token belongs
// to, capitalized like token.
func toggleCounterpart(token string, pairs [][2]string) (string, bool) {
	for _, p := range pairs {
		for i, member := range p {
			if strings.EqualFold(token, member) {
				return matchCase(token, p[1-i]), true
			}
		}
	}
	return "", false
}

// matchCase copies orig's capitalization onto repl: all-lower, all-upper and
// leading-capital are reproduced, anything else takes repl verbatim.
func matchCase(orig, repl string) string {
	switch {
	case orig == strings.ToLower(orig):
		return strings.ToLower(repl)
	case orig == strings.ToUpper(orig):
		return strings.ToUpper(repl)
	case orig == titleCase(orig):
		return titleCase(repl)
	default:
		return repl
	}
}

// titleCase upper-cases the first rune and lower-cases the rest.
func titleCase(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}

// toggleValue flips the known value under (or after) every caret and records
// the dot.
func (m *Model) toggleValue() {
	pairs := m.togglePairs()
	m.fanMutate(func(rec *history.Recorder, pos, _ buffer.Position) buffer.Position {
		runes := []rune(m.buf.Line(pos.Line))
		start, end, repl, ok := toggleTokenAt(runes, pos.Col, pairs)
		if !ok {
			return pos
		}
		rec.Apply(buffer.Edit{
			Range: buffer.Range{
				Start: buffer.Position{Line: pos.Line, Col: start},
				End:   buffer.Position{Line: pos.Line, Col: end},
			},
			Text: repl,
		})
		return buffer.Position{Line: pos.Line, Col: start}
	})
	m.dot = &dotCommand{run: func(mm *Model) { mm.toggleValue() }}
}
