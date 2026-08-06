package langhttp

import (
	"strings"
	"testing"
)

func fmtHTTP(t *testing.T, src string) string {
	t.Helper()
	out, err := formatHTTP(src, "    ")
	if err != nil {
		t.Fatalf("formatHTTP: %v", err)
	}
	return out
}

// TestFormatFoldsQuery: a request line's query unfolds onto ?/& continuation
// lines, one parameter per line, values byte-identical (#1602 — no
// re-encoding, see #1601).
func TestFormatFoldsQuery(t *testing.T) {
	src := "GET https://example.com/xy?entities=%5B%22sistrix%22%5D&domains=%5B%22sistrix.com%22%5D\n"
	want := strings.Join([]string{
		"GET https://example.com/xy",
		"    ? entities = %5B%22sistrix%22%5D",
		"    & domains = %5B%22sistrix.com%22%5D",
		"",
	}, "\n")
	if got := fmtHTTP(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatNormalizesExistingFold: already folded spellings normalize to one
// parameter per line with canonical spacing, merging with inline params.
func TestFormatNormalizesExistingFold(t *testing.T) {
	src := strings.Join([]string{
		"GET https://example.com/xy?a=1",
		"  ?b=2 & c=3",
		"\t&d=4",
		"",
	}, "\n")
	want := strings.Join([]string{
		"GET https://example.com/xy",
		"    ? a = 1",
		"    & b = 2",
		"    & c = 3",
		"    & d = 4",
		"",
	}, "\n")
	if got := fmtHTTP(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatValuelessAndEmptyParams: a flag param stays bare, an empty value
// keeps its "=" — both round-trip through the parser unchanged.
func TestFormatValuelessAndEmptyParams(t *testing.T) {
	src := "GET /path?flag&empty=&k=v HTTP/1.1\n"
	want := strings.Join([]string{
		"GET /path HTTP/1.1",
		"    ? flag",
		"    & empty =",
		"    & k = v",
		"",
	}, "\n")
	if got := fmtHTTP(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatRequestLineWithoutQueryUntouched: no query, nothing to fold; the
// request line only collapses its spacing.
func TestFormatRequestLineWithoutQueryUntouched(t *testing.T) {
	src := "GET   https://example.com/plain   HTTP/1.1\n"
	if got := fmtHTTP(t, src); got != "GET https://example.com/plain HTTP/1.1\n" {
		t.Fatalf("got %q", got)
	}
}

// TestFormatHeadersAndBody: header spacing normalizes, the body separates by
// exactly one blank line and its content stays byte-identical.
func TestFormatHeadersAndBody(t *testing.T) {
	src := strings.Join([]string{
		"POST /submit?x=1",
		"Content-Type:application/json",
		"Accept:   application/json",
		"",
		"",
		"{\"a\":  1}",
		"",
	}, "\n")
	want := strings.Join([]string{
		"POST /submit",
		"    ? x = 1",
		"Content-Type: application/json",
		"Accept: application/json",
		"",
		"{\"a\":  1}",
		"",
	}, "\n")
	if got := fmtHTTP(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatKeepsSeparatorsCommentsAndMalformedBlocks: ### separators, block
// comments and anything the parser would reject pass through verbatim.
func TestFormatKeepsSeparatorsCommentsAndMalformedBlocks(t *testing.T) {
	src := strings.Join([]string{
		"# leading comment",
		"GET /a?x=1",
		"",
		"### named request",
		"// block comment",
		"not a request line at all",
		"",
	}, "\n")
	want := strings.Join([]string{
		"# leading comment",
		"GET /a",
		"    ? x = 1",
		"",
		"### named request",
		"// block comment",
		"not a request line at all",
		"",
	}, "\n")
	if got := fmtHTTP(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatCommentInFoldRegionKeepsVerbatim: a comment interleaved with
// query continuations cannot be reordered losslessly — the head of that
// request stays byte-verbatim.
func TestFormatCommentInFoldRegionKeepsVerbatim(t *testing.T) {
	src := strings.Join([]string{
		"GET /a?x=1",
		"  ? y=2",
		"  # why y",
		"  & z=3",
		"Header:  v",
		"",
	}, "\n")
	want := strings.Join([]string{
		"GET /a?x=1",
		"  ? y=2",
		"  # why y",
		"  & z=3",
		"Header: v",
		"",
	}, "\n")
	if got := fmtHTTP(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestFormatBareQuestionMarkAndEmptyFragments: shapes whose fold would not
// round-trip byte-identically — a bare trailing "?" and inline "&&" — stay
// untouched.
func TestFormatBareQuestionMarkAndEmptyFragments(t *testing.T) {
	for _, src := range []string{
		"GET /path?\n",
		"GET /path?a=1&&b=2\n",
	} {
		if got := fmtHTTP(t, src); got != src {
			t.Fatalf("src %q must stay untouched, got %q", src, got)
		}
	}
}

// TestFormatRoundTripsThroughParser: for a representative mixed file the
// formatted output parses to exactly the same requests (the guard formatHTTP
// itself enforces — this asserts it holds rather than aborts).
func TestFormatRoundTripsThroughParser(t *testing.T) {
	src := strings.Join([]string{
		"GET https://example.com/xyt?entities=%5B%2522sistrix%2522%5D&date=2026-07-31",
		"Authorization:Bearer ${TOKEN}",
		"",
		"### create",
		"POST https://example.com/items?verbose",
		"Content-Type: application/json",
		"",
		"{\"name\": \"x\"}",
		"",
	}, "\n")
	out := fmtHTTP(t, src)
	// Formatting is idempotent: a second pass changes nothing.
	if again := fmtHTTP(t, out); again != out {
		t.Fatalf("not idempotent:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
	if !strings.Contains(out, "    ? entities = %5B%2522sistrix%2522%5D") {
		t.Fatalf("value must stay byte-identical, got:\n%s", out)
	}
}

// TestFormatTabIndent: with tab indentation the continuation lines use one
// tab.
func TestFormatTabIndent(t *testing.T) {
	out, err := formatHTTP("GET /a?x=1\n", "\t")
	if err != nil {
		t.Fatal(err)
	}
	if out != "GET /a\n\t? x = 1\n" {
		t.Fatalf("got %q", out)
	}
}
