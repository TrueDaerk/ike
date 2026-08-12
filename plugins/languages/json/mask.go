package langjson

// mask.go masks the values people should not have on screen by accident
// (#1813), the JSON half of the dotenv masking of #1623: in a JSON member the
// key names the value, so `"password": "hunter2"` or `"stripe_secret_key": …`
// can be recognised without ever looking at what follows. The value's *content*
// is emitted as a stand-in span carrying secret.Mask — the quotes stay visible,
// so the member still reads as a string — and the editor's conceal layer draws
// the mask instead of the source runes, revealing them positionally under the
// caret (#1594).
//
// The heuristic itself lives in internal/secret, shared with the dotenv
// producer: which keys name a credential is decided in exactly one place, so
// the built-in tables and the user's own patterns (editor.secret_masking_keys,
// #1712) apply here exactly as they do there.

import (
	"ike/internal/lang"
	"ike/internal/secret"
)

// maskSpans returns the stand-in spans for the string values of secret-suspect
// keys in lines.
//
// The scan is a plain string-token walk rather than a grammar query: it is what
// every other span producer in this package does, it costs one pass, and it
// survives a buffer that does not parse yet (a JSON file is invalid for most of
// the time it is being edited — exactly when a freshly pasted credential is on
// screen). Only the key directly in front of a value decides, so a nested
// object masks by its own members' keys and never by the key it hangs under.
func maskSpans(lines []string) []lang.Span {
	var out []lang.Span
	// A member may be broken over two lines ("password":\n  "hunter2"). The
	// key of a line ending in a dangling colon carries over to the next one,
	// so the wrapped value is not left in the clear.
	pending := false
	for li, line := range lines {
		out, pending = appendMaskSpans(out, li, []rune(line), pending)
	}
	return out
}

// appendMaskSpans scans one line, starting inside the value position of a
// suspect key when carry says the previous line ended on one. It returns the
// same signal for the next line.
func appendMaskSpans(out []lang.Span, li int, runes []rune, carry bool) ([]lang.Span, bool) {
	i := skipSpace(runes, 0)
	if carry && i < len(runes) && runes[i] == '"' {
		start, end, next, ok := scanString(runes, i)
		if !ok {
			return out, false
		}
		out = appendMask(out, li, start, end)
		i = next
	}
	for ; i < len(runes); i++ {
		if runes[i] != '"' {
			continue
		}
		keyStart, keyEnd, afterKey, ok := scanString(runes, i)
		if !ok {
			// An unterminated string ends the line as far as members go.
			return out, false
		}
		i = afterKey - 1
		colon := skipSpace(runes, afterKey)
		if colon >= len(runes) || runes[colon] != ':' {
			continue
		}
		if !secret.Suspect(string(runes[keyStart:keyEnd])) {
			continue
		}
		val := skipSpace(runes, colon+1)
		if val >= len(runes) {
			// The value wraps to the next line.
			return out, true
		}
		if runes[val] != '"' {
			// Numbers, booleans, objects and arrays are not masked: the mask
			// is a stand-in for a credential, and a structure has no single
			// span to stand in for.
			continue
		}
		start, end, afterVal, ok := scanString(runes, val)
		if !ok {
			return out, false
		}
		out = appendMask(out, li, start, end)
		i = afterVal - 1
	}
	return out, false
}

// appendMask adds the span covering [start, end) — the string's content, quotes
// excluded. An empty value is skipped: there is nothing to hide, and a mask
// over nothing only reads as a value that is not there.
func appendMask(out []lang.Span, li, start, end int) []lang.Span {
	if end <= start {
		return out
	}
	return append(out, lang.Span{
		Line:     li,
		StartCol: start,
		EndCol:   end,
		Capture:  secret.Capture,
		Replace:  secret.Mask,
	})
}

// scanString reads the JSON string opening at runes[q] (which must be a quote)
// and returns the bounds of its content, the index just past the closing quote,
// and whether the string was terminated on this line. Backslash escapes are
// consumed pairwise, so a `\"` does not end the string.
func scanString(runes []rune, q int) (start, end, next int, ok bool) {
	start = q + 1
	for i := start; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			i++
		case '"':
			return start, i, i + 1, true
		}
	}
	return 0, 0, len(runes), false
}

// skipSpace returns the index of the first non-space rune at or after i.
func skipSpace(runes []rune, i int) int {
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	return i
}
