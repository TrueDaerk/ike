package langsystemd

// lint.go is the systemd linter (#3065), fed by the directive table: it warns
// about a section the file's unit type does not take (`[Service]` in a
// `.timer`) or the table does not know, an unknown directive in a known
// section (with a "did you mean" for a close match), a known directive in the
// wrong section (`ExecStart=` under `[Unit]`) and a value outside the enum of
// an enumerated or boolean directive. systemd itself only logs these and goes
// on, which is what makes them easy to miss.
//
// Exempt: `X-` sections and directives (systemd ignores them), empty
// assignments (`ExecStart=` resets the list), values carrying `%` specifiers
// or `$` variables (expanded at load or run time), continued values, and
// anything before the first section header — a line-scoped pass over a huge
// file lints one line at a time and cannot see the header.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"ike/internal/lang"
)

// lintBooleans are the spellings systemd's parse_boolean accepts, compared
// case-insensitively — more than the highlighter styles as a boolean.
var lintBooleans = map[string]bool{
	"yes": true, "no": true, "true": true, "false": true, "on": true, "off": true,
	"1": true, "0": true, "y": true, "n": true, "t": true, "f": true,
}

// numericEnums are the enumerated directives whose parser falls back to a
// number (systemd's string tables "with fallback").
var numericEnums = map[string]bool{
	"SyslogLevel": true, "LogLevelMax": true, "SyslogFacility": true, "IOSchedulingClass": true,
}

// enumHintMax caps the values an invalid-value message lists.
const enumHintMax = 8

// systemdLint is the language's lang.Language.Lint hook.
func systemdLint(path string, lines []string) []lang.Note {
	typ := unitType(path)
	var out []lang.Note
	section, inSection, continued := "", false, false
	for li, line := range lines {
		runes := []rune(line)
		start, end := trimIndex(runes)
		switch {
		case start >= end:
			continued = false
		case runes[start] == '#' || runes[start] == ';':
		case continued:
			continued = runes[end-1] == '\\'
		case runes[start] == '[' && runes[end-1] == ']':
			section, inSection = string(runes[start+1:end-1]), true
			if n, ok := sectionNote(li, start+1, end-1, section, typ); ok {
				out = append(out, n)
			}
		default:
			var notes []lang.Note
			notes, continued = directiveNotes(li, runes, start, end, section, inSection, typ)
			out = append(out, notes...)
		}
	}
	return out
}

// sectionNote checks the section header name spanning [start, end) of line li.
func sectionNote(li, start, end int, name, typ string) (lang.Note, bool) {
	if IsExtension(name) {
		return lang.Note{}, false
	}
	note := lang.Note{Line: li, StartCol: start, EndCol: end, Severity: lang.NoteWarn}
	s, ok := LookupSection(name)
	switch {
	case !ok:
		names := make([]string, len(Sections))
		for i, s := range Sections {
			names[i] = s.Name
		}
		note.Message = fmt.Sprintf("unknown section [%s]", name) + suggestion(name, names, "[", "]")
	case !validIn(s, typ):
		note.Message = fmt.Sprintf("[%s] is not valid in a .%s unit", name, typ)
	default:
		return lang.Note{}, false
	}
	return note, true
}

// directiveNotes checks one `Name=value` line and reports whether its value
// continues on the next line.
func directiveNotes(li int, runes []rune, start, end int, section string, inSection bool, typ string) ([]lang.Note, bool) {
	eq := slices.Index(runes[start:end], '=')
	if eq < 0 {
		return nil, false
	}
	eq += start
	ve := trimEnd(runes, eq+1, end)
	continued := ve > eq+1 && runes[ve-1] == '\\'
	ke := trimEnd(runes, start, eq)
	name := string(runes[start:ke])
	if !inSection || ke == start || IsExtension(section) || IsExtension(name) {
		return nil, continued
	}
	if _, ok := LookupSection(section); !ok {
		return nil, continued // the header carries the note
	}
	dir, ok := Lookup(section, name)
	if !ok {
		return []lang.Note{{Line: li, StartCol: start, EndCol: ke, Severity: lang.NoteWarn,
			Message: unknownDirective(name, section, typ)}}, continued
	}
	vs := eq + 1
	for vs < ve && isSpace(runes[vs]) {
		vs++
	}
	value := string(runes[vs:ve])
	if continued || value == "" || strings.ContainsAny(value, "%$") || valueOK(dir, value) {
		return nil, continued
	}
	return []lang.Note{{Line: li, StartCol: vs, EndCol: ve, Severity: lang.NoteWarn,
		Message: invalidValue(dir, value)}}, false
}

