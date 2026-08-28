package ghissues

// qualifier.go is the match input's structured qualifier layer (#2110): inside
// the filter overlay's match row a token like "label:bug", "is:closed" (alias
// "state:") or "sort:oldest" is not fuzzy text — once terminated by a space
// (or by enter closing the overlay) it is extracted from the input and written
// into the same filter model the overlay's sections edit, so the chips row and
// the section rows reflect it immediately and one line can express a whole
// filter. The rest of the line stays the fuzzy pattern. A label name with
// spaces is quoted: label:"help wanted". A token that reads like a qualifier
// but is not one (unknown name, bad value) stays literal fuzzy text and the
// match row explains why. This is an optional power layer on top of the
// overlay's sections — ':' needs Shift on QWERTZ (#48), so every qualifier has
// a keyboard path through the sections too.

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"ike/internal/issuefilter"
)

// qualifier is one recognized, terminated token: the canonical dimension it
// writes ("label", "state", "sort") and its validated value.
type qualifier struct {
	name  string
	value string
}

// qualNames is what the name completion offers, in offer order: every
// spelling the shared schema accepts (#2156), sorted.
var qualNames = sortedStrings(issuefilter.Schema.Names())

// stateValues are the state gate's value candidates, sorted. Like every
// vocabulary here it is read off issuefilter.Schema — the live input and the
// config reader cannot drift, because there is only one list (#2156).
var stateValues = sortedStrings(schemaValues("is"))

// schemaValues returns one schema field's vocabulary, nil when the field is
// free-form or absent.
func schemaValues(name string) []string {
	f, ok := issuefilter.Schema.Lookup(name)
	if !ok {
		return nil
	}
	return f.Values
}

// sortedStrings returns a sorted copy of list.
func sortedStrings(list []string) []string {
	out := append([]string(nil), list...)
	sort.Strings(out)
	return out
}

// sortValues lists the sort order names, sorted for the completion.
func sortValues() []string { return sortedStrings(schemaValues("sort")) }

// splitTokens cuts the match line into space-separated tokens, keeping quoted
// spans (label:"help wanted") together, and reports whether the line ends in a
// separator space — the terminator that turns the last token into a qualifier.
//
// It is the live-input twin of filterexpr.Tokenize (#2156): the same splitting
// and quoting rules, but a half-typed unterminated quote is tolerated rather
// than an error, and the trailing separator is reported so the token still
// being typed can be left alone.
func splitTokens(input string) (toks []string, trailingSpace bool) {
	var cur []rune
	inQuote := false
	for _, r := range input {
		if r == '"' {
			inQuote = !inQuote
		}
		if r == ' ' && !inQuote {
			if len(cur) > 0 {
				toks = append(toks, string(cur))
				cur = nil
			}
			trailingSpace = true
			continue
		}
		cur = append(cur, r)
		trailingSpace = false
	}
	if len(cur) > 0 {
		toks = append(toks, string(cur))
	}
	return toks, trailingSpace
}

// unquote strips the quotes a spaced value was wrapped in.
func unquote(s string) string { return strings.ReplaceAll(s, `"`, "") }

// parseQualifier reads one token. ok means the token is a valid qualifier;
// note (only for tokens shaped like a qualifier) explains why it is not —
// the match row renders it so an ignored token is never silent.
func parseQualifier(tok string) (q qualifier, ok bool, note string) {
	i := strings.IndexByte(tok, ':')
	if i <= 0 {
		return qualifier{}, false, ""
	}
	name := strings.ToLower(tok[:i])
	for _, r := range name {
		if r < 'a' || r > 'z' {
			return qualifier{}, false, ""
		}
	}
	val := unquote(tok[i+1:])
	f, known := issuefilter.Schema.Lookup(name)
	if !known {
		return qualifier{}, false, "unknown qualifier \"" + name + ":\" — matched literally"
	}
	switch f.Name {
	case "label":
		if val == "" {
			return qualifier{}, false, "label: wants a label name"
		}
		return qualifier{name: "label", value: val}, true, ""
	case "is":
		// The overlay's own name for the dimension is "state"; "is" is the
		// spelling the shared schema canonicalizes on.
		if !inList(f.Values, val) {
			return qualifier{}, false, name + ": wants open, closed or all"
		}
		return qualifier{name: "state", value: val}, true, ""
	case "sort":
		if !inList(f.Values, val) {
			return qualifier{}, false, "sort: wants " + strings.Join(sortValues(), ", ")
		}
		return qualifier{name: "sort", value: val}, true, ""
	}
	return qualifier{}, false, ""
}

