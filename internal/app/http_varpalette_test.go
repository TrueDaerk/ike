package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/httpfile"
	"ike/internal/httphistory"
	"ike/internal/palette"
	"ike/internal/registry"
	"ike/internal/settings"
)

// http_varpalette_test.go covers the HTTP Variables palette (#3122): rows,
// origins and values, masking, the Enter targets and the copy chord.

// httpVarsSrc defines two file variables (one expanding through the
// environment), a capturing request and a request using them.
const httpVarsSrc = "@apiBase = {{domain}}/api\n" +
	"@user = paste-me\n" +
	"\n" +
	"### auth\n" +
	"# @capture session = .session\n" +
	"POST https://example.com/login\n" +
	"\n" +
	"### list\n" +
	"GET {{apiBase}}/items\n"

// httpVarsApp opens httpVarsSrc next to a one-environment env file (the
// private file supplying the secret), with a project global variable and a
// captured value stored two minutes ago (plus a newer capture-less run of
// the same request). It runs on the real registry, so the commands, their
// palette-context chords and the HTTP Variables settings page exist.
func httpVarsApp(t *testing.T) (Model, string, time.Time) {
	t.Helper()
	m := NewWith(registry.Global(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = out.(Model)
	if m.onboardingOpen() {
		m = m.closeOnboarding().(Model)
	}
	prev := config.Get()
	c, _ := config.Load(config.Options{})
	c.Editor.SecretMasking = true
	c.HTTP.Variables = []config.HTTPVariable{{Name: "region", Value: "eu-{{domain}}", Scope: config.ProjectScope}}
	config.Set(c)
	t.Cleanup(func() { config.Set(prev) })

	path := httpVarFile(t, httpVarsSrc, map[string]string{
		httpfile.EnvFileName:        "{\n  \"dev\": {\n    \"domain\": \"https://dev.example.com\",\n    \"apiToken\": \"public\"\n  }\n}\n",
		httpfile.PrivateEnvFileName: "{\n  \"dev\": {\n    \"apiToken\": \"s3cret\"\n  }\n}\n",
	})
	at := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	store := httphistory.New(httpHistoryDir())
	store.Append(path, "auth", httphistory.Entry{Time: at, Status: "200 OK", StatusCode: 200, Proto: "HTTP/1.1",
		Body: []byte(`{"session":"abc123"}`), Captured: map[string]string{"session": "abc123"}})
	store.Append(path, "auth", httphistory.Entry{Time: at.Add(time.Minute), Status: "500 Internal Server Error", StatusCode: 500, Proto: "HTTP/1.1"})

	out, _ = m.Update(explorer.OpenFileMsg{Path: path})
	m = out.(Model)
	return m, path, at
}

// openHTTPVars runs http.variables and returns the listed rows.
func openHTTPVars(t *testing.T, m Model) (Model, []palette.Item) {
	t.Helper()
	out, _ := m.Update(HTTPVariablesMsg{})
	m = out.(Model)
	if !m.palette.IsOpen() {
		t.Fatalf("http.variables must open the palette; notices: %v", notices(m))
	}
	if prefix, ok := m.palette.LockedPrefix(); !ok || prefix != httpVarsPrefix {
		t.Fatalf("palette locked to %q, want the HTTP Variables mode", prefix)
	}
	return m, m.httpVarsPick.Results("", palette.Context{})
}

// httpVarItem returns the row titled name.
func httpVarItem(t *testing.T, items []palette.Item, name string) palette.Item {
	t.Helper()
	for _, it := range items {
		if it.Title == name {
			return it
		}
	}
	t.Fatalf("no row %q in %v", name, httpVarTitles(items))
	return palette.Item{}
}

func httpVarTitles(items []palette.Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Title)
	}
	return out
}

// TestHTTPVarsPaletteRows: every rung shows with its origin word and value,
// sorted by name; a placeholder value shows written → expanded, a secret is
// masked, and the captured row names its request and age.
func TestHTTPVarsPaletteRows(t *testing.T) {
	withStore(t)
	m, _, _ := httpVarsApp(t)
	if c, ok := m.reg.Command(httpVariablesCommand); !ok || c.Title != "HTTP Variables" {
		t.Fatalf("command %s = %+v (ok=%v), want title \"HTTP Variables\"", httpVariablesCommand, c, ok)
	}
	m, items := openHTTPVars(t, m)
	if v := m.palette.View(); !strings.Contains(v, "ctrl+c copy value") {
		t.Errorf("the footer must document the copy key:\n%s", v)
	}
	want :=[]string{"apiBase", "apiToken", "domain", "region", "session", "user"}
	if got := httpVarTitles(items); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows %v, want %v", got, want)
	}
	for name, detail := range map[string]string{
		"apiBase":  "file · {{domain}}/api → https://dev.example.com/api",
		"apiToken": "env dev · ••••",
		"domain":   "env dev · https://dev.example.com",
		"region":   "global project · eu-{{domain}} → eu-https://dev.example.com",
		"session":  "response · abc123  ← POST /login (### auth) · 2m ago",
		"user":     "file · paste-me",
	} {
		if got := httpVarItem(t, items, name).Detail; got != detail {
			t.Errorf("%s detail = %q, want %q", name, got, detail)
		}
	}
}

