package langpython

import (
	"testing"

	"ike/internal/lang"
)

// The blank init() registers Python; comment toggling (0120) reads this
// metadata. Python has no block comment syntax — line comments only.
func TestPythonCommentMetadata(t *testing.T) {
	line, block, ok := lang.Comments("app.py")
	if !ok || line != "#" {
		t.Fatalf("Comments(app.py) = %q %v %v", line, block, ok)
	}
	if block[0] != "" || block[1] != "" {
		t.Fatalf("python must declare no block comment, got %v", block)
	}
}
