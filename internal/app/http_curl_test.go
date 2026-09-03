package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/httpfile"
)

// http_curl_test.go covers the app half of the curl conversions (#1994): the
// import prompt (clipboard prefill, parse, block appended to the focused
// .http file, ignored-flag notice) and http.copyAsCurl.

// curlApp opens an .http file holding src and returns the model focused on it.
func curlApp(t *testing.T, src string) (Model, string) {
	t.Helper()
	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "req.http")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return out.(Model), path
}

// stubClipboard replaces both clipboard seams for one test and returns a
// pointer to what was written.
func stubClipboard(t *testing.T, read string) *string {
	t.Helper()
	oldRead, oldWrite := clipboardRead, clipboardWrite
	written := new(string)
	clipboardRead = func() string { return read }
	clipboardWrite = func(s string) { *written = s }
	t.Cleanup(func() { clipboardRead, clipboardWrite = oldRead, oldWrite })
	return written
}

// TestImportCurlPromptAppendsBlock: the devtools spelling imports into a
// correct request block appended to the open file.
func TestImportCurlPromptAppendsBlock(t *testing.T) {
	m, _ := curlApp(t, "### existing\nGET https://example.com/a\n")
	stubClipboard(t, "")

	out, _ := m.Update(ImportCurlMsg{})
	m = out.(Model)
	if !m.curlImportPromptOpen() {
		t.Fatal("http.importCurl must open the prompt")
	}
	m = typeInto(m, `curl 'https://api.example.com/v1/things' -X POST -H 'accept: application/json' --data-raw '{"n":1}'`)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.curlImportPromptOpen() {
		t.Error("enter must close the prompt")
	}

	f := httpfile.Parse(m.activeEditor().Text())
	if len(f.Errors) != 0 {
		t.Fatalf("imported file has parse errors: %v", f.Errors)
	}
	if len(f.Requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(f.Requests))
	}
	r := f.Requests[1]
	if r.Name != "POST /v1/things" {
		t.Errorf("name = %q", r.Name)
	}
	if r.Method != "POST" || r.Target != "https://api.example.com/v1/things" {
		t.Errorf("request line = %s %s", r.Method, r.Target)
	}
	if v, _ := r.Header("accept"); v != "application/json" {
		t.Errorf("accept header = %q", v)
	}
	if r.Body != `{"n":1}` {
		t.Errorf("body = %q", r.Body)
	}
	// The caret lands on the imported request line so it can be run at once.
	if line, _ := m.activeEditor().CursorPos(); line+1 != r.Line {
		t.Errorf("cursor on line %d, want %d", line+1, r.Line)
	}
	if n := lastNotification(t, m); !strings.Contains(n, "added ### POST /v1/things") {
		t.Errorf("notification = %q", n)
	}
}

