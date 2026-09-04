package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/openapi"
)

// openapi_import_url_test.go covers the URL half of the import prompt
// (#2009): the first enter validates, the second imports, and a failed check
// paints the popup red instead of confirming anything.

// specURLServer serves miniSpec at path and 404s everywhere else.
func specURLServer(t *testing.T, path string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(miniSpec))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// promptBody renders the prompt's current body, which is what the user reads.
func promptBody(t *testing.T, m Model) string {
	t.Helper()
	c := m.shell.Content()
	if c == nil {
		t.Fatal("the prompt installs no content")
	}
	return c.Render(100)
}

// openURLPrompt opens the import prompt with input replaced by url.
func openURLPrompt(t *testing.T, url string) Model {
	t.Helper()
	m := httpApp(t)
	out, _ := m.Update(ImportOpenAPIMsg{})
	m = out.(Model)
	m.oapiImportInput.Text, m.oapiImportInput.Cur = "", 0
	return typeInto(m, url)
}

// TestOpenAPIImportURLValidatesThenImports (#2009): a base URL is resolved by
// the first enter — showing the probed-to URL — and imported by the second,
// into the working directory.
func TestOpenAPIImportURLValidatesThenImports(t *testing.T) {
	srv := specURLServer(t, "/v3/api-docs")
	dir := t.TempDir()
	t.Chdir(dir)
	invalidateCwd()
	t.Cleanup(invalidateCwd)

	m := openURLPrompt(t, srv.URL)
	if !strings.Contains(promptBody(t, m), "enter check URL") {
		t.Errorf("a URL must announce the check, body:\n%s", promptBody(t, m))
	}

	// First enter: the prompt stays open and runs discovery off-loop.
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if !m.openAPIImportPromptOpen() {
		t.Fatal("checking a URL must keep the prompt open")
	}
	if !m.oapiChecking || cmd == nil {
		t.Fatal("enter on a URL must start the check")
	}
	check, ok := cmd().(openAPICheckDoneMsg)
	if !ok {
		t.Fatalf("want an openAPICheckDoneMsg, got %T", cmd())
	}
	if check.err != nil {
		t.Fatal(check.err)
	}
	out, _ = m.Update(check)
	m = out.(Model)
	if m.oapiCheckDisc == nil {
		t.Fatal("a resolved spec must arm the confirm")
	}
	body := promptBody(t, m)
	if !strings.Contains(body, srv.URL+"/v3/api-docs") {
		t.Errorf("the resolved URL must be shown before confirming, body:\n%s", body)
	}
	if !strings.Contains(body, "enter import") {
		t.Errorf("the hint must switch to import, body:\n%s", body)
	}
	if m.shell.Accent() != nil {
		t.Error("a successful check must leave the popup in its normal colour")
	}

	// Second enter: import the document the check fetched.
	out, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.openAPIImportPromptOpen() {
		t.Error("confirming must close the prompt")
	}
	if cmd == nil {
		t.Fatal("confirming must start the import")
	}
	done, ok := cmd().(openAPIImportDoneMsg)
	if !ok {
		t.Fatalf("want an openAPIImportDoneMsg, got %T", cmd())
	}
	if done.err != nil {
		t.Fatal(done.err)
	}
	want := filepath.Join(dir, "api-docs.http")
	if done.res.HTTPFile != want {
		t.Errorf("wrote %s, want %s", done.res.HTTPFile, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("the generated file must exist: %v", err)
	}

	out, _ = m.Update(done)
	m = out.(Model)
	if ed := m.activeEditor(); ed == nil || !strings.HasSuffix(ed.Path(), "api-docs.http") {
		t.Error("a finished URL import must open the generated file")
	}
	if n := lastNotification(t, m); !strings.Contains(n, "1 requests → api-docs.http") {
		t.Errorf("notification = %q", n)
	}
}

// TestOpenAPIImportURLFailureTurnsPromptRed (#2009): nothing at the probed
// paths blocks the confirm, reddens the popup and names the reason.
func TestOpenAPIImportURLFailureTurnsPromptRed(t *testing.T) {
	srv := specURLServer(t, "/nowhere")
	m := openURLPrompt(t, srv.URL)

	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	check := cmd().(openAPICheckDoneMsg)
	if check.err == nil {
		t.Fatal("a server without a spec must fail the check")
	}
	out, _ = m.Update(check)
	m = out.(Model)

	if !m.openAPIImportPromptOpen() {
		t.Fatal("a failed check must keep the prompt open")
	}
	if m.oapiCheckDisc != nil {
		t.Error("a failed check must not arm the confirm")
	}
	if m.shell.Accent() != m.pal().Error {
		t.Errorf("a failed check must turn the popup red, accent = %v", m.shell.Accent())
	}
	body := promptBody(t, m)
	for _, want := range []string{"no OpenAPI document at", "/v3/api-docs"} {
		if !strings.Contains(body, want) {
			t.Errorf("body must name the reason %q:\n%s", want, body)
		}
	}

	// Enter again re-runs the check rather than importing anything.
	out, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if !m.openAPIImportPromptOpen() || !m.oapiChecking {
		t.Fatal("enter after a failure must retry the check")
	}
	if _, ok := cmd().(openAPICheckDoneMsg); !ok {
		t.Error("enter after a failure must not import")
	}
}

// TestOpenAPIImportURLEditInvalidatesCheck (#2009): editing the input after a
// successful check drops what was verified — a confirm always refers to the
// URL on screen — and clears the red border of a failed one.
func TestOpenAPIImportURLEditInvalidatesCheck(t *testing.T) {
	srv := specURLServer(t, "/openapi.json")
	m := openURLPrompt(t, srv.URL)

	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	out, _ = m.Update(cmd().(openAPICheckDoneMsg))
	m = out.(Model)
	if m.oapiCheckDisc == nil {
		t.Fatal("the check must succeed first")
	}
	seq := m.oapiCheckSeq

	m = typeInto(m, "x")
	if m.oapiCheckDisc != nil {
		t.Error("an edited URL must not stay verified")
	}
	if m.oapiCheckSeq == seq {
		t.Error("an edit must invalidate the in-flight check")
	}

	// A stale answer for the previous input is dropped.
	out, _ = m.Update(openAPICheckDoneMsg{seq: seq, input: srv.URL, disc: &openapi.Discovery{URL: "stale"}})
	m = out.(Model)
	if m.oapiCheckDisc != nil {
		t.Error("a stale check result must be ignored")
	}
}

// TestOpenAPIImportURLPasteInvalidatesCheck (#2009): pasting a URL over a
// verified one drops the verification too — a paste is an edit.
func TestOpenAPIImportURLPasteInvalidatesCheck(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(ImportOpenAPIMsg{})
	m = out.(Model)
	m.oapiCheckDisc = &openapi.Discovery{URL: "https://api.example.com/openapi.json"}

	out, _ = m.Update(tea.PasteMsg{Content: "https://other.example.com"})
	m = out.(Model)
	if m.oapiCheckDisc != nil {
		t.Error("a paste must invalidate the verified URL")
	}
}

// TestOpenAPIImportURLTabDoesNotComplete (#2009): tab is filesystem
// completion; a URL has nothing to complete against and must be left alone.
func TestOpenAPIImportURLTabDoesNotComplete(t *testing.T) {
	m := openURLPrompt(t, "https://api.example.com/v3")
	out, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = out.(Model)
	if m.oapiImportInput.Text != "https://api.example.com/v3" {
		t.Errorf("tab rewrote the URL to %q", m.oapiImportInput.Text)
	}
}

// TestOpenAPIImportLocalPathUnchanged (#2009): a path still imports on the
// very first enter — the URL flow must not slow the file flow down.
func TestOpenAPIImportLocalPathUnchanged(t *testing.T) {
	spec := specFile(t, "mini.yaml")
	m := openURLPrompt(t, spec)

	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.openAPIImportPromptOpen() {
		t.Error("a path must import on the first enter")
	}
	if cmd == nil {
		t.Fatal("enter must start the import")
	}
	if _, ok := cmd().(openAPIImportDoneMsg); !ok {
		t.Errorf("want an openAPIImportDoneMsg, got %T", cmd())
	}
}
