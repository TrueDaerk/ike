package langphp

import (
	"strings"

	"ike/internal/lang"
)

var _ lang.StatementCompleter = toolchain{}

// phpBlockWords open a brace block; phpModifiers may precede a declaration.
var (
	phpBlockWords = []string{
		"function", "class", "interface", "trait", "enum", "if", "else", "elseif",
		"for", "foreach", "while", "do", "switch", "match", "try", "catch", "finally",
	}
	phpModifiers = []string{"public", "protected", "private", "static", "abstract", "final", "readonly"}
	// phpExpressionWords are the block-shaped expressions: assigned or
	// returned, their block closes with "};". An arrow fn has an expression
	// body and no block, so it is not one of them.
	phpExpressionWords = []string{"function", "static function", "match", "new class"}
)

// CompleteStatement implements lang.StatementCompleter (#2726): a
// declaration or control-flow header without its "{" gets its parentheses
// closed and " {" appended, with the "}" on its own line; an assigned
// closure or match expression closes with "};". Every other statement gets
// its ";" — JetBrains' behaviour — and the caret moves to the next line.
// The PHP open/close tags are left alone.
func (toolchain) CompleteStatement(line string) (lang.StatementCompletion, bool) {
	stmt := strings.TrimSpace(line)
	if strings.HasPrefix(stmt, "<?") || strings.HasSuffix(stmt, "?>") {
		return lang.StatementCompletion{}, false
	}
	return lang.BraceCompletion(line, phpHeader, true)
}

// phpHeader classifies stmt: a declaration or control-flow header closes
// with "}", an expression-shaped block with "};".
func phpHeader(stmt string) (string, bool) {
	if _, ok := lang.AssignedExpression(stmt, phpExpressionWords); ok {
		return "};", true
	}
	if lang.LeadingKeyword(stmt, phpModifiers, phpBlockWords) {
		return "}", true
	}
	return "", false
}
