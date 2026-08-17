package lang

// postfix.go is the per-language postfix-completion table (#1913): the
// JetBrains-style templates that rewrite the expression *before* the dot
// (`err.nil` → `if err == nil { … }`) instead of inserting at the cursor.
// Templates live on the Language like ScopeNodes and FoldNodes do, so a
// language plugin contributes its own set and the completion source
// (internal/complete/postfix) stays language-neutral.

// PostfixTemplate is one postfix template. Trigger is the word typed after the
// dot ("if", "nil", "for"); Body is an LSP-snippet-syntax expansion in which
// every occurrence of the ExprPlaceholder token is replaced by the detected
// expression text. Detail is the short popup preview shown next to the label.
//
// A template may restrict itself to expressions that look like an error value
// via ErrorLike — Go's `.err` only makes sense on `err`, `myErr`, `e` and
// friends, and offering it on every expression would be noise.
type PostfixTemplate struct {
	Trigger   string
	Body      string
	Detail    string
	ErrorLike bool
}

// ExprPlaceholder is the token a PostfixTemplate body uses for the detected
// expression. It is deliberately not `$`-prefixed: bodies pass through the LSP
// snippet expander afterwards, where `$1`/`$0` are the tabstops.
const ExprPlaceholder = "EXPR"

// PostfixFor returns the postfix templates and the Tree-sitter expression node
// kinds registered for the buffer at path. Both are empty for a language that
// registers none — postfix completion is then simply inert for that file.
func PostfixFor(path string) (templates []PostfixTemplate, exprNodes []string) {
	l, ok := ByPath(path)
	if !ok {
		return nil, nil
	}
	return l.Postfix, l.PostfixExprNodes
}