// TestImportCurlPrefillsFromClipboard: a "Copy as cURL" still on the
// clipboard is offered as the prompt's content, wrapped lines folded onto the
// single input line.
func TestImportCurlPrefillsFromClipboard(t *testing.T) {
	m, _ := curlApp(t, "")
	stubClipboard(t, "curl 'https://example.com/x' \\\n  -H 'a: b'\n")

	out, _ := m.Update(ImportCurlMsg{})
	m = out.(Model)
	if want := "curl 'https://example.com/x' -H 'a: b'"; m.curlImportInput.Text != want {
		t.Fatalf("prefill = %q, want %q", m.curlImportInput.Text, want)
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	f := httpfile.Parse(m.activeEditor().Text())
	if len(f.Requests) != 1 || f.Requests[0].Target != "https://example.com/x" {
		t.Fatalf("imported %d requests: %+v", len(f.Requests), f.Requests)
	}
}

// TestImportCurlIgnoredFlagsNotice: unsupported flags are named in a notice.
func TestImportCurlIgnoredFlagsNotice(t *testing.T) {
	m, _ := curlApp(t, "")
	stubClipboard(t, "")
	out, _ := m.Update(ImportCurlMsg{})
	m = out.(Model)
	m = typeInto(m, "curl -sLk https://example.com/x")
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	n := lastNotification(t, m)
	for _, flag := range []string{"-L", "-k", "-s"} {
		if !strings.Contains(n, flag) {
			t.Errorf("notification %q must name %s", n, flag)
		}
	}
}

// TestImportCurlRejectsNonCurl keeps a bad command out of the buffer.
func TestImportCurlRejectsNonCurl(t *testing.T) {
	m, _ := curlApp(t, "### a\nGET https://example.com/a\n")
	stubClipboard(t, "")
	before := m.activeEditor().Text()

	out, _ := m.Update(ImportCurlMsg{})
	m = out.(Model)
	m = typeInto(m, "wget https://example.com/x")
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if got := m.activeEditor().Text(); got != before {
		t.Errorf("buffer changed on a failed import:\n%s", got)
	}
	if n := lastNotification(t, m); !strings.Contains(n, "not a curl command") {
		t.Errorf("notification = %q", n)
	}
}

// TestImportCurlGatesOnFileType: the prompt never opens over a non-.http file.
func TestImportCurlGatesOnFileType(t *testing.T) {
	m := httpApp(t)
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	out, _ = m.Update(ImportCurlMsg{})
	m = out.(Model)
	if m.curlImportPromptOpen() {
		t.Fatal("http.importCurl must not open over a non-.http file")
	}
}

// TestImportCurlGatesOnEditorMode: the block is spliced through the editor's
// paste path, which would replace a visual selection instead of appending.
func TestImportCurlGatesOnEditorMode(t *testing.T) {
	m, _ := curlApp(t, "### a\nGET https://example.com/a\n")
	stubClipboard(t, "")
	m = drainKey(m, tea.KeyPressMsg{Code: 'v', Text: "v"}) // visual mode
	if m.activeEditor().ModeName() != editor.Visual {
		t.Fatalf("editor mode = %v, want visual", m.activeEditor().ModeName())
	}
	out, _ := m.Update(ImportCurlMsg{})
	m = out.(Model)
	if m.curlImportPromptOpen() {
		t.Fatal("http.importCurl must not open in visual mode")
	}
	if n := lastNotification(t, m); !strings.Contains(n, "insert/visual mode") {
		t.Errorf("notification = %q", n)
	}
}

// TestCopyRequestAsCurlSubstitutesVariables covers the acceptance criterion
// that an exported command carries the substituted values.
func TestCopyRequestAsCurlSubstitutesVariables(t *testing.T) {
	src := "@host = api.example.com\n\n### thing\nPOST https://{{host}}/things\n" +
		"Content-Type: application/json\nAuthorization: Bearer {{$env IKE_TEST_TOKEN}}\n\n{\"a\":1}\n"
	m, _ := curlApp(t, src)
	t.Setenv("IKE_TEST_TOKEN", "s3cr3t")
	written := stubClipboard(t, "")

	// Land the caret inside the request block.
	m.activeEditor().SetCursor(3, 0)
	out, _ := m.Update(HTTPCopyAsCurlMsg{})
	m = out.(Model)

	want := `curl https://api.example.com/things -X POST -H 'Content-Type: application/json' ` +
		`-H 'Authorization: Bearer s3cr3t' --data-raw '{"a":1}'`
	if *written != want {
		t.Errorf("clipboard =\n%s\nwant\n%s", *written, want)
	}
	if n := lastNotification(t, m); !strings.Contains(n, "as curl") {
		t.Errorf("notification = %q", n)
	}
}

// TestCopyRequestAsCurlReportsUnresolved: a missing variable fails loudly and
// leaves the clipboard alone.
func TestCopyRequestAsCurlReportsUnresolved(t *testing.T) {
	m, _ := curlApp(t, "### thing\nGET https://{{nowhere}}/things\n")
	written := stubClipboard(t, "")
	out, _ := m.Update(HTTPCopyAsCurlMsg{})
	m = out.(Model)
	if *written != "" {
		t.Errorf("clipboard written with %q", *written)
	}
	if n := lastNotification(t, m); !strings.Contains(n, "unresolved placeholders") {
		t.Errorf("notification = %q", n)
	}
}

// TestCopyRequestAsCurlExternalBody rebases the external body onto the .http
// file's directory, so the command runs from anywhere.
func TestCopyRequestAsCurlExternalBody(t *testing.T) {
	m, path := curlApp(t, "### up\nPOST https://example.com/up\n\n< ./payload.json\n")
	written := stubClipboard(t, "")
	out, _ := m.Update(HTTPCopyAsCurlMsg{})
	m = out.(Model)
	want := "curl https://example.com/up -X POST --data-binary " +
		"@" + filepath.Join(filepath.Dir(path), "payload.json")
	if *written != want {
		t.Errorf("clipboard = %q, want %q", *written, want)
	}
}

// TestCopyRequestAsCurlRoundTripsMultipartAndAuth is the round-trip
// acceptance criterion at the app level: an imported multipart + basic auth
// command comes back out of "Copy as curl" unchanged.
func TestCopyRequestAsCurlRoundTripsMultipartAndAuth(t *testing.T) {
	m, _ := curlApp(t, "")
	written := stubClipboard(t, "")
	cmd := `curl https://example.com/upload -F 'field=value' -F 'photo=@/tmp/a.png;type=image/png' -u alice:secret`

	out, _ := m.Update(ImportCurlMsg{})
	m = out.(Model)
	m = typeInto(m, cmd)
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)

	out, _ = m.Update(HTTPCopyAsCurlMsg{})
	m = out.(Model)
	// The exported command is curl's own spelling of the block — flag order
	// follows the request, not the original line — so the comparison is over
	// what it means: importing it again yields the very same request.
	for _, part := range []string{"-u alice:secret", "-F field=value", "-F 'photo=@/tmp/a.png;type=image/png'"} {
		if !strings.Contains(*written, part) {
			t.Errorf("exported command %q is missing %q", *written, part)
		}
	}
	first, err := httpfile.ParseCurl(cmd)
	if err != nil {
		t.Fatal(err)
	}
	again, err := httpfile.ParseCurl(*written)
	if err != nil {
		t.Fatalf("re-importing %q: %v", *written, err)
	}
	if !reflect.DeepEqual(first.Request, again.Request) {
		t.Errorf("round trip changed the request:\nfirst  %+v\nsecond %+v", first.Request, again.Request)
	}
}

