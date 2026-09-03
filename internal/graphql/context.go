package graphql

import (
	"regexp"
	"strings"
)

// context.go answers the one question schema-aware completion asks: standing
// *here* in a query document, what may be written next (#2423)? The answer is
// a walk of the text before the caret, never a full parse — a query being typed
// is by definition unfinished, and a parser that insists on a closed document
// says nothing exactly when help is wanted.
//
// The walk keeps a stack of type names. Entering a selection set pushes the
// type of the field that opened it; leaving one pops. That is enough to answer
// "fields of what?" at any depth, and — remembering the field whose argument
// list is open — "arguments of what?" inside a `(`.

// CaretKind classifies what a caret position may complete.
type CaretKind int

const (
	// CaretNone is a position nothing schema-aware belongs at — inside a
	// string, at the top level of a document that names no operation root.
	CaretNone CaretKind = iota
	// CaretField is inside a selection set: the fields of Caret.Type apply.
	CaretField
	// CaretArgument is inside a field's argument list: the arguments of
	// Caret.Type's Caret.Field apply.
	CaretArgument
	// CaretType is a position naming a type — after the ":" of a variable
	// definition, or after "... on". Every type name in the schema applies.
	CaretType
)

// Caret is the resolved position: what applies there, and against what.
type Caret struct {
	Kind CaretKind
	// Type is the object/interface type whose fields (CaretField) or whose
	// field's arguments (CaretArgument) apply.
	Type string
	// Field names the field whose argument list is open (CaretArgument).
	Field string
	// Prefix is the partial word already typed at the caret, which the caller
	// filters candidates by.
	Prefix string
}

// token is one lexical piece of a query document.
type token struct {
	text  string
	punct bool
}

// tokenize splits a comment- and string-stripped document into names,
// "$variables", "..." spreads and single punctuation characters. Numbers do
// not need a class of their own: they never begin a completion.
func tokenize(src string) []token {
	var out []token
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			i++
		case isNameStart(c) || c == '$':
			j := i + 1
			for j < len(src) && isNameRune(src[j]) {
				j++
			}
			out = append(out, token{text: src[i:j]})
			i = j
		case c == '.' && strings.HasPrefix(src[i:], "..."):
			out = append(out, token{text: "...", punct: true})
			i += 3
		default:
			out = append(out, token{text: string(c), punct: true})
			i++
		}
	}
	return out
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isNameRune(c byte) bool { return isNameStart(c) || (c >= '0' && c <= '9') }

