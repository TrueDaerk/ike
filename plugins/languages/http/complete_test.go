package langhttp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/complete"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// completeAt feeds src into a source and completes at the cursor marked by
// "|" in the source text — the marker is removed before the buffer is stored.
func completeAt(t *testing.T, src string) []ilsp.CompletionItem {
	t.Helper()
	idx := strings.Index(src, "|")
	if idx < 0 {
		t.Fatal("source must mark the cursor with |")
	}
	clean := strings.Replace(src, "|", "", 1)
	line := strings.Count(clean[:idx], "\n")
	col := len([]rune(clean[strings.LastIndex(clean[:idx], "\n")+1 : idx]))

	s := newHTTPSource()
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "/p/req.http", Text: clean})
	items, err := s.Complete(context.Background(), complete.Request{
		Path: "/p/req.http", Line: line, Col: col,
	})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func labels(items []ilsp.CompletionItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func has(items []ilsp.CompletionItem, label string) bool {
	for _, it := range items {
		if it.Label == label {
			return true
		}
	}
	return false
}

func insertFor(items []ilsp.CompletionItem, label string) string {
	for _, it := range items {
		if it.Label == label {
			return it.InsertText
		}
	}
	return ""
}

func TestCompleteHeaderNamesInsertColon(t *testing.T) {
	items := completeAt(t, "GET https://api.test/x\nCont|\n")
	if !has(items, "Content-Type") {
		t.Fatalf("Content-Type must be offered, got %v", labels(items))
	}
	if got := insertFor(items, "Content-Type"); got != "Content-Type: " {
		t.Errorf("insert text: %q, want %q", got, "Content-Type: ")
	}
	if has(items, "GET") {
		t.Error("methods must not leak into the header block")
	}
}

func TestCompleteHeaderNameAfterHyphen(t *testing.T) {
	items := completeAt(t, "GET https://api.test/x\nContent-|\n")
	if !has(items, "Content-Type") || !has(items, "Content-Length") {
		t.Errorf("hyphenated prefix must still match: %v", labels(items))
	}
	if has(items, "Accept") {
		t.Errorf("unrelated headers must be filtered out: %v", labels(items))
	}
}

// TestCompleteHeaderNameFuzzy: matching is a subsequence, not a prefix
// (#1292) — "Cen" must reach Content-Encoding, which a prefix match missed.
func TestCompleteHeaderNameFuzzy(t *testing.T) {
	cases := []struct{ src, want string }{
		{"GET https://api.test/x\nCen|\n", "Content-Encoding"},
		{"GET https://api.test/x\nctype|\n", "Content-Type"},
		{"GET https://api.test/x\naenc|\n", "Accept-Encoding"},
		{"GET https://api.test/x\nidem|\n", "Idempotency-Key"},
		{"GET https://api.test/x\nxreqid|\n", "X-Request-Id"},
	}
	for _, c := range cases {
		items := completeAt(t, c.src)
		if !has(items, c.want) {
			t.Errorf("%q: want %q offered, got %v", c.src, c.want, labels(items))
		}
	}
	// Unrelated headers still stay out.
	if items := completeAt(t, "GET https://api.test/x\nCen|\n"); has(items, "Host") {
		t.Errorf("nonsense matches must not pass: %v", labels(items))
	}
}

// TestCompleteMethodFuzzy: the request line matches loosely too.
func TestCompleteMethodFuzzy(t *testing.T) {
	items := completeAt(t, "OPS|\n")
	if !has(items, "OPTIONS") {
		t.Errorf("OPTIONS must be offered for %q, got %v", "OPS", labels(items))
	}
}

func TestCompleteHeaderValues(t *testing.T) {
	cases := []struct{ src, want string }{
		{"POST https://api.test/x\nContent-Type: jso|\n", "application/json"},
		{"POST https://api.test/x\nAccept: |\n", "application/json"},
		{"POST https://api.test/x\nAuthorization: Bea|\n", "Bearer "},
		{"POST https://api.test/x\nCache-Control: no-|\n", "no-cache"},
		{"POST https://api.test/x\nAccept-Encoding: gz|\n", "gzip"},
		{"POST https://api.test/x\nContent-Type: wwwform|\n", "application/x-www-form-urlencoded"},
		{"POST https://api.test/x\nSec-Fetch-Mode: cors|\n", "cors"},
		{"POST https://api.test/x\nX-HTTP-Method-Override: pa|\n", "PATCH"},
	}
	for _, c := range cases {
		items := completeAt(t, c.src)
		if !has(items, c.want) {
			t.Errorf("%q: want %q offered, got %v", c.src, c.want, labels(items))
		}
	}
}

func TestCompleteUnknownHeaderHasNoValues(t *testing.T) {
	items := completeAt(t, "GET https://api.test/x\nX-Whatever: |\n")
	if len(items) != 0 {
		t.Errorf("an unknown header offers no values, got %v", labels(items))
	}
}

func TestCompleteMethodOnRequestLine(t *testing.T) {
	items := completeAt(t, "PO|\n")
	if !has(items, "POST") {
		t.Fatalf("POST must be offered, got %v", labels(items))
	}
	if has(items, "GET") {
		t.Errorf("the typed prefix must filter: %v", labels(items))
	}
	// After a "###" separator too.
	items = completeAt(t, "### create\nDEL|\n")
	if !has(items, "DELETE") {
		t.Errorf("methods must complete in a later block: %v", labels(items))
	}
}

func TestCompleteHTTPVersion(t *testing.T) {
	items := completeAt(t, "GET https://api.test/x HTTP/1|\n")
	if !has(items, "HTTP/1.1") || !has(items, "HTTP/1.0") {
		t.Fatalf("versions must be offered, got %v", labels(items))
	}
	if has(items, "HTTP/2") {
		t.Errorf("the typed prefix must filter: %v", labels(items))
	}
}

func TestCompleteNothingInTargetPosition(t *testing.T) {
	if items := completeAt(t, "GET https://api|\n"); len(items) != 0 {
		t.Errorf("a URL completes nothing locally, got %v", labels(items))
	}
}

func TestCompleteNothingInBodyOrComments(t *testing.T) {
	body := "POST https://api.test/x\nContent-Type: application/json\n\n{\"na|\"}\n"
	if items := completeAt(t, body); len(items) != 0 {
		t.Errorf("a body completes nothing, got %v", labels(items))
	}
	comment := "GET https://api.test/x\n# Cont|\n"
	if items := completeAt(t, comment); len(items) != 0 {
		t.Errorf("a comment completes nothing, got %v", labels(items))
	}
	slash := "GET https://api.test/x\n// Acce|\n"
	if items := completeAt(t, slash); len(items) != 0 {
		t.Errorf("a // comment completes nothing, got %v", labels(items))
	}
}

// TestCompleteBodyOfEarlierBlockDoesNotLeak: a header-looking line inside a
// body stays a body line, and the next block starts fresh.
func TestCompleteAcrossBlocks(t *testing.T) {
	src := strings.Join([]string{
		"POST https://api.test/a",
		"Content-Type: application/json",
		"",
		"{\"Accep|\": 1}",
		"### second",
		"GET https://api.test/b",
		"Acce",
	}, "\n")
	if items := completeAt(t, src); len(items) != 0 {
		t.Errorf("body of the first block completes nothing, got %v", labels(items))
	}

	src = strings.Join([]string{
		"POST https://api.test/a",
		"Content-Type: application/json",
		"",
		"{\"a\": 1}",
		"### second",
		"GET https://api.test/b",
		"Acce|",
	}, "\n")
	items := completeAt(t, src)
	if !has(items, "Accept") {
		t.Errorf("the second block's header block completes, got %v", labels(items))
	}
}

// TestCompleteAfterFoldedQueryLines: the query continuation form (#1269) is
// part of the request line, so headers still complete behind it, and the
// continuation lines themselves complete nothing.
func TestCompleteAfterFoldedQueryLines(t *testing.T) {
	src := "GET https://api.test/x\n  ? a = 1\n  & b = 2\nAcce|\n"
	if items := completeAt(t, src); !has(items, "Accept") {
		t.Errorf("headers must complete after folded query lines, got %v", labels(items))
	}
	if items := completeAt(t, "GET https://api.test/x\n  ? a|\n"); len(items) != 0 {
		t.Errorf("a folded query line completes nothing, got %v", labels(items))
	}
}

func TestCompleteOnlyInHTTPFiles(t *testing.T) {
	s := newHTTPSource()
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "/p/main.go", Text: "Cont\n"})
	items, err := s.Complete(context.Background(), complete.Request{Path: "/p/main.go", Line: 0, Col: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("a Go file must not get .http completions: %v", labels(items))
	}
}

