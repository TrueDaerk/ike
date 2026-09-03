// Package idcase detects and converts identifier naming styles — camelCase,
// snake_case, kebab-case, PascalCase and SCREAMING_SNAKE (#2418). It is the
// text half of the editor's "Cycle Case" command: pure string work, so the
// editor side only has to decide *which* text to hand over.
//
// The package is deliberately conservative. A token that is not an identifier
// at all — punctuation, a number, a path, a sentence — has no style and is
// returned untouched rather than guessed at, so cycling in prose can never
// mangle the buffer.
package idcase

import (
	"strings"
	"unicode"
)

// Style is one identifier naming style, or Unknown for a token that names none.
type Style int

const (
	// Unknown is "not an identifier, or no style can be read off it".
	Unknown   Style = iota
	Camel           // fooBarBaz
	Snake           // foo_bar_baz
	Kebab           // foo-bar-baz
	Pascal          // FooBarBaz
	Screaming       // FOO_BAR_BAZ
)

// String names the style for notices and tests.
func (s Style) String() string {
	switch s {
	case Camel:
		return "camelCase"
	case Snake:
		return "snake_case"
	case Kebab:
		return "kebab-case"
	case Pascal:
		return "PascalCase"
	case Screaming:
		return "SCREAMING_SNAKE"
	default:
		return "unknown"
	}
}

// cycle is the rotation the editor's editor.case.cycle command walks:
// camelCase → snake_case → kebab-case → PascalCase → SCREAMING_SNAKE → camelCase.
var cycle = []Style{Camel, Snake, Kebab, Pascal, Screaming}

// Next returns the style that follows s in the cycle. Unknown stays Unknown.
func Next(s Style) Style {
	for i, c := range cycle {
		if c == s {
			return cycle[(i+1)%len(cycle)]
		}
	}
	return Unknown
}

// affixes splits the leading and trailing runs of `_` off core, so a Go blank
// prefix or a Python dunder keeps its underscores through a conversion.
func affixes(s string) (prefix, core, suffix string) {
	i := 0
	for i < len(s) && s[i] == '_' {
		i++
	}
	j := len(s)
	for j > i && s[j-1] == '_' {
		j--
	}
	return s[:i], s[i:j], s[j:]
}

// identifier reports whether core is an identifier body: letters, digits, `_`
// and `-` only, with at least one letter, and not starting with a digit. The
// separators may not sit next to each other or at an edge (that would be an
// expression like `a - b`, not a name), and the two never mix.
func identifier(core string) bool {
	if core == "" {
		return false
	}
	r := []rune(core)
	if unicode.IsDigit(r[0]) || r[0] == '-' || r[len(r)-1] == '-' || r[len(r)-1] == '_' {
		return false
	}
	letters, dash, under := false, false, false
	for i, c := range r {
		switch {
		case unicode.IsLetter(c):
			letters = true
		case unicode.IsDigit(c):
		case c == '-' || c == '_':
			if i == 0 || r[i-1] == '-' || r[i-1] == '_' {
				return false
			}
			if c == '-' {
				dash = true
			} else {
				under = true
			}
		default:
			return false
		}
	}
	return letters && !(dash && under)
}

// Detect reads the style of s, or Unknown when s is not an identifier or its
// spelling names no single style (`Foo_bar`).
func Detect(s string) Style {
	_, core, _ := affixes(s)
	if !identifier(core) {
		return Unknown
	}
	upper, lower := false, false
	for _, c := range core {
		if unicode.IsUpper(c) {
			upper = true
		} else if unicode.IsLower(c) {
			lower = true
		}
	}
	switch {
	case strings.ContainsRune(core, '-'):
		// Kebab is a lowercase-only spelling; `Foo-Bar` names no style.
		if upper {
			return Unknown
		}
		return Kebab
	case strings.ContainsRune(core, '_'):
		switch {
		case upper && !lower:
			return Screaming
		case lower && !upper:
			return Snake
		default:
			return Unknown
		}
	case upper && !lower:
		// A single all-caps word (`URL`) reads as SCREAMING_SNAKE: that is the
		// style whose one-word spelling it is.
		return Screaming
	case unicode.IsUpper([]rune(core)[0]):
		return Pascal
	default:
		return Camel
	}
}

// Words splits core into its words: at `_`/`-` separators and at camel humps,
// keeping acronym runs whole (`HTTPServer` → HTTP, Server) and digits with the
// word they follow (`utf8Name` → utf8, Name).
func Words(core string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	r := []rune(core)
	for i, c := range r {
		switch {
		case c == '_' || c == '-':
			flush()
		case unicode.IsUpper(c):
			// A hump starts a word, except inside an acronym run — there the
			// break comes one rune later, before the lowercase tail.
			prevLower := i > 0 && (unicode.IsLower(r[i-1]) || unicode.IsDigit(r[i-1]))
			nextLower := i+1 < len(r) && unicode.IsLower(r[i+1])
			prevUpper := i > 0 && unicode.IsUpper(r[i-1])
			if prevLower || (prevUpper && nextLower) {
				flush()
			}
			cur = append(cur, c)
		default:
			cur = append(cur, c)
		}
	}
	flush()
	return out
}

// title lowercases w and uppercases its first rune (`HTTP` → `Http`), the
// spelling camelCase and PascalCase use for a word after the first.
func title(w string) string {
	r := []rune(strings.ToLower(w))
	if len(r) == 0 {
		return ""
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// join renders words in style.
func join(words []string, style Style) string {
	if len(words) == 0 {
		return ""
	}
	switch style {
	case Snake:
		return strings.ToLower(strings.Join(words, "_"))
	case Kebab:
		return strings.ToLower(strings.Join(words, "-"))
	case Screaming:
		return strings.ToUpper(strings.Join(words, "_"))
	case Pascal:
		var sb strings.Builder
		for _, w := range words {
			sb.WriteString(title(w))
		}
		return sb.String()
	case Camel:
		var sb strings.Builder
		sb.WriteString(strings.ToLower(words[0]))
		for _, w := range words[1:] {
			sb.WriteString(title(w))
		}
		return sb.String()
	}
	return strings.Join(words, "")
}

// Convert rewrites s in style, preserving any leading/trailing underscores. A
// token with no readable style, or an Unknown target, is returned unchanged.
func Convert(s string, style Style) string {
	prefix, core, suffix := affixes(s)
	if style == Unknown || Detect(s) == Unknown {
		return s
	}
	words := Words(core)
	if len(words) == 0 {
		return s
	}
	return prefix + join(words, style) + suffix
}

// Cycle rewrites s in the style that follows its own. A non-identifier is
// returned unchanged with ok false, so callers can say so instead of editing.
//
// Styles that spell s identically are skipped: a one-word name like `count` is
// its own camelCase, snake_case and kebab-case spelling, and a key that
// visibly does nothing twice in a row reads as broken. Cycling it therefore
// steps count → Count → COUNT → count.
func Cycle(s string) (out string, ok bool) {
	cur := Detect(s)
	if cur == Unknown {
		return s, false
	}
	for i := 0; i < len(cycle); i++ {
		cur = Next(cur)
		if out := Convert(s, cur); out != s {
			return out, true
		}
	}
	return s, true
}
