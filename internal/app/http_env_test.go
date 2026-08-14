package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/explorer"
	"ike/internal/httpfile"
	"ike/internal/palette"
)

// httpVarFile writes a .http file plus the given sibling files (environment
// files, bodies) into a fresh directory and returns the .http file's path.
func httpVarFile(t *testing.T, src string, siblings map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "req.http")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range siblings {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// TestHTTPRunResolvesFileVariables: the acceptance case — `@host=…` plus
// `GET {{host}}/path` dispatches with the substituted URL (#1867).
func TestHTTPRunResolvesFileVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path + " " + r.Header.Get("X-Token")))
	}))
	defer srv.Close()

	m := httpApp(t)
	path := httpVarFile(t, "@host="+srv.URL+"\n@token=s3cret\nGET {{host}}/my/path\nX-Token: {{token}}\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("http.run must dispatch")
	}
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if got := string(resp.Resp.Body); got != "/my/path s3cret" {
		t.Errorf("server saw %q", got)
	}
}

// TestHTTPRunResolvesSingleEnvironment: a directory whose http-client.env.json
// names exactly one environment needs no choice — it is simply used, with the
// private file overriding same-named values (#1867).
func TestHTTPRunResolvesSingleEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path + " " + r.Header.Get("X-Token")))
	}))
	defer srv.Close()

	m := httpApp(t)
	path := httpVarFile(t, "GET {{host}}/from/env\nX-Token: {{token}}\n", map[string]string{
		httpfile.EnvFileName:        `{"dev": {"host": "` + srv.URL + `", "token": "public"}}`,
		httpfile.PrivateEnvFileName: `{"dev": {"token": "s3cret"}}`,
	})
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if got := string(resp.Resp.Body); got != "/from/env s3cret" {
		t.Errorf("server saw %q, want the private token", got)
	}
}

// TestHTTPEnvPickerSelectsEnvironment: with several environments the picker
// lists them, the choice persists per directory, and the next dispatch
// resolves against it — in-file definitions still winning (#1867).
func TestHTTPEnvPickerSelectsEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()

	m := httpApp(t)
	path := httpVarFile(t, "@path=/mine\nGET {{host}}{{path}}\n", map[string]string{
		httpfile.EnvFileName: `{"dev": {"host": "` + srv.URL + `", "path": "/dev"}, "prod": {"host": "https://example.invalid"}}`,
	})
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	// Nothing chosen yet: the unresolved {{host}} names the way out.
	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp := drainHTTPResponse(t, cmd)
	if resp.Err == nil {
		t.Fatal("an unselected environment must not resolve {{host}}")
	}
	if !strings.Contains(resp.Err.Error(), "http.selectEnvironment") ||
		!strings.Contains(resp.Err.Error(), "dev, prod") {
		t.Errorf("the error must name the choice: %v", resp.Err)
	}
	out, _ = m.Update(resp) // ends the in-flight entry, so the retry below is no duplicate
	m = out.(Model)

	out, _ = m.Update(HTTPSelectEnvMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("http.selectEnvironment must open the picker")
	}
	items := m.httpEnvs.Results("", palette.Context{})
	if len(items) != 2 || items[0].Title != "dev" || items[1].Title != "prod" {
		t.Fatalf("picker rows: %+v, want [dev prod]", items)
	}
	out, _ = m.Update(items[0].Msg)
	m = out.(Model)
	if got := m.httpEnv.Get(filepath.Dir(path)); got != "dev" {
		t.Fatalf("selection: %q, want dev", got)
	}

	out, cmd = m.Update(HTTPRunMsg{})
	m = out.(Model)
	resp = drainHTTPResponse(t, cmd)
	if resp.Err != nil {
		t.Fatal(resp.Err)
	}
	if got := string(resp.Resp.Body); got != "/mine" {
		t.Errorf("server saw %q — the in-file @path must beat the environment", got)
	}

	// The choice survives a restart: a fresh store reads it back.
	if got := loadHTTPEnv().Get(filepath.Dir(path)); got != "dev" {
		t.Errorf("persisted selection: %q, want dev", got)
	}
}

