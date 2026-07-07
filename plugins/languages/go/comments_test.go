package langgo

import (
	"testing"

	"ike/internal/lang"
)

// The blank init() registers Go; comment toggling (0120) reads this metadata.
func TestGoCommentMetadata(t *testing.T) {
	line, block, ok := lang.Comments("main.go")
	if !ok || line != "//" || block != [2]string{"/*", "*/"} {
		t.Fatalf("Comments(main.go) = %q %v %v", line, block, ok)
	}
}
