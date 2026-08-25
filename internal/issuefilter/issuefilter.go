// Package issuefilter parses the filter expression the Issues tool window's
// default filter and its named saved filters are written in (#2115).
//
// The syntax is the one the filter overlay's match input already accepts
// (#2110, internal/ghissues/qualifier.go), so an expression can be typed in
// the pane and pasted into the config unchanged:
//
//	is:open label:bug label:"good first issue" sort:oldest crash
//
// `is:` (alias `state:`) takes open, closed or all; `label:` is repeated once
// per label and OR'd; `sort:` names a list order; everything else is the
// fuzzy match pattern, so a bare word means "match this". A value with spaces
// is double-quoted.
//
// The one deliberate difference from the live input is strictness. In the
// overlay an unknown qualifier stays literal fuzzy text — you are mid-typing
// and can see it — while a config value has no reader to correct it, so here
// it is an error the settings form and the config diagnostics can name.
//
// It is a leaf package on purpose: the config validator, the settings form
// and the pane all read the same expression, so none of them owns the syntax.
// ghissues/qualifier_conformance_test.go pins the two parsers to each other.
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
	// Sort is the list order, "" when the expression did not name one.
	Sort string
	// Match is the fuzzy pattern.
	Match string
}

// Empty reports whether the expression narrows nothing.
func (s Spec) Empty() bool {
	return s.State == "" && len(s.Labels) == 0 && s.Sort == "" && s.Match == ""
}

// States is the state qualifier's vocabulary, in the order error messages
// list it. Exported so the pane's conformance test can walk it.
var States = []string{"open", "closed", "all"}

// SortOrders is the sort qualifier's vocabulary. It mirrors the pane's own
// list; the conformance test fails if the two ever drift.
var SortOrders = []string{"relevance", "newest", "oldest", "updated", "number"}

// Parse reads one filter expression. It rejects an unknown qualifier, an
// unknown value and an unterminated quote with a message naming what is
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
		case "is", "state":
			if !oneOf(val, States) {
				return Spec{}, fmt.Errorf("%s: wants %s", key, strings.Join(States, ", "))
			}
			spec.State = val
		case "label":
			if val == "" {
				return Spec{}, fmt.Errorf("label: wants a label name")
			}
			if !oneOf(val, spec.Labels) {
				spec.Labels = append(spec.Labels, val)
			}
		case "sort":
			if !oneOf(val, SortOrders) {
				return Spec{}, fmt.Errorf("sort: wants %s", strings.Join(SortOrders, ", "))
			}
			spec.Sort = val
		default:
			return Spec{}, fmt.Errorf("unknown qualifier %q, use is:, label: or sort: — anything else is match text", key+":")
		}
	}
	spec.Match = strings.Join(match, " ")
	return spec, nil
}

// ParseSaved reads one "name=expression" saved-filter entry (the
// issues.saved_filters list). The name is everything before the first "=";
// it may not be empty, and the expression must parse and narrow something.
func ParseSaved(entry string) (string, Spec, error) {
	name, expr, ok := strings.Cut(entry, "=")
	name = strings.TrimSpace(name)
	if !ok {
		return "", Spec{}, fmt.Errorf("missing \"=\": write a saved filter as name=is:open label:bug")
	}
	if name == "" {
		return "", Spec{}, fmt.Errorf("missing name: write a saved filter as name=is:open label:bug")
	}
	spec, err := Parse(expr)
	if err != nil {
		return "", Spec{}, err
	}
	if spec.Empty() {
		return "", Spec{}, fmt.Errorf("saved filter %q narrows nothing, name at least one of is:, label:, sort: or some match text", name)
	}
	return name, spec, nil
}

// Format writes a Spec back as an expression. It is the inverse of Parse for
// every expression Parse accepts, up to qualifier order and quoting.
func Format(s Spec) string {
	var parts []string
	if s.State != "" {
		parts = append(parts, "is:"+quote(s.State))
	}
	for _, l := range s.Labels {
		parts = append(parts, "label:"+quote(l))
	}
	if s.Sort != "" {
		parts = append(parts, "sort:"+quote(s.Sort))
	}
	if s.Match != "" {
		parts = append(parts, s.Match)
	}
	return strings.Join(parts, " ")
}

// quote wraps a value in double quotes when it carries a space, so Format's
// output parses back. Quotes are the only grouping the syntax has — there is
// no escape, exactly as in the live match input.
func quote(v string) string {
	if !strings.ContainsAny(v, " \t") {
		return v
	}
	return "\"" + strings.ReplaceAll(v, "\"", "") + "\""
}

// token is one tokenized word plus the byte offset of the first colon written
// outside quotes (-1 when there is none). Quoting decides what is a
// qualifier: label:"good first issue" splits at its unquoted colon, while a
// fully quoted "fix:tests" is plain match text.
type token struct {
	text  string
	colon int
}

// tokenize splits an expression on whitespace, keeping double-quoted runs
// together. A quote may open mid-word (label:"good first issue"), which is
// how a qualifier carries a value with spaces; the quotes themselves are
// dropped from the token, mirroring the live input's unquote.
func tokenize(expr string) ([]token, error) {
	var out []token
	var cur strings.Builder
	inWord, inQuote := false, false
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

// qualifier splits a token at its unquoted colon. It is a qualifier only when
// that colon exists and the key before it is all ASCII letters (read
// case-insensitively), so a bare match word carrying a colon ("fix#2:tests")
// stays match text — the same rule the live input applies.
func qualifier(t token) (string, string, bool) {
	if t.colon < 0 {
		return "", "", false
	}
	key, val := strings.ToLower(t.text[:t.colon]), t.text[t.colon+1:]
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