// TestRecentFilesSurvivesHTTPEnvPrefix guards #1878: httpEnvPrefix used to
// collide with palette.RecentPrefix ('%'), so once the env picker had been
// opened once, cmd+e (palette.recentFiles) silently opened the HTTP
// environment picker instead of Recent Files.
func TestRecentFilesSurvivesHTTPEnvPrefix(t *testing.T) {
	m := httpApp(t)
	path := httpVarFile(t, "GET {{host}}\n", map[string]string{
		httpfile.EnvFileName: `{"dev": {"host": "https://dev.invalid"}, "prod": {"host": "https://example.invalid"}}`,
	})
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	// Open and close the env picker once, so its mode is live and its
	// prefix is registered — the state that used to shadow the recent-files
	// prefix in palette.byPrefix.
	out, _ = m.Update(HTTPSelectEnvMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("http.selectEnvironment must open the picker")
	}
	m.palette.Close()

	out, _ = m.Update(ShowRecentFilesMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatal("palette.recentFiles must open the palette")
	}
	if !strings.Contains(m.palette.View(), "Recent files…") {
		t.Errorf("cmd+e must open Recent Files, not the HTTP env picker: %s", m.palette.View())
	}
}

// TestHTTPEnvPickerClearsSelection: the "(no environment)" row appears only
// once something is selected, and picking it drops the selection again.
func TestHTTPEnvPickerClearsSelection(t *testing.T) {
	m := httpApp(t)
	path := httpVarFile(t, "GET {{host}}/x\n", map[string]string{
		httpfile.EnvFileName: `{"dev": {"host": "http://a"}, "prod": {"host": "http://b"}}`,
	})
	dir := filepath.Dir(path)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, _ = m.Update(HTTPSelectEnvMsg{})
	m = out.(Model)
	if items := m.httpEnvs.Results("", palette.Context{}); len(items) != 2 {
		t.Fatalf("without a selection there is nothing to clear: %+v", items)
	}
	out, _ = m.Update(SelectHTTPEnvMsg{Dir: dir, Name: "prod"})
	m = out.(Model)

	out, _ = m.Update(HTTPSelectEnvMsg{})
	m = out.(Model)
	items := m.httpEnvs.Results("", palette.Context{})
	if len(items) != 3 || items[2].Title != "(no environment)" {
		t.Fatalf("picker rows: %+v, want the clear row last", items)
	}
	if items[1].Badge != "●" {
		t.Errorf("the active environment must be marked: %+v", items[1])
	}
	out, _ = m.Update(items[2].Msg)
	m = out.(Model)
	if got := m.httpEnv.Get(dir); got != "" {
		t.Errorf("selection: %q, want cleared", got)
	}
}

// TestHTTPEnvPickerWithoutEnvFile: a file with no environment file explains
// itself instead of opening an empty palette (#1867).
func TestHTTPEnvPickerWithoutEnvFile(t *testing.T) {
	m := httpApp(t)
	path := httpVarFile(t, "@host=http://a\n\nGET {{host}}/x\n", nil)
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, _ = m.Update(HTTPSelectEnvMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Error("no environments must not open an empty picker")
	}
}

// TestHTTPRunRejectsMalformedEnvFile: a broken http-client.env.json aborts the
// dispatch with a message naming the file — sending a request with unresolved
// variables because of a JSON typo would be worse (#1867).
func TestHTTPRunRejectsMalformedEnvFile(t *testing.T) {
	m := httpApp(t)
	path := httpVarFile(t, "GET {{host}}/x\n", map[string]string{
		httpfile.EnvFileName: `{"dev": {`,
	})
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)

	out, cmd := m.Update(HTTPRunMsg{})
	m = out.(Model)
	if cmd != nil {
		if _, ok := cmd().(HTTPResponseMsg); ok {
			t.Fatal("a malformed environment file must not dispatch")
		}
	}
}
