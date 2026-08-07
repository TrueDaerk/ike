package nethint

import (
	"strings"

	"ike/internal/lang"
)

// spans.go is the context half of the network hints (#1653): where in a buffer
// a run of characters is a CIDR prefix or a hostname rather than arbitrary
// text. Both literals carry their own syntax, so — unlike the cron hint's
// five bare fields (#1624) — the shape alone is evidence enough:
//
//   - A CIDR prefix is an address followed by `/` and a prefix length that
//     net/netip parses. The guard is positional: the address run must not
//     start right after a `/` or a token character, so a URL path segment
//     (`https://host/10.0.0.0/8`) is never read as a prefix.
//   - A punycode name is a host run carrying an `xn--` label. Host runs stop
//     at `/`, so a URL's authority is scanned and its path is not.
//
// Two producers: Spans scans whole lines, for the config-like buffers where
// every line is data (YAML, TOML, ini, dotenv, .http); QuotedSpans scans
// string literals only, for source files where a bare run of characters is
// code — the same split the other hint families draw.

// Spans produces the hints for a config-style buffer, ready to append to a
// language's lang.Language.Spans output.
func Spans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		out = append(out, lineSpans(li, []rune(line), 0)...)
	}
	return out
}

// QuotedSpans produces the hints inside the quoted string literals of a
// buffer — the source-code contexts, where an unquoted run is code and only a
// string can hold an address or a hostname.
func QuotedSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		for i := 0; i < len(runes); i++ {
			q := runes[i]
			if q != '"' && q != '\'' && q != '`' {
				continue
			}
			j := i + 1
			for j < len(runes) && runes[j] != q {
				j++
			}
			if j >= len(runes) {
				break
			}
			out = append(out, lineSpans(li, runes[i+1:j], i+1)...)
			i = j
		}
	}
	return out
}

// lineSpans scans one stretch of text — a whole line or the inside of a
// string literal — emitting a hint per literal found. offset is the rune
// column the stretch starts at, so the spans carry buffer columns.
func lineSpans(li int, runes []rune, offset int) []lang.Span {
	var out []lang.Span
	for i := 0; i < len(runes); {
		if !isHostRune(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && isHostRune(runes[j]) {
			j++
		}
		run := string(runes[i:j])
		// A CIDR prefix continues past the run: the `/` and the prefix length
		// are not host characters.
		if end, ok := prefixEnd(runes, j); ok && !pathSegment(runes, i) {
			text := string(runes[i:end])
			if hint, ok := DescribeCIDR(text); ok {
				out = append(out, span(li, offset+i, offset+end, text, hint, CIDRCapture))
				i = end
				continue
			}
		}
		host, port := trimPort(run)
		if decoded, mixed, ok := DecodeIDN(host); ok {
			capture := IDNCapture
			if mixed {
				capture = IDNMixedCapture
			}
			out = append(out, span(li, offset+i, offset+j, run, decoded+port, capture))
		}
		i = j
	}
	return out
}

// span builds the stand-in: the span covers the raw literal and Replace
// repeats it with the hint appended, so the hint reads as an annotation after
// the literal and the raw text is one caret motion away (#1594).
func span(li, start, end int, text, hint, capture string) lang.Span {
	return lang.Span{
		Line: li, StartCol: start, EndCol: end,
		Capture: capture, Replace: text + Gap + hint,
	}
}

// prefixEnd reports whether a `/<digits>` prefix length follows the host run
// ending at i, returning the column past the digits. The digits must end the
// literal — `10.0.0.0/8/foo` is a path, not a prefix.
func prefixEnd(runes []rune, i int) (int, bool) {
	if i >= len(runes) || runes[i] != '/' {
		return 0, false
	}
	j := i + 1
	for j < len(runes) && isDigit(runes[j]) {
		j++
	}
	if j == i+1 || j-(i+1) > 3 {
		return 0, false
	}
	// The prefix length ends the literal. A trailing `.` is the exception: it
	// ends a sentence far more often than it continues a number, so only a
	// dotted digit (`/8.5`) disqualifies the run.
	if j < len(runes) {
		switch r := runes[j]; {
		case r == '.':
			if j+1 < len(runes) && isDigit(runes[j+1]) {
				return 0, false
			}
		case isHostRune(r) || r == '/':
			return 0, false
		}
	}
	return j, true
}

// pathSegment reports whether the run starting at i sits after a `/`, which
// makes it a URL or filesystem path segment rather than a literal of its own.
func pathSegment(runes []rune, i int) bool {
	return i > 0 && runes[i-1] == '/'
}

// isHostRune reports whether r can appear inside an address or hostname run.
// `/` is deliberately excluded: it ends the authority of a URL and starts a
// CIDR prefix length, both of which are handled by the callers.
func isHostRune(r rune) bool {
	switch {
	case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		return true
	case r == '.' || r == ':' || r == '-' || r == '_':
		return true
	}
	return false
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// trimPort splits a `:port` suffix off a host run, so a hostname carrying one
// still decodes; the suffix is put back into the hint unchanged.
func trimPort(host string) (string, string) {
	i := strings.LastIndexByte(host, ':')
	if i < 0 || strings.Count(host, ":") > 1 {
		return host, "" // an IPv6 literal, not a port
	}
	for _, r := range host[i+1:] {
		if !isDigit(r) {
			return host, ""
		}
	}
	if i+1 == len(host) {
		return host, ""
	}
	return host[:i], host[i:]
}
