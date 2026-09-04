package structval

import "github.com/BurntSushi/toml"

// toml.go is the TOML rule (tree-sitter-toml kinds: table / pair / bare_key /
// dotted_key / quoted_key / string / integer / array / inline_table). The
// grammar labels no fields, so a pair's halves are read positionally: the
// first named child is the key, the last is the value.
//
// Decoding goes through the TOML parser rather than through an unquote, for
// the same reason YAML does: basic, literal and the two multi-line forms all
// mean different things about escapes and about the leading newline, and the
// parser already knows all four.

// tomlValue implements the TOML rule. Outer is the whole `key = value` pair,
// or, inside an array, the element itself.
func tomlValue(src string, chain []Node) (Value, bool) {
	for i, n := range chain {
		if n.Kind == "pair" {
			named := namedChildren(n)
			if len(named) < 2 {
				continue // a key with nothing assigned yet
			}
			return Value{
				Inner: tomlDecode(text(src, named[len(named)-1])),
				Outer: text(src, n),
			}, true
		}
		if el, ok := elementOf(chain, i, "array"); ok {
			raw := text(src, el)
			return Value{Inner: tomlDecode(raw), Outer: raw}, true
		}
	}
	return Value{}, false
}

// tomlDecode returns the string a TOML value denotes, or the value's raw text
// when it denotes anything else — a number, a date, an array, an inline table
// — which is then copied exactly as the buffer holds it.
func tomlDecode(raw string) string {
	var m map[string]any
	if _, err := toml.Decode("v = "+raw, &m); err == nil {
		if s, ok := m["v"].(string); ok {
			return s
		}
	}
	return raw
}
