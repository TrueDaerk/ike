package langshell

import (
	"regexp"
	"strings"

	"ike/internal/lang"
)

var _ lang.StatementCompleter = toolchain{}

// shellFunction matches a function header: `name()`, `function name` or
// `function name()`.
var shellFunction = regexp.MustCompile(`^(function\s+[\w.:-]+(\s*\(\s*\))?|[\w.:-]+\s*\(\s*\))$`)

// CompleteStatement implements lang.StatementCompleter (#2726): `if` and
// `elif` get "; then", `for` / `while` / `until` get "; do", `case` gets
// " in", a function header gets " {", each with its closing word (fi, done,
// esac, }) on its own line unless the header was already complete. Any
// other line has nothing to complete.
func (toolchain) CompleteStatement(line string) (lang.StatementCompletion, bool) {
	head := strings.TrimRight(line, " \t")
	stmt := strings.TrimSpace(head)
	if stmt == "" || lang.IsCommentLine(stmt) {
		return lang.StatementCompletion{}, false
	}
	// open completes a header with suffix unless its keyword (then, do, in,
	// {) already ends the line — at a word boundary, so `if $undo` is not
	// mistaken for a finished loop header.
	open := func(suffix string, tail ...string) (lang.StatementCompletion, bool) {
		word := strings.TrimLeft(suffix, "; ")
		complete := strings.HasSuffix(stmt, " "+word) || strings.HasSuffix(stmt, ";"+word) ||
			(word == "{" && strings.HasSuffix(stmt, "{"))
		if complete {
			return lang.StatementCompletion{Head: head, Body: true, Tail: tail}, true
		}
		return lang.StatementCompletion{Head: strings.TrimSuffix(head, ";") + suffix, Body: true, Tail: tail}, true
	}
	switch {
	case lang.LeadingKeyword(stmt, nil, []string{"if"}):
		return open("; then", "fi")
	case lang.LeadingKeyword(stmt, nil, []string{"elif"}):
		return open("; then")
	case stmt == "else":
		return lang.StatementCompletion{Head: head, Body: true}, true
	case lang.LeadingKeyword(stmt, nil, []string{"for", "while", "until"}):
		return open("; do", "done")
	case lang.LeadingKeyword(stmt, nil, []string{"case"}):
		return open(" in", "esac")
	case shellFunction.MatchString(strings.TrimSpace(strings.TrimSuffix(stmt, "{"))):
		return open(" {", "}")
	}
	return lang.StatementCompletion{}, false
}
