package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpfile"
	"ike/internal/openapi"
)

// openapi_import_test.go covers the app half of the OpenAPI import (#1939):
// the prompt, the off-loop run, and what a finished import does to the
// session.

const miniSpec = `openapi: 3.0.3
info:
  title: Mini
  version: "1"
servers:
  - url: https://api.example.com
security:
  - bearerAuth: []
paths:
  /things/{id}:
    get:
      operationId: getThing
      summary: Read a thing
      tags: [things]
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: integer
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema:
                type: object
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`

// specFile writes the mini spec into a fresh directory.
func specFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(miniSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestOpenAPIImportPromptRunsImport (#1939): http.importOpenAPI opens a path
// prompt, enter runs the import off-loop, and the finished import opens the
// generated file with a summary notification.
func TestOpenAPIImportPromptRunsImport(t *testing.T) {
	m := httpApp(t)
	spec := specFile(t, "mini.yaml")

	out, _ := m.Update(ImportOpenAPIMsg{})
	m = out.(Model)
	if !m.openAPIImportPromptOpen() {
		t.Fatal("http.importOpenAPI must open the import prompt")
	}

	// Replace the seeded "./" with the spec's absolute path.
	m.oapiImportInput, m.oapiImportPos = "", 0
	m = typeInto(m, spec)
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = out.(Model)
	if m.openAPIImportPromptOpen() {
		t.Error("enter must close the prompt")
	}
	if cmd == nil {
		t.Fatal("enter must start the import")
	}
	done, ok := cmd().(openAPIImportDoneMsg)
	if !ok {
		t.Fatalf("want an openAPIImportDoneMsg, got %T", cmd())
	}
	if done.err != nil {
		t.Fatal(done.err)
	}

	out, _ = m.Update(done)
	m = out.(Model)
	want := filepath.Join(filepath.Dir(spec), "mini.http")
	if done.res.HTTPFile != want {
		t.Errorf("wrote %s, want %s", done.res.HTTPFile, want)
	}
	if ed := m.activeEditor(); ed == nil || !strings.HasSuffix(ed.Path(), "mini.http") {
		t.Error("a finished import must open the generated file")
	}
	if n := lastNotification(t, m); !strings.Contains(n, "1 requests → mini.http") {
		t.Errorf("notification = %q", n)
	}

	// The generated file and its environments are what the client expects.
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	f := httpfile.Parse(string(data))
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("generated file: %d requests, errors %v", len(f.Requests), f.Errors)
	}
	if f.Requests[0].Target != "{{host}}/things/{{id}}" {
		t.Errorf("target = %q", f.Requests[0].Target)
	}
	envs, err := httpfile.LoadEnvironments(filepath.Dir(spec))
	if err != nil {
		t.Fatal(err)
	}
	if got := envs.Vars(openapi.EnvName)["host"]; got != "https://api.example.com" {
		t.Errorf("host = %q", got)
	}
	if _, ok := envs.Vars(openapi.EnvName)["bearerAuth"]; !ok {
		t.Error("the credential must be seeded in the private environment")
	}
}

// TestOpenAPIImportPromptCancels (#1939): esc closes the prompt without
// writing anything.
func TestOpenAPIImportPromptCancels(t *testing.T) {
	m := httpApp(t)
	spec := specFile(t, "mini.yaml")

	out, _ := m.Update(ImportOpenAPIMsg{})
	m = out.(Model)
	out, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.openAPIImportPromptOpen() {
		t.Error("esc must close the prompt")
	}
	if cmd != nil {
		t.Error("esc must not start an import")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(spec), "mini.http")); err == nil {
		t.Error("a cancelled import must write nothing")
	}
}

// TestOpenAPIImportReportsError (#1939): a document the importer rejects
// surfaces as an error notification and opens nothing.
func TestOpenAPIImportReportsError(t *testing.T) {
	m := httpApp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(path, []byte(`{"swagger":"2.0","paths":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := runOpenAPIImport(path)().(openAPIImportDoneMsg)
	if msg.err == nil {
		t.Fatal("a Swagger 2.0 document must be rejected")
	}
	out, _ := m.Update(msg)
	m = out.(Model)
	if n := lastNotification(t, m); !strings.Contains(n, "Swagger 2.0 is not supported") {
		t.Errorf("notification = %q, want the reason", n)
	}
	if ed := m.activeEditor(); ed != nil && strings.HasSuffix(ed.Path(), ".http") {
		t.Error("a failed import must open nothing")
	}
}

// TestPasteReachesOpenAPIImportPrompt (#1939): the prompt takes a paste at
// its cursor like every other single-line prompt (#1873).
func TestPasteReachesOpenAPIImportPrompt(t *testing.T) {
	m := httpApp(t)
	out, _ := m.Update(ImportOpenAPIMsg{})
	m = out.(Model)
	before := m.oapiImportInput

	out, _ = m.Update(tea.PasteMsg{Content: "openapi.yaml"})
	m = out.(Model)
	if m.oapiImportInput != before+"openapi.yaml" {
		t.Fatalf("oapiImportInput = %q, want %q", m.oapiImportInput, before+"openapi.yaml")
	}
}

// lastNotification returns the newest entry of the notification history ring.
func lastNotification(t *testing.T, m Model) string {
	t.Helper()
	if len(m.history) == 0 {
		t.Fatal("no notification was posted")
	}
	return m.history[0].text
}