// TestHTTPVarsPaletteFilter: the query narrows by name, origin and value —
// but never by a masked secret's real value.
func TestHTTPVarsPaletteFilter(t *testing.T) {
	withStore(t)
	m, _, _ := httpVarsApp(t)
	m, _ = openHTTPVars(t, m)
	for query, want := range map[string]string{
		"sess":            "session",
		"global":          "region",
		"paste-me":        "user",
		"dev.example.com": "apiBase,domain,region",
		"s3cret":          "",
	} {
		got := strings.Join(httpVarTitles(m.httpVarsPick.Results(query, palette.Context{})), ",")
		if got != want {
			t.Errorf("query %q: rows %q, want %q", query, got, want)
		}
	}
}

// TestHTTPVarsPaletteProcessEnv: a global variable the process environment
// shadows lists as "process env" with the process value, and Enter on it
// explains instead of jumping.
func TestHTTPVarsPaletteProcessEnv(t *testing.T) {
	m, _, _ := httpVarsApp(t)
	t.Setenv("IKE_TEST_3122", "from-process")
	c := *config.Get()
	c.HTTP.Variables = append(c.HTTP.Variables, config.HTTPVariable{Name: "IKE_TEST_3122", Value: "global", Scope: config.UserScope})
	config.Set(&c)
	m, items := openHTTPVars(t, m)
	it := httpVarItem(t, items, "IKE_TEST_3122")
	if it.Detail != "process env · from-process" {
		t.Fatalf("detail = %q", it.Detail)
	}
	clearNotices(&m)
	out, _ := m.Update(it.Msg)
	m = out.(Model)
	if !strings.Contains(strings.Join(notices(m), "\n"), "defined by the process environment") {
		t.Errorf("notices %v, want the process-environment notice", notices(m))
	}
}

// TestHTTPVarsPaletteJumpFile: Enter on a file variable moves the caret to
// its `@name =` line.
func TestHTTPVarsPaletteJumpFile(t *testing.T) {
	m, _, _ := httpVarsApp(t)
	m, items := openHTTPVars(t, m)
	out, _ := m.Update(httpVarItem(t, items, "user").Msg)
	m = out.(Model)
	if line, _ := m.activeEditor().CursorPos(); line != 1 {
		t.Errorf("caret on line %d, want 1 (the @user line)", line)
	}
}

// TestHTTPVarsPaletteJumpCapture: Enter on a captured variable shows the
// response that produced it in the response pane — the older entry, not the
// newer run that captured nothing.
func TestHTTPVarsPaletteJumpCapture(t *testing.T) {
	withStore(t)
	m, _, _ := httpVarsApp(t)
	m, items := openHTTPVars(t, m)
	out, _ := m.Update(httpVarItem(t, items, "session").Msg)
	m = out.(Model)
	p := m.httpPanel()
	if p == nil {
		t.Fatal("the response pane must open")
	}
	if idx, n := p.HistoryIndex(); idx != 1 || n != 2 {
		t.Errorf("history %d/%d, want entry 1 of 2 (the capturing response)", idx, n)
	}
	if resp := p.CurrentResponse(); resp == nil || resp.StatusCode != 200 {
		t.Errorf("shown response %+v, want the 200 that captured the value", resp)
	}
}

