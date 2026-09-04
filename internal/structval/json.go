package structval

import "encoding/json"

// json.go is the JSON rule (tree-sitter-json kinds: object / pair with the
// key and value fields / array / string / number / true / false / null).
//
// The caret is somewhere in a document; the value it "is in" is the innermost
// one that has a name or a slot: the value half of the enclosing pair, or —
// when the nearer container is an array — the element itself. Standing on the
// key half yields the same pair's value, so `gy` on a key answers "what is
// this set to" without moving the caret first.

// jsonValue implements the JSON rule. Outer is the whole pair (`"k": v`) or,
// for an array element, the element itself: an element has no key to include.
func jsonValue(src string, chain []Node) (Value, bool) {
	for i, n := range chain {
		if n.Kind == "pair" {
			val, ok := childByField(n, "value")
			if !ok {
				continue // a half-typed pair: nothing on the right yet
			}
			return Value{Inner: jsonDecode(src, val), Outer: text(src, n)}, true
		}
		if el, ok := elementOf(chain, i, "array"); ok {
			return Value{Inner: jsonDecode(src, el), Outer: text(src, el)}, true
		}
	}
	return Value{}, false
}

// jsonDecode returns the value's decoded form: a string node loses its quotes
// and resolves its escapes (so an embedded JSON document or a `\n`-joined log
// line lands on the clipboard as the text it encodes), every other node is
// copied exactly as the buffer holds it — a pretty-printed object stays
// pretty-printed.
func jsonDecode(src string, n Node) string {
	raw := text(src, n)
	if n.Kind == "string" {
		var s string
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			return s
		}
	}
	return raw
}
