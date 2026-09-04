package langhttp

// mask.go masks the credential-carrying values of a .http buffer (#2345),
// the request-file half of the dotenv masking of #1623: an
// `Authorization: Bearer eyJ…` or `X-Api-Key: …` header puts a live
// credential on screen more reliably than any config file, and the header
// name says so. Header names have their own conventions —
// secret.Suspect("Authorization") is deliberately false (AUTHOR is a public
// marker) — so a table of the credential-carrying names decides first and
// internal/secret's tables second, keeping editor.secret_masking_keys in
// force for house-style header names. A scheme word (`Bearer`, `Basic`)
// stays readable: it is the credential's type, not the credential.
//
// In-file `@token = value` variable definitions mask under the same rule the
// dotenv producer applies to its keys.
//
// basicAuthSpans is the sibling decode (#2345): the base64 payload of
// `Authorization: Basic …` is the one place base64 is the convention in a
// request file, so it decodes like a Secret manifest's data: values — but the
// masks are emitted first, so with masking on the credential renders as ••••,
// never as its decoded self.

import (
	"strings"

	"ike/internal/escapes"
	"ike/internal/httpfile"
	"ike/internal/lang"
	"ike/internal/secret"
)

// credentialHeaders are the header names whose value is a credential by
// convention, lowercased.
var credentialHeaders = map[string]bool{
	"authorization":        true,
	"proxy-authorization":  true,
	"cookie":               true,
	"set-cookie":           true,
	"x-api-key":            true,
	"api-key":              true,
	"x-auth-token":         true,
	"x-access-token":       true,
	"x-amz-security-token": true,
}

// authSchemes are the scheme words a masked Authorization value keeps
// readable, lowercased.
var authSchemes = map[string]bool{
	"basic": true, "bearer": true, "digest": true, "negotiate": true, "ntlm": true,
}

// maskSpans returns the stand-in spans for the credential values of the
// buffer: suspect headers inside every request block, and suspect @variable
// definitions anywhere.
func maskSpans(f *httpfile.File, lines []string) []lang.Span {
	var out []lang.Span
	for _, hl := range headerLines(f, lines) {
		runes := []rune(lines[hl])
		name, vs, ok := headerValue(runes)
		if !ok || !suspectHeader(name) {
			continue
		}
		ve := len(runes)
		for ve > vs && (runes[ve-1] == ' ' || runes[ve-1] == '\t') {
			ve--
		}
		// The scheme word stays readable; only the credential after it masks.
		if w := wordEnd(runes, vs); w < ve && authSchemes[strings.ToLower(string(runes[vs:w]))] {
			vs = skipWS(runes, w)
		}
		if vs < ve && !hasPlaceholder(string(runes[vs:ve])) {
			out = append(out, secret.Span(hl, vs, ve))
		}
	}
	for li, line := range lines {
		name, value, ok := httpfile.VarDefinition(line)
		if !ok || value == "" || hasPlaceholder(value) || !secret.Suspect(name) {
			continue
		}
		if vs, ve, ok := valueBounds(line, value); ok {
			out = append(out, secret.Span(li, vs, ve))
		}
	}
	return out
}

// hasPlaceholder reports whether a value carries a "{{name}}" or "${name}"
// reference. A placeholder is indirection, not the credential itself: masking
// it would hide nothing secret while destroying the placeholder's own
// styling (#1880).
func hasPlaceholder(value string) bool {
	return strings.Contains(value, "{{") || strings.Contains(value, "${")
}

// basicAuthSpans decodes the base64 payload of `Authorization: Basic …`
// headers into a conceal-with-stand-in span, escapes.DecodeBase64's rules
// deciding what qualifies.
func basicAuthSpans(f *httpfile.File, lines []string) []lang.Span {
	var out []lang.Span
	for _, hl := range headerLines(f, lines) {
		runes := []rune(lines[hl])
		name, vs, ok := headerValue(runes)
		if !ok {
			continue
		}
		switch strings.ToLower(name) {
		case "authorization", "proxy-authorization":
		default:
			continue
		}
		w := wordEnd(runes, vs)
		if !strings.EqualFold(string(runes[vs:w]), "Basic") {
			continue
		}
		ts := skipWS(runes, w)
		te := wordEnd(runes, ts)
		if ts >= te {
			continue
		}
		text, ok := escapes.DecodeBase64(string(runes[ts:te]))
		if !ok {
			continue
		}
		out = append(out, lang.Span{
			Line: hl, StartCol: ts, EndCol: te,
			Capture: escapes.Base64Capture, Replace: text,
		})
	}
	return out
}

// headerLines collects the line indices of every request's header block —
// the stretch between the request line and the body (or block end), the same
// bounds the value ranges of querySpans use.
func headerLines(f *httpfile.File, lines []string) []int {
	var out []int
	for _, r := range f.Requests {
		li := r.Line - 1
		if li < 0 || li >= len(lines) {
			continue
		}
		last := r.BlockEnd - 1
		if r.BodyStart > 0 {
			last = r.BodyStart - 2
		}
		for j := li + 1; j <= last && j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" || isCommentLine(lines[j]) {
				continue
			}
			out = append(out, j)
		}
	}
	return out
}

// headerValue reads a `Name: value` header line, returning the name and the
// rune column where the value starts. ok is false for lines that are not
// headers — folded query lines, body-less separators.
func headerValue(runes []rune) (string, int, bool) {
	colon := -1
	for i, r := range runes {
		if r == ':' {
			colon = i
			break
		}
		if !headerNameRune(r) {
			return "", 0, false
		}
	}
	if colon <= 0 {
		return "", 0, false
	}
	return string(runes[:colon]), skipWS(runes, colon+1), true
}

// suspectHeader reports whether a header name carries a credential: the
// conventional names first, the shared key tables second.
func suspectHeader(name string) bool {
	return credentialHeaders[strings.ToLower(name)] || secret.Suspect(name)
}

// valueBounds locates value inside line, as rune columns.
func valueBounds(line, value string) (int, int, bool) {
	idx := strings.Index(line, value)
	if idx < 0 {
		return 0, 0, false
	}
	start := len([]rune(line[:idx]))
	return start, start + len([]rune(value)), true
}

// headerNameRune reports a rune that may appear in a header name.
func headerNameRune(r rune) bool {
	return r == '-' || r == '_' || r >= '0' && r <= '9' ||
		r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// wordEnd returns the index past the whitespace-free word starting at i.
func wordEnd(runes []rune, i int) int {
	for i < len(runes) && runes[i] != ' ' && runes[i] != '\t' {
		i++
	}
	return i
}

// skipWS returns the index of the first non-blank rune at or after i.
func skipWS(runes []rune, i int) int {
	for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t') {
		i++
	}
	return i
}