// TestHTTPVarsPaletteJumpEnv: Enter on an environment variable opens the env
// file that supplies it at the key's line — the private file for a value it
// overrides.
func TestHTTPVarsPaletteJumpEnv(t *testing.T) {
	m, path, _ := httpVarsApp(t)
	m, items := openHTTPVars(t, m)
	out, _ := m.Update(httpVarItem(t, items, "domain").Msg)
	m = out.(Model)
	ed := m.activeEditor()
	if ed == nil || ed.Path() != filepath.Join(filepath.Dir(path), httpfile.EnvFileName) {
		t.Fatalf("active editor %v, want the env file", ed)
	}
	if line, _ := ed.CursorPos(); line != 2 {
		t.Errorf("caret on line %d, want 2 (the domain key)", line)
	}

	m, items = openHTTPVarsFrom(t, m, path)
	out, _ = m.Update(httpVarItem(t, items, "apiToken").Msg)
	m = out.(Model)
	if ed := m.activeEditor(); ed == nil || filepath.Base(ed.Path()) != httpfile.PrivateEnvFileName {
		t.Fatalf("active editor %v, want the private env file", ed)
	}
}

// openHTTPVarsFrom focuses path again and reopens the palette.
func openHTTPVarsFrom(t *testing.T, m Model, path string) (Model, []palette.Item) {
	t.Helper()
	out, _ := m.Update(explorer.OpenFileMsg{Path: path})
	return openHTTPVars(t, out.(Model))
}

// TestHTTPVarsPaletteJumpGlobal: Enter on a global variable opens the HTTP
// Variables settings page.
func TestHTTPVarsPaletteJumpGlobal(t *testing.T) {
	m, _, _ := httpVarsApp(t)
	m, items := openHTTPVars(t, m)
	out, _ := m.Update(httpVarItem(t, items, "region").Msg)
	m = out.(Model)
	if !m.settings.IsOpen() || m.settings.PageTitle() != settings.HTTPVarsPageTitle {
		t.Errorf("settings open=%v page=%q, want the HTTP Variables page", m.settings.IsOpen(), m.settings.PageTitle())
	}
}

// TestHTTPVarsPaletteCopy: ctrl+c in the palette copies the focused row's
// unmasked, expanded value and keeps the palette open; outside the mode the
// chord is not claimed.
func TestHTTPVarsPaletteCopy(t *testing.T) {
	var copied string
	orig := clipboardWrite
	clipboardWrite = func(s string) { copied = s }
	t.Cleanup(func() { clipboardWrite = orig })

	m, _, _ := httpVarsApp(t)
	m, _ = openHTTPVars(t, m)
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	for _, r := range "apiToken" {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	m = runHTTPVarsKey(t, m, ctrlC)
	if copied != "s3cret" {
		t.Errorf("copied %q, want the unmasked private value", copied)
	}
	if !m.palette.IsOpen() {
		t.Error("copying must keep the palette open")
	}

	m.palette.Close()
	m, _ = openHTTPVars(t, m)
	for _, r := range "apiBase" {
		out, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = out.(Model)
	}
	m = runHTTPVarsKey(t, m, ctrlC)
	if copied != "https://dev.example.com/api" {
		t.Errorf("copied %q, want the expanded value", copied)
	}

	// Another palette mode does not hand the chord to http.copyVariable.
	m.palette.Close()
	out, _ := m.Update(HTTPSelectEnvMsg{})
	m = out.(Model)
	copied = ""
	if _, handled := m.paletteBindingCmd(ctrlC); handled {
		t.Error("ctrl+c must not be claimed outside the HTTP Variables palette")
	}
}

// runHTTPVarsKey feeds a key and the messages its command produces back
// into the model.
func runHTTPVarsKey(t *testing.T, m Model, key tea.KeyPressMsg) Model {
	t.Helper()
	out, cmd := m.Update(key)
	m = out.(Model)
	for _, msg := range cmdMsgs(cmd) {
		out, _ = m.Update(msg)
		m = out.(Model)
	}
	return m
}

// TestHTTPVarsPaletteNeedsHTTPBuffer: from a non-.http buffer the command
// explains itself and opens nothing; an .http file without variables opens
// an empty list.
func TestHTTPVarsPaletteNeedsHTTPBuffer(t *testing.T) {
	m := httpApp(t)
	dir := t.TempDir()
	txt := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(txt, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.Update(explorer.OpenFileMsg{Path: txt})
	m = out.(Model)
	out, _ = m.Update(HTTPVariablesMsg{})
	m = out.(Model)
	if m.palette.IsOpen() {
		t.Fatal("no palette from a non-.http buffer")
	}
	if !strings.Contains(strings.Join(notices(m), "\n"), "focus an .http file first") {
		t.Errorf("notices %v, want the focus notice", notices(m))
	}

	empty := filepath.Join(dir, "empty.http")
	if err := os.WriteFile(empty, []byte("GET https://example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, items := openHTTPVarsFrom(t, m, empty)
	if len(items) != 0 {
		t.Errorf("rows %v, want none", httpVarTitles(items))
	}
}