// Analyze resolves the caret at the end of before — the query text up to the
// caret — against a schema. A nil schema, or a document whose operation root
// the schema does not name, yields CaretNone: completion offers nothing rather
// than a list from the wrong type.
func Analyze(s *Schema, before string) Caret {
	c := Caret{Prefix: trailingName(before)}
	if s == nil {
		return c
	}
	stripped := StripComments(before)
	if inStringOrComment(before) {
		return c
	}
	toks := tokenize(stripped)

	// The operation root: the document's leading keyword, or "query" for the
	// anonymous shorthand. Only the tokens before the first "{" can name it.
	root := s.RootType(leadingKeyword(toks))

	// stack[len(stack)-1] is the type whose selection set the caret is in;
	// "" marks a set on a type the schema does not describe (a union member,
	// a scalar), where nothing is offered.
	var stack []string
	pending := ""     // the last name seen at this level: the field a "{" would open
	spreadOn := false // the previous token was "on", so the next name is a type
	// Argument state: parens holds the depth of open "(", argField/argType the
	// field the outermost open one belongs to.
	parens := 0
	argField, argType := "", ""
	// varDefs marks the operation's own "(" — its contents are variable
	// definitions, so a name after ":" there is a type, not an argument value.
	varDefs := false
	sawSelection := false

	for i, t := range toks {
		switch {
		case !t.punct:
			if spreadOn {
				// "... on Type" — the fragment's selection set is on Type.
				pending, spreadOn = t.text, false
				continue
			}
			if t.text == "on" && i > 0 && toks[i-1].text == "..." {
				spreadOn = true
				continue
			}
			if parens == 0 && !strings.HasPrefix(t.text, "$") {
				pending = t.text
			}
		case t.text == "{":
			if !sawSelection {
				// The operation's own selection set.
				sawSelection = true
				stack = append(stack, root)
				pending = ""
				continue
			}
			next := ""
			if top := currentType(stack); top != "" && pending != "" {
				if f, ok := s.FieldByName(top, pending); ok {
					next = f.TypeName
				}
			}
			stack = append(stack, next)
			pending = ""
		case t.text == "}":
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			pending = ""
		case t.text == "(":
			if parens == 0 {
				if !sawSelection {
					varDefs = true
				} else {
					argType, argField = currentType(stack), pending
				}
			}
			parens++
		case t.text == ")":
			if parens > 0 {
				parens--
			}
			if parens == 0 {
				varDefs, argField, argType = false, "", ""
			}
		}
	}

	switch {
	case typeConditionRE.MatchString(stripped):
		// "... on <Type>": the name being written is a type condition, whether
		// or not the walk has already consumed it as one.
		c.Kind, c.Type = CaretType, ""
	case parens > 0 && varDefs:
		// Inside `query Foo($id: …)`: a name after ":" is a type.
		if afterColon(stripped) {
			c.Kind = CaretType
		}
	case parens > 0 && argField != "" && argType != "":
		if afterColon(stripped) {
			// The value side of "arg:" — the schema cannot say what a literal
			// should be, so nothing is offered.
			return c
		}
		c.Kind, c.Type, c.Field = CaretArgument, argType, argField
	case sawSelection && len(stack) > 0:
		if top := currentType(stack); top != "" {
			c.Kind, c.Type = CaretField, top
		}
	}
	return c
}

// typeConditionRE matches a caret sitting in the type name of an inline
// fragment's type condition ("... on Char|"), including the moment right after
// the "on" where nothing is typed yet.
var typeConditionRE = regexp.MustCompile(`\.\.\.\s*on\s+[A-Za-z_0-9]*$`)

// currentType is the type of the innermost open selection set.
func currentType(stack []string) string {
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

// leadingKeyword returns the operation keyword before the first "{", "query"
// when the document opens with the anonymous shorthand.
func leadingKeyword(toks []token) string {
	for _, t := range toks {
		if t.punct {
			if t.text == "{" {
				return "query"
			}
			continue
		}
		switch t.text {
		case "query", "mutation", "subscription":
			return t.text
		}
		return "query"
	}
	return "query"
}

// afterColon reports whether the last structural character before the caret's
// partial word is a ":" — the position a type name or an argument value goes.
func afterColon(before string) bool {
	s := strings.TrimRight(before, "_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s = strings.TrimRight(s, " \t\n\r[]!")
	return strings.HasSuffix(s, ":")
}

// trailingName is the partial word the caret is typing, "" when it sits on
// whitespace or punctuation. A leading "$" is kept out of it: the popup filters
// names, and the sigil is already on the line.
func trailingName(before string) string {
	i := len(before)
	for i > 0 && isNameRune(before[i-1]) {
		i--
	}
	return before[i:]
}

// inStringOrComment reports whether the caret sits inside an unclosed string
// literal or behind a "#" comment marker, the two places nothing schema-aware
// applies. Only the caret's own line matters: both end at the newline.
func inStringOrComment(before string) bool {
	line := before
	if i := strings.LastIndexByte(before, '\n'); i >= 0 {
		line = before[i+1:]
	}
	quotes := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			if quotes%2 == 1 {
				i++ // an escape inside a string never closes it
			}
		case '"':
			quotes++
		case '#':
			if quotes%2 == 0 {
				return true // a comment runs to the end of the line
			}
		}
	}
	return quotes%2 == 1
}
