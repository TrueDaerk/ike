// Package structval extracts the structural value under the caret from a
// syntax-tree snapshot (#2499): the payload the editor's `gy` copies decoded
// and `gY` copies raw.
//
// Editing a manifest, a fixture or a lockfile, the thing one wants on the
// clipboard is almost never the buffer text — it is the *value*: the JSON
// string without its quotes and with its escapes resolved (an embedded JSON
// document, a multi-line log line), the YAML block scalar folded per its
// header, the inner markup of an HTML element, the TOML value of a key. Doing
// that by hand means a visual selection plus an escape round-trip afterwards.
//
// The package is a leaf: pure Go, no CGo, no Tree-sitter, no registry import,
// like internal/docpath. It works on a Node snapshot of the ancestor chain at
// the caret, which internal/highlight fills from its Tree-sitter parse
// (SyntaxChainAt) — the grammar token is opaque outside that package, so the
// walk lives there and the *meaning* lives here, where it can be tested
// against every language without a parser in the loop.
//
// The rules per language family are deliberately small and uniform:
//
//   - JSON/TOML: the innermost key/value pair the caret is in yields its
//     value; inside an array, the element itself does. A string value is
//     decoded, everything else is copied as the buffer holds it.
//   - YAML: the innermost mapping pair or sequence item yields its value —
//     scalars decoded through the YAML parser (so block scalars fold or keep
//     their newlines exactly as YAML says), collections dedented back into a
//     document that parses on its own.
//   - HTML/XML: the innermost element yields the markup between its tags, an
//     attribute its (entity-decoded) value.
//   - Every other language: the innermost string literal, decoded.
//
// Nothing is resolved beyond that — an anchor, an alias or a $ref is copied as
// written, exactly like internal/docpath reports a path as written.
package structval

import "strings"

// Node is a language-agnostic snapshot of one syntax node. Start and End are
// byte offsets into the source the chain was taken from — the buffer's lines
// joined with "\n", which is what the caller must slice.
//
// Children holds the node's direct children (named and anonymous, with the
// field name the parent gives each one) and is only one level deep: the
// extraction rules never need a grandchild, and snapshotting a whole subtree
// for a copy command would cost the size of the document.
type Node struct {
	Kind  string
	Field string
	Named bool
	Start int
	End   int

	Children []Node
}

// Value is what the two commands copy: Inner is `gy`'s decoded payload, Outer
// is `gY`'s raw enclosing construct — the whole key/value pair, the element
// with its tags, the literal with its quotes.
//
// Inner is empty when the construct has no inner form at all (a self-closing
// element, a key without a value); the caller reports that as "nothing to
// copy" rather than putting an empty clipboard entry in the history.
type Value struct {
	Inner string
	Outer string
}

// NoValue is the notice both commands show when the caret is not inside
// anything this package recognises.
const NoValue = "no structural value under the cursor"

// Extract returns the structural value at the caret. chain is the ancestor
// chain innermost first, src the source those nodes index into, langID the
// buffer's registered language id. ok is false when nothing matches — an empty
// chain, a language without a grammar, or a caret sitting between values.
//
// A language family that finds nothing falls through to the string-literal
// rule, so the caret inside a bare JSON document or a YAML plain scalar still
// copies something useful instead of declining.
func Extract(langID, src string, chain []Node) (Value, bool) {
	if len(chain) == 0 {
		return Value{}, false
	}
	var (
		v  Value
		ok bool
	)
	switch familyOf(langID) {
	case famJSON:
		v, ok = jsonValue(src, chain)
	case famYAML:
		v, ok = yamlValue(src, chain)
	case famMarkup:
		v, ok = markupValue(src, chain)
	case famTOML:
		v, ok = tomlValue(src, chain)
	}
	if ok {
		return v, true
	}
	return literalValue(src, chain)
}

// family groups the languages that share one extraction rule.
type family int

const (
	famNone family = iota
	famJSON
	famYAML
	famMarkup
	famTOML
)

// familyOf maps a registered language id onto its rule. The jsonc extension
// resolves to the "json" language, and ansible shares YAML's syntax — the same
// aliasing internal/docpath applies.
func familyOf(langID string) family {
	switch langID {
	case "json", "jsonc", "json5", "ndjson":
		return famJSON
	case "yaml", "ansible":
		return famYAML
	case "html", "xml":
		return famMarkup
	case "toml":
		return famTOML
	}
	return famNone
}

// text slices n out of src, tolerating a snapshot that no longer matches the
// source (a caller that edited in between) rather than panicking.
func text(src string, n Node) string {
	if n.Start < 0 || n.End > len(src) || n.Start > n.End {
		return ""
	}
	return src[n.Start:n.End]
}

// between slices the gap between two sibling nodes — an element's inner
// markup, which is everything the two tags do not cover.
func between(src string, a, b Node) string {
	if a.End < 0 || b.Start > len(src) || a.End > b.Start {
		return ""
	}
	return src[a.End:b.Start]
}

// childByField returns the child n gives the named field, which is how the
// JSON, YAML and HTML grammars label the halves of a pair.
func childByField(n Node, field string) (Node, bool) {
	for _, c := range n.Children {
		if c.Field == field {
			return c, true
		}
	}
	return Node{}, false
}

// childByKind returns n's first child of any of kinds, in the order given —
// the fallback for grammars that label nothing (TOML) or that spell the same
// role differently across dialects (HTML's start_tag vs XML's STag).
func childByKind(n Node, kinds ...string) (Node, bool) {
	for _, want := range kinds {
		for _, c := range n.Children {
			if c.Kind == want {
				return c, true
			}
		}
	}
	return Node{}, false
}

// namedChildren returns n's named children, dropping punctuation and comments:
// what is left is the construct's actual parts.
func namedChildren(n Node) []Node {
	var out []Node
	for _, c := range n.Children {
		if c.Named && !isComment(c.Kind) {
			out = append(out, c)
		}
	}
	return out
}

// isComment reports whether kind names a comment node. Comments sit inside
// arrays like elements do (JSONC, TOML), and copying one as "the value under
// the cursor" would be a lie.
func isComment(kind string) bool { return strings.Contains(kind, "comment") }

// elementOf returns chain[i] when it is an element of the container kind named
// by kinds — an array item, which is a value with no key of its own.
func elementOf(chain []Node, i int, kinds ...string) (Node, bool) {
	n := chain[i]
	if !n.Named || isComment(n.Kind) || i+1 >= len(chain) {
		return Node{}, false
	}
	for _, k := range kinds {
		if chain[i+1].Kind == k {
			return n, true
		}
	}
	return Node{}, false
}