// TestCompleteLargeBufferDropsText: a large-file change clears the cached
// text instead of holding a huge string, and completion stays silent.
func TestCompleteLargeBufferDropsText(t *testing.T) {
	s := newHTTPSource()
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "/p/req.http", Text: "GET /x\nAcce\n"})
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: "/p/req.http", Large: true})
	items, err := s.Complete(context.Background(), complete.Request{Path: "/p/req.http", Line: 1, Col: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("a dropped buffer completes nothing, got %v", labels(items))
	}
}

// TestSourceClaimsHTTPBuffersExclusively guards #1302: the popup in a .http
// file must never carry buffer words or project identifiers.
func TestSourceClaimsHTTPBuffersExclusively(t *testing.T) {
	s := &httpSource{}
	for _, path := range []string{"/x/api.http", "/x/api.rest"} {
		if !s.Exclusive(path) {
			t.Errorf("%s must be claimed exclusively", path)
		}
	}
	for _, path := range []string{"/x/main.go", "/x/notes.md", ""} {
		if s.Exclusive(path) {
			t.Errorf("%s must not be claimed", path)
		}
	}
}

// completeAtIn is completeAt with the buffer stored under a real directory,
// so path completion (#1707) has a filesystem to look at.
func completeAtIn(t *testing.T, dir, src string) []ilsp.CompletionItem {
	t.Helper()
	idx := strings.Index(src, "|")
	if idx < 0 {
		t.Fatal("source must mark the cursor with |")
	}
	clean := strings.Replace(src, "|", "", 1)
	line := strings.Count(clean[:idx], "\n")
	col := len([]rune(clean[strings.LastIndex(clean[:idx], "\n")+1 : idx]))

	path := filepath.Join(dir, "req.http")
	s := newHTTPSource()
	s.Observe(host.EditorEvent{Kind: host.EditorChange, Path: path, Text: clean})
	items, err := s.Complete(context.Background(), complete.Request{
		Path: path, Line: line, Col: col,
	})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// bodyFileDir builds a fixture directory next to the request file.
func bodyFileDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"leads.csv", "letters.txt", "other.bin"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestCompleteBodyFilePath guards #1707: after `< ` in a request body the
// popup offers files relative to the .http file — for the whole-body
// directive (#1305) and for a multipart part include alike.
func TestCompleteBodyFilePath(t *testing.T) {
	dir := bodyFileDir(t)

	// Whole-body directive.
	items := completeAtIn(t, dir, "POST https://api.test/x\n\n< le|\n")
	if !has(items, "leads.csv") || !has(items, "letters.txt") {
		t.Fatalf("want leads.csv and letters.txt, got %v", labels(items))
	}
	if has(items, "other.bin") {
		t.Fatalf("prefix must filter, got %v", labels(items))
	}

	// The `<@` substitute spelling completes too.
	items = completeAtIn(t, dir, "POST https://api.test/x\n\n<@ le|\n")
	if !has(items, "leads.csv") {
		t.Fatalf("<@ must complete paths, got %v", labels(items))
	}

	// Multipart part include.
	items = completeAtIn(t, dir,
		"POST https://api.test/import/\n"+
			"Content-Type: multipart/form-data; boundary=bound\n"+
			"\n"+
			"--bound\n"+
			"Content-Disposition: form-data; name=\"import\"\n"+
			"\n"+
			"< |\n")
	if !has(items, "leads.csv") || !has(items, "other.bin") {
		t.Fatalf("empty path must list the directory, got %v", labels(items))
	}
}

// TestCompleteBodyStaysQuiet: an ordinary body line still offers nothing —
// only the file directive completes (#1302 exclusivity stays intact).
func TestCompleteBodyStaysQuiet(t *testing.T) {
	dir := bodyFileDir(t)
	if items := completeAtIn(t, dir, "POST https://api.test/x\n\nle|\n"); len(items) != 0 {
		t.Fatalf("plain body must complete nothing, got %v", labels(items))
	}
	// "<" without the following whitespace is not yet a directive.
	if items := completeAtIn(t, dir, "POST https://api.test/x\n\n<le|\n"); len(items) != 0 {
		t.Fatalf("'<' without space must complete nothing, got %v", labels(items))
	}
}
