package lang

import (
	"strings"
	"unicode"
)

// statement.go is the language seam behind the editor's Complete Current
// Statement command (#2726, JetBrains' cmd+shift+enter): finishing the
// syntactic shell of the line being typed — the missing closers, the block
// colon or brace, the closing line — so the caret can go straight into the
// body. The editor owns the buffer edit (indentation, undo, caret); the
// language owns the rule, as an optional Toolchain extension exactly like
// RunCommandProvider. A language without the extension is "not supported",
// which the editor reports; a supported language that finds nothing to
// complete on the line is a silent no-op.

// StatementCompletion is what completing one line means, in language terms.
// Everything is relative to the line's own indentation: the editor keeps
// Head's leading whitespace, indents the body one level deeper by the
// buffer's own settings, and places Tail at Head's indentation.
type StatementCompletion struct {
	// Head replaces the whole caret line: the original text with its
	// unclosed brackets balanced and the block opener (":" / " {" / "; then")
	// appended. Equal to the line (modulo trailing whitespace) when the
	// header was already complete — the editor then only moves the caret.
	Head string
	// Body reports that Head opens a block: the caret lands on a new line
	// one indent level deeper (or on the block's existing first line). A
	// false Body is a simple statement — the caret moves to a fresh line at
	// Head's own indentation.
	Body bool
	// Tail holds the lines closing the block ("}", "fi", "done", "esac"),
	// inserted after the body line at Head's indentation when the block is
	// not closed yet.
	Tail []string
}

// StatementCompleter is the optional Toolchain extension a language
// implements to take part in Complete Current Statement. line is the caret
// line verbatim (indentation included). ok=false means the line holds
// nothing the language knows how to complete — no edit, no error.
type StatementCompleter interface {
	CompleteStatement(line string) (StatementCompletion, bool)
}

// CompleteStatement runs langID's completer over line. supported=false when
// the language (or its Toolchain) has no StatementCompleter; ok=false when
// the completer found nothing to do.
func CompleteStatement(langID, line string) (c StatementCompletion, supported, ok bool) {
	sc, found := statementCompleterFor(langID)
	if !found {
		return StatementCompletion{}, false, false
	}
	c, ok = sc.CompleteStatement(line)
	return c, true, ok
}

// SupportsStatementCompletion reports whether langID ships a
// StatementCompleter — the per-language table the wiki lists.
func SupportsStatementCompletion(langID string) bool {
	_, found := statementCompleterFor(langID)
	return found
}

func statementCompleterFor(langID string) (StatementCompleter, bool) {
	l, found := ByID(langID)
	if !found || l.Toolchain == nil {
		return nil, false
	}
	sc, isSC := l.Toolchain.(StatementCompleter)
	return sc, isSC
}

// CloseBrackets appends the closers of line's unclosed brackets, innermost
// first, so `def abc(` becomes `def abc()` and `f([1, (2` becomes
// `f([1, (2)])`. openers names the opening brackets that count (any subset
// of "([{"); brackets inside single-, double- or backtick-quoted text are
// skipped, and a stray closer with no opener is left alone.
func CloseBrackets(line, openers string) string {
	open := unclosedOpeners(line, openers)
	var b strings.Builder
	b.WriteString(line)
	for i := len(open) - 1; i >= 0; i-- {
		b.WriteRune(bracketClosers[rune(line[open[i]])])
	}
	return b.String()
}

// unclosedOpeners returns the byte offsets of line's unclosed opening
// brackets, outermost first, skipping quoted text.
func unclosedOpeners(line, openers string) []int {
	var stack []int
	var quote rune
	escaped := false
	for i, r := range line {
		switch {
		case quote != 0:
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'' || r == '`':
			quote = r
		case strings.ContainsRune(openers, r):
			if _, ok := bracketClosers[r]; ok {
				stack = append(stack, i)
			}
		case r == ')' || r == ']' || r == '}':
			if n := len(stack); n > 0 && bracketClosers[rune(line[stack[n-1]])] == r {
				stack = stack[:n-1]
			}
		}
	}
	return stack
}

var bracketClosers = map[rune]rune{'(': ')', '[': ']', '{': '}'}

// TopLevelIndex returns the byte index of the first ch in line that sits
// outside every bracket pair and quoted string, -1 when there is none: the
// block colon of `def f(x: int) -> dict[str, int]:` is found, the annotation
// colon inside the parentheses is not.
func TopLevelIndex(line string, ch rune) int {
	depth := 0
	var quote rune
	escaped := false
	for i, r := range line {
		switch {
		case quote != 0:
			if escaped {
				escaped = false
			} else if r == '\\' {
				escaped = true
			} else if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'' || r == '`':
			quote = r
		case r == '(' || r == '[' || r == '{':
			depth++
		case r == ')' || r == ']' || r == '}':
			if depth > 0 {
				depth--
			}
		case r == ch && depth == 0:
			return i
		}
	}
	return -1
}

// LeadingKeyword reports whether stmt — a statement with its indentation
// removed — starts with one of keywords, after an optional leading "}" (the
// `} else {` shape) and any run of skip words (modifiers such as "public
// static", "async", "export default"). A keyword only counts at a word
// boundary followed by whitespace, "(", ":", "{" or the end of the line, and
// never when the next token is a lone "=": `match = re.match(...)` assigns
// to a variable, it does not open a block.
func LeadingKeyword(stmt string, skip, keywords []string) bool {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stmt), "}"))
	for changed := true; changed; {
		changed = false
		for _, w := range skip {
			if after, ok := cutKeyword(rest, w); ok && after != "" {
				rest, changed = after, true
				break
			}
		}
	}
	for _, w := range keywords {
		if after, ok := cutKeyword(rest, w); ok {
			if len(after) > 0 && after[0] == '=' && !strings.HasPrefix(after, "==") {
				return false
			}
			return true
		}
	}
	return false
}