// inList reports whether v is in list.
func inList(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// extractQualifiers pulls every terminated valid qualifier out of input and
// returns the remaining fuzzy pattern. A token is terminated when another
// token follows it, when the line ends in a space, or when final is set
// (enter closing the overlay). Invalid or unknown tokens stay in the pattern.
// Without an extraction the input comes back verbatim.
func extractQualifiers(input string, final bool) (rest string, quals []qualifier) {
	toks, trailingSpace := splitTokens(input)
	kept := make([]string, 0, len(toks))
	for i, tok := range toks {
		if i < len(toks)-1 || trailingSpace || final {
			if q, ok, _ := parseQualifier(tok); ok {
				quals = append(quals, q)
				continue
			}
		}
		kept = append(kept, tok)
	}
	if len(quals) == 0 {
		return input, nil
	}
	rest = strings.Join(kept, " ")
	if trailingSpace && rest != "" {
		// The separator survives so the next word does not glue onto the
		// pattern the extraction left behind.
		rest += " "
	}
	return rest, quals
}

// qualNote is the match row's explanation for the first terminated token that
// reads like a qualifier but is not one. Derived from the input on render, so
// it can never go stale. The still-unterminated last token is exempt — it is
// being typed.
func qualNote(input string) string {
	toks, trailingSpace := splitTokens(input)
	for i, tok := range toks {
		if i == len(toks)-1 && !trailingSpace {
			break
		}
		if _, ok, note := parseQualifier(tok); !ok && note != "" {
			return note
		}
	}
	return ""
}

// parseStateName maps a validated is:/state: value onto the state gate.
func parseStateName(s string) StateFilter {
	switch s {
	case "closed":
		return FilterClosed
	case "all":
		return FilterAll
	}
	return FilterOpen
}

// applyMatchQualifiers extracts the terminated qualifiers from the match input
// and writes them into the filter model the overlay sections edit — the chips
// update exactly as if the sections had been used. The returned command is a
// refetch when a state qualifier needs a listing the pane does not hold.
func (m *Model) applyMatchQualifiers(final bool) tea.Cmd {
	rest, quals := extractQualifiers(m.fInput, final)
	if len(quals) == 0 {
		return nil
	}
	m.fInput = rest
	m.fCur = len([]rune(rest))
	var cmd tea.Cmd
	for _, q := range quals {
		switch q.name {
		case "label":
			// Repeatable, and the same OR semantics as the label section.
			m.labelSel[q.value] = true
		case "state":
			if c := m.setState(parseStateName(q.value)); c != nil {
				cmd = c
			}
		case "sort":
			m.sortTouched = true
			m.sort = parseSort(q.value)
		}
	}
	m.keepSelection()
	return cmd
}

// matchCompletion is the inline ghost the match row renders after the cursor
// and tab accepts: the rest of a qualifier name ("la" → "bel:"), or the rest
// of a value — label names from the label section's own source, state and
// sort values from their enums. Only offered with the cursor at the end of
// the input, on the first candidate the typed prefix matches. A completed
// value ends in the terminating space, so accepting it applies immediately.
func (m *Model) matchCompletion() string {
	if m.fCur != len([]rune(m.fInput)) {
		return ""
	}
	toks, trailingSpace := splitTokens(m.fInput)
	if trailingSpace || len(toks) == 0 {
		return ""
	}
	tok := toks[len(toks)-1]
	if i := strings.IndexByte(tok, ':'); i > 0 {
		return m.completeValue(strings.ToLower(tok[:i]), tok[i+1:])
	}
	low := strings.ToLower(tok)
	for _, name := range qualNames {
		if strings.HasPrefix(name, low) && len(low) < len(name) {
			return name[len(low):]
		}
	}
	return ""
}

// valueCandidates lists a qualifier's completion values, sorted.
func (m *Model) valueCandidates(name string) []string {
	switch name {
	case "is", "state":
		return stateValues
	case "sort":
		return sortValues()
	case "label":
		labels := m.filterLabels()
		out := make([]string, 0, len(labels))
		for _, l := range labels {
			out = append(out, l.Name)
		}
		return out
	}
	return nil
}

// completeValue is the ghost for a value prefix. A label name with spaces is
// only offered once the value was opened with a quote — the ghost extends the
// input at the cursor and cannot retro-insert the opening quote.
func (m *Model) completeValue(name, val string) string {
	typed := unquote(val)
	quoted := strings.HasPrefix(val, `"`)
	for _, cand := range m.valueCandidates(name) {
		if !strings.HasPrefix(cand, typed) || cand == typed {
			continue
		}
		rest := cand[len(typed):]
		if quoted {
			return rest + `" `
		}
		if strings.ContainsRune(cand, ' ') {
			continue
		}
		return rest + " "
	}
	return ""
}

// acceptCompletion is tab on the match row: it applies the inline ghost, and
// — when the ghost completed a whole qualifier — extracts it right away.
// Reports false without a ghost, so tab stays row navigation then.
func (m *Model) acceptCompletion() (tea.Cmd, bool) {
	ghost := m.matchCompletion()
	if ghost == "" {
		return nil, false
	}
	m.fInput += ghost
	m.fCur = len([]rune(m.fInput))
	cmd := m.applyMatchQualifiers(false)
	m.resetCursors()
	m.applyFilter()
	return cmd, true
}