// TestCopyRequestAsHttpieSubstitutesVariables is the httpie half of
// TestCopyRequestAsCurlSubstitutesVariables (#2384): the same block, the same
// variable chain, the other format.
func TestCopyRequestAsHttpieSubstitutesVariables(t *testing.T) {
	src := "@host = api.example.com\n\n### thing\nPOST https://{{host}}/things\n" +
		"Content-Type: application/json\nAuthorization: Bearer {{$env IKE_TEST_TOKEN}}\n\n{\"a\":1}\n"
	m, _ := curlApp(t, src)
	t.Setenv("IKE_TEST_TOKEN", "s3cr3t")
	written := stubClipboard(t, "")

	m.activeEditor().SetCursor(3, 0)
	out, _ := m.Update(HTTPCopyAsHttpieMsg{})
	m = out.(Model)

	want := `http POST https://api.example.com/things Content-Type:application/json ` +
		`'Authorization:Bearer s3cr3t' a:=1`
	if *written != want {
		t.Errorf("clipboard =\n%s\nwant\n%s", *written, want)
	}
	if n := lastNotification(t, m); !strings.Contains(n, "as httpie") {
		t.Errorf("notification = %q", n)
	}
}

// TestCopyRequestAsHttpieReportsUnresolved: the shared export path names the
// format it failed in, and leaves the clipboard alone.
func TestCopyRequestAsHttpieReportsUnresolved(t *testing.T) {
	m, _ := curlApp(t, "### thing\nGET https://{{nowhere}}/things\n")
	written := stubClipboard(t, "")
	out, _ := m.Update(HTTPCopyAsHttpieMsg{})
	m = out.(Model)
	if *written != "" {
		t.Errorf("clipboard written with %q", *written)
	}
	n := lastNotification(t, m)
	if !strings.Contains(n, "unresolved placeholders") || !strings.Contains(n, "httpie:") {
		t.Errorf("notification = %q", n)
	}
}

// TestCopyRequestAsHttpieExternalBody rebases the external body the same way
// the curl export does — httpie reads it off stdin.
func TestCopyRequestAsHttpieExternalBody(t *testing.T) {
	m, path := curlApp(t, "### up\nPOST https://example.com/up\n\n< ./payload.json\n")
	written := stubClipboard(t, "")
	out, _ := m.Update(HTTPCopyAsHttpieMsg{})
	m = out.(Model)
	want := "http POST https://example.com/up < " +
		filepath.Join(filepath.Dir(path), "payload.json")
	if *written != want {
		t.Errorf("clipboard = %q, want %q", *written, want)
	}
}
