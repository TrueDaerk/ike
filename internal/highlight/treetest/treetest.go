// Package treetest is the test-side reader of a lang.ComplexitySpec (#2914):
// the per-language spec tests (plugins/languages/<lang>/complexity_test.go)
// and the audit ledger (cmd/ike/complexity_audit_test.go) use it to check
// that a spec names the grammar's real node kinds and finds the probe's
// functions. It is deliberately not the complexity engine
// (internal/complexity, #2913): the CCN it reports is the audit's reference
// count — 1 + Branch nodes + boolean operators per unit, nested units
// excluded — with none of the cognitive rules, naming or nesting logic the
// engine owns.
package treetest

import (
	"ike/internal/highlight"
	"ike/internal/lang"
)

// Function is one measured unit a spec finds in a probe tree.
type Function struct {
	Name   string // text of the spec's name field, "" for an anonymous unit
	Kind   string // the node kind that started the unit
	Line   int    // 0-based line of the unit's first token
	Params int    // named children of the ParamsField node, one per `name` field where a child declares several
	CCN    int    // 1 + Branch nodes + BoolOps operators inside the unit, nested units excluded
}

// Functions walks root with spec and returns every unit in pre-order.
func Functions(root *highlight.SyntaxNode, src string, spec *lang.ComplexitySpec) []Function {
	if root == nil || spec == nil {
		return nil
	}
	var out []Function
	var visit func(n *highlight.SyntaxNode)
	visit = func(n *highlight.SyntaxNode) {
		if field, ok := spec.FunctionNodes[n.Kind]; ok {
			f := Function{Kind: n.Kind, Line: n.StartLine, CCN: 1}
			if field != "" {
				f.Name = n.Child(field).Text(src)
			}
			if spec.ParamsField != "" {
				f.Params = countParams(n.Child(spec.ParamsField))
			}
			f.CCN += branches(n, spec, true)
			out = append(out, f)
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	return out
}

// branches counts the Branch nodes and boolean operators below n, stopping
// at a nested unit (which is measured on its own). top marks n itself as the
// unit's root, which is never a nested unit.
func branches(n *highlight.SyntaxNode, spec *lang.ComplexitySpec, top bool) int {
	if !top {
		if _, unit := spec.FunctionNodes[n.Kind]; unit {
			return 0
		}
	}
	count := 0
	if !top && contains(spec.Branch, n.Kind) {
		count++
	}
	if n.Operator != "" && contains(spec.BoolOps, n.Operator) {
		count++
	}
	for _, c := range n.Children {
		count += branches(c, spec, false)
	}
	return count
}

// countParams counts the parameters of a parameter-list node: one per named
// child, except a child declaring several names (Go's `a, b int`), which
// counts once per `name` field.
func countParams(list *highlight.SyntaxNode) int {
	if list == nil {
		return 0
	}
	n := 0
	for _, p := range list.Children {
		names := 0
		for _, c := range p.Children {
			if c.Field == "name" {
				names++
			}
		}
		if names > 1 {
			n += names
		} else {
			n++
		}
	}
	return n
}

// Kinds returns the set of node kinds in the tree under root.
func Kinds(root *highlight.SyntaxNode) map[string]bool {
	out := map[string]bool{}
	var visit func(n *highlight.SyntaxNode)
	visit = func(n *highlight.SyntaxNode) {
		if n == nil {
			return
		}
		out[n.Kind] = true
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	return out
}

// Operators returns the set of operator tokens the tree under root carries.
func Operators(root *highlight.SyntaxNode) map[string]bool {
	out := map[string]bool{}
	var visit func(n *highlight.SyntaxNode)
	visit = func(n *highlight.SyntaxNode) {
		if n == nil {
			return
		}
		if n.Operator != "" {
			out[n.Operator] = true
		}
		for _, c := range n.Children {
			visit(c)
		}
	}
	visit(root)
	return out
}

// SpecKinds returns every node kind a spec names, in declaration order:
// FunctionNodes (sorted by the caller if order matters), Branch, Else,
// Nesting, Jumps.
func SpecKinds(spec *lang.ComplexitySpec) []string {
	var out []string
	for k := range spec.FunctionNodes {
		out = append(out, k)
	}
	out = append(out, spec.Branch...)
	out = append(out, spec.Else...)
	out = append(out, spec.Nesting...)
	out = append(out, spec.Jumps...)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
