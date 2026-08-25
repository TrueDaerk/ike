package langhttp

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"ike/internal/epochtime"
	"ike/internal/httpfile"
	"ike/internal/jwt"
	"ike/internal/lang"
	"ike/internal/nethint"
	"ike/internal/numhint"
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
// key=value&… structure), plus — since #1880 — every "{{name}}" placeholder
// and "@name = value" definition in the buffer. The grammar only builds a
// `variable` node for a placeholder in some contexts (a header's value gets
// one, but the request target stays one opaque url node and an embedded
// JSON/XML/HTML body is re-highlighted as a plain string literal by the
// region overlay), so a Go-computed span is the one path that reaches every
// site consistently; percent/query overlays elsewhere in this file still
// skip placeholder ranges (via placeholderRanges) so they never fight the
// placeholder's own capture for the same cells.
//
// The value conceals (#1684) ride on the same structure. Everything the
// producer already recognises as a *value* — a query parameter, a folded
// query line, a header value, an inline body line — is collected as a
// valueRange and handed to the epoch decoder (#1618) and the number hints
// (#1627). Both scan the range's own text, so the request line's trailing
// " HTTP/1.1" never bounds a query value, and both only match a token after a
// separator, so keys, header names and JSON member names never conceal.

// querySpans is the lang.Language.Spans hook (#1585).
func querySpans(lines []string) []lang.Span {
	f := httpfile.Parse(strings.Join(lines, "\n"))
	// Placeholders (#1880) are claimed first, so they win over everything
	// else this producer computes for the same cells — a "{{host}}" sitting
	// inside a variable definition's value, say.
	out := placeholderSpans(lines)
	// Capture directives (#1993) are claimed right after: the line is a
	// comment to the grammar, and its parts must read as the structure they
	// are (capture.go).
	out = append(out, captureSpans(lines)...)
	// JWTs (#1619) are scanned over the whole buffer, not per request region:
	// they show up in an Authorization header, in a body, in a @token variable
	// and in a pasted response block alike. Detection is structural — three
	// base64url segments whose first two decode to JSON — so scanning
	// everything cannot mis-dim ordinary text.
	out = append(out, jwt.Spans(lines)...)
	// Network literals (#1653): a punycode authority in a request target and
	// a CIDR prefix in a header or body value. Scanned over the whole buffer
	// for the same reason JWTs are — a host shows up in a target, an
	// X-Forwarded-For header and a body alike.
	out = append(out, nethint.Spans(lines)...)
	// Variable definitions (#1867, #1880): the grammar's own query captures
	// an "@name" as @variable but has no capture for its value — appended
	// after the JWT/network passes so a JWT-shaped definition value keeps
	// its own dimmed signature instead of reading as one flat string.
	out = append(out, variableDefinitionSpans(lines)...)
	// Multipart boundaries and per-part headers (#2135): claimed here, right
	// after the definitions, so a placeholder inside a part header
	// ("Content-Disposition: form-data; name=\"{{field}}\"") still reads as
	// a placeholder first — placeholderSpans ran before it in this slice.
	out = append(out, multipartSpans(lines)...)
	// The value ranges collected along the way, for the number hints below
	// (#1684). Every entry is a stretch of a line that holds values rather
	// than structure: a query string, a header value, an inline body line.
	var values []valueRange
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
			// Query parameters are value positions (#1684): the range stops
			// at the target's end, so the trailing " HTTP/1.1" never counts as
			// the character following a value.
			values = append(values, valueRange{li, qPos, tEnd})
		}
		// The path portion gets its own capture (#1594), distinct from the
		// grammar's url string covering scheme and authority. Emitted after
		// the percent/query spans so those win inside the path.
		if ps, pe := pathBounds(runes, tStart, tEnd, qPos); ps >= 0 {
			out = appendExcluding(out, li, ps, pe, "label", ph)
		}
		// The "scheme://authority" prefix reads as three segments rather than
		// one blob (#1740), subtly: the scheme dims to the comment colour, the
		// "://" separator takes the same punctuation colour the query
		// separators use, and the authority keeps the url string colour
		// through the string.special sub-capture (which falls back to
		// "string" in every built-in theme). Emitted last, so the percent,
		// query and network-literal (#1653) spans keep their columns.
		if p, ok := targetPrefix(runes, tStart, tEnd, qPos); ok {
			out = appendExcluding(out, li, p.scheme[0], p.scheme[1], "comment", ph)
			out = appendExcluding(out, li, p.sep[0], p.sep[1], "punctuation", ph)
			out = appendExcluding(out, li, p.authority[0], p.authority[1], "string.special", ph)
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
		// Header and folded-query lines are value positions too (#1684):
		// "Content-Length: 1048576" reads as a byte size like any other keyed
		// number, and a folded "&limit=100000" like a query parameter. The
		// stretch runs from the line after the request line to the blank line
		// opening the body, bounded by the request's own block.
		last := r.BlockEnd - 1
		if r.BodyStart > 0 {
			last = r.BodyStart - 2
		}
		for j := li + 1; j <= last && j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" || isCommentLine(lines[j]) {
				continue
			}
			values = append(values, valueRange{j, 0, len([]rune(lines[j]))})
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
		// An inline body is one long value position (#1618, #1684). It is JSON
		// far more often than not, and the value context is strict enough to
		// stay silent on anything else: a bare number only decodes where a
		// format would put a value, so a plain-text body is left untouched.
		// External bodies have no lines here.
		if r.BodyStart > 0 && r.BodyFile == "" {
			for j := r.BodyStart - 1; j < r.BodyEnd && j < len(lines); j++ {
				values = append(values, valueRange{j, 0, len([]rune(lines[j]))})
			}
		}
	}
	// The epoch (#1618) and number (#1627) stand-ins over every value range
	// collected above (#1684). Both scanners only ever match a token *after* a
	// separator, so a query key, a header name or a JSON member name is never
	// concealed. The numbers run second and step aside where a stand-in of
	// another family — an epoch, a JWT, a percent escape, a network literal —
	// already claimed the same columns.
	var stamps []lang.Span
	var hints []numhint.Hint
	for _, v := range values {
		stamps = appendEpochSpans(stamps, lines, v)
		hints = append(hints, v.hints(lines)...)
	}
	// A value whose key names the unit keeps its own reading (#1685): the
	// timestamp over the same digits gives way before anything is emitted.
	out = append(out, numhint.Allowed(stamps, hints)...)
	taken := standIns(out)
	return append(out, numhint.Except(numhint.HintSpans(hints), taken)...)
}

