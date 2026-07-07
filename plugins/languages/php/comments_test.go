package langphp

import (
	"testing"

	"ike/internal/lang"
)

// The blank init() registers PHP; comment toggling (0120) reads this metadata.
func TestPHPCommentMetadata(t *testing.T) {
	line, block, ok := lang.Comments("index.php")
	if !ok || line != "//" || block != [2]string{"/*", "*/"} {
		t.Fatalf("Comments(index.php) = %q %v %v", line, block, ok)
	}
}
