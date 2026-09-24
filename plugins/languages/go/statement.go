package langgo

import (
	"regexp"

	"ike/internal/lang"
)

var _ lang.StatementCompleter = toolchain{}

var (
	// goBlockWords open a brace block at the start of a statement.
	goBlockWords = []string{"func", "if", "else", "for", "switch", "select"}
	// goTypeHeader is a struct or interface declaration (`type T struct`,
	// `type T[K comparable] interface`), goFuncLiteral a goroutine or defer
	// of a closure, which closes with its call: `}()`.
	goTypeHeader  = regexp.MustCompile(`^type\s+\w+(\[[^\]]*\])?\s+(struct|interface)$`)
	goFuncLiteral = regexp.MustCompile(`^(go|defer)\s+func\b`)
)

// CompleteStatement implements lang.StatementCompleter (#2726): a func /
// if / for / switch / select / struct / interface header without its "{"
// gets its parentheses closed and " {" appended, with the "}" on its own
// line; `go func()` and `defer func()` close with "}()". Go has no
// semicolon rule, so any other line has nothing to complete.
func (toolchain) CompleteStatement(line string) (lang.StatementCompletion, bool) {
	return lang.BraceCompletion(line, goHeader, false)
}

// goHeader classifies stmt (indentation and a trailing "{" removed).
func goHeader(stmt string) (string, bool) {
	switch {
	case goFuncLiteral.MatchString(stmt):
		return "}()", true
	case goTypeHeader.MatchString(stmt), lang.LeadingKeyword(stmt, nil, goBlockWords):
		return "}", true
	}
	if _, ok := lang.AssignedExpression(stmt, []string{"func"}); ok {
		return "}", true
	}
	return "", false
}
