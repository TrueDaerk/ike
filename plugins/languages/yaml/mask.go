package langyaml

// mask.go masks the values people should not have on screen by accident
// (#2345), the YAML half of the dotenv masking of #1623: in a mapping pair
// the key names the value, so `password: hunter2` can be recognised without
// ever looking at what follows — the same recipe the dotenv, JSON and Python
// producers use, asking internal/secret so the built-in tables and the
// user's own patterns (editor.secret_masking_keys) hold identically.
//
// A second producer keys on the document rather than the key: every value in
// the stringData: block of a Kubernetes Secret manifest is a secret by
// declaration — the block exists to hold plaintext credentials — so each one
// masks whatever its key says. The base64 data: block needs no mask of its
// own: its values are unreadable as written, and their decoded stand-ins
// (escapes.Base64YAMLSpans) only render where the caret is not.

import (
	"strings"

	"ike/internal/lang"
	"ike/internal/linescan"
	"ike/internal/secret"
)

// maskSpans returns the stand-in spans for the secret values of a YAML
// buffer: suspect-keyed mapping values plus Secret-manifest stringData
// entries.
func maskSpans(lines []string) []lang.Span {
	out := keyMaskSpans(lines)
	return append(out, stringDataSpans(lines)...)
}

// keyMaskSpans masks the value of every mapping pair whose key is
// secret-suspect. Only inline scalar values mask: a key opening a block
// scalar or a nested mapping has no value on its own line to cover.
func keyMaskSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		key, vs, ve, ok := mappingValue(runes)
		if !ok || !secret.Suspect(key) {
			continue
		}
		out = appendValueMask(out, li, runes, vs, ve)
	}
	return out
}

// mappingValue reads one `key: value` line — sequence markers skipped
// ("- password: …"), the key's quotes stripped — and returns the key plus
// the value's rune-column bounds, trailing comment cut. ok is false for
// comment lines, keyless lines, keys holding whitespace and empty values.
func mappingValue(runes []rune) (key string, vs, ve int, ok bool) {
	i := linescan.SkipSpace(runes, 0)
	for i < len(runes) && runes[i] == '-' && i+1 < len(runes) && linescan.IsSpace(runes[i+1]) {
		i = linescan.SkipSpace(runes, i+1)
	}
	if i >= len(runes) || runes[i] == '#' {
		return "", 0, 0, false
	}
	colon := -1
	for j := i; j < len(runes); j++ {
		if runes[j] == ':' && (j+1 >= len(runes) || linescan.IsSpace(runes[j+1])) {
			colon = j
			break
		}
	}
	if colon < 0 {
		return "", 0, 0, false
	}
	key = strings.Trim(strings.TrimSpace(string(runes[i:colon])), `"'`)
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", 0, 0, false
	}
	vs = linescan.SkipSpace(runes, colon+1)
	ve = trimSpace(runes, vs, linescan.CommentStart(runes, vs))
	if vs >= ve {
		return "", 0, 0, false
	}
	// A block-scalar introducer or a nested-structure opener carries no value
	// on this line; an anchor or alias names a node, not a credential.
	switch runes[vs] {
	case '|', '>', '&', '*', '{', '[':
		return "", 0, 0, false
	}
	return key, vs, ve, true
}

// appendValueMask masks the value in [vs, ve): a quoted scalar masks its
// content and keeps the quotes, the JSON producer's convention.
func appendValueMask(out []lang.Span, li int, runes []rune, vs, ve int) []lang.Span {
	if q := runes[vs]; (q == '"' || q == '\'') && ve-vs >= 2 && runes[ve-1] == q {
		vs, ve = vs+1, ve-1
	}
	if vs >= ve {
		return out
	}
	return append(out, secret.Span(li, vs, ve))
}

// stringDataSpans masks every value in the stringData: block of a Kubernetes
// Secret document, mirroring the document walk of escapes.Base64YAMLSpans.
func stringDataSpans(lines []string) []lang.Span {
	var out []lang.Span
	start := 0
	for i := 0; i <= len(lines); i++ {
		if i < len(lines) && strings.TrimRight(lines[i], " \t") != "---" {
			continue
		}
		out = appendStringData(out, lines, start, i)
		start = i + 1
	}
	return out
}

// appendStringData scans one YAML document, lines[start:end).
func appendStringData(out []lang.Span, lines []string, start, end int) []lang.Span {
	if !secretKind(lines[start:end]) {
		return out
	}
	for i := start; i < end; i++ {
		if strings.TrimSpace(lines[i]) != "stringData:" {
			continue
		}
		ind := indentWidth(lines[i])
		for j := i + 1; j < end; j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			if indentWidth(lines[j]) <= ind {
				i = j - 1
				break
			}
			runes := []rune(lines[j])
			// A suspect key's value is already covered by keyMaskSpans; this
			// pass adds the entries whose key alone would not mask.
			if key, vs, ve, ok := mappingValue(runes); ok && !secret.Suspect(key) {
				out = appendValueMask(out, j, runes, vs, ve)
			}
		}
	}
	return out
}

// secretKind reports whether the document declares kind: Secret at the top
// level.
func secretKind(lines []string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "kind:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "kind:"))
			return strings.Trim(v, `"'`) == "Secret"
		}
	}
	return false
}

// indentWidth counts the leading blank columns.
func indentWidth(line string) int {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	return len(line)
}

// trimSpace shrinks end back over trailing whitespace within [start, end).
func trimSpace(runes []rune, start, end int) int {
	if end > len(runes) {
		end = len(runes)
	}
	for end > start && linescan.IsSpace(runes[end-1]) {
		end--
	}
	return end
}
