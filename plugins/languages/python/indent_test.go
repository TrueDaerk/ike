package langpython

import (
	"testing"

	"ike/internal/lang"
)

// The blank init() registers Python; smart indentation (0260) reads this
// metadata. A ":" suffix (def/if/for…) and open brackets deepen the next line.
func TestPythonIndentMetadata(t *testing.T) {
	suf, ok := lang.IndentAfter("app.py")
	if !ok {
		t.Fatal("python must declare indent openers")
	}
	want := map[string]bool{":": true, "(": true, "[": true, "{": true}
	if len(suf) != len(want) {
		t.Fatalf("IndentAfter(app.py) = %v", suf)
	}
	for _, s := range suf {
		if !want[s] {
			t.Fatalf("unexpected opener %q in %v", s, suf)
		}
	}
}
