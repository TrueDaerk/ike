//go:build cgo

package langpython

import (
	"testing"

	"ike/internal/highlight"
)

// TestPythonScopes guards sticky scroll (#168, #1910): the scope-collecting
// parse yields the enclosing definitions of Python source, pre-ordered and
// multi-line only, so a nested method pins its class and def headers.
func TestPythonScopes(t *testing.T) {
	lines := []string{
		"class C:",              // 0
		"    def m(self):",      // 1
		"        def inner():",  // 2
		"            return 1",  // 3
		"        return inner",  // 4
		"",                      // 5
		"def top():",            // 6
		"    return 2",          // 7
	}
	_, scopes, _ := highlight.HighlightScoped("main.py", lines)
	want := []highlight.Scope{
		{HeaderLine: 0, EndLine: 4}, // class C
		{HeaderLine: 1, EndLine: 4}, // def m
		{HeaderLine: 2, EndLine: 3}, // def inner
		{HeaderLine: 6, EndLine: 7}, // def top
	}
	if len(scopes) != len(want) {
		t.Fatalf("scopes = %v, want %v", scopes, want)
	}
	for i := range want {
		if scopes[i] != want[i] {
			t.Errorf("scope[%d] = %v, want %v", i, scopes[i], want[i])
		}
	}
	// Line 3 sits inside all three nested definitions, outermost first.
	got := highlight.EnclosingScopes(scopes, 3)
	if len(got) != 3 || got[0].HeaderLine != 0 || got[1].HeaderLine != 1 || got[2].HeaderLine != 2 {
		t.Errorf("EnclosingScopes(line 3) = %v, want class C + def m + def inner", got)
	}
}
