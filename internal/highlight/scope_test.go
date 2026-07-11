package highlight

import "testing"

// Scopes in pre-order, as parseScoped emits them: an outer type at 0-20
// holding a method at 2-10 with a func literal at 4-8, then a sibling at 12-18.
func testScopes() []Scope {
	return []Scope{
		{HeaderLine: 0, EndLine: 20},
		{HeaderLine: 2, EndLine: 10},
		{HeaderLine: 4, EndLine: 8},
		{HeaderLine: 12, EndLine: 18},
	}
}

func TestEnclosingScopesNesting(t *testing.T) {
	got := EnclosingScopes(testScopes(), 6)
	want := []Scope{{0, 20}, {2, 10}, {4, 8}}
	if len(got) != len(want) {
		t.Fatalf("EnclosingScopes(6) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scope[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestEnclosingScopesHeaderLineExcluded(t *testing.T) {
	// The header line itself is not "inside" the scope: a viewport whose first
	// line is the header already shows it, so it must not pin.
	got := EnclosingScopes(testScopes(), 2)
	if len(got) != 1 || got[0].HeaderLine != 0 {
		t.Errorf("EnclosingScopes(2) = %v, want only the outer scope", got)
	}
}

func TestEnclosingScopesSiblingsDontOverlap(t *testing.T) {
	got := EnclosingScopes(testScopes(), 14)
	if len(got) != 2 || got[0].HeaderLine != 0 || got[1].HeaderLine != 12 {
		t.Errorf("EnclosingScopes(14) = %v, want outer + second sibling", got)
	}
}

func TestEnclosingScopesCollapsesSharedHeader(t *testing.T) {
	// A Go type_declaration and its type_spec start on the same line; only one
	// sticky row must result.
	scopes := []Scope{{HeaderLine: 0, EndLine: 9}, {HeaderLine: 0, EndLine: 9}}
	if got := EnclosingScopes(scopes, 5); len(got) != 1 {
		t.Errorf("shared header should collapse to one scope, got %v", got)
	}
}

func TestEnclosingScopesOutside(t *testing.T) {
	if got := EnclosingScopes(testScopes(), 25); got != nil {
		t.Errorf("EnclosingScopes(25) = %v, want nil", got)
	}
}
