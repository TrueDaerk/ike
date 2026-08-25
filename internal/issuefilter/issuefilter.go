// Package issuefilter parses the small filter expression the Issues tool
// window's default filter and its named saved filters are written in (#2115).
//
// An expression is a space-separated list of qualifiers over the three
// dimensions a filter narrows by — the state gate, the labels and the fuzzy
// match text:
//
//	state:open label:bug label:"good first issue" match:crash
//
// Anything that is not a qualifier is match text, so the shorthand "crash"
// means "match:crash"; a value with spaces is double-quoted. Labels are OR'd
// exactly as the label picker does, and a repeated qualifier is how a second
// label is added — commas never separate anything, because the settings UI
// already edits saved filters as a comma-separated list.
//
// It is a leaf package on purpose: the config validator, the settings form
// and the pane itself all have to read the same expression, so none of them
// owns the syntax.
package issuefilter

import (
	"fmt"
	"strings"
)

// Spec is one parsed expression. Every field is optional — an empty Spec
// narrows nothing, and a consumer seeding itself from it leaves the
// dimensions the expression did not name alone.
type Spec struct {
	// State is the state gate: "open", "closed", "all", or "" when the
	// expression did not name one.
	State string
	// Labels are the selected label names, in the order they were written.
	Labels []string
	// Match is the fuzzy pattern.
	Match string
}

// Empty reports whether the expression narrows nothing.
func (s Spec) Empty() bool { return s.State == "" && len(s.Labels) == 0 && s.Match == "" }

// states is the state qualifier's vocabulary, in the order the error message
// lists it.
var states = []string{"open", "closed", "all"}

// Parse reads one filter expression. It rejects an unknown qualifier, an
// unknown state and an unterminated quote with a message naming what is
// valid — the settings form shows it verbatim, so it has to read as advice.
func Parse(expr string) (Spec, error) {
	toks, err := tokenize(expr)
	if err != nil {
		return Spec{}, err
	}
	var spec Spec
	var match []string
	for _, t := range toks {
		key, val, ok := qualifier(t)
		if !ok {
			match = append(match, t.text)
			continue
		}
		switch key {
		case "state":
			if !oneOf(val, states) {
				return Spec{}, fmt.Errorf("unknown state %q, use one of %s", val, strings.Join(states, ", "))
			}
			spec.State = val
		case "label":
			if val == "" {
				return Spec{}, fmt.Errorf("label: needs a label name")
			}
			if !oneOf(val, spec.Labels) {
				spec.Labels = append(spec.Labels, val)
			}
		case "match":
			if val != "" {
				match = append(match, val)
			}
		default:
			return Spec{}, fmt.Errorf("unknown qualifier %q, use state:, label: or match:", key+":")
		}
	}
	spec.Match = strings.Join(match, " ")
	return spec, nil
}

// ParseSaved reads one "name=expression" saved-filter entry (the
// issues.saved_filters list). The name is everything before the first "=";
// it may not be empty, and the expression must parse.
func ParseSaved(entry string) (string, Spec, error) {
	name, expr, ok := strings.Cut(entry, "=")
	name = strings.TrimSpace(name)
	if !ok {
		return "", Spec{}, fmt.Errorf("missing \"=\": write a saved filter as name=state:open label:bug")
	}
	if name == "" {
		return "", Spec{}, fmt.Errorf("missing name: write a saved filter as name=state:open label:bug")
	}
	spec, err := Parse(expr)
	if err != nil {
		return "", Spec{}, err
	}
	if spec.Empty() {
		return "", Spec{}, fmt.Errorf("saved filter %q narrows nothing, name at least one of state:, label: or match:", name)
	}
	return name, spec, nil
}

// Format writes a Spec back as an expression. It is the inverse of Parse for
// every expression Parse accepts, up to qualifier order and quoting.
func Format(s Spec) string {
	var parts []string
	if s.State != "" {
		parts = append(parts, "state:"+quote(s.State))
	}
	for _, l := range s.Labels {
		parts = append(parts, "label:"+quote(l))
	}
	if s.Match != "" {
		parts = append(parts, "match:"+quote(s.Match))
	}
	return strings.Join(parts, " ")
}

// quote wraps a value in double quotes when it carries a space or a quote of
// its own, so Format's output parses back.
func quote(v string) string {
	if !strings.ContainsAny(v, " \t\"") {
		return v
	}
	return "\"" + strings.ReplaceAll(v, "\"", "\\\"") + "\""
}

// token is one tokenized word plus the byte offset of the first colon that
// was written outside quotes (-1 when there is none). Quoting decides what is
// a qualifier: label:"good first issue" splits at its unquoted colon, while a
// fully quoted "fix:tests" is plain match text.
type token struct {
	text  string
	colon int
}

// tokenize splits an expression on whitespace, keeping double-quoted runs
// together and honouring a backslash-escaped quote inside them. A quote may
// open mid-word (label:"good first issue"), which is how a qualifier carries
// a value with spaces.
func tokenize(expr string) ([]token, error) {
	var out []token
	var cur strings.Builder
	inWord, inQuote, esc := false, false, false
	colon := -1
	flush := func() {
		if inWord {
			out = append(out, token{text: cur.String(), colon: colon})
			cur.Reset()
			inWord, colon = false, -1
		}
	}
	for _, r := range expr {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case inQuote && r == '\\':
			esc = true
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
	if inQuote || esc {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	return out, nil
}

// qualifier splits a token at its unquoted colon. It is a qualifier only when
// that colon exists and the key before it is all ASCII lower-case letters, so
// a bare match word carrying a colon ("Fix:tests") stays match text.
func qualifier(t token) (string, string, bool) {
	if t.colon < 0 {
		return "", "", false
	}
	key, val := t.text[:t.colon], t.text[t.colon+1:]
	if key == "" {
		return "", "", false
	}
	for _, r := range key {
		if r < 'a' || r > 'z' {
			return "", "", false
		}
	}
	return key, val, true
}

// oneOf reports whether v is in list.
func oneOf(v string, list []string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