// placeholderSpans finds every "{{name}}" placeholder in the buffer and
// styles it as punctuation braces around a @variable name (#1880), matching
// the styling `(variable name: (_) @variable)` gives a placeholder the
// grammar does reach. Scanned over the whole buffer, not per request region
// — a placeholder shows up in a request target, a header value and a body
// alike, and the request target and an embedded JSON/XML/HTML body are ones
// the grammar itself cannot reach into (see the package doc above querySpans).
func placeholderSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		runes := []rune(line)
		for j := 0; j+1 < len(runes); j++ {
			if runes[j] != '{' || runes[j+1] != '{' {
				continue
			}
			end := -1
			for k := j + 2; k+1 < len(runes); k++ {
				if runes[k] == '}' && runes[k+1] == '}' {
					end = k
					break
				}
			}
			if end < 0 {
				continue
			}
			out = append(out,
				lang.Span{Line: i, StartCol: j, EndCol: j + 2, Capture: "punctuation"},
				lang.Span{Line: i, StartCol: j + 2, EndCol: end, Capture: "variable"},
				lang.Span{Line: i, StartCol: end, EndCol: end + 2, Capture: "punctuation"},
			)
			j = end + 1
		}
	}
	return out
}

// variableDefinitionSpans styles the value of an in-file "@name = value"
// definition (#1867, #1880): the grammar's query captures the name as
// @variable and the "=" as @operator but has no capture for the value at
// all, so it renders unstyled. httpfile.VarDefinition already recognises the
// line and trims the value; this locates that same substring in the source
// line to style it distinctly from its name.
func variableDefinitionSpans(lines []string) []lang.Span {
	var out []lang.Span
	for i, line := range lines {
		_, value, ok := httpfile.VarDefinition(line)
		if !ok || value == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		rest := line[eq+1:]
		idx := strings.Index(rest, value)
		if idx < 0 {
			continue
		}
		startByte := eq + 1 + idx
		startCol := utf8.RuneCountInString(line[:startByte])
		endCol := startCol + utf8.RuneCountInString(value)
		out = append(out, lang.Span{Line: i, StartCol: startCol, EndCol: endCol, Capture: "string"})
	}
	return out
}

