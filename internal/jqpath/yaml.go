package jqpath

// yaml.go is the positioned YAML side: yaml.v3's node tree carries Line and
// Column for every node, so unlike JSON no custom parser is needed — only a
// walk that keeps the positions the playground's decoder throws away. The
// value shapes mirror internal/jqplay/yaml.go exactly (aliases resolved under
// a budget, merge keys folded with YAML's precedence, non-string keys
// stringified, numbers as json.Number), so a query selects here what it
// selects in the yq playground.
//
// Positions under aliases point where the text is: selecting `*base` itself
// highlights the alias, while selecting a key inside it highlights the
// anchored source the key is written at.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
	"gopkg.in/yaml.v3"
)

// maxYAMLNodes caps the alias expansion, the same guard (and the same reason)
// as jqplay.MaxYAMLNodes: aliases make the decoded size unbounded in the
// source size.
const maxYAMLNodes = 1 << 20

var errYAMLTooBig = errors.New("input is not valid YAML: the aliases expand to more values than the search will hold")

// parseYAMLNodes decodes text as a YAML document stream into positioned nodes.
// yaml.v3 numbers lines across the whole stream, so a `---`-separated file
// keeps absolute positions per document.
func parseYAMLNodes(text string) ([]*node, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("input is empty")
	}
	dec := yaml.NewDecoder(strings.NewReader(text))
	budget := maxYAMLNodes
	var docs []*node
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("input is not valid YAML: %s", yamlErrDetail(err))
		}
		n, err := yamlNode(&doc, &budget)
		if err != nil {
			return nil, err
		}
		docs = append(docs, n)
	}
	if len(docs) == 0 {
		return nil, errors.New("input is empty")
	}
	return docs, nil
}

// yamlErrDetail strips yaml.v3's "yaml: " preamble and keeps the first line,
// the same trim the playground applies.
func yamlErrDetail(err error) string {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimPrefix(strings.TrimSpace(msg), "yaml: ")
}

// yamlNode converts one yaml.Node into a positioned node, spending one unit of
// budget per produced value.
func yamlNode(n *yaml.Node, budget *int) (*node, error) {
	if *budget <= 0 {
		return nil, errYAMLTooBig
	}
	*budget--
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return &node{span: yamlSpan(n)}, nil // `---` with nothing under it
		}
		return yamlNode(n.Content[0], budget)
	case yaml.AliasNode:
		if n.Alias == nil {
			return &node{span: yamlSpan(n)}, nil
		}
		out, err := yamlNode(n.Alias, budget)
		if err != nil {
			return nil, err
		}
		// The alias site is where this value stands in the document; the
		// children keep the anchored source's positions. The token is
		// `*name`, one cell wider than the anchor name.
		aliased := *out
		aliased.span = yamlSpan(n)
		aliased.span.End = aliased.span.Start + 1 + len([]rune(n.Value))
		return &aliased, nil
	case yaml.SequenceNode:
		out := &node{span: yamlContainerSpan(n)}
		val := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			child, err := yamlNode(c, budget)
			if err != nil {
				return nil, err
			}
			out.arr = append(out.arr, child)
			val = append(val, child.val)
		}
		out.val = val
		return out, nil
	case yaml.MappingNode:
		return yamlMappingNode(n, budget)
	case yaml.ScalarNode:
		return &node{val: yamlScalar(n), span: yamlScalarSpan(n)}, nil
	}
	return &node{span: yamlSpan(n)}, nil
}

// yamlMappingNode converts a mapping, folding merge keys (`<<: *base`) with
// YAML's precedence: an explicit key beats anything merged, and among several
// merged sources the first one wins.
func yamlMappingNode(n *yaml.Node, budget *int) (*node, error) {
	out := &node{obj: map[string]*node{}, span: yamlContainerSpan(n)}
	val := make(map[string]any, len(n.Content)/2)
	var merges []*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Tag == "!!merge" {
			merges = append(merges, v)
			continue
		}
		key, err := yamlNodeKey(k, budget)
		if err != nil {
			return nil, err
		}
		child, err := yamlNode(v, budget)
		if err != nil {
			return nil, err
		}
		out.obj[key] = child
		val[key] = child.val
	}
	for _, m := range merges {
		src, err := yamlNode(m, budget)
		if err != nil {
			return nil, err
		}
		// A merge value is a mapping or a sequence of mappings; anything else
		// is malformed and contributes nothing.
		for _, one := range mergeNodeSources(src) {
			for k, v := range one.obj {
				if _, taken := out.obj[k]; !taken {
					out.obj[k] = v
					val[k] = v.val
				}
			}
		}
	}
	out.val = val
	return out, nil
}

// mergeNodeSources normalizes a `<<` value into the mappings it merges in.
func mergeNodeSources(n *node) []*node {
	if n.obj != nil {
		return []*node{n}
	}
	var out []*node
	for _, e := range n.arr {
		if e.obj != nil {
			out = append(out, e)
		}
	}
	return out
}

// yamlNodeKey renders a mapping key the way jqplay.yamlKey does: a scalar
// keeps its source text, a structured key is named by its compact JSON.
func yamlNodeKey(n *yaml.Node, budget *int) (string, error) {
	if n.Kind == yaml.ScalarNode {
		return n.Value, nil
	}
	v, err := yamlNode(n, budget)
	if err != nil {
		return "", err
	}
	out, err := gojq.Marshal(v.val)
	if err != nil {
		return fmt.Sprintf("%v", v.val), nil
	}
	return string(out), nil
}

// yamlSpan converts a node's 1-based start to a one-cell span; used where no
// better extent is known.
func yamlSpan(n *yaml.Node) Span {
	line, col := max(0, n.Line-1), max(0, n.Column-1)
	return Span{Line: line, Start: col, End: col + 1}
}

// yamlContainerSpan highlights a container from its first token through the
// end of that line — the `-` or first key of a block collection, the `{` or
// `[` of a flow one.
func yamlContainerSpan(n *yaml.Node) Span {
	s := yamlSpan(n)
	s.End = -1
	return s
}

// yamlScalarSpan highlights a scalar token. A plain single-line scalar is
// written exactly as its value, so its width is the value's; every other
// style (quoted, folded, literal) highlights through the end of the line.
func yamlScalarSpan(n *yaml.Node) Span {
	s := yamlSpan(n)
	if n.Style == 0 && !strings.Contains(n.Value, "\n") && n.Value != "" {
		s.End = s.Start + len([]rune(n.Value))
		return s
	}
	s.End = -1
	return s
}

// yamlScalar mirrors jqplay's scalar conversion so both features decode a
// value identically: nulls, bools, ints and floats resolve by tag with
// json.Number keeping every digit; everything else stays the string it was
// written as.
func yamlScalar(n *yaml.Node) any {
	switch n.Tag {
	case "!!null":
		return nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err == nil {
			return b
		}
		return n.Value
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			return json.Number(strconv.FormatInt(i, 10))
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return json.Number(strconv.FormatUint(u, 10))
		}
		if _, ok := new(big.Int).SetString(n.Value, 10); ok {
			return json.Number(n.Value)
		}
		return n.Value
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return n.Value
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return f
		}
		if isDecimal(n.Value) {
			return json.Number(n.Value)
		}
		return json.Number(strconv.FormatFloat(f, 'g', -1, 64))
	}
	return n.Value
}

// isDecimal reports whether s is already a JSON number literal (jqplay's rule).
func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9') && r != '-' && r != '+' && r != '.' && r != 'e' && r != 'E' {
			return false
		}
	}
	return true
}
