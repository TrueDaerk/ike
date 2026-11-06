package langhttp

import (
	"context"
	"strings"
	"testing"

	"ike/internal/format"
)

// rangeSrc is a three-request file whose every block has something the
// formatter would normalize (header spacing, request-line spacing).
var rangeSrc = strings.Join([]string{
	"POST   https://example.com/a",    // 0
	"Content-Type:application/json",   // 1
	"",                                // 2
	"{",                               // 3
	`  "a":   1`,                      // 4
	"}",                               // 5
	"",                                // 6
	"### second",                      // 7
	"GET   https://example.com/b?x=1", // 8
	"Accept:text/plain",               // 9
	"",                                // 10
	"### third",                       // 11
	"POST https://example.com/c",      // 12
	"X-Id:7",                          // 13
	"",                                // 14
	"",                                // 15
	`{"c":3}`,                         // 16
}, "\n")

func fmtRange(t *testing.T, src string, start, end int) string {
	t.Helper()
	out, err := formatRangeHTTP(src, start, end, "    ")
	if err != nil {
		t.Fatalf("formatRangeHTTP: %v", err)
	}
	return out
}

func lineDiff(t *testing.T, got string, want []string) {
	t.Helper()
	if got != strings.Join(want, "\n") {
		t.Fatalf("got:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

// TestFormatRangeBodyOnly: a selection inside one body touches only that
// body; the block's own header and every other block stay verbatim.
func TestFormatRangeBodyOnly(t *testing.T) {
	if got := fmtRange(t, rangeSrc, 3, 5); got != rangeSrc {
		t.Fatalf("body-only range changed lines outside the body:\n%s", got)
	}
	// the third block's body sits behind two blank lines
	if got := fmtRange(t, rangeSrc, 16, 16); got != rangeSrc {
		t.Fatalf("body-only range changed lines outside the body:\n%s", got)
	}
}

// TestFormatRangeSingleBlock: touching a header reformats exactly that block.
func TestFormatRangeSingleBlock(t *testing.T) {
	want := strings.Split(rangeSrc, "\n")
	want[0] = "POST https://example.com/a"
	want[1] = "Content-Type: application/json"
	lineDiff(t, fmtRange(t, rangeSrc, 1, 1), want)

	// a selection reaching from the header into the body is still one block
	lineDiff(t, fmtRange(t, rangeSrc, 1, 4), want)
}

// TestFormatRangeMultiBlock: a selection spanning two blocks reformats both
// in full and leaves the third and every separator verbatim.
func TestFormatRangeMultiBlock(t *testing.T) {
	want := []string{
		"POST https://example.com/a",
		"Content-Type: application/json",
		"",
		"{",
		`  "a":   1`,
		"}",
		"",
		"### second",
		"GET https://example.com/b",
		"    ? x = 1",
		"Accept: text/plain",
		"",
		"### third",
		"POST https://example.com/c",
		"X-Id:7",
		"",
		"",
		`{"c":3}`,
	}
	lineDiff(t, fmtRange(t, rangeSrc, 4, 9), want)
}

// TestFormatRangeSeparatorVerbatim: separator lines never change, and a
// selection covering only a separator is a no-op.
func TestFormatRangeSeparatorVerbatim(t *testing.T) {
	src := "GET   /a\n###   odd   separator  \nGET   /b"
	if got := fmtRange(t, src, 1, 1); got != src {
		t.Fatalf("separator-only range changed the buffer: %q", got)
	}
	if got := fmtRange(t, src, 0, 2); got != "GET /a\n###   odd   separator  \nGET /b" {
		t.Fatalf("got %q", got)
	}
}

// TestFormatRangeProvider: the registered provider is range-capable and
// treats an end-exclusive line-wise selection as ending on the line before.
func TestFormatRangeProvider(t *testing.T) {
	p, ok, wholeOnly := format.ResolveRange("http", "x.http")
	if !ok || wholeOnly || p.FormatRange == nil {
		t.Fatalf("http range provider missing (ok=%v wholeFileOnly=%v)", ok, wholeOnly)
	}
	res, err := p.FormatRange(context.Background(), format.Request{
		Path: "x.http", Language: "http",
		Lines:   strings.Split(rangeSrc, "\n"),
		Options: format.Options{TabWidth: 4, UseSpaces: true},
	}, format.Pos{Line: 3, Col: 0}, format.Pos{Line: 7, Col: 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text == nil || *res.Text != rangeSrc {
		t.Fatalf("got %v", res.Text)
	}
}
