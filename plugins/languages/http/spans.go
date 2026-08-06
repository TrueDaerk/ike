package langhttp

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"ike/internal/epochtime"
	"ike/internal/httpfile"
	"ike/internal/jwt"
	"ike/internal/lang"
)

// spans.go produces the Go-computed highlight spans for .http buffers
// (#1585). The rest-nvim grammar captures a request target as one opaque url
// node, so the query string reads as a single-coloured blob; this producer
// overlays key/value/separator spans over it and marks percent-encoded
// sequences as conceal-with-stand-in ranges, so "%20" renders as the space it
// encodes on lines the caret is not on.
//
// Covered: the request line's target, folded query continuation lines
// (#1269) and application/x-www-form-urlencoded request bodies (the same
// key=value&… structure). Placeholder regions ({{…}} and ${…}) are skipped —
// the grammar styles those more specifically, and concealing inside them
// would hide the placeholder's own syntax.

// querySpans is the lang.Language.Spans hook (#1585).
func querySpans(lines []string) []lang.Span {
	f := httpfile.Parse(strings.Join(lines, "\n"))
	// JWTs (#1619) are scanned over the whole buffer, not per request region:
	// they show up in an Authorization header, in a body, in a @token variable
	// and in a pasted response block alike. Detection is structural — three
	// base64url segments whose first two decode to JSON — so scanning
	// everything cannot mis-dim ordinary text.
	out := jwt.Spans(lines)
	for _, r := range f.Requests {
		li := r.Line - 1
		if li < 0 || li >= len(lines) {
			continue
		}
		runes := []rune(lines[li])
		tStart, tEnd := targetBounds(runes)
		if tStart < 0 {
			continue
		}
		ph := placeholderRanges(runes, tStart, tEnd)
		out = appendPercentSpans(out, li, runes, tStart, tEnd, ph)
		qPos := indexFrom(runes, tStart, tEnd, '?')
		if qPos >= 0 {
			out = appendQuerySpans(out, li, runes, qPos, tEnd, ph)
		}
		// The path portion gets its own capture (#1594), distinct from the
		// grammar's url string covering scheme and authority. Emitted after
		// the percent/query spans so those win inside the path.
		if ps, pe := pathBounds(runes, tStart, tEnd, qPos); ps >= 0 {
			out = appendExcluding(out, li, ps, pe, "label", ph)
		}
		// Folded query continuation lines (#1269): indented lines starting
		// with "?" or "&" extend the target until the first header, blank
		// line or comment break — mirroring httpfile's folding rules.
		for j := li + 1; j < len(lines); j++ {
			if isCommentLine(lines[j]) {
				continue
			}
			t := strings.TrimSpace(lines[j])
			if t == "" || (!strings.HasPrefix(t, "?") && !strings.HasPrefix(t, "&")) {
				break
			}
			lr := []rune(lines[j])
			from := leadingSpace(lr)
			lph := placeholderRanges(lr, from, len(lr))
			out = appendPercentSpans(out, j, lr, from, len(lr), lph)
			out = appendQuerySpans(out, j, lr, from, len(lr), lph)
		}
		// A form-urlencoded body is the same key=value&… structure — the
		// same pass applies (#1585). External bodies have no lines here.
		if ct, ok := r.Header("Content-Type"); ok && r.BodyStart > 0 && r.BodyFile == "" &&
			strings.Contains(strings.ToLower(ct), "application/x-www-form-urlencoded") {
			for j := r.BodyStart - 1; j < r.BodyEnd && j < len(lines); j++ {
				lr := []rune(lines[j])
				lph := placeholderRanges(lr, 0, len(lr))
				out = appendPercentSpans(out, j, lr, 0, len(lr), lph)
				out = appendQuerySpans(out, j, lr, 0, len(lr), lph)
			}
		}
		// Epoch timestamps in an inline body (#1618). The body is JSON far
		// more often than not, and the JSON-value context is strict enough to
		// stay silent on anything else: a bare number only decodes where JSON
		// would put a value, so a form-urlencoded or plain-text body is left
		// untouched.
		if r.BodyStart > 0 && r.BodyFile == "" {
			for j := r.BodyStart - 1; j < r.BodyEnd && j < len(lines); j++ {
				out = append(out, epochtime.LineSpans(j, lines[j], epochtime.JSONValue)...)
			}
		}
	}
	return out
}

// pathBounds locates the path portion of a request target in [from, to): the
// first "/" after the "scheme://authority" prefix — or the target start for
// origin-form targets ("/api/users") — through the query (qPos, -1 for
// none). start -1 when the target has no path portion.
func pathBounds(runes []rune, from, to, qPos int) (start, end int) {
	end = to
	if qPos >= 0 {
		end = qPos
	}
	start = -1
	for i := from; i+2 < end; i++ {
		if runes[i] == ':' && runes[i+1] == '/' && runes[i+2] == '/' {
			start = indexFrom(runes, i+3, end, '/')
			return start, end
		}
	}
	if from < end && runes[from] == '/' {
		return from, end
	}
	return -1, end
}

// appendExcluding emits capture spans over [from, to), split around the
// placeholder ranges so their own captures survive.
func appendExcluding(out []lang.Span, line, from, to int, capture string, ph [][2]int) []lang.Span {
	start := from
	flush := func(end int) {
		if start < end {
			out = append(out, lang.Span{Line: line, StartCol: start, EndCol: end, Capture: capture})
		}
	}
	for i := from; i < to; i++ {
		if inRanges(ph, i) {
			flush(i)
			start = i + 1
		}
	}
	flush(to)
	return out
}

