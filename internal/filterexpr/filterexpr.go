// Package filterexpr is the filter-expression core every list pane filters
// with (#2156). It owns the syntax the Issues pane introduced (#2110/#2115)
// — fielded terms plus free match text —
//
//	severity:error file:internal/**/*.go missing return
//
// and nothing about any one pane: a pane declares a Schema naming the fields
// it understands and their vocabularies, and gets the same tokenizer, the
// same quoting rules, the same error wording and the same match helpers as
// every other pane. Problems, Usages, the TODO index and internal/issuefilter
// are all schemas over this package, so a user who learned the syntax in one
// pane can type it in the next.
//
// The syntax is deliberately tiny. A token "name:value" whose name is all
// ASCII letters is a fielded term; everything else is match text, joined by
// single spaces in written order. Double quotes group a value with spaces
// (label:"good first issue") and are the only escape there is — the same rule
// the Issues pane's live match input applies.
//
// Parsing is strict: an unknown field, a value outside a closed vocabulary
// and an unterminated quote are errors naming what is valid, because the
// readers of a whole expression (a config value, a settings form, a pane's
// filter line) have no other way to say "that token did nothing". A live,
// mid-typing input that wants leniency keeps its own splitter implementing
// the same rules — internal/ghissues/qualifier.go is the one such twin — and
// validates against a Schema from here, so the vocabularies cannot drift.
package filterexpr

import (
	"fmt"
	"strings"

	"ike/internal/fuzzy"
	"ike/internal/pathglob"
)

// Term is one fielded term: the field's canonical name (never an alias) and
// its value, unquoted.
type Term struct {
	Field string
	Value string
}

// Query is one parsed expression: the fielded terms in written order plus the
// free match text. Every part is optional — an empty Query narrows nothing.
type Query struct {
	Terms []Term
	Match string
}

// Empty reports whether the query narrows nothing.
func (q Query) Empty() bool { return len(q.Terms) == 0 && q.Match == "" }

// Has reports whether the query named field at least once.
func (q Query) Has(field string) bool {
	for _, t := range q.Terms {
		if t.Field == field {
			return true
		}
	}
	return false
}

// Value returns the field's last value, "" when the query did not name it.
// The last one wins for the single-valued fields (a second is:open after an
// is:closed reads as a correction).
func (q Query) Value(field string) string {
	out := ""
	for _, t := range q.Terms {
		if t.Field == field {
			out = t.Value
		}
	}
	return out
}

// Values returns every value written for field, in order and deduplicated —
// the repeatable fields (label:, tag:) OR their values together.
func (q Query) Values(field string) []string {
	var out []string
	for _, t := range q.Terms {
		if t.Field != field || contains(out, t.Value) {
			continue
		}
		out = append(out, t.Value)
	}
	return out
}

// Field describes one qualifier a pane accepts.
type Field struct {
	// Name is the canonical name a parsed Term carries.
	Name string
	// Aliases are alternative spellings ("state" for "is", "sev" for
	// "severity"); they parse to Name.
	Aliases []string
	// Values is the closed vocabulary, nil for a free-form value (a path
	// glob, a label name). A value outside a closed vocabulary is an error.
	Values []string
	// ValueDoc names what the field wants in the error message; it defaults
	// to the vocabulary, so only a free-form field has to set it ("a label
	// name"). A free-form field without a ValueDoc accepts an empty value.
	ValueDoc string
	// Doc is the one-line help the pane's footer or help overlay shows.
	Doc string
}

// Accepts reports whether name (already lower-cased) spells this field.
func (f Field) Accepts(name string) bool {
	return name == f.Name || contains(f.Aliases, name)
}

// wants is the error message's tail: what the field would have taken.
func (f Field) wants() string {
	if f.ValueDoc != "" {
		return f.ValueDoc
	}
	return strings.Join(f.Values, ", ")
}

// check validates one written value against the field.
func (f Field) check(written, val string) error {
	if len(f.Values) > 0 {
		if !contains(f.Values, val) {
			return fmt.Errorf("%s: wants %s", written, f.wants())
		}
		return nil
	}
	if val == "" && f.ValueDoc != "" {
		return fmt.Errorf("%s: wants %s", written, f.wants())
	}
	return nil
}

// Schema is the field set of one pane's filter language. The zero Schema
// accepts no field at all, so every token is match text.
type Schema struct {
	Fields []Field
}

// Lookup resolves a written field name (case-insensitively) to its field.
func (s Schema) Lookup(name string) (Field, bool) {
	low := strings.ToLower(name)
	for _, f := range s.Fields {
		if f.Accepts(low) {
			return f, true
		}
	}
	return Field{}, false
}

// Names lists every accepted spelling with its colon, canonical name first
// per field then its aliases — the offer order of a name completion.
func (s Schema) Names() []string {
	out := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		out = append(out, f.Name+":")
		for _, a := range f.Aliases {
			out = append(out, a+":")
		}
	}
	return out
}

// Hint is the advice an unknown-field error carries: "use is:, label: or
// sort:", built from the canonical names in declaration order.
func (s Schema) Hint() string {
	names := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		names = append(names, f.Name+":")
	}
	switch len(names) {
	case 0:
		return "this filter takes no qualifiers"
	case 1:
		return "use " + names[0]
	}
	return "use " + strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// Parse reads one expression against the schema. It rejects an unknown field,
