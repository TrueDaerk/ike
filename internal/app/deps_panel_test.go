package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/deps"
	"ike/internal/depspanel"
	"ike/internal/host"
	"ike/internal/intention"
	"ike/internal/pane"
	"ike/internal/registry"
)

func depsApp(t *testing.T) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), host.MapConfig{})
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func depsResult() deps.Result {
	return deps.Result{Root: "/p", Manifests: []deps.ManifestDeps{{
		Path: "/p/go.mod", Provider: "go",
		Deps: []deps.Dep{
			{Name: "github.com/foo/bar", Current: "v1.2.3", Latest: "v1.3.0", Line: 6},
			{Name: "github.com/baz/qux", Current: "v0.1.0", Line: 7,
				Vulns: []deps.Vuln{{ID: "GO-2024-1", Summary: "bad parser"}}},
		},
	}}}
}

func TestDepsToggleLifecycle(t *testing.T) {
	m := depsApp(t)
	before := m.activeWS().Panes.Focused()

	out, _ := m.Update(DepsToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.DepsKey) || m.activeWS().Panes.Focused() != pane.DepsKey {
		t.Fatalf("first toggle must open + focus the panel (focus=%q)", m.activeWS().Panes.Focused())
	}

	out, _ = m.Update(DepsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != before {
		t.Fatalf("focus = %q, want %q", m.activeWS().Panes.Focused(), before)
	}

	out, _ = m.Update(DepsToggleMsg{})
	m = out.(Model)
	if m.activeWS().Panes.Focused() != pane.DepsKey {
		t.Fatal("third toggle must re-focus the panel")
	}
}

func TestDepsScanFeedsPanelAndProblems(t *testing.T) {
	m := depsApp(t)
	t.Cleanup(func() { deps.SetSnapshot(deps.Result{}) })

	out, _ := m.Update(DepsToggleMsg{})
	m = out.(Model)
	out, _ = m.Update(DepsScanMsg{Result: depsResult()})
	m = out.(Model)

	p := m.depsPanel()
	if p == nil || p.Rows() != 3 {
		t.Fatalf("panel must list 1 header + 2 deps, got %v", p)
	}
	// The vulnerable entry lands in the Problems store as a warning under
	// the "deps" source, anchored at the manifest line.
	ds := m.probStore.Get("/p/go.mod")
	if len(ds) != 1 || ds[0].Severity != 2 || ds[0].Source != "deps" || ds[0].Code != "GO-2024-1" {
		t.Fatalf("problems feed = %+v", ds)
	}
	if ds[0].Range.Start.Line != 6 { // 0-based line of manifest line 7
		t.Fatalf("anchor line = %d, want 6", ds[0].Range.Start.Line)
	}
	// The snapshot serves the hover/intention seams.
	if d, prov, ok := deps.DepAt("/p/go.mod", 6); !ok || prov != "go" || d.Latest != "v1.3.0" {
		t.Fatalf("snapshot DepAt = %+v %s %v", d, prov, ok)
	}
}

func TestDepsHoverServesSnapshot(t *testing.T) {
	deps.SetSnapshot(depsResult())
	t.Cleanup(func() { deps.SetSnapshot(deps.Result{}) })

	text, ok := depsLocalHover("/p/go.mod", 5, 0, nil) // 0-based line 5 = manifest line 6
	if !ok {
		t.Fatal("hover must claim a scanned manifest line")
	}
	for _, want := range []string{"github.com/foo/bar", "v1.2.3", "v1.3.0"} {
		if !containsStr(text, want) {
			t.Fatalf("hover missing %q: %s", want, text)
		}
	}
	if _, ok := depsLocalHover("/p/go.mod", 20, 0, nil); ok {
		t.Fatal("hover must pass on non-dependency lines")
	}
	if _, ok := depsLocalHover("/p/main.go", 5, 0, nil); ok {
		t.Fatal("hover must pass outside manifest files")
	}
}

func TestDepsIntentionOffersUpdate(t *testing.T) {
	deps.SetSnapshot(depsResult())
	t.Cleanup(func() { deps.SetSnapshot(deps.Result{}) })

	prov := depsIntention()
	items := prov.Items(intentionCtx("/p/go.mod", 5))
	if len(items) != 1 || items[0].CommandID != "deps.updateLatest" {
		t.Fatalf("items = %+v", items)
	}
	if got := items[0].Title; !containsStr(got, "v1.3.0") {
		t.Fatalf("title must name the latest version: %q", got)
	}
	// Up-to-date and vulnerable-but-current lines offer nothing.
	if items := prov.Items(intentionCtx("/p/go.mod", 6)); len(items) != 0 {
		t.Fatalf("line 7 has no update, items = %+v", items)
	}
}

func TestDepsVulnsDialogOpens(t *testing.T) {
	m := depsApp(t)
	out, _ := m.Update(depspanel.VulnsMsg{Dep: depsResult().Manifests[0].Deps[1]})
	m = out.(Model)
	if !m.depsVulnsDialogOpen() {
		t.Fatal("v must open the details dialog")
	}
	out, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = out.(Model)
	if m.depsVulnsDialogOpen() {
		t.Fatal("esc must close the details dialog")
	}
}

func TestDepsMissingToolsDialogOnManualScan(t *testing.T) {
	m := depsApp(t)
	res := deps.Result{Missing: []deps.MissingTool{{Provider: "go", Tool: deps.Tool{Name: "govulncheck", Hint: "go install …", Optional: true}}}}
	out, _ := m.Update(DepsScanMsg{Result: res, Manual: true})
	m = out.(Model)
	if !m.depsMissingDialogOpen() {
		t.Fatal("a manual scan with missing tools must raise the dialog")
	}
}

func intentionCtx(path string, line int) intention.Context {
	return intention.Context{Path: path, Line: line}
}

func containsStr(hay, needle string) bool { return strings.Contains(hay, needle) }
