package structval

import "html"

// markup.go is the HTML/XML rule. The two grammars spell the same roles
// differently — tree-sitter-html has element / start_tag / end_tag /
// attribute / quoted_attribute_value, the vendored XML grammar has element /
// STag / ETag / Attribute / AttValue — so every lookup names both.
//
// An element's *value* is what sits between its tags, markup and all:
// `<p>Hello <b>x</b></p>` yields `Hello <b>x</b>`, because that is the payload
// one pastes into another document or a fixture. Entities are left alone there
// — the text is markup, and decoding `&amp;` inside it would produce a
// document that no longer means the same thing. An attribute is the opposite
// case: its value is plain text, so its entities *are* resolved.

// markupValue implements the HTML/XML rule. Outer is the element with its
// tags, or the whole `name="value"` attribute.
func markupValue(src string, chain []Node) (Value, bool) {
	for _, n := range chain {
		switch n.Kind {
		case "attribute", "Attribute":
			val, ok := childByKind(n, "quoted_attribute_value", "attribute_value", "AttValue")
			if !ok {
				continue // a bare attribute with no value
			}
			return Value{
				Inner: html.UnescapeString(trimQuotes(text(src, val))),
				Outer: text(src, n),
			}, true
		case "element":
			outer := text(src, n)
			start, sok := childByKind(n, "start_tag", "STag")
			end, eok := childByKind(n, "end_tag", "ETag")
			if !sok || !eok {
				// A void or self-closing element (`<br/>`, `<img …>`) has no
				// inside; gY still copies the tag, gy reports there is
				// nothing to take.
				return Value{Outer: outer}, true
			}
			return Value{Inner: between(src, start, end), Outer: outer}, true
		}
	}
	return Value{}, false
}

// trimQuotes strips one matching pair of ASCII quotes, which is how XML's
// AttValue (and HTML's quoted_attribute_value) carries its delimiters. An
// unquoted HTML attribute value has none and passes through.
func trimQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	q := s[0]
	if (q == '"' || q == '\'') && s[len(s)-1] == q {
		return s[1 : len(s)-1]
	}
	return s
}
