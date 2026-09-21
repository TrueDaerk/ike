package lsp

import "testing"

// TestUndefinedMember covers the classifier the trait suppression pass
// (#2669) matches on: the code table, the message shape as a secondary
// check, and the source guard.
func TestUndefinedMember(t *testing.T) {
	cases := []struct {
		name     string
		d        Diagnostic
		wantOK   bool
		wantKind UndefinedMemberKind
		wantName string
	}{
		{
			name:     "method",
			d:        Diagnostic{Source: "intelephense", Code: "P1013", Message: "Undefined method 'abc'."},
			wantOK:   true,
			wantKind: UndefinedMethod,
			wantName: "abc",
		},
		{
			name:     "property keeps its dollar",
			d:        Diagnostic{Source: "intelephense", Code: "P1014", Message: "Undefined property '$x'."},
			wantOK:   true,
			wantKind: UndefinedProperty,
			wantName: "$x",
		},
		{
			name:     "class constant",
			d:        Diagnostic{Source: "intelephense", Code: "P1012", Message: "Undefined class constant 'K'."},
			wantOK:   true,
			wantKind: UndefinedClassConst,
			wantName: "K",
		},
		{
			name:     "unprefixed code",
			d:        Diagnostic{Source: "intelephense", Code: "1013", Message: "Undefined method 'abc'."},
			wantOK:   true,
			wantKind: UndefinedMethod,
			wantName: "abc",
		},
		{
			name:   "another code",
			d:      Diagnostic{Source: "intelephense", Code: "P1006", Message: "Expected type 'int'. Found 'null'."},
			wantOK: false,
		},
		{
			name:   "another server",
			d:      Diagnostic{Source: "phpstan", Code: "P1013", Message: "Undefined method 'abc'."},
			wantOK: false,
		},
		{
			name:   "code without the matching noun",
			d:      Diagnostic{Source: "intelephense", Code: "P1013", Message: "Undefined variable 'abc'."},
			wantOK: false,
		},
		{
			name:   "message names nothing",
			d:      Diagnostic{Source: "intelephense", Code: "P1013", Message: "Undefined method."},
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, name, ok := UndefinedMember(tc.d)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if kind != tc.wantKind {
				t.Errorf("kind = %v, want %v", kind, tc.wantKind)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}
