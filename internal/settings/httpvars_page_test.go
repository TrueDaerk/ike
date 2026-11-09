package settings

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/secret"
)

// httpvars_page_test.go covers the global .http variables editor: the
// tools_page_test harness (stubHost, restoreConfig, apply) driving
// HTTPVarsPage and httpVarForm end to end against temp user and project
// settings files.

func httpVarsPage(t *testing.T) (*HTTPVarsPage, *stubHost) {
	t.Helper()
	restoreConfig(t)
	opts := testOpts(t)
	opts.ProjectRoot = t.TempDir()
	p := NewHTTPVarsPage(opts)
	h := &stubHost{}
	p.SetSubPanelHost(h)
	return p, h
}

// httpVarFormTop returns the open variable form, failing when none is pushed.
func httpVarFormTop(t *testing.T, h *stubHost) *httpVarForm {
	t.Helper()
	f, ok := h.top().(*httpVarForm)
	if !ok {
		t.Fatal("expected an open variable form sub-panel")
	}
	return f
}

// httpVarType feeds a string into the open form rune by rune.
func httpVarType(f *httpVarForm, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

// addHTTPVar drives the full add flow: open form, fill name/value, save.
func addHTTPVar(t *testing.T, p *HTTPVarsPage, h *stubHost, name, value string) {
	t.Helper()
	p.Update(key("a"))
	f := httpVarFormTop(t, h)
	httpVarType(f, name)
	f.Update(key("tab"))
	httpVarType(f, value)
	apply(t, f.Update(key("enter")))
	if h.top() != nil {
		t.Fatalf("save must pop the form (note %q)", f.note)
	}
}

// httpVarsGot renders the live variables as "scope:name=value".
func httpVarsGot() string {
	var out []string
	for _, v := range config.Get().HTTP.Variables {
		out = append(out, esScopeLabel(v.Scope)+":"+v.Name+"="+v.Value)
	}
	return strings.Join(out, " ")
}

func TestHTTPVarsPageAddPersistsPerScope(t *testing.T) {
	p, h := httpVarsPage(t)
	addHTTPVar(t, p, h, "host", "https://user.test")
	p.Update(key("s")) // add to the project scope from now on
	addHTTPVar(t, p, h, "host", "https://project.test")
	addHTTPVar(t, p, h, "api", "{{host}}/api")
	if got, want := httpVarsGot(), "project:host=https://project.test project:api={{host}}/api user:host=https://user.test"; got != want {
		t.Fatalf("variables = %q, want %q", got, want)
	}
	// A restart reads the same lists back from the two files.
	c, diags := config.Load(p.opts)
	if len(diags) != 0 {
		t.Fatalf("diags: %+v", diags)
	}
	project, user := c.HTTP.HTTPVariableMaps()
	if project["host"] != "https://project.test" || project["api"] != "{{host}}/api" || user["host"] != "https://user.test" {
		t.Fatalf("after reload: project %v, user %v", project, user)
	}
}

func TestHTTPVarsPageValidation(t *testing.T) {
	p, h := httpVarsPage(t)
	addHTTPVar(t, p, h, "host", "x")
	for _, c := range []struct{ name, want string }{
		{"", "name is required"},
		{"1bad", `invalid name "1bad"`},
		{"has space", `invalid name "has space"`},
		{"host", "a user variable named host already exists"},
	} {
		p.Update(key("a"))
		f := httpVarFormTop(t, h)
		httpVarType(f, c.name)
		if cmd := f.Update(key("enter")); cmd != nil {
			t.Fatalf("%q: an invalid form must not write", c.name)
		}
		if !strings.Contains(f.note, c.want) {
			t.Errorf("%q: note %q, want it to contain %q", c.name, f.note, c.want)
		}
		if !strings.Contains(f.View(120, 10), f.note) {
			t.Errorf("%q: the view must show the note", c.name)
		}
		f.Update(key("esc"))
	}
	// The same name in the other scope is no duplicate.
	p.Update(key("s"))
	addHTTPVar(t, p, h, "host", "y")
	if got := httpVarsGot(); got != "project:host=y user:host=x" {
		t.Fatalf("variables = %q", got)
	}
}

func TestHTTPVarsPageEditKeepsScope(t *testing.T) {
	p, h := httpVarsPage(t)
	p.Update(key("s"))
	addHTTPVar(t, p, h, "host", "old")
	p.Update(key("s")) // back to user: an edit still writes the row's own scope
	p.sel = 0
	p.Update(key("enter"))
	f := httpVarFormTop(t, h)
	if f.form[0] != "host" || f.form[1] != "old" {
		t.Fatalf("form seeded with %v", f.form)
	}
	f.Focus(1)
	for range "old" {
		f.Update(key("backspace"))
	}
	httpVarType(f, "new")
	apply(t, f.Update(key("enter")))
	if got := httpVarsGot(); got != "project:host=new" {
		t.Fatalf("variables = %q", got)
	}
	// Re-saving under its own name is not a duplicate.
	p.Update(key("enter"))
	f = httpVarFormTop(t, h)
	if cmd := f.Update(key("enter")); cmd == nil {
		t.Fatalf("editing an entry under its own name must save (note %q)", f.note)
	}
}

func TestHTTPVarsPageDelete(t *testing.T) {
	p, h := httpVarsPage(t)
	addHTTPVar(t, p, h, "a", "1")
	addHTTPVar(t, p, h, "b", "2")
	p.sel = 0
	p.Update(key("d"))
	apply(t, confirmVia(t, h))
	if got := httpVarsGot(); got != "user:b=2" {
		t.Fatalf("after delete = %q", got)
	}
	p.Update(key("d"))
	apply(t, confirmVia(t, h))
	if got := httpVarsGot(); got != "" {
		t.Fatalf("after deleting the last = %q", got)
	}
}

func TestHTTPVarsPageEscCancelsWithoutWriting(t *testing.T) {
	p, h := httpVarsPage(t)
	p.Update(key("a"))
	f := httpVarFormTop(t, h)
	httpVarType(f, "host")
	if cmd := f.Update(key("esc")); cmd != nil {
		t.Fatal("esc must not write")
	}
	if h.top() != nil {
		t.Fatal("esc must pop the form")
	}
	if got := httpVarsGot(); got != "" {
		t.Fatalf("variables = %q", got)
	}
}

func TestHTTPVarsPageViewMasksSecretsAndMarksOverrides(t *testing.T) {
	p, h := httpVarsPage(t)
	c := *config.Get()
	c.Editor.SecretMasking = true
	config.Set(&c)
	addHTTPVar(t, p, h, "apiToken", "s3cr3t-value")
	addHTTPVar(t, p, h, "host", "https://user.test")
	p.Update(key("s"))
	addHTTPVar(t, p, h, "host", "https://project.test")
	view := p.View(120, 12)
	if strings.Contains(view, "s3cr3t-value") {
		t.Errorf("a secret-looking value must render masked:\n%s", view)
	}
	if !strings.Contains(view, secret.Mask) {
		t.Errorf("the mask must show in place of the value:\n%s", view)
	}
	if !strings.Contains(view, "https://user.test") || !strings.Contains(view, "overridden by project") {
		t.Errorf("the shadowed user variable must be listed and marked:\n%s", view)
	}
	// The form masks the value too while the name is secret-suspect.
	p.sel = 1
	for i, v := range config.Get().HTTP.Variables {
		if v.Name == "apiToken" {
			p.sel = i
		}
	}
	p.Update(key("enter"))
	if fv := httpVarFormTop(t, h).View(80, 10); strings.Contains(fv, "s3cr3t-value") {
		t.Errorf("the form must mask a secret value:\n%s", fv)
	}

	// With masking off the value reads plainly.
	c2 := *config.Get()
	c2.Editor.SecretMasking = false
	config.Set(&c2)
	if view := p.View(120, 12); !strings.Contains(view, "s3cr3t-value") {
		t.Errorf("with secret masking off the value must show:\n%s", view)
	}
}

func TestHTTPVarsPageEmptyHint(t *testing.T) {
	p, _ := httpVarsPage(t)
	if view := p.View(100, 10); !strings.Contains(view, "press a to add one") {
		t.Errorf("empty list must hint at adding:\n%s", view)
	}
}
