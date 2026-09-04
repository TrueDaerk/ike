package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// es_page_test.go covers the Elasticsearch endpoints editor (#1927): the
// tools_page_test harness (stubHost, restoreConfig, apply) driving ESPage and
// esForm end to end against a temp settings file.

func esPage(t *testing.T) (*ESPage, *stubHost) {
	t.Helper()
	restoreConfig(t)
	p := NewESPage(testOpts(t))
	h := &stubHost{}
	p.SetSubPanelHost(h)
	return p, h
}

// esFormTop returns the open endpoint form, failing when none is pushed.
func esFormTop(t *testing.T, h *stubHost) *esForm {
	t.Helper()
	f, ok := h.top().(*esForm)
	if !ok {
		t.Fatal("expected an open endpoint form sub-panel")
	}
	return f
}

// esType feeds a string into the open form rune by rune.
func esType(f *esForm, s string) {
	for _, r := range s {
		f.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
}

// addEndpoint drives the full add flow: open form, fill name/url, save.
func addEndpoint(t *testing.T, p *ESPage, h *stubHost, name, url string) {
	t.Helper()
	p.Update(key("a"))
	f := esFormTop(t, h)
	esType(f, name)
	f.Update(key("tab"))
	esType(f, url)
	apply(t, f.Update(key("enter")))
	if h.top() != nil {
		t.Fatal("save must pop the form")
	}
}

func TestESPageAddWritesAndReloads(t *testing.T) {
	p, h := esPage(t)
	addEndpoint(t, p, h, "prod", "https://es.example.com:9200")
	got := config.Get().Elasticsearch.Endpoints
	if len(got) != 1 || got[0].Name != "prod" || got[0].URL != "https://es.example.com:9200" {
		t.Fatalf("entries after add = %+v", got)
	}
	// The optional auth fields stay out of the written TOML when empty.
	data, err := os.ReadFile(p.opts.UserPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "username") || strings.Contains(string(data), "api_key") {
		t.Fatalf("empty auth fields must not be written:\n%s", data)
	}
}

func TestESPageAddBasicAuth(t *testing.T) {
	p, h := esPage(t)
	p.Update(key("a"))
	f := esFormTop(t, h)
	esType(f, "prod")
	f.Update(key("tab"))
	esType(f, "http://localhost:9200")
	f.Update(key("tab"))
	esType(f, "elastic")
	f.Update(key("tab"))
	esType(f, "secret")
	apply(t, f.Update(key("enter")))
	got := config.Get().Elasticsearch.Endpoints
	if len(got) != 1 || got[0].Username != "elastic" || got[0].Password != "secret" {
		t.Fatalf("entries = %+v", got)
	}
	// The list marks the auth scheme.
	if view := p.View(100, 12); !strings.Contains(view, "· basic") {
		t.Fatalf("list must badge basic auth, view=%q", view)
	}
}

func TestESPageAddAPIKey(t *testing.T) {
	p, h := esPage(t)
	p.Update(key("a"))
	f := esFormTop(t, h)
	esType(f, "cloud")
	f.Update(key("tab"))
	esType(f, "https://cloud.es.io")
	for i := 0; i < 3; i++ { // username, password → api key
		f.Update(key("tab"))
	}
	esType(f, "b64key==")
	apply(t, f.Update(key("enter")))
	got := config.Get().Elasticsearch.Endpoints
	if len(got) != 1 || got[0].APIKey != "b64key==" {
		t.Fatalf("entries = %+v", got)
	}
	if view := p.View(100, 12); !strings.Contains(view, "· api-key") {
		t.Fatalf("list must badge api-key auth, view=%q", view)
	}
}

// TestESPageValidation walks every validate() rule: each rejects with its
// exact message and never persists.
func TestESPageValidation(t *testing.T) {
	p, h := esPage(t)
	addEndpoint(t, p, h, "taken", "http://localhost:9200")

	// reject drives the form to a failed save and checks the exact note.
	reject := func(f *esForm, want string) {
		t.Helper()
		if cmd := f.Update(key("enter")); cmd != nil {
			t.Fatalf("invalid form must not save (want note %q)", want)
		}
		if f.note != want {
			t.Fatalf("note = %q, want %q", f.note, want)
		}
	}
	// fresh opens a blank form seeded with the given fields.
	fresh := func(fields ...string) *esForm {
		t.Helper()
		if h.top() != nil {
			h.top().(*esForm).Update(key("esc"))
		}
		p.Update(key("a"))
		f := esFormTop(t, h)
		for i, v := range fields {
			f.form[i] = v
		}
		return f
	}

	reject(fresh(), "name is required")
	reject(fresh("taken", "http://localhost:9200"), "an endpoint named taken already exists")
	reject(fresh("fresh"), "url is required")
	reject(fresh("fresh", "ftp://x"), "url must start with http:// or https://")
	reject(fresh("fresh", "http://"), "url has no host")
	reject(fresh("fresh", "http://x", "elastic", "", "b64key=="),
		"basic auth and api key are mutually exclusive")
	reject(fresh("fresh", "http://x", "", "secret", ""), "password needs a username")

	// Every rejection above stayed unpersisted: only the seed entry exists.
	if got := config.Get().Elasticsearch.Endpoints; len(got) != 1 || got[0].Name != "taken" {
		t.Fatalf("rejected forms must not persist, got %+v", got)
	}

	// The rule set is not over-strict: an api key alone saves.
	f := fresh("keyed", "http://x", "", "", "b64key==")
	apply(t, f.Update(key("enter")))
	if got := config.Get().Elasticsearch.Endpoints; len(got) != 2 {
		t.Fatalf("api key alone must save, got %+v", got)
	}
}

func TestESPageEditRoundTrip(t *testing.T) {
	p, h := esPage(t)
	addEndpoint(t, p, h, "prod", "http://old:9200")
	p.sel = 0
	p.Update(key("enter")) // edit
	f := esFormTop(t, h)
	if f.form[0] != "prod" || f.form[1] != "http://old:9200" {
		t.Fatalf("edit must seed the form, form=%v", f.form)
	}
	f.Update(key("tab")) // to url
	for range "http://old:9200" {
		f.Update(key("backspace"))
	}
	esType(f, "http://new:9200")
	apply(t, f.Update(key("enter")))
	got := config.Get().Elasticsearch.Endpoints
	if len(got) != 1 || got[0].URL != "http://new:9200" {
		t.Fatalf("entries after edit = %+v", got)
	}
}

func TestESPageEditingOwnNameIsNotADuplicate(t *testing.T) {
	p, h := esPage(t)
	addEndpoint(t, p, h, "solo", "http://localhost:9200")
	p.sel = 0
	p.Update(key("enter"))
	apply(t, esFormTop(t, h).Update(key("enter"))) // save unchanged: own name must pass
	if got := config.Get().Elasticsearch.Endpoints; len(got) != 1 || got[0].Name != "solo" {
		t.Fatalf("entries = %+v", got)
	}
}

func TestESPageDelete(t *testing.T) {
	p, h := esPage(t)
	addEndpoint(t, p, h, "one", "http://one:9200")
	addEndpoint(t, p, h, "two", "http://two:9200")
	p.sel = 0
	p.Update(key("d"))
	apply(t, confirmVia(t, h))
	got := config.Get().Elasticsearch.Endpoints
	if len(got) != 1 || got[0].Name != "two" {
		t.Fatalf("entries after delete = %+v", got)
	}
}

// TestESPageScopeToggle: "s" flips the write target; at project scope the
// list lands in <root>/.ike/settings.toml, not the user file.
func TestESPageScopeToggle(t *testing.T) {
	restoreConfig(t)
	root := t.TempDir()
	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: root,
	}
	p := NewESPage(opts)
	h := &stubHost{}
	p.SetSubPanelHost(h)

	if p.scope != config.UserScope {
		t.Fatalf("scope must default to user, got %v", p.scope)
	}
	p.Update(key("s"))
	if p.scope != config.ProjectScope {
		t.Fatalf("s must toggle to project, got %v", p.scope)
	}
	if view := p.View(100, 12); !strings.Contains(view, "writes to the project layer") {
		t.Fatalf("footer must show the write target, view=%q", view)
	}

	addEndpoint(t, p, h, "proj", "http://localhost:9200")
	data, err := os.ReadFile(filepath.Join(root, ".ike", "settings.toml"))
	if err != nil {
		t.Fatalf("project settings file must exist: %v", err)
	}
	if !strings.Contains(string(data), "proj") || !strings.Contains(string(data), "elasticsearch") {
		t.Fatalf("project file must hold the endpoint list:\n%s", data)
	}
	if _, err := os.Stat(opts.UserPath); !os.IsNotExist(err) {
		t.Fatalf("user file must stay untouched at project scope, err=%v", err)
	}
	// Toggling back writes to the user layer again.
	p.Update(key("s"))
	if p.scope != config.UserScope {
		t.Fatalf("s must toggle back to user, got %v", p.scope)
	}
}