// unknownDirective is the message for name, unknown in section: where it
// belongs when another section knows it, otherwise a close match if any.
func unknownDirective(name, section, typ string) string {
	var homes, anyHomes []string
	for _, s := range Sections {
		if _, ok := Lookup(s.Name, name); ok {
			anyHomes = append(anyHomes, "["+s.Name+"]")
			if validIn(s, typ) {
				homes = append(homes, "["+s.Name+"]")
			}
		}
	}
	if len(homes) == 0 {
		homes = anyHomes
	}
	if len(homes) > 0 {
		return fmt.Sprintf("%s= belongs in %s, not [%s]", name, orList(homes), section)
	}
	s, _ := LookupSection(section)
	names := make([]string, len(s.Directives))
	for i, dir := range s.Directives {
		names[i] = dir.Name
	}
	return fmt.Sprintf("unknown directive %s= in [%s]", name, section) + suggestion(name, names, "", "=")
}

// valueOK reports whether value is valid for an enumerated or boolean
// directive; every other kind is free-form to the linter.
func valueOK(dir Directive, value string) bool {
	switch dir.Kind {
	case KindBool:
		return lintBooleans[strings.ToLower(value)]
	case KindEnum:
		for _, v := range dir.Values {
			// `file:` takes a path after the colon, `fd` an optional `:name`.
			if value == v || (strings.HasSuffix(v, ":") && strings.HasPrefix(value, v)) || strings.HasPrefix(value, v+":") {
				return true
			}
		}
		if (slices.Contains(dir.Values, "yes") || slices.Contains(dir.Values, "no")) && lintBooleans[strings.ToLower(value)] {
			return true
		}
		if numericEnums[dir.Name] {
			_, err := strconv.Atoi(value)
			return err == nil
		}
		return false
	}
	return true
}

// invalidValue is the message for value, rejected by valueOK.
func invalidValue(dir Directive, value string) string {
	msg := fmt.Sprintf("invalid value %q for %s=", value, dir.Name)
	if dir.Kind == KindBool {
		return msg + " — expected a boolean (yes/no, true/false, on/off, 1/0)"
	}
	if s := suggestion(value, dir.Values, "", ""); s != "" {
		return msg + s
	}
	values := dir.Values
	more := ""
	if len(values) > enumHintMax {
		values, more = values[:enumHintMax], ", …"
	}
	return msg + " — expected one of " + strings.Join(values, ", ") + more
}

// suggestion is " — did you mean <pre><match><post>?" for the closest of
// candidates to word, or "" when none is close.
func suggestion(word string, candidates []string, pre, post string) string {
	if m, ok := closest(word, candidates); ok {
		return fmt.Sprintf(" — did you mean %s%s%s?", pre, m, post)
	}
	return ""
}

// closest returns the candidate nearest to word by case-insensitive edit
// distance (a transposition counts once), if within reach: one edit for a
// short word, two otherwise. Ties go to the earlier candidate.
func closest(word string, candidates []string) (string, bool) {
	limit := 2
	if len([]rune(word)) <= 4 {
		limit = 1
	}
	lw := strings.ToLower(word)
	best, bestDist := "", limit+1
	for _, c := range candidates {
		if d := editDistance(lw, strings.ToLower(c)); d < bestDist {
			best, bestDist = c, d
		}
	}
	return best, bestDist <= limit
}

// editDistance is the optimal-string-alignment distance between a and b:
// insertions, deletions, substitutions and adjacent transpositions.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev2 := make([]int, len(rb)+1)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				cur[j] = min(cur[j], prev2[j-2]+1)
			}
		}
		prev2, prev, cur = prev, cur, prev2
	}
	return prev[len(rb)]
}

// orList joins items as "a", "a or b", "a, b or c".
func orList(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}
