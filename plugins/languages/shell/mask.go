package langshell

// mask.go masks the values people should not have on screen by accident
// (#2345), the shell half of the dotenv masking of #1623: an
// `export DB_PASSWORD=hunter2` names its value exactly the way a dotenv line
// does. Recognised are the assignment shapes shell actually has — a bare
// `NAME=value` and one behind `export`/`declare`/`local`/`readonly`/`typeset`
// (flags skipped) — with internal/secret deciding the key, so the built-in
// tables and editor.secret_masking_keys hold identically to every other
// producer. The value masks to the end of its word (quoted values mask their
// content and keep the quotes); everything after the assignment — the command
// the variable prefixes — stays readable.

import (
	"strings"

	"ike/internal/lang"
	"ike/internal/linescan"
	"ike/internal/secret"
)

// assignPrefixes are the builtins that take NAME=value operands.
var assignPrefixes = map[string]bool{
	"export": true, "declare": true, "local": true, "readonly": true, "typeset": true,
}

// maskSpans returns the stand-in spans for the secret assignments of a shell
// buffer.
func maskSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		words := linescan.Words(runes, 0)
		if len(words) == 0 {
			continue
		}
		first := string(runes[words[0][0]:words[0][1]])
		if strings.HasPrefix(first, "#") {
			continue
		}
		if !assignPrefixes[first] {
			// A bare assignment: only the first word can be one.
			out = appendAssignMask(out, li, runes, words[0])
			continue
		}
		for _, w := range words[1:] {
			if strings.HasPrefix(string(runes[w[0]:w[1]]), "-") {
				continue // a flag (declare -x)
			}
			out = appendAssignMask(out, li, runes, w)
		}
	}
	return out
}

// appendAssignMask masks the value of the word [w[0], w[1]) when it is a
// `NAME=value` assignment to a secret-suspect name. The word split is
// whitespace-based, so a quoted value containing spaces masks only up to the
// first space — masking less than the value is the one direction this
// producer cannot take, so the mask is extended to the closing quote instead.
func appendAssignMask(out []lang.Span, li int, runes []rune, w [2]int) []lang.Span {
	eq := -1
	for i := w[0]; i < w[1]; i++ {
		if runes[i] == '=' {
			eq = i
			break
		}
	}
	if eq < 0 {
		return out
	}
	name := string(runes[w[0]:eq])
	if !validName(name) || !secret.Suspect(name) {
		return out
	}
	vs, ve := eq+1, w[1]
	if vs >= len(runes) || vs >= ve {
		return out
	}
	if q := runes[vs]; q == '"' || q == '\'' {
		// The value is quoted: it ends at the closing quote, spaces included.
		for i := vs + 1; i < len(runes); i++ {
			if runes[i] == '\\' && q == '"' {
				i++
				continue
			}
			if runes[i] == q {
				ve = i
				break
			}
		}
		vs++
		if ve <= vs {
			ve = len(runes) // unterminated: mask to the line end, the safe reading
		}
	}
	if vs < ve {
		out = append(out, secret.Span(li, vs, ve))
	}
	return out
}

// validName reports whether name is a shell variable name.
func validName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