// cutKeyword strips word from the start of s at a word boundary, returning
// the remainder with its leading whitespace removed.
func cutKeyword(s, word string) (string, bool) {
	if !strings.HasPrefix(s, word) {
		return "", false
	}
	after := s[len(word):]
	if after == "" {
		return "", true
	}
	r := []rune(after)[0]
	switch {
	case unicode.IsSpace(r), r == '(', r == ':', r == '{':
		return strings.TrimLeft(after, " \t"), true
	}
	return "", false
}

// BraceCompletion is the completion shape the C-family languages share
// (PHP, Go, JavaScript/TypeScript): a block header gets its brackets
// balanced and " {" appended, with the closer on its own line; a header
// already ending in "{" is complete and only opens its body. header decides
// whether stmt (the line without indentation, without a trailing "{") opens
// a block and names the line that closes it ("}" for most, "}()" for a Go
// `go func()`, "};" for an assigned function expression).
//
// A header nested in an unclosed call — `array_map(function ($x)`,
// `xs.forEach((x) =>` — is recognised through the call's bracket: only the
// header's own brackets are balanced, and the wrapping call's closers (plus
// the statement's ";" in the semicolon languages) join the closing line:
// `});`.
//
// With semicolons, any other non-comment line is a simple statement: it
// gets a ";" unless it already ends in one (or in another terminator), and
// the caret moves on to the next line. Without semicolons such lines have
// nothing to complete.
func BraceCompletion(line string, header func(stmt string) (closer string, ok bool), semicolons bool) (StatementCompletion, bool) {
	orig := strings.TrimRight(line, " \t")
	stmt := strings.TrimSpace(orig)
	if stmt == "" || IsCommentLine(stmt) {
		return StatementCompletion{}, false
	}
	head := orig
	complete := strings.HasSuffix(stmt, "{")
	if complete {
		head = strings.TrimRight(strings.TrimSuffix(head, "{"), " \t")
	}
	start, opener, closer, ok := wrappedHeader(head, header)
	if !ok && complete {
		// Any other line ending in "{" (a composite literal, an object)
		// opens a plain brace block.
		start, opener, closer, ok = 0, -1, "}", true
	}
	if ok {
		tail := closer
		if opener >= 0 {
			outer := head[:opener+1]
			tail += CloseBrackets(outer, "([")[len(outer):]
			if semicolons {
				tail += ";"
			}
		}
		head = head[:start] + CloseBrackets(head[start:], "([")
		if complete {
			head = orig
		} else {
			head += " {"
		}
		return StatementCompletion{Head: head, Body: true, Tail: []string{tail}}, true
	}
	if !semicolons {
		return StatementCompletion{}, false
	}
	if strings.HasSuffix(stmt, ";") || strings.HasSuffix(stmt, "}") || strings.HasSuffix(stmt, ":") || strings.HasSuffix(stmt, ",") {
		return StatementCompletion{Head: orig}, true
	}
	return StatementCompletion{Head: CloseBrackets(orig, "([") + ";"}, true
}

// wrappedHeader finds the block header in head: inside each unclosed
// bracket, outermost first — the whole bracket content, then its last
// argument (`xs.forEach((x) =>`, `HandleFunc("/", func(w, r)`) — and
// finally the line itself. start is the byte offset where the header text
// begins and opener that of the bracket wrapping it (-1 when the line
// itself is the header).
func wrappedHeader(head string, header func(stmt string) (closer string, ok bool)) (start, opener int, closer string, ok bool) {
	for _, i := range unclosedOpeners(head, "([") {
		inner := head[i+1:]
		if c, ok := header(strings.TrimSpace(inner)); ok {
			return i + 1, i, c, true
		}
		if j := lastTopLevel(inner, ','); j >= 0 {
			if c, ok := header(strings.TrimSpace(inner[j+1:])); ok {
				return i + 1 + j + 1, i, c, true
			}
		}
	}
	if c, ok := header(strings.TrimSpace(head)); ok {
		return 0, -1, c, true
	}
	return 0, -1, "", false
}

// lastTopLevel is TopLevelIndex for the last occurrence of ch.
func lastTopLevel(line string, ch rune) int {
	last := -1
	for off := 0; off < len(line); {
		i := TopLevelIndex(line[off:], ch)
		if i < 0 {
			break
		}
		last = off + i
		off = last + 1
	}
	return last
}

// IsCommentLine reports whether stmt is a whole-line comment in the common
// shapes ("//", "#", "/*", "*", "--"), which no completer touches.
func IsCommentLine(stmt string) bool {
	for _, p := range []string{"//", "#", "/*", "* ", "*/", "--"} {
		if strings.HasPrefix(stmt, p) {
			return true
		}
	}
	return stmt == "*"
}

// AssignedExpression reports whether stmt assigns or returns an expression
// whose text starts with one of keywords (`$x = match ($y)`, `const f =
// function ()`, `return function ()`): the block then closes with "};" in
// the semicolon languages. rhs is the expression text.
func AssignedExpression(stmt string, keywords []string) (rhs string, ok bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(stmt), "}"))
	if after, isReturn := cutKeyword(rest, "return"); isReturn {
		rest = after
	} else if i := strings.Index(rest, "="); i >= 0 && !strings.HasPrefix(rest[i:], "==") && !strings.HasPrefix(rest[i:], "=>") {
		rest = strings.TrimSpace(rest[i+1:])
	} else {
		return "", false
	}
	if LeadingKeyword(rest, nil, keywords) {
		return rest, true
	}
	return "", false
}
