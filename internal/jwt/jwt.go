// Package jwt detects JSON Web Tokens in a line of text and decodes their
// readable halves (#1619). It is the counterpart of internal/epochtime for the
// other opaque blob that fills .http and .env files: a token like
// "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.dBjftJ…" carries a JSON header and a
// JSON payload that no one can read by eye, plus a signature that is not
// human-meaningful at all.
//
// Two consumers use it, through two seams:
//
//   - Span producers (.http, .env) call LineSpans to dim the signature segment
//     in place, so the eye skips the noise and lands on the claims.
//   - The editor calls At + Token.Popup for the decode action, which shows the
//     header and payload as pretty-printed JSON near the token, with the
//     registered time claims (exp/iat/nbf/…) annotated as UTC dates through
//     internal/epochtime (#1618). A claim outside the plausible epoch range
//     simply stays a raw number.
//
// Detection is structural, never a guess: a run of base64url characters must
// split into exactly three non-empty dot-separated segments, and the first two
// must base64url-decode to JSON objects. That rules out version strings, file
// names and hashes — nothing but an actual JWT decodes to "{…}" twice.
package jwt

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"ike/internal/epochtime"
	"ike/internal/lang"
)

// Capture is the highlight capture carried by the signature span. The editor
// resolves it faint (highlight.go) unless a theme.captures.jwt.signature key
// overrides it, so the segment reads as the meaningless blob it is.
const Capture = "jwt.signature"

// Token is one decoded JWT: the raw text as it stands in the buffer plus the
// decoded JSON of its first two segments and the still-encoded signature.
type Token struct {
	Raw       string
	Header    string // decoded header JSON, as decoded (not pretty-printed)
	Payload   string // decoded payload JSON
	Signature string // third segment, left encoded
}

// Match is one token found on a line: the half-open rune-column range
// [Start, End) it occupies, the column its signature segment starts at, and
// the decoded token.
type Match struct {
	Start, End int
	SigStart   int
	Token      Token
}

// Scan returns the JWTs on line, left to right.
func Scan(line string) []Match {
	runes := []rune(line)
	var out []Match
	for i := 0; i < len(runes); {
		if !isTokenRune(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && isTokenRune(runes[j]) {
			j++
		}
		// A token ending a sentence ("… eyJ….eyJ….sig.") pulls the punctuation
		// into the run; trailing dots are never part of a signature we accept.
		end := j
		for end > i && runes[end-1] == '.' {
			end--
		}
		if m, ok := match(runes, i, end); ok {
			out = append(out, m)
		}
		i = j
	}
	return out
}

// At returns the token covering column col on line — or, when the caret is not
// on one, the first token of the line, so the decode action still answers for
// a caret parked at the start of an "Authorization:" header.
func At(line string, col int) (Match, bool) {
	ms := Scan(line)
	if len(ms) == 0 {
		return Match{}, false
	}
	for _, m := range ms {
		if col >= m.Start && col < m.End {
			return m, true
		}
	}
	return ms[0], true
}

// LineSpans produces the highlight spans for line li: one dimming span per
// token, covering the signature segment only. The header and payload keep
// whatever the host language styles them as — they are the part worth reading.
func LineSpans(li int, line string) []lang.Span {
	var out []lang.Span
	for _, m := range Scan(line) {
		out = append(out, lang.Span{
			Line: li, StartCol: m.SigStart, EndCol: m.End, Capture: Capture,
		})
	}
	return out
}

// Spans is LineSpans over a whole buffer.
func Spans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = append(out, LineSpans(li, line)...)
	}
	return out
}

// Decode parses a bare token string. It reports false for anything that is not
// three non-empty segments whose first two base64url-decode to JSON objects.
func Decode(raw string) (Token, bool) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return Token{}, false
	}
	for _, p := range parts {
		if p == "" || !isBase64URL(p) {
			return Token{}, false
		}
	}
	header, ok := decodeSegment(parts[0])
	if !ok {
		return Token{}, false
	}
	payload, ok := decodeSegment(parts[1])
	if !ok {
		return Token{}, false
	}
	return Token{Raw: raw, Header: header, Payload: payload, Signature: parts[2]}, true
}

// Popup renders the token for the decode popup (#1619): the header and payload
// as pretty-printed JSON in fenced blocks — so the hover renderer syntax-
// highlights them — separated by rules, with the signature named but not
// decoded. Registered time claims carry their UTC date as a trailing comment.
func (t Token) Popup() string {
	var b strings.Builder
	b.WriteString("Header\n")
	b.WriteString("```json\n" + prettyJSON(t.Header) + "\n```\n")
	b.WriteString("---\n")
	b.WriteString("Payload\n")
	b.WriteString("```json\n" + prettyJSON(t.Payload) + "\n```\n")
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("Signature: %s (%d chars, not decoded)", ellipsis(t.Signature, 24), len(t.Signature)))
	return b.String()
}

// match validates the run [i, j) as a token and builds its Match.
func match(runes []rune, i, j int) (Match, bool) {
	raw := string(runes[i:j])
	tok, ok := Decode(raw)
	if !ok {
		return Match{}, false
	}
	// The signature starts one rune past the second dot; both dots are inside
	// the run, so counting runes is exact.
	sigStart := i
	dots := 0
	for k := i; k < j; k++ {
		if runes[k] == '.' {
			dots++
			if dots == 2 {
				sigStart = k + 1
				break
			}
		}
	}
	return Match{Start: i, End: j, SigStart: sigStart, Token: tok}, true
}

// decodeSegment base64url-decodes one segment and requires the result to be a
// JSON object. Padding is tolerated: JWTs are written unpadded, but a token
// pasted from a tool that pads must still decode.
func decodeSegment(seg string) (string, bool) {
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(seg, "="))
	if err != nil {
		return "", false
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return "", false
	}
	return string(trimmed), true
}

// registered names the time-valued registered claims (RFC 7519 §4.1 plus the
// OIDC ones that follow the same convention): their numeric values are seconds
// since the epoch and read as dates.
var registered = map[string]bool{
	"exp": true, "iat": true, "nbf": true, "auth_time": true, "updated_at": true,
}

// claimLine matches one pretty-printed `"key": 1234567890,` row.
var claimLine = regexp.MustCompile(`^(\s*")([A-Za-z_]+)("\s*:\s*)(\d+)(,?)$`)

// prettyJSON indents a decoded segment and annotates the registered time
// claims with their UTC form (#1618). A value outside the plausible epoch
// range keeps its raw number — the date decoding is a soft dependency.
func prettyJSON(raw string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err != nil {
		return raw
	}
	lines := strings.Split(buf.String(), "\n")
	for i, line := range lines {
		g := claimLine.FindStringSubmatch(line)
		if g == nil || !registered[g[2]] {
			continue
		}
		if when, ok := epochtime.Decode(g[4]); ok {
			lines[i] = line + "  // " + when
		}
	}
	return strings.Join(lines, "\n")
}

// ellipsis truncates s to max runes with a trailing ellipsis.
func ellipsis(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// isTokenRune reports whether r may appear inside a token run: the base64url
// alphabet plus the segment separator.
func isTokenRune(r rune) bool { return r == '.' || isBase64Rune(r) }

func isBase64Rune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
}

// isBase64URL reports whether s consists solely of base64url characters
// (padding included).
func isBase64URL(s string) bool {
	for _, r := range s {
		if !isBase64Rune(r) && r != '=' {
			return false
		}
	}
	return true
}
