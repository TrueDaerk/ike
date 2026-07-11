package langphp

import (
	"testing"

	"ike/internal/lang"
)

// The blank init() registers PHP; smart indentation (0260) reads this metadata.
func TestPHPIndentMetadata(t *testing.T) {
	suf, ok := lang.IndentAfter("index.php")
	if !ok {
		t.Fatal("php must declare indent openers")
	}
	want := map[string]bool{"{": true, "(": true, "[": true}
	if len(suf) != len(want) {
		t.Fatalf("IndentAfter(index.php) = %v", suf)
	}
	for _, s := range suf {
		if !want[s] {
			t.Fatalf("unexpected opener %q in %v", s, suf)
		}
	}
}
