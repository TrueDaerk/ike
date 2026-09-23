package vaultinline

// scalar.go is the plain-value half of the inline vault actions (#2712): the
// probe behind "Encrypt value with Ansible Vault" — which mapping values are
// a scalar the action can replace — and the YAML quoting the inverse
// "Decrypt vault value to plain text" writes a plaintext back with.

import (
	"strconv"
	"strings"
)

// Scalar is a plain or quoted scalar value of a `key: value` line: Start and
// End are the rune columns of the value as written (quotes included), Text
// the value it denotes.
type Scalar struct {
	Key        string
	Start, End int
	Text       string
}

// ScalarAt returns the mapping value of line when it is an inline scalar an
// encrypt action could replace: a `key: value` pair (sequence dashes
// skipped), the value a plain or quoted scalar — not a tag, a block
// indicator, an anchor, an alias, a flow collection or an empty value, and
// not a comment line. A trailing comment is cut.
func ScalarAt(line string) (Scalar, bool) {
	runes := []rune(line)
	i := skipSpace(runes, 0)
	for i < len(runes) && runes[i] == '-' && (i+1 >= len(runes) || isSpace(runes[i+1])) {
		i = skipSpace(runes, i+1)
	}
	if i >= len(runes) || runes[i] == '#' {
		return Scalar{}, false
	}
	colon := -1
	for j := i; j < len(runes); j++ {
		if runes[j] == ':' && (j+1 >= len(runes) || isSpace(runes[j+1])) {
			colon = j
			break
		}
	}
	if colon < 0 {
		return Scalar{}, false
	}
	key := strings.Trim(strings.TrimSpace(string(runes[i:colon])), `"'`)
	if key == "" || strings.ContainsAny(key, " \t") {
		return Scalar{}, false
	}
	vs := skipSpace(runes, colon+1)
	if vs >= len(runes) {
		return Scalar{}, false
	}
	var ve int
	switch q := runes[vs]; q {
	case '|', '>', '&', '*', '{', '[', '!', '#':
		return Scalar{}, false
	case '"', '\'':
		ve = closingQuote(runes, vs)
		if ve < 0 {
			return Scalar{}, false
		}
		ve++
	default:
		ve = len(runes)
		for j := vs; j+1 < len(runes); j++ {
			if runes[j+1] == '#' && isSpace(runes[j]) {
				ve = j + 1
				break
			}
		}
		for ve > vs && isSpace(runes[ve-1]) {
			ve--
		}
	}
	if vs >= ve {
		return Scalar{}, false
	}
	return Scalar{Key: key, Start: vs, End: ve, Text: Unquote(string(runes[vs:ve]))}, true
}

// closingQuote returns the index of the quote closing the one at open, or -1.
func closingQuote(runes []rune, open int) int {
	q := runes[open]
	for j := open + 1; j < len(runes); j++ {
		switch {
		case q == '"' && runes[j] == '\\':
			j++
		case q == '\'' && runes[j] == '\'' && j+1 < len(runes) && runes[j+1] == '\'':
			j++
		case runes[j] == q:
			return j
		}
	}
	return -1
}

// Unquote returns the value a scalar denotes: a double-quoted scalar with
// its escapes resolved, a single-quoted one with two quotes folded to one, a plain one as
// written.
func Unquote(v string) string {
	if len(v) < 2 {
		return v
	}
	switch q := v[0]; {
	case q == '"' && v[len(v)-1] == '"':
		if s, err := strconv.Unquote(v); err == nil {
			return s
		}
		return v[1 : len(v)-1]
	case q == '\'' && v[len(v)-1] == '\'':
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	}
	return v
}

// Quote writes a plaintext as a YAML scalar: plain when nothing in it would
// change its reading, double-quoted otherwise — a line break, a `#` or `: `
// sequence, a leading indicator character, leading or trailing space, or a
// value YAML would read as a bool, null or number rather than a string.
func Quote(s string) string {
	if plainSafe(s) {
		return s
	}
	return strconv.Quote(s)
}

// plainSafe reports whether s can be written unquoted and still read back as
// exactly this string.
func plainSafe(s string) bool {
	if s == "" || s != strings.TrimSpace(s) {
		return false
	}
	if strings.ContainsAny(s, "\n\r\t#") || strings.Contains(s, ": ") || strings.HasSuffix(s, ":") {
		return false
	}
	if strings.ContainsAny(s[:1], "-?:,[]{}&*!|>'\"%@`") {
		return false
	}
	switch strings.ToLower(s) {
	case "true", "false", "yes", "no", "on", "off", "null", "~", "y", "n":
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return false
	}
	if _, err := strconv.ParseInt(s, 0, 64); err == nil {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
