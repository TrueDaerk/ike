package langdockerfile

// mask.go masks the values people should not have on screen by accident
// (#2345), the Dockerfile half of the dotenv masking of #1623: an
// `ENV DB_PASSWORD=hunter2` bakes the credential into the image and its name
// says so, exactly like a dotenv key. Both spellings of ENV/ARG are read —
// the `KEY=value` operands (several per line) and the legacy space-separated
// `ENV KEY value`, whose value runs to the end of the line. internal/secret
// decides the key, so the built-in tables and editor.secret_masking_keys
// hold identically to every other producer.

import (
	"strings"

	"ike/internal/lang"
	"ike/internal/linescan"
	"ike/internal/secret"
)

// maskSpans returns the stand-in spans for the secret ENV/ARG values of a
// Dockerfile.
func maskSpans(lines []string) []lang.Span {
	var out []lang.Span
	for li, line := range lines {
		runes := []rune(line)
		words := linescan.Words(runes, 0)
		if len(words) < 2 {
			continue
		}
		switch strings.ToUpper(string(runes[words[0][0]:words[0][1]])) {
		case "ENV", "ARG":
		default:
			continue
		}
		if eqIn(runes, words[1]) < 0 {
			// The legacy form: `ENV KEY value…`, one key, value to line end.
			key := string(runes[words[1][0]:words[1][1]])
			if len(words) >= 3 && secret.Suspect(key) {
				out = append(out, secret.Span(li, words[2][0], trimEnd(runes)))
			}
			continue
		}
		for _, w := range words[1:] {
			out = appendPairMask(out, li, runes, w)
		}
	}
	return out
}

// appendPairMask masks the value of one `KEY=value` operand whose key is
// secret-suspect. A quoted value masks its content and keeps the quotes.
func appendPairMask(out []lang.Span, li int, runes []rune, w [2]int) []lang.Span {
	eq := eqIn(runes, w)
	if eq < 0 {
		return out
	}
	key := string(runes[w[0]:eq])
	if !secret.Suspect(key) {
		return out
	}
	vs, ve := eq+1, w[1]
	if q := runes[vs]; vs < ve && (q == '"' || q == '\'') && ve-vs >= 2 && runes[ve-1] == q {
		vs, ve = vs+1, ve-1
	}
	if vs < ve {
		out = append(out, secret.Span(li, vs, ve))
	}
	return out
}

// eqIn returns the column of the first `=` inside the word, or -1.
func eqIn(runes []rune, w [2]int) int {
	for i := w[0]; i < w[1]; i++ {
		if runes[i] == '=' {
			return i
		}
	}
	return -1
}

// trimEnd returns the line length with trailing whitespace stripped.
func trimEnd(runes []rune) int {
	end := len(runes)
	for end > 0 && linescan.IsSpace(runes[end-1]) {
		end--
	}
	return end
}
