package langweb

import (
	"strings"

	"ike/internal/lang"
)

var _ lang.StatementCompleter = tsToolchain{}

var (
	// tsBlockWords open a brace block; tsModifiers may precede a declaration.
	tsBlockWords = []string{
		"function", "class", "interface", "enum", "namespace", "module", "if", "else",
		"for", "while", "do", "switch", "try", "catch", "finally",
	}
	tsModifiers = []string{
		"export", "default", "async", "static", "public", "private", "protected",
		"abstract", "declare", "override", "readonly",
	}
	// tsExpressionWords are the block-shaped expressions: assigned or
	// returned, their block closes with "};".
	tsExpressionWords = []string{"function", "async function", "class"}
)

// CompleteStatement implements lang.StatementCompleter (#2726) for
// JavaScript and TypeScript: a declaration or control-flow header, or an
// arrow function about to open a block body (`const f = (a) =>`), gets its
// parentheses closed and " {" appended with the closer on its own line —
// "};" when the block is an assigned or returned expression. Every other
// statement gets its ";" and the caret moves to the next line.
func (tsToolchain) CompleteStatement(line string) (lang.StatementCompletion, bool) {
	return lang.BraceCompletion(line, tsHeader, true)
}

// tsHeader classifies stmt (indentation and a trailing "{" removed).
func tsHeader(stmt string) (string, bool) {
	if strings.HasSuffix(stmt, "=>") {
		assigned := lang.TopLevelIndex(stmt, '=') < strings.LastIndex(stmt, "=>")
		if assigned || strings.HasPrefix(stmt, "return ") {
			return "};", true
		}
		return "}", true
	}
	if _, ok := lang.AssignedExpression(stmt, tsExpressionWords); ok {
		return "};", true
	}
	if lang.LeadingKeyword(stmt, tsModifiers, tsBlockWords) {
		return "}", true
	}
	return "", false
}