// a value outside a closed vocabulary and an unterminated quote with a
// message naming what is valid — a settings form and a pane's filter line
// both show it verbatim, so it has to read as advice.
func (s Schema) Parse(expr string) (Query, error) {
	toks, err := Tokenize(expr)
	if err != nil {
		return Query{}, err
	}
	var q Query
	var match []string
	for _, t := range toks {
		name, val, ok := t.Qualifier()
		if !ok {
			match = append(match, t.Text)
			continue
		}
		f, known := s.Lookup(name)
		if !known {
			return Query{}, fmt.Errorf("unknown qualifier %q, %s — anything else is match text", name+":", s.Hint())
		}
		if err := f.check(name, val); err != nil {
			return Query{}, err
		}
		q.Terms = append(q.Terms, Term{Field: f.Name, Value: val})
	}
	q.Match = strings.Join(match, " ")
	return q, nil
}

// Format writes a query back as an expression: the terms in order, then the
// match text. It is the inverse of Parse up to term order and quoting.
func Format(q Query) string {
	parts := make([]string, 0, len(q.Terms)+1)
	for _, t := range q.Terms {
		parts = append(parts, t.Field+":"+Quote(t.Value))
	}
	if q.Match != "" {
		parts = append(parts, q.Match)
	}
	return strings.Join(parts, " ")
}

// Quote wraps a value in double quotes when it carries whitespace, so a
// formatted expression parses back. Quotes are the only grouping the syntax
// has — there is no escape — so an embedded quote is dropped rather than
// escaped.
func Quote(v string) string {
	if !strings.ContainsAny(v, " \t") {
		return v
	}
	return "\"" + strings.ReplaceAll(v, "\"", "") + "\""
}

// Token is one tokenized word plus the byte offset of the first colon written
// outside quotes (-1 when there is none). Quoting decides what is a field:
// label:"good first issue" splits at its unquoted colon, while a fully quoted
// "fix:tests" is plain match text.
type Token struct {
	Text  string
	Colon int
}

// Tokenize splits an expression on whitespace, keeping double-quoted runs
// together. A quote may open mid-word (label:"good first issue"), which is
// how a field carries a value with spaces; the quotes themselves are dropped
// from the token.
func Tokenize(expr string) ([]Token, error) {
	var out []Token
	var cur strings.Builder
	inWord, inQuote := false, false
	colon := -1
	flush := func() {
		if inWord {
			out = append(out, Token{Text: cur.String(), Colon: colon})
			cur.Reset()
			inWord, colon = false, -1
		}
	}
	for _, r := range expr {
		switch {
		case r == '"':
			inWord, inQuote = true, !inQuote
		case !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			flush()
		default:
			if r == ':' && !inQuote && colon < 0 {
				colon = cur.Len()
			}
			inWord = true
			cur.WriteRune(r)
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return out, nil
}

// Qualifier splits a token at its unquoted colon. It is a fielded term only
// when that colon exists and the name before it is all ASCII letters (read
// case-insensitively), so a match word carrying a colon ("fix#2:tests") stays
// match text. The returned name is lower-cased; the value keeps its case.
func (t Token) Qualifier() (name, value string, ok bool) {
	if t.Colon < 0 {
		return "", "", false
	}
	name, value = strings.ToLower(t.Text[:t.Colon]), t.Text[t.Colon+1:]
	if name == "" {
		return "", "", false
	}
	for _, r := range name {
		if r < 'a' || r > 'z' {
			return "", "", false
		}
	}
	return name, value, true
}

// MatchText gates a haystack by the free match text with the Issues pane's
// semantics (internal/fuzzy): a case-insensitive subsequence match, scored so
// a relevance order can rank by it. An empty pattern passes everything.
func MatchText(pattern, text string) (score int, ok bool) {
	if pattern == "" {
		return 0, true
	}
	res, ok := fuzzy.Match(pattern, text)
	if !ok {
		return 0, false
	}
	return res.Score, true
}

// MatchPath gates a path by a file: value. A pattern carrying glob meta
// characters is matched with internal/pathglob against the whole path and
// against its trailing segments, so "*.go" matches "internal/app/app.go" the
// way one expects while typing a filter; a meta-free pattern is a
// case-insensitive substring, which is what a half-typed directory name is.
// An empty pattern matches nothing — an explicit file: with no value would
// otherwise silently widen back to everything.
func MatchPath(pattern, path string) bool {
	if pattern == "" {
		return false
	}
	slashed := strings.ReplaceAll(path, "\\", "/")
	if !strings.ContainsAny(pattern, "*?[{") {
		return strings.Contains(strings.ToLower(slashed), strings.ToLower(pattern))
	}
	if pathglob.Match(pattern, slashed) {
		return true
	}
	// A pattern without a leading "**/" still reads as "somewhere below":
	// "*.go" and "app/*.go" both match a project-relative path's tail.
	for i := 0; i < len(slashed); i++ {
		if slashed[i] == '/' && pathglob.Match(pattern, slashed[i+1:]) {
			return true
		}
	}
	return false
}

// contains reports whether v is in list.
func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
