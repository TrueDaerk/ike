package langphp

// entities.go decodes HTML character references in the markup portions of a
// PHP buffer (#2345): a .php file is an HTML document with code islands, and
// the `&amp;` in its prose is exactly the entity the html language decodes
// (#1620). Only the markup decodes — inside `<?php … ?>` an `&amp;` is a
// string's literal content or an operator soup, and reinterpreting it would
// lie — so the producer tracks the open/close tags across lines: a buffer
// starts in HTML mode (PHP's own processing model), `<?` enters code,
// `?>` leaves it.

import (
	"ike/internal/escapes"
	"ike/internal/lang"
)

// entitySpans produces the entity stand-ins for the HTML portions of a PHP
// buffer.
func entitySpans(lines []string) []lang.Span {
	var out []lang.Span
	inPHP := false
	for li, line := range lines {
		runes := []rune(line)
		start := 0 // start of the current HTML stretch
		for i := 0; i < len(runes); i++ {
			if inPHP {
				if runes[i] == '?' && i+1 < len(runes) && runes[i+1] == '>' {
					inPHP = false
					i++
					start = i + 1
				}
				continue
			}
			if runes[i] == '<' && i+1 < len(runes) && runes[i+1] == '?' {
				out = appendHTMLEntities(out, li, runes, start, i)
				inPHP = true
				i++
			}
		}
		if !inPHP {
			out = appendHTMLEntities(out, li, runes, start, len(runes))
		}
	}
	return out
}

// appendHTMLEntities decodes the references in the HTML stretch [from, to).
func appendHTMLEntities(out []lang.Span, li int, runes []rune, from, to int) []lang.Span {
	if from >= to {
		return out
	}
	for _, s := range escapes.EntityLineSpans(li, string(runes[from:to]), escapes.EntityHTML) {
		s.StartCol += from
		s.EndCol += from
		out = append(out, s)
	}
	return out
}
