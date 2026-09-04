package structval

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// yaml.go is the YAML rule (tree-sitter-yaml kinds: block_mapping_pair and
// flow_pair with the key and value fields, block_sequence_item, block_node,
// flow_node, block_scalar, …).
//
// Two things make YAML different from JSON here. A scalar's *written* form and
// its value are far apart — `|`, `>`, `>-`, quotes, anchors and tags all
// change what the document means — so the decoded form comes from the YAML
// parser rather than from a hand-rolled unquote. And a nested block node is
// sliced out of the buffer with only its first line's indentation removed (the
// node starts at the first key, the lines below it keep theirs), so the text is
// dedented back into something that parses on its own.

// yamlValue implements the YAML rule. Outer is the whole pair — key, colon and
// node, exactly as written — or the whole sequence item including its dash.
func yamlValue(src string, chain []Node) (Value, bool) {
	for i, n := range chain {
		switch n.Kind {
		case "block_mapping_pair", "flow_pair":
			val, ok := childByField(n, "value")
			if !ok {
				continue // a key with no value yet
			}
			return Value{Inner: yamlDecode(text(src, val)), Outer: text(src, n)}, true
		case "block_sequence_item":
			named := namedChildren(n)
			if len(named) == 0 {
				continue // a lone "-"
			}
			return Value{Inner: yamlDecode(text(src, named[0])), Outer: text(src, n)}, true
		}
		if el, ok := elementOf(chain, i, "flow_sequence"); ok {
			return Value{Inner: yamlDecode(text(src, el)), Outer: text(src, el)}, true
		}
	}
	return Value{}, false
}

// yamlDecode turns a node's raw text into what `gy` copies: the string a
// scalar denotes, or the node's own YAML text when it is a collection.
//
// The scalar test is the parse itself — a document that unmarshals to a string
// *is* a scalar, whatever syntax produced it, and one that unmarshals to a
// number, a map or a sequence is not. Trying the raw text first matters:
// dedenting a block scalar would strip the very indentation its header counts.
func yamlDecode(raw string) string {
	if s, ok := yamlScalar(raw); ok {
		return s
	}
	if ded := dedentContinuation(raw); ded != raw {
		if s, ok := yamlScalar(ded); ok {
			return s
		}
		return ded
	}
	return raw
}

// yamlScalar reports whether s is a YAML document denoting a string, and what
// that string is. An unresolvable alias or a broken fragment simply is not one.
//
// The text is terminated with a newline first. A node sliced out of the buffer
// stops at its last character, and a literal block scalar's final line break
// is *part of its value* under YAML's default chomping — without the
// terminator `|` would drop the newline the file actually holds. Chomping
// collapses the repeated terminator when the slice already ends in one, so
// adding it is safe for every form.
func yamlScalar(s string) (string, bool) {
	var v any
	if err := yaml.Unmarshal([]byte(strings.TrimRight(s, "\n")+"\n"), &v); err != nil {
		return "", false
	}
	str, ok := v.(string)
	return str, ok
}

// dedentContinuation removes the common indentation of every line *after* the
// first. A nested block node starts at its first key, so slicing it out of the
// buffer leaves line one flush and every line below it indented — the shape no
// YAML parser accepts. Blank lines carry no indentation and do not count
// towards the common prefix.
func dedentContinuation(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) < 2 {
		return s
	}
	indent := -1
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if indent < 0 || n < indent {
			indent = n
		}
	}
	if indent <= 0 {
		return s
	}
	for i, l := range lines[1:] {
		if len(l) >= indent {
			lines[i+1] = l[indent:]
		} else {
			lines[i+1] = strings.TrimLeft(l, " ")
		}
	}
	return strings.Join(lines, "\n")
}
