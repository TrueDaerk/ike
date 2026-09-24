package langpython

import (
	"strings"

	"ike/internal/lang"
)

var _ lang.StatementCompleter = toolchain{}

// pyBlockWords are the statements that open an indented block and therefore
// end in a colon. "async" is a prefix of def/for/with, not a header itself.
var pyBlockWords = []string{
	"def", "class", "if", "elif", "else", "for", "while",
	"try", "except", "finally", "with", "match", "case",
}

// CompleteStatement implements lang.StatementCompleter (#2726): a block
// header gets its unclosed brackets balanced and the colon appended, and
// opens its body; a header that already ends in ":" only opens the body; a
// one-line compound statement (`if x: return`) is complete as it stands and
// the caret moves on. Any other line — an expression, an assignment, a
// comment — has nothing to complete.
func (toolchain) CompleteStatement(line string) (lang.StatementCompletion, bool) {
	head := strings.TrimRight(line, " \t")
	stmt := strings.TrimSpace(head)
	if stmt == "" || lang.IsCommentLine(stmt) || !pyHeader(stmt) {
		return lang.StatementCompletion{}, false
	}
	if i := lang.TopLevelIndex(stmt, ':'); i >= 0 {
		if i == len(stmt)-1 {
			return lang.StatementCompletion{Head: head, Body: true}, true
		}
		return lang.StatementCompletion{Head: head}, true
	}
	return lang.StatementCompletion{Head: lang.CloseBrackets(head, "([{") + ":", Body: true}, true
}

// pyHeader reports whether stmt opens a block. The soft keywords match and
// case count only followed by whitespace — `match(x)` is a call.
func pyHeader(stmt string) bool {
	if !lang.LeadingKeyword(stmt, []string{"async"}, pyBlockWords) {
		return false
	}
	for _, soft := range []string{"match", "case"} {
		if strings.HasPrefix(stmt, soft) {
			rest := stmt[len(soft):]
			return rest != "" && (rest[0] == ' ' || rest[0] == '\t')
		}
	}
	return true
}