// valueRange is the rune-column stretch [From, To) of line Line that holds
// values rather than structure (#1684).
type valueRange struct {
	Line, From, To int
}

// text returns the range's text, clipped to the line.
func (v valueRange) text(lines []string) (string, bool) {
	if v.Line < 0 || v.Line >= len(lines) {
		return "", false
	}
	runes := []rune(lines[v.Line])
	from, to := v.From, v.To
	if to > len(runes) {
		to = len(runes)
	}
	if from < 0 || from >= to {
		return "", false
	}
	return string(runes[from:to]), true
}

// hints produces the number-readability hints for the range, mapped back onto
// the line's own columns. Hints rather than spans: the ones whose key named
// the unit take the columns away from the epoch stand-ins (#1685).
func (v valueRange) hints(lines []string) []numhint.Hint {
	text, ok := v.text(lines)
	if !ok {
		return nil
	}
	out := numhint.LineHints(v.Line, text)
	for i := range out {
		out[i].Span.StartCol += v.From
		out[i].Span.EndCol += v.From
	}
	return out
}

// appendEpochSpans decodes the Unix timestamps in a value range. Scanning the
// range's own text rather than the whole line is what lets a query parameter
// decode: the value ends where the target does, not at " HTTP/1.1".
func appendEpochSpans(out []lang.Span, lines []string, v valueRange) []lang.Span {
	text, ok := v.text(lines)
	if !ok {
		return out
	}
	for _, m := range epochtime.Scan(text, epochtime.Value) {
		out = append(out, lang.Span{
			Line: v.Line, StartCol: v.From + m.Start, EndCol: v.From + m.End,
			Capture: epochtime.Capture, Replace: m.Text,
		})
	}
	return out
}

// standIns keeps the spans carrying a stand-in — the ones that would fight a
// number hint for the same cells.
func standIns(spans []lang.Span) []lang.Span {
	var out []lang.Span
	for _, s := range spans {
		if s.Replace != "" {
			out = append(out, s)
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

// prefixBounds holds the three parts of an absolute-form request target's
// "scheme://authority" prefix, each as a [start, end) rune-column range
// (#1740). An empty authority ("http://" alone) keeps a zero-width range,
// which emits no span.
type prefixBounds struct {
	scheme    [2]int
	sep       [2]int
	authority [2]int
}

// targetPrefix splits the "scheme://authority" prefix of the request target in
// [from, to), with the query at qPos (-1 for none). ok is false for
// origin-form targets ("/api/users") and for anything without a "://" before
// the path — those have no scheme and no authority to differentiate.
func targetPrefix(runes []rune, from, to, qPos int) (prefixBounds, bool) {
	end := to
	if qPos >= 0 && qPos < end {
		end = qPos
	}
	sep := -1
	for i := from; i+2 < end; i++ {
		if runes[i] == ':' && runes[i+1] == '/' && runes[i+2] == '/' {
			sep = i
			break
		}
	}
	if sep <= from {
		return prefixBounds{}, false
	}
	authStart := sep + 3
	authEnd := indexFrom(runes, authStart, end, '/')
	if authEnd < 0 {
		authEnd = end
	}
	return prefixBounds{
		scheme:    [2]int{from, sep},
		sep:       [2]int{sep, authStart},
		authority: [2]int{authStart, authEnd},
	}, true
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