// targetBounds locates the request-target token on a request line: the
// second whitespace-separated field. Returns start -1 when the line has none.
func targetBounds(runes []rune) (start, end int) {
	i := leadingSpace(runes)
	for i < len(runes) && !unicode.IsSpace(runes[i]) { // method
		i++
	}
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	if i == len(runes) {
		return -1, -1
	}
	start = i
	for i < len(runes) && !unicode.IsSpace(runes[i]) {
		i++
	}
	return start, i
}

// leadingSpace returns the index of the first non-space rune.
func leadingSpace(runes []rune) int {
	i := 0
	for i < len(runes) && unicode.IsSpace(runes[i]) {
		i++
	}
	return i
}

// indexFrom returns the index of the first occurrence of r in [from, to).
func indexFrom(runes []rune, from, to int, r rune) int {
	for i := from; i < to && i < len(runes); i++ {
		if runes[i] == r {
			return i
		}
	}
	return -1
}

// placeholderRanges collects the {{…}} and ${…} regions in [from, to) so the
// query/percent passes can leave them to the grammar's own captures.
func placeholderRanges(runes []rune, from, to int) [][2]int {
	var out [][2]int
	for i := from; i < to && i < len(runes); i++ {
		var close string
		switch {
		case runes[i] == '{' && i+1 < to && runes[i+1] == '{':
			close = "}}"
		case runes[i] == '$' && i+1 < to && runes[i+1] == '{':
			close = "}"
		default:
			continue
		}
		end := to
		for j := i + 2; j < to; j++ {
			if strings.HasPrefix(string(runes[j:to]), close) {
				end = j + len(close)
				break
			}
		}
		out = append(out, [2]int{i, end})
		i = end - 1
	}
	return out
}

// inRanges reports whether col falls inside one of the ranges.
func inRanges(ranges [][2]int, col int) bool {
	for _, r := range ranges {
		if col >= r[0] && col < r[1] {
			return true
		}
	}
	return false
}

// appendQuerySpans overlays the query-parameter structure of [from, to):
// "?", "&" and the first "=" of each parameter as punctuation, keys as
// property, values as constant (#1594 — distinct from the url string).
// Tokens split around placeholder ranges; whitespace (folded lines allow
// "? a = 1") stays unstyled.
func appendQuerySpans(out []lang.Span, line int, runes []rune, from, to int, ph [][2]int) []lang.Span {
	if to > len(runes) {
		to = len(runes)
	}
	inValue := false
	tokStart := -1
	tokCapture := func() string {
		if inValue {
			// Distinct from the url's own @string capture (#1594), so
			// values read apart from the surrounding url.
			return "constant"
		}
		return "property"
	}
	flush := func(end int) {
		if tokStart < 0 {
			return
		}
		s, e := tokStart, end
		for s < e && unicode.IsSpace(runes[s]) {
			s++
		}
		for e > s && unicode.IsSpace(runes[e-1]) {
			e--
		}
		if s < e {
			out = append(out, lang.Span{Line: line, StartCol: s, EndCol: e, Capture: tokCapture()})
		}
		tokStart = -1
	}
	for i := from; i < to; i++ {
		if inRanges(ph, i) {
			flush(i)
			continue
		}
		switch runes[i] {
		case '?', '&':
			flush(i)
			out = append(out, lang.Span{Line: line, StartCol: i, EndCol: i + 1, Capture: "punctuation"})
			inValue = false
		case '=':
			if !inValue {
				flush(i)
				out = append(out, lang.Span{Line: line, StartCol: i, EndCol: i + 1, Capture: "punctuation"})
				inValue = true
				continue
			}
			fallthrough
		default:
			if tokStart < 0 {
				tokStart = i
			}
		}
	}
	flush(to)
	return out
}

// appendPercentSpans marks percent-encoded sequences in [from, to): each
// maximal %XX run decodes as UTF-8, and every decoded printable rune becomes
// a conceal-with-stand-in span over its source triples ("%C3%A4" conceals as
// "ä" across all six columns). Triples that do not decode to a printable
// rune keep a plain escape span, so they still read as an encoding.
func appendPercentSpans(out []lang.Span, line int, runes []rune, from, to int, ph [][2]int) []lang.Span {
	if to > len(runes) {
		to = len(runes)
	}
	for i := from; i < to; {
		if runes[i] != '%' || i+2 >= to || !isHex(runes[i+1]) || !isHex(runes[i+2]) || inRanges(ph, i) {
			i++
			continue
		}
		runStart := i
		var bytes []byte
		for i+2 < to && runes[i] == '%' && isHex(runes[i+1]) && isHex(runes[i+2]) && !inRanges(ph, i) {
			bytes = append(bytes, byte(hexVal(runes[i+1])<<4|hexVal(runes[i+2])))
			i += 3
		}
		col := runStart
		for len(bytes) > 0 {
			r, size := utf8.DecodeRune(bytes)
			end := col + 3*size
			if r != utf8.RuneError && (r == ' ' || unicode.IsGraphic(r)) && !unicode.IsControl(r) {
				out = append(out, lang.Span{Line: line, StartCol: col, EndCol: end, Capture: "escape", Replace: string(r)})
			} else {
				out = append(out, lang.Span{Line: line, StartCol: col, EndCol: end, Capture: "escape"})
			}
			bytes = bytes[size:]
			col = end
		}
	}
	return out
}

// isHex reports whether r is a hexadecimal digit.
func isHex(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
}

// hexVal returns the value of a hexadecimal digit rune.
func hexVal(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	default:
		return int(r-'A') + 10
	}
}
