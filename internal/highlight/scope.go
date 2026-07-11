package highlight

// scope.go is the pure-Go side of sticky scroll (#168): the scope model the
// CGo parser fills (parse_cgo.go collects nodes whose kind is listed in the
// language's ScopeNodes) and the enclosing-scope lookup the editor queries
// per render. Like the span model, it compiles with or without cgo.

// Scope is one sticky-scroll scope: a multi-line declaration whose header line
// is pinned while the viewport is inside its body. Lines are 0-based buffer
// lines; the scope covers [HeaderLine, EndLine] inclusive.
type Scope struct {
	HeaderLine int
	EndLine    int
}

// EnclosingScopes returns the scopes containing line whose header is strictly
// above it, outermost first. Scopes must be in pre-order (outer before inner),
// which is how the parser emits them; scopes sharing a header line (e.g. a Go
// type_declaration and its type_spec) are collapsed into one.
func EnclosingScopes(scopes []Scope, line int) []Scope {
	var out []Scope
	for _, s := range scopes {
		if s.HeaderLine < line && line <= s.EndLine {
			if n := len(out); n > 0 && out[n-1].HeaderLine == s.HeaderLine {
				continue
			}
			out = append(out, s)
		}
	}
	return out
}
