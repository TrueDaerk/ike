package langgo

import (
	"testing"

	"ike/internal/lang"
)

// The blank init() registers Go; smart indentation (0260) reads this metadata.
func TestGoIndentMetadata(t *testing.T) {
	suf, ok := lang.IndentAfter("main.go")
	if !ok {
		t.Fatal("go must declare indent openers")
	}
	want := map[string]bool{"{": true, "(": true, "[": true}
	if len(suf) != len(want) {
		t.Fatalf("IndentAfter(main.go) = %v", suf)
	}
	for _, s := range suf {
		if !want[s] {
			t.Fatalf("unexpected opener %q in %v", s, suf)
		}
	}
}
