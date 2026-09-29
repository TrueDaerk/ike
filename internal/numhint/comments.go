package numhint

import (
	"strings"
	"unicode"

	"ike/internal/lang"
)

// comments.go is the per-occurrence unit override (#2816): a line comment that
// names a unit decides the reading of the literals on its line. Real configs
// reuse one key with different units, and the unit is written where humans
// put it — `"timeout": 500,  # seconds` — which neither the key words nor the
// field-unit mapping (#1685) can express.
//
//   - A trailing comment applies to the literals on its own line.
//   - A full-line comment applies to the next line, and to that line only: a
//     blank line or any other statement in between breaks the link. In a
//     stack of comment lines the nearest one naming a unit wins, and a
//     trailing comment beats the comment above.
//
// The comment is matched word by word, case-insensitively, against the unit
// vocabulary of the in-scope families — the duration words, `bytes` and the
// radix words — and the first recognised word wins. A word only counts whole:
// `# msg` names nothing, and an apostrophe stays part of its word, so `it's`
// is not a seconds comment.

// Comment is the unit a line comment names: the reading, the word that named
// it and the comment itself, leader included. The zero value names nothing.
type Comment struct {
	Unit Unit
	Word string
	Text string
}

// Found reports whether the comment names a unit.
func (c Comment) Found() bool { return c.Word != "" }

// DefaultLeaders are the line-comment leaders of the format-neutral scan — the
// log renderer, and any producer that does not know its language.
var DefaultLeaders = []string{"#", "//"}

// extraLeaders are the leaders a language accepts beyond the one the registry
// records as its toggle marker: ini files take `;` as well as `#`, PHP takes
// `#` as well as `//`, and JSON (JSONC in practice) records none at all.
var extraLeaders = map[string][]string{
	"ini":    {";"},
	"php":    {"#"},
	"json":   {"//"},
	"ndjson": {"//"},
}

// CommentLeaders returns the line-comment leaders of a language id: the
// registry's LineComment plus the extras above. An unknown language, or one
// with no line comment, starts from DefaultLeaders instead.
func CommentLeaders(langID string) []string {
	var out []string
	if l, ok := lang.ByID(langID); ok && l.LineComment != "" {
		out = append(out, l.LineComment)
	} else {
		out = append(out, DefaultLeaders...)
	}
	for _, x := range extraLeaders[langID] {
		if !containsString(out, x) {
			out = append(out, x)
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// commentUnits are the words a comment may name a unit with: the duration
// words of unitWords, the byte words and the radix words. `us` and `min` are
// left out — "let us know", "min 1, max 10" are prose far more often than
// units, and `micros`/`mins` say the same unambiguously. The mapping-only
// words (`none`, `size`, `group`, the timestamps) are out of scope: a comment
// overrides the reading of the duration, byte-size and radix families only.
var commentUnits = func() map[string]Unit {
	out := map[string]Unit{
		"bytes": {Kind: UnitBytes},
		"byte":  {Kind: UnitBytes},
		"octal": {Kind: UnitOctal},
		"hex":   {Kind: UnitHex},
	}
	for w, d := range unitWords {
		if w == "us" || w == "min" {
			continue
		}
		out[w] = Unit{Kind: UnitDuration, Base: d}
	}
	return out
}()

// CommentUnit reads the first unit word out of a comment's text.
func CommentUnit(text string) (Comment, bool) {
	runes := []rune(text)
	for i := 0; i < len(runes); {
		if !commentWordRune(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && commentWordRune(runes[j]) {
			j++
		}
		word := strings.ToLower(string(runes[i:j]))
		if u, ok := commentUnits[word]; ok {
			return Comment{Unit: u, Word: word, Text: strings.TrimSpace(text)}, true
		}
		i = j
	}
	return Comment{}, false
}

// commentWordRune reports whether r continues a comment word. The apostrophe
// belongs to the word so a contraction never splits off a unit letter.
func commentWordRune(r rune) bool {
	return r == '_' || r == '\'' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// commentAt returns the rune index a trailing comment opens at: a leader at
// the line start or after a blank, outside any quoted string. It returns
// len(runes) when the line has none. An unterminated quote is plain text — a
// YAML scalar's apostrophe must not hide the comment after it.
func commentAt(runes []rune, leaders []string) int {
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '"' || r == '\'' {
			if j, ok := closingQuote(runes, i); ok {
				i = j
			}
			continue
		}
		if i > 0 && !isSpace(runes[i-1]) {
			continue
		}
		if leaderAt(runes, i, leaders) {
			return i
		}
	}
	return len(runes)
}

// closingQuote returns the index of the quote closing the string opened at i,
// honouring backslash escapes inside double quotes.
func closingQuote(runes []rune, i int) (int, bool) {
	q := runes[i]
	for j := i + 1; j < len(runes); j++ {
		switch runes[j] {
		case '\\':
			if q == '"' {
				j++
			}
		case q:
			return j, true
		}
	}
	return 0, false
}

// leaderAt reports whether one of the leaders starts at rune index i.
func leaderAt(runes []rune, i int, leaders []string) bool {
	for _, l := range leaders {
		lr := []rune(l)
		if len(lr) > 0 && i+len(lr) <= len(runes) && string(runes[i:i+len(lr)]) == l {
			return true
		}
	}
	return false
}

// fullLineComment reports whether the line is a comment and nothing else.
func fullLineComment(runes []rune, leaders []string) bool {
	i := skipSpace(runes, 0)
	return i < len(runes) && leaderAt(runes, i, leaders)
}

// CommentScan tracks the comment context down a buffer, one line at a time,
// for producers that walk the lines themselves (the code-constant scan,
// #1701). Feed it every line in order; Line returns the comment deciding that
// line's literals.
type CommentScan struct {
	leaders []string
	above   Comment
}

// NewCommentScan starts a scan with the given leaders (DefaultLeaders when
// none are given).
func NewCommentScan(leaders ...string) *CommentScan {
	if len(leaders) == 0 {
		leaders = DefaultLeaders
	}
	return &CommentScan{leaders: leaders}
}

// Line returns the comment override for the line and advances the context: a
// trailing comment naming a unit first, the comment line above second. A
// full-line comment returns the zero Comment — it holds no literals — and
// carries its unit to the next line.
func (c *CommentScan) Line(runes []rune) Comment {
	if fullLineComment(runes, c.leaders) {
		if u, ok := CommentUnit(string(runes[skipSpace(runes, 0):])); ok {
			c.above = u // the nearest comment naming a unit wins
		}
		return Comment{}
	}
	above := c.above
	c.above = Comment{}
	if at := commentAt(runes, c.leaders); at < len(runes) {
		if u, ok := CommentUnit(string(runes[at:])); ok {
			return u
		}
	}
	return above
}
