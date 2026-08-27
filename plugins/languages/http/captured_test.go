package langhttp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/httpfile"
	"ike/internal/httphistory"
	ilsp "ike/internal/lsp"
)

// captured_test.go covers the response rung of "{{" completion (#2158): the
// values `# @capture` directives took out of earlier responses, and the
// origin each offered name carries.

// detailFor returns the Detail of the item labelled label ("" when absent).
func detailFor(items []ilsp.CompletionItem, label string) string {
	for _, it := range items {
		if it.Label == label {
			return it.Detail
		}
	}
	return ""
}

// storeCapture writes one stored response for source/key, the way a dispatch
// would (internal/app appends to the same store).
func storeCapture(t *testing.T, source, key string, captured map[string]string) {
	t.Helper()
	httphistory.New(httpHistoryDir()).Append(source, key, httphistory.Entry{
		Time: time.Now(), Status: "200 OK", StatusCode: 200, Captured: captured,
	})
}

// chainSrc is a two-request chain: the first captures a task id, the second
// polls it. The cursor sits in the poll request's placeholder.
const chainSrc = "### start\n# @capture task = .task\nPOST https://x.test/reindex\n\n" +
	"### poll\nGET https://x.test/tasks/{{ta|\n"

// TestCompletePlaceholderCapturedResponseVar: once the capturing request has
// run, its value is in the history and the name completes as a response
// variable.
func TestCompletePlaceholderCapturedResponseVar(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	source := filepath.Join(dir, "req.http")

	items := completeAtIn(t, dir, chainSrc)
	if got := detailFor(items, "task"); got != "capture" {
		t.Fatalf("before the run the declared name is a promise, detail = %q", got)
	}

	f := httpfile.Parse("### start\n# @capture task = .task\nPOST https://x.test/reindex\n")
	storeCapture(t, source, f.Requests[0].Key(), map[string]string{"task": "node-1:42"})

	items = completeAtIn(t, dir, chainSrc)
	if !has(items, "task") {
		t.Fatalf("want the captured task offered, got %v", labels(items))
	}
	if got := detailFor(items, "task"); got != httpfile.OriginResponse {
		t.Errorf("detail = %q, want %q", got, httpfile.OriginResponse)
	}
	if got := insertFor(items, "task"); got != "task}}" {
		t.Errorf("insert text = %q, want %q", got, "task}}")
	}
}

// TestCompletePlaceholderCapturedNeedsADirective: a stored value of another
// file's request never leaks in — only the requests of this file that declare
// a directive are looked up.
func TestCompletePlaceholderCapturedNeedsADirective(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	storeCapture(t, filepath.Join(dir, "other.http"), "1", map[string]string{"token": "leaked"})

	items := completeAtIn(t, dir, "GET https://x.test/{{to|\n")
	if has(items, "token") {
		t.Errorf("another file's captured value must not complete: %v", labels(items))
	}
}

// TestCompletePlaceholderOriginsPerSource: the three sources are offered
// together and each item says where it came from — including which
// environment answered.
func TestCompletePlaceholderOriginsPerSource(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	dir := t.TempDir()
	env := `{"dev": {"envvar": "https://dev.example.com"}}`
	if err := os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "@filevar = 1\n### start\n# @capture respvar = .id\nPOST https://x.test/a\n\n" +
		"### poll\nGET https://x.test/{{|\n"
	f := httpfile.Parse("### start\n# @capture respvar = .id\nPOST https://x.test/a\n")
	storeCapture(t, filepath.Join(dir, "req.http"), f.Requests[0].Key(), map[string]string{"respvar": "7"})

	items := completeAtIn(t, dir, src)
	want := map[string]string{
		"filevar": httpfile.OriginFile,
		"envvar":  httpfile.OriginEnv + " dev",
		"respvar": httpfile.OriginResponse,
	}
	for name, detail := range want {
		if got := detailFor(items, name); got != detail {
			t.Errorf("%s detail = %q, want %q (offered: %v)", name, got, detail, labels(items))
		}
	}
}

// TestCompletePlaceholderInsideADefinition: a definition's value may build on
// another variable, so its unclosed "{{" completes like any other (#2158) —
// while the name being defined still completes nothing.
func TestCompletePlaceholderInsideADefinition(t *testing.T) {
	items := completeAt(t, "@host = https://x.test\n@api = {{ho|\nGET {{api}}/a\n")
	if !has(items, "host") {
		t.Fatalf("want host offered inside the definition, got %v", labels(items))
	}
	if items := completeAt(t, "@host = https://x.test\n@ho|\nGET https://x.test/\n"); len(items) != 0 {
		t.Errorf("the name being defined completes nothing: %v", labels(items))
	}
}

// TestCompletePlaceholderKeepsAutoClosedBraces: auto-closing pairs (#517)
// answer a typed "{{" with "{{|}}", so the item must insert the bare name —
// adding its own "}}" would leave `{{host}}}}` behind (#2158).
func TestCompletePlaceholderKeepsAutoClosedBraces(t *testing.T) {
	items := completeAt(t, "@host = https://x.test\nGET {{|}}\n")
	if got := insertFor(items, "host"); got != "host" {
		t.Errorf("insert text = %q, want %q — the braces are already there", got, "host")
	}
	items = completeAt(t, "@host = https://x.test\nGET {{|\n")
	if got := insertFor(items, "host"); got != "host}}" {
		t.Errorf("insert text = %q, want %q — nothing closes the placeholder yet", got, "host}}")
	}
}

// TestPlaceholderTriggerChar: the source claims "{" so the popup opens on the
// braces themselves, not only once a letter follows them (#2158).
func TestPlaceholderTriggerChar(t *testing.T) {
	s := newHTTPSource()
	if !s.TriggerChar("{") {
		t.Error(`"{" must trigger the http source`)
	}
	if s.TriggerChar(".") {
		t.Error(`"." belongs to postfix completion, not to the http source`)
	}
}
