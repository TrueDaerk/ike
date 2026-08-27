package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/httpfile"
)

// http_vars_test.go covers the unknown-variable warning (#2158): which
// references are flagged, when the check runs, and how it shares a file's
// diagnostics with the capture reporter.

// warningMessages returns the messages of the diagnostics stored for path.
func warningMessages(m Model, path string) []string {
	var out []string
	for _, d := range m.probStore.Get(path) {
		out = append(out, d.Message)
	}
	return out
}

// TestHTTPVarDiagnosticsFlagsOnlyUnknownNames: a reference is flagged when no
// rung of the chain defines it — and a capture directive's own name counts as
// defined before it ever ran.
func TestHTTPVarDiagnosticsFlagsOnlyUnknownNames(t *testing.T) {
	src := "@host = https://x.test\n" +
		"### start\n" +
		"# @capture task = .task\n" +
		"POST {{host}}/reindex\n" +
		"\n" +
		"### poll\n" +
		"GET {{host}}/tasks/{{task}}\n" +
		"X-Env: {{envonly}}\n" +
		"X-Bad: {{hsot}}\n"
	f := httpfile.Parse(src)
	vars := &httpfile.Vars{File: f.VarMap(), Env: map[string]string{"envonly": "1"}}

	diags := httpVarDiagnostics(src, f, vars)
	if len(diags) != 1 {
		t.Fatalf("diagnostics: %v", diags)
	}
	d := diags[0]
	if !strings.Contains(d.Message, "hsot") {
		t.Errorf("message: %q", d.Message)
	}
	if d.Range.Start.Line != 8 || d.Range.Start.Col != 7 || d.Range.End.Col != 15 {
		t.Errorf("range: %+v, want line 8 cols 7-15", d.Range)
	}
	if d.Severity != 2 || d.Source != httpVarsSource || d.Code != "unknown-variable" {
		t.Errorf("severity %d source %q code %q", d.Severity, d.Source, d.Code)
	}
}

// TestHTTPVarDiagnosticsFlagsEveryOccurrence: the fix goes wherever the name
// is written, so each use is marked rather than only the first.
func TestHTTPVarDiagnosticsFlagsEveryOccurrence(t *testing.T) {
	src := "GET {{nope}}/a?x={{nope}}\n"
	f := httpfile.Parse(src)
	if diags := httpVarDiagnostics(src, f, &httpfile.Vars{}); len(diags) != 2 {
		t.Errorf("both occurrences must be marked: %v", diags)
	}
}

// TestHTTPVarsLintMarksTheBuffer: the editor-level path — an open file with a
// typo'd reference carries a warning, and fixing the definition clears it.
func TestHTTPVarsLintMarksTheBuffer(t *testing.T) {
	m := httpApp(t)
	path := httpVarFile(t, "@host = https://x.test\nGET {{hsot}}/a\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	m.lintHTTPVars(path)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 1 {
		t.Fatalf("editor warnings: %d, want 1 (%v)", warns, warningMessages(m, path))
	}

	m.activeEditor().RestoreText("@hsot = https://x.test\nGET {{hsot}}/a\n")
	m.lintHTTPVars(path)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 0 {
		t.Errorf("a defined name must clear the warning, got %d (%v)", warns, warningMessages(m, path))
	}
}

// TestHTTPVarsLintKnowsTheProcessEnvironment: the chain's last rung counts
// too — a dispatch resolves `{{HOME}}` from the process environment, so the
// lint must not call it unknown.
func TestHTTPVarsLintKnowsTheProcessEnvironment(t *testing.T) {
	t.Setenv("IKE_HTTP_VAR_TEST", "https://x.test")
	m := httpApp(t)
	path := httpVarFile(t, "GET {{IKE_HTTP_VAR_TEST}}/a\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	m.lintHTTPVars(path)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 0 {
		t.Errorf("an exported name must not warn, got %d (%v)", warns, warningMessages(m, path))
	}
}

// TestHTTPVarsLintRunsAfterTheBufferGoesQuiet: the change seam marks the
// buffer and the tick lints it — one pass per quiet period, not per keystroke.
func TestHTTPVarsLintRunsAfterTheBufferGoesQuiet(t *testing.T) {
	m := httpApp(t)
	path := httpVarFile(t, "GET {{nope}}/a\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	for i := 0; i < 3; i++ {
		out, _ = m.Update(editor.SyncMsg{Path: path})
		m = out.(Model)
	}
	if n := m.httpVarsDeb.Pending(); n != 1 {
		t.Fatalf("a burst of edits must collapse into one pending lint, got %d", n)
	}
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 0 {
		t.Fatalf("nothing may be published before the buffer went quiet, got %d", warns)
	}

	m.lintDueHTTPVars(time.Now().Add(time.Minute))
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 1 {
		t.Errorf("the quiet buffer must be linted, got %d warnings", warns)
	}
	if n := m.httpVarsDeb.Pending(); n != 0 {
		t.Errorf("the mark must be consumed, %d left", n)
	}
}

// TestHTTPVarsLintFollowsTheSelectedEnvironment: switching the environment
// switches the set of known names, and the warnings follow immediately.
func TestHTTPVarsLintFollowsTheSelectedEnvironment(t *testing.T) {
	m := httpApp(t)
	envs := `{"dev": {"host": "https://dev.test"}, "prod": {"other": "1"}}`
	path := httpVarFile(t, "GET {{host}}/a\n", map[string]string{
		httpfile.EnvFileName: envs,
	})
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	dir := filepath.Dir(path)

	// Nothing chosen while two environments exist: the file is not linted at
	// all — every environment name would read as unknown.
	m.lintHTTPVars(path)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 0 {
		t.Fatalf("an unmade environment choice must not warn, got %d", warns)
	}

	out, _ = m.Update(SelectHTTPEnvMsg{Dir: dir, Name: "prod"})
	m = out.(Model)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 1 {
		t.Fatalf("prod does not define host, want 1 warning, got %d (%v)", warns, warningMessages(m, path))
	}

	out, _ = m.Update(SelectHTTPEnvMsg{Dir: dir, Name: "dev"})
	m = out.(Model)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 0 {
		t.Errorf("dev defines host, want no warning, got %d (%v)", warns, warningMessages(m, path))
	}
}

// TestHTTPVarsAndCaptureDiagnosticsCoexist: the two producers of an .http
// file's diagnostics share the set — a dispatch reporting a failed capture
// must not erase the unknown-variable warnings, and the other way round.
func TestHTTPVarsAndCaptureDiagnosticsCoexist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"other":1}`))
	}))
	defer srv.Close()

	m := httpApp(t)
	// The failing capture and the unknown reference sit in different
	// requests: an unresolved placeholder aborts its own dispatch, so the
	// request that runs must not hold one.
	path := httpVarFile(t, "### start\n# @capture task = .task\nGET "+srv.URL+"/x\n\n"+
		"### poll\nGET "+srv.URL+"/y?v={{nope}}\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	m.lintHTTPVars(path)
	if _, warns := m.activeEditor().DiagnosticCounts(); warns != 1 {
		t.Fatalf("the unknown variable must be marked, got %d", warns)
	}

	m.activeEditor().SetCursor(2, 0) // the "start" request
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatalf("the request itself must succeed: %v", resp.Err)
	}
	out, _ = m.Update(resp)
	m = out.(Model)

	msgs := warningMessages(m, path)
	if len(msgs) != 2 {
		t.Fatalf("both producers must be reported: %v", msgs)
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "capture task") || !strings.Contains(joined, "nope") {
		t.Errorf("diagnostics: %v", msgs)
	}
}
