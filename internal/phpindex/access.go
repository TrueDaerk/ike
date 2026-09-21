package phpindex

// access.go resolves the member access under a position (Epic 0520, #2670).
// Navigation and hover need to know *which* member the cursor names before
// they can ask Lookup for it, and they must get that from the syntax tree
// rather than a regular expression: `$this->abc(`, `$this->abc` and
// `self::K` are three different node shapes, and a regex over the line
// cannot tell the member name from the receiver, an argument or a string.
//
// MemberAccessAt parses the text once (highlight.SyntaxTree, the same
// snapshot the extractor reads) and walks from the innermost node containing
// the position up to the nearest member access. Only accesses whose receiver
// is `$this`, `self` or `static` resolve — those are exactly the ones the
// server mis-resolves inside a trait body; `$other->abc()` stays the
// server's business.
//
// Without a syntax tree (no cgo) nothing resolves, so every caller degrades
// to "the index has no answer".

import (
	"strings"

	"ike/internal/highlight"
)

// Access is one resolved member access: the member's written name and the
// kinds a declaration of it may have, in the order Lookup should try them.
// A `::` access names a constant or an enum case, and an arrow access may
// name a property or a method (`$this->abc` inside `$this->abc()` when the
// cursor rests on the operator), so a single kind would miss half of them.
type Access struct {
	// Name is the member as written — a property keeps its `$` after `::`.
	Name string
	// Kinds are the declaration kinds to look for, nearest reading first.
	Kinds []MemberKind
	// Static says the access went through `self::` / `static::`.
	Static bool
}

// MemberAccessAt returns the `$this->` / `self::` / `static::` member access
// under pos in text, and whether there is one. The position may rest
// anywhere in the access except on its receiver: on the member name, on the
// operator or inside the call's parentheses — `$this->abc(` resolves the
// same as `$this->abc`.
func MemberAccessAt(text string, pos Pos) (Access, bool) {
	lines := strings.Split(text, "\n")
	root := highlight.SyntaxTree("php", lines)
	if root == nil {
		return Access{}, false // no cgo: nothing to resolve
	}
	src := strings.Join(lines, "\n")
	for _, n := range pathTo(root, pos) {
		a, receiver, ok := accessOf(n, src)
		if !ok {
			continue
		}
		if receiver.Contains(pos.Line, pos.Col) {
			return Access{}, false // standing on `$this` / `self`, not on the member
		}
		return a, true
	}
	return Access{}, false
}

// pathTo lists the named nodes containing pos, innermost first.
func pathTo(root *highlight.SyntaxNode, pos Pos) []*highlight.SyntaxNode {
	var out []*highlight.SyntaxNode
	n := root
	for n != nil {
		out = append(out, n)
		var next *highlight.SyntaxNode
		for _, c := range n.Children {
			if c.Contains(pos.Line, pos.Col) {
				next = c
				break
			}
		}
		n = next
	}
	// Innermost first, so the nearest enclosing access wins in a nested
	// expression like `$this->outer($this->inner())`.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// accessOf classifies one node as a member access on `$this` / `self` /
// `static`, returning the access and the receiver node the caller checks the
// position against.
func accessOf(n *highlight.SyntaxNode, src string) (Access, *highlight.SyntaxNode, bool) {
	switch n.Kind {
	case "member_call_expression", "nullsafe_member_call_expression":
		return arrowAccess(n, src, MemberMethod, MemberProperty)
	case "member_access_expression", "nullsafe_member_access_expression":
		return arrowAccess(n, src, MemberProperty, MemberMethod)
	case "scoped_call_expression":
		return scopedAccess(n, src, n.Child("name"), MemberMethod)
	case "scoped_property_access_expression":
		return scopedAccess(n, src, n.Child("name"), MemberProperty)
	case "class_constant_access_expression":
		// The grammar gives this one no field names: the scope is the first
		// child, the member the trailing identifier.
		return scopedAccess(n, src, lastOfKind(n, "name"), MemberConst, MemberCase)
	}
	return Access{}, nil, false
}

// arrowAccess resolves a `$this->member` access; any other receiver passes.
func arrowAccess(n *highlight.SyntaxNode, src string, kinds ...MemberKind) (Access, *highlight.SyntaxNode, bool) {
	obj := n.Child("object")
	if obj == nil || obj.Kind != "variable_name" || obj.Text(src) != "$this" {
		return Access{}, nil, false
	}
	name := n.Child("name")
	if name == nil || name.Kind != "name" {
		return Access{}, nil, false // `$this->$dynamic`: no name to look up
	}
	return Access{Name: name.Text(src), Kinds: kinds}, obj, true
}

// scopedAccess resolves a `self::` / `static::` access; `parent::` and a
// written class name pass, because the server resolves those itself.
func scopedAccess(n *highlight.SyntaxNode, src string, name *highlight.SyntaxNode, kinds ...MemberKind) (Access, *highlight.SyntaxNode, bool) {
	scope := n.Child("scope")
	if scope == nil && len(n.Children) > 0 {
		scope = n.Children[0]
	}
	if scope == nil || scope.Kind != "relative_scope" {
		return Access{}, nil, false
	}
	switch strings.ToLower(scope.Text(src)) {
	case "self", "static":
	default:
		return Access{}, nil, false
	}
	if name == nil {
		return Access{}, nil, false
	}
	return Access{Name: name.Text(src), Kinds: kinds, Static: true}, scope, true
}

// lastOfKind returns the last child of the given kind, or nil.
func lastOfKind(n *highlight.SyntaxNode, kind string) *highlight.SyntaxNode {
	var out *highlight.SyntaxNode
	for _, c := range n.Children {
		if c.Kind == kind {
			out = c
		}
	}
	return out
}
