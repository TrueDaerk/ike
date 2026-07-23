package transport

import (
	"strings"
	"testing"
)

// errtail_test.go covers #990: extracting the decisive error line from a
// crashing server's stderr tail.

func TestErrorLineNodeDump(t *testing.T) {
	// Node prints the offending source "line" — here a huge minified chunk —
	// then the real error plus stack.
	stderr := "/usr/lib/node_modules/intelephense/lib/intelephense.js:2\n" +
		strings.Repeat("x", 500_000) + "\n" +
		"\n" +
		"SyntaxError: Unexpected token '?'\n" +
		"    at wrapSafe (node:internal/modules/cjs/loader:1281:20)\n" +
		"    at Module._compile (node:internal/modules/cjs/loader:1321:27)\n"
	got := ErrorLine(stderr)
	if got != "SyntaxError: Unexpected token '?'" {
		t.Fatalf("ErrorLine = %q", got)
	}
}

func TestErrorLineGoPanic(t *testing.T) {
	stderr := "some chatter\npanic: runtime error: index out of range [3] with length 2\n\ngoroutine 1 [running]:\nmain.main()\n\t/tmp/x.go:5 +0x1d\n"
	got := ErrorLine(stderr)
	if !strings.Contains(got, "panic: runtime error") {
		t.Fatalf("ErrorLine = %q", got)
	}
}

func TestErrorLineSkipsStackFrames(t *testing.T) {
	// A frame mentioning "error" in a symbol/path must not win over the
	// message above it.
	stderr := "TypeError: x is not a function\n    at Object.error (/srv/error-utils.js:10:3)\n"
	if got := ErrorLine(stderr); got != "TypeError: x is not a function" {
		t.Fatalf("ErrorLine = %q", got)
	}
}

func TestErrorLineNothingQualifies(t *testing.T) {
	if got := ErrorLine("just some logging\nand more logging\n"); got != "" {
		t.Fatalf("ErrorLine = %q, want empty", got)
	}
	if got := ErrorLine(""); got != "" {
		t.Fatalf("ErrorLine(empty) = %q, want empty", got)
	}
}

func TestErrorLineTruncatesLongMessage(t *testing.T) {
	long := "Error: " + strings.Repeat("m", 250)
	got := ErrorLine(long + "\n")
	if len([]rune(got)) != errorLineCap+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("ErrorLine length = %d, want %d incl. ellipsis", len([]rune(got)), errorLineCap+1)
	}
}
