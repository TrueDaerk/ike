package escapes

import "ike/internal/lang"

// base64json.go is the JSON half of the Secret-manifest base64 decoding
// (#2345): a Kubernetes Secret is written as JSON just as legitimately as in
// YAML (`kubectl get secret -o json`, jsonnet output), and its `"data":`
// values are the same base64 convention Base64YAMLSpans decodes. The walk is
// a token scan rather than a parse, like every other producer in this
// package: it survives a buffer mid-edit, and it splits documents on
// top-level braces, so an ndjson stream of manifests decodes per line.

// jsonToken is one token the scan cares about: a string (with the bounds of
// its content) or a single structural rune ({ } [ ] :).
type jsonToken struct {
	li, start, end int    // rune columns; for a string, the content bounds
	text           string // the string's content; "" for a structural rune
	r              rune   // the structural rune; 0 for a string
}

// Base64JSONSpans produces conceal-with-stand-in spans for the base64 values
// in the data: block of a Kubernetes Secret document written as JSON. Only
// documents declaring `"kind": "Secret"` decode, and only values whose
// payload is printable text — the same rules as the YAML producer.
func Base64JSONSpans(lines []string) []lang.Span {
	var out []lang.Span
	toks := jsonTokens(lines)
	depth := 0
	docStart := -1
	for i, t := range toks {
		switch t.r {
		case '{', '[':
			if depth == 0 && t.r == '{' {
				docStart = i
			}
			depth++
		case '}', ']':
			depth--
			if depth == 0 && docStart >= 0 {
				out = appendSecretDoc(out, toks[docStart:i+1])
				docStart = -1
			}
		}
	}
	return out
}

// appendSecretDoc emits the spans for one top-level document, toks[0] being
// its opening brace and the last token its closing one.
func appendSecretDoc(out []lang.Span, toks []jsonToken) []lang.Span {
	if !jsonSecretKind(toks) {
		return out
	}
	from, to := jsonDataRegion(toks)
	if from < 0 {
		return out
	}
	for i := from + 1; i < to; i++ {
		t := toks[i]
		if t.r != 0 || toks[i-1].r != ':' {
			continue // keys precede a colon; only values follow one
		}
		text, ok := DecodeBase64(t.text)
		if !ok {
			continue
		}
		out = append(out, lang.Span{
			Line: t.li, StartCol: t.start, EndCol: t.end,
			Capture: Base64Capture, Replace: text,
		})
	}
	return out
}

// jsonSecretKind reports whether the document declares "kind": "Secret" at
// its top level.
func jsonSecretKind(toks []jsonToken) bool {
	depth := 0
	for i, t := range toks {
		switch {
		case t.r == '{' || t.r == '[':
			depth++
		case t.r == '}' || t.r == ']':
			depth--
		case t.r == 0 && depth == 1 && t.text == "kind" &&
			i+2 < len(toks) && toks[i+1].r == ':' && toks[i+2].r == 0:
			return toks[i+2].text == "Secret"
		}
	}
	return false
}

// jsonDataRegion returns the token indices of the braces delimiting the
// top-level "data" object, or from = -1 when the document has none.
func jsonDataRegion(toks []jsonToken) (from, to int) {
	depth := 0
	for i, t := range toks {
		switch {
		case t.r == '{' || t.r == '[':
			if depth == 1 && t.r == '{' && i >= 2 &&
				toks[i-1].r == ':' && toks[i-2].r == 0 && toks[i-2].text == "data" {
				for j, d := i+1, 1; j < len(toks); j++ {
					switch toks[j].r {
					case '{', '[':
						d++
					case '}', ']':
						if d--; d == 0 {
							return i, j
						}
					}
				}
				return -1, -1
			}
			depth++
		case t.r == '}' || t.r == ']':
			depth--
		}
	}
	return -1, -1
}

// jsonTokens scans the buffer into the tokens the document walk reads:
// strings (JSON strings never span lines) and the structural runes. Anything
// else — numbers, booleans, commas, stray text — carries no structure the
// walk needs and is skipped.
func jsonTokens(lines []string) []jsonToken {
	var out []jsonToken
	for li, line := range lines {
		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			switch r := runes[i]; r {
			case '"':
				start := i + 1
				closed := false
				for i++; i < len(runes); i++ {
					if runes[i] == '\\' {
						i++
						continue
					}
					if runes[i] == '"' {
						closed = true
						break
					}
				}
				if !closed {
					break // an unterminated string ends the line's tokens
				}
				out = append(out, jsonToken{li: li, start: start, end: i, text: string(runes[start:i])})
			case '{', '}', '[', ']', ':':
				out = append(out, jsonToken{li: li, start: i, end: i + 1, r: r})
			}
		}
	}
	return out
}
