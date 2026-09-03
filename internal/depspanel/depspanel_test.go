package depspanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/deps"
)

func sample() deps.Result {
	return deps.Result{Root: "/p", Manifests: []deps.ManifestDeps{
		{Path: "/p/go.mod", Provider: "go", Deps: []deps.Dep{
			{Name: "github.com/foo/bar", Current: "v1.2.3", Latest: "v1.3.0", Line: 6},
			{Name: "github.com/baz/qux", Current: "v0.1.0", Indirect: true, Line: 7,
				Vulns: []deps.Vuln{{ID: "GO-2024-1", Summary: "bad"}}},
		}},
		{Path: "/p/package.json", Provider: "npm", Deps: []deps.Dep{
			{Name: "left-pad", Current: "1.3.0", Line: 4},
		}},
	}}
}

func newPanel() Model {
	m := New(nil)
	m.SetSize(80, 12)
	m.Set(sample())
	return m
}

func key(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestRowsGroupByManifest(t *testing.T) {
	m := newPanel()
	if m.Rows() != 5 {
		t.Fatalf("want 2 headers + 3 deps, got %d", m.Rows())
	}
	if !m.rows[0].header || m.rows[0].manifest.Path != "/p/go.mod" {
		t.Fatalf("first row must be the go.mod header: %+v", m.rows[0])
	}
	if m.rows[3].manifest.Path != "/p/package.json" || !m.rows[3].header {
		t.Fatalf("second group must be package.json: %+v", m.rows[3])
	}
}

func TestEnterOpensManifestAtDepLine(t *testing.T) {
	m := newPanel()
	m.cursor = 1 // github.com/foo/bar
	cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must emit a command")
	}
	msg, ok := cmd().(OpenLocationMsg)
	if !ok || msg.Path != "/p/go.mod" || msg.Line != 5 {
		t.Fatalf("bad open msg: %+v", msg)
	}
}

func TestBumpOnlyOnOutdatedRows(t *testing.T) {
	m := newPanel()
	m.cursor = 1 // outdated
	cmd := m.Update(key("u"))
	if cmd == nil {
		t.Fatal("u on an outdated row must emit a bump")
	}
	msg := cmd().(BumpMsg)
	if msg.Provider != "go" || msg.Dep.Name != "github.com/foo/bar" {
		t.Fatalf("bad bump msg: %+v", msg)
	}
	m.cursor = 4 // left-pad, up to date
	if m.Update(key("u")) != nil {
		t.Fatal("u on an up-to-date row must do nothing")
	}
}

func TestVulnsKeyOnlyOnVulnerableRows(t *testing.T) {
	m := newPanel()
	m.cursor = 2 // vulnerable qux
	cmd := m.Update(key("v"))
	if cmd == nil {
		t.Fatal("v on a vulnerable row must emit details")
	}
	if msg := cmd().(VulnsMsg); msg.Dep.Name != "github.com/baz/qux" {
		t.Fatalf("bad vulns msg: %+v", msg)
	}
	m.cursor = 1
	if m.Update(key("v")) != nil {
		t.Fatal("v on a clean row must do nothing")
	}
}

func TestRefreshKey(t *testing.T) {
	m := newPanel()
	cmd := m.Update(key("r"))
	if cmd == nil {
		t.Fatal("r must emit a refresh")
	}
	if _, ok := cmd().(RefreshMsg); !ok {
		t.Fatal("r must emit RefreshMsg")
	}
}

func TestStateFilterHidesFreshDeps(t *testing.T) {
	m := newPanel()
	m.filter.SetTerm("state", "vulnerable")
	m.Refresh()
	if m.Rows() != 2 {
		t.Fatalf("want 1 header + 1 vulnerable dep, got %d", m.Rows())
	}
	if m.rows[1].d.Name != "github.com/baz/qux" {
		t.Fatalf("wrong dep kept: %+v", m.rows[1])
	}
}

func TestFreeTextFiltersByName(t *testing.T) {
	m := newPanel()
	m.filter.SetText("left")
	m.Refresh()
	if m.Rows() != 2 || m.rows[1].d.Name != "left-pad" {
		t.Fatalf("free text must match names: %d rows", m.Rows())
	}
}

func TestViewRendersColumns(t *testing.T) {
	m := newPanel()
	v := m.View()
	for _, want := range []string{"go.mod", "v1.2.3 → v1.3.0", "indirect", "1 vuln", "left-pad"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view missing %q:\n%s", want, v)
		}
	}
}