// TestESFormMasksSecrets: the password and api key rows render bullets, never
// the typed value.
func TestESFormMasksSecrets(t *testing.T) {
	p, h := esPage(t)
	p.Update(key("a"))
	f := esFormTop(t, h)
	for i := 0; i < 3; i++ { // name, url, username → password
		f.Update(key("tab"))
	}
	esType(f, "hunter2")
	view := f.View(80, 12)
	if strings.Contains(view, "hunter2") {
		t.Fatalf("password must render masked, view=%q", view)
	}
	if !strings.Contains(view, strings.Repeat("•", len("hunter2"))) {
		t.Fatalf("mask must keep the rune count, view=%q", view)
	}
	// The real text is intact behind the mask.
	if f.form[3] != "hunter2" {
		t.Fatalf("form[3] = %q", f.form[3])
	}
}

func TestESPageEscCancelsWithoutWriting(t *testing.T) {
	p, h := esPage(t)
	p.Update(key("a"))
	f := esFormTop(t, h)
	esType(f, "ghost")
	f.Update(key("esc"))
	if h.top() != nil {
		t.Fatal("esc must pop the form")
	}
	if got := config.Get().Elasticsearch.Endpoints; len(got) != 0 {
		t.Fatalf("cancel must not write, got %+v", got)
	}
}

func TestESPageViewListsEntriesAndHints(t *testing.T) {
	p, h := esPage(t)
	v := p.View(100, 20)
	if !strings.Contains(v, "no endpoints configured") {
		t.Fatalf("empty view = %q", v)
	}
	if !strings.Contains(v, "writes to the user layer") {
		t.Fatalf("view must show the default write target, view=%q", v)
	}
	addEndpoint(t, p, h, "prod", "https://es.example.com")
	v = p.View(100, 20)
	if !strings.Contains(v, "prod") || !strings.Contains(v, "https://es.example.com") {
		t.Fatalf("view must list the entry:\n%s", v)
	}
}
