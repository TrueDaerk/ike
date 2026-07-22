package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/lang"
)

// pythonpkg_test.go covers package management on the toolchain page (#571):
// backend selection, command construction, outdated parsing, and the key
// flow (install input, uninstall confirm, refresh).

func TestDetectPkgBackend(t *testing.T) {
	root := t.TempDir()
	interpInside := filepath.Join(root, ".venv", "bin", "python")
	uvLook := func(name string) string {
		if name == "uv" {
			return "/bin/uv"
		}
		return ""
	}
	noUv := func(string) string { return "" }

	if got := detectPkgBackend(root, interpInside, noUv); got != pkgBackendPip {
		t.Fatalf("no uv on PATH = %v, want pip", got)
	}
	if got := detectPkgBackend(root, interpInside, uvLook); got != pkgBackendUvPip {
		t.Fatalf("no manifest = %v, want uv pip", got)
	}
	for _, f := range []string{"pyproject.toml", "uv.lock"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := detectPkgBackend(root, interpInside, uvLook); got != pkgBackendUvProject {
		t.Fatalf("uv project = %v, want uv project", got)
	}
	// An interpreter OUTSIDE the project never routes through the manifest.
	if got := detectPkgBackend(root, "/elsewhere/bin/python", uvLook); got != pkgBackendUvPip {
		t.Fatalf("outside interpreter = %v, want uv pip", got)
	}
}

func TestPkgCommandsPerBackend(t *testing.T) {
	const root, interp = "/proj", "/proj/.venv/bin/python"
	flat := func(cmds []pkgCmd) string {
		var parts []string
		for _, c := range cmds {
			parts = append(parts, c.bin+" "+strings.Join(c.args, " "))
		}
		return strings.Join(parts, " && ")
	}
	cases := []struct {
		backend pkgBackend
		action  pkgAction
		want    string
	}{
		{pkgBackendUvProject, pkgInstall, "uv add --directory /proj requests==2.0"},
		{pkgBackendUvProject, pkgUninstall, "uv remove --directory /proj requests==2.0"},
		{pkgBackendUvProject, pkgUpgrade, "uv lock --directory /proj --upgrade-package requests==2.0 && uv sync --directory /proj"},
		{pkgBackendUvPip, pkgInstall, "uv pip install --python /proj/.venv/bin/python requests==2.0"},
		{pkgBackendUvPip, pkgUninstall, "uv pip uninstall --python /proj/.venv/bin/python requests==2.0"},
		{pkgBackendUvPip, pkgUpgrade, "uv pip install --python /proj/.venv/bin/python --upgrade requests==2.0"},
		{pkgBackendPip, pkgInstall, "/proj/.venv/bin/python -m pip install requests==2.0"},
		{pkgBackendPip, pkgUninstall, "/proj/.venv/bin/python -m pip uninstall -y requests==2.0"},
		{pkgBackendPip, pkgUpgrade, "/proj/.venv/bin/python -m pip install --upgrade requests==2.0"},
	}
	for _, c := range cases {
		if got := flat(pkgCommands(c.backend, c.action, "requests==2.0", root, interp)); got != c.want {
			t.Errorf("backend %v action %v:\n got %q\nwant %q", c.backend, c.action, got, c.want)
		}
	}
}

func TestParseOutdated(t *testing.T) {
	out := `[{"name":"Requests","version":"2.31.0","latest_version":"2.32.3"},
	{"name":"typing_extensions","version":"4.0.0","latest_version":"4.12.0"}]`
	got := parseOutdated(out)
	if got["requests"] != "2.32.3" || got["typing-extensions"] != "4.12.0" {
		t.Fatalf("parseOutdated = %v", got)
	}
	if len(parseOutdated("nonsense")) != 0 || len(parseOutdated("")) != 0 {
		t.Fatal("malformed output must degrade to empty")
	}
}

func TestNormalizeAndBaseName(t *testing.T) {
	if normalizePkg("Typing._Extensions") != "typing-extensions" {
		t.Fatalf("normalizePkg = %q", normalizePkg("Typing._Extensions"))
	}
	for spec, want := range map[string]string{
		"requests==2.0":  "requests",
		"requests>=1":    "requests",
		"plain":          "plain",
		"pkg~=1.0":       "pkg",
	} {
		if got := pkgBaseName(spec); got != want {
			t.Errorf("pkgBaseName(%q) = %q, want %q", spec, got, want)
		}
	}
}

func TestDecisiveLine(t *testing.T) {
	out := "Collecting nope\n  ERROR: No matching distribution found for nope\n"
	if got := decisiveLine(out); !strings.Contains(got, "ERROR: No matching") {
		t.Fatalf("decisiveLine = %q", got)
	}
	if got := decisiveLine("a\nb\n\n"); got != "b" {
		t.Fatalf("last non-empty = %q", got)
	}
	if decisiveLine("") != "" {
		t.Fatal("empty output stays empty")
	}
}

// pkgTestPage builds a page whose selected row is python with an explicit
// fake toolchain interpreter, plus recording runners.
func pkgTestPage(t *testing.T, interp string) (*ToolchainPage, *[]string) {
	t.Helper()
	restoreConfig(t)
	// Override the package-level python registration (Server-only) with a
	// toolchain that detects our interpreter; restore it afterwards so the
	// other tests keep their expectations.
	lang.Register(lang.Language{ID: "python", Toolchain: fakeTC{detected: interp},
		Server: &lang.ServerSpec{Language: "python", Command: "pyright-langserver"}})
	t.Cleanup(func() {
		lang.Register(lang.Language{ID: "python",
			Server: &lang.ServerSpec{Language: "python", Command: "pyright-langserver"}})
	})
	p := NewToolchainPage(testOpts(t), t.TempDir(), nil)
	calls := &[]string{}
	p.run = func(name string, args ...string) string {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		if strings.Contains(strings.Join(args, " "), "freeze") {
			return "alpha==1.0\nbravo==2.0\n"
		}
		return ""
	}
	p.runErr = func(_ context.Context, name string, args ...string) (string, error) {
		*calls = append(*calls, "! "+name+" "+strings.Join(args, " "))
		return "", nil
	}
	p.look = func(string) string { return "" } // no uv: pip backend
	for i, r := range p.rows() {
		if r.lang.ID == "python" {
			p.sel = i
		}
	}
	return p, calls
}

func TestPkgViewKeyFlow(t *testing.T) {
	interp := "/fake/bin/python"
	p, calls := pkgTestPage(t, interp)

	// `i` opens the view and fires the listing + outdated fetches.
	cmd := p.Update(key("i"))
	if !p.pkgViewing || cmd == nil {
		t.Fatal("i must open the package view with fetch commands")
	}
	p.Receive(PackagesMsg{Path: interp, Pkgs: []pkgInfo{{Name: "alpha", Version: "1.0"}, {Name: "bravo", Version: "2.0"}}})
	p.Receive(OutdatedMsg{Path: interp, Latest: map[string]string{"alpha": "1.5"}})

	// The latest column marks the upgradable row.
	view := strings.Join(p.renderPackages(), "\n")
	if !strings.Contains(view, "↑ 1.5") {
		t.Fatalf("upgradable row must show the latest marker:\n%s", view)
	}

	// `+` opens the install input; typing + enter launches the action.
	p.Update(key("+"))
	if p.pkgMode != "input" {
		t.Fatal("+ must open the install input")
	}
	for _, r := range "zulu==9" {
		p.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	cmd = p.Update(key("enter"))
	if cmd == nil || p.pkgBusy == "" {
		t.Fatal("enter must launch the install and mark busy")
	}
	msg := cmd() // run synchronously: pip backend install + refresh
	am, ok := msg.(PkgActionMsg)
	if !ok || am.Err != nil {
		t.Fatalf("action msg = %#v", msg)
	}
	joined := strings.Join(*calls, "\n")
	if !strings.Contains(joined, "! /fake/bin/python -m pip install zulu==9") {
		t.Fatalf("pip install call missing:\n%s", joined)
	}
	if len(am.Pkgs) == 0 {
		t.Fatal("success must carry the refreshed listing")
	}
	p.Receive(am)
	if p.pkgBusy != "" || !strings.Contains(p.pkgState, "✓ install zulu==9") {
		t.Fatalf("state after action = busy %q, state %q", p.pkgBusy, p.pkgState)
	}

	// `-` asks for confirmation; `y` uninstalls the selection.
	p.pkgSel = 0
	p.Update(key("-"))
	if p.pkgMode != "confirm" {
		t.Fatal("- must ask for confirmation")
	}
	cmd = p.Update(key("y"))
	if cmd == nil {
		t.Fatal("y must launch the uninstall")
	}
	_ = cmd()
	if !strings.Contains(strings.Join(*calls, "\n"), "! /fake/bin/python -m pip uninstall -y alpha") {
		t.Fatalf("pip uninstall call missing:\n%s", strings.Join(*calls, "\n"))
	}

	// `u` upgrades the selection.
	p.pkgBusy = ""
	cmd = p.Update(key("u"))
	if cmd == nil {
		t.Fatal("u must launch the upgrade")
	}
	_ = cmd()
	if !strings.Contains(strings.Join(*calls, "\n"), "! /fake/bin/python -m pip install --upgrade alpha") {
		t.Fatalf("pip upgrade call missing:\n%s", strings.Join(*calls, "\n"))
	}
}

func TestPkgActionFailureShowsDecisiveLine(t *testing.T) {
	interp := "/fake/bin/python"
	p, _ := pkgTestPage(t, interp)
	p.runErr = func(_ context.Context, _ string, _ ...string) (string, error) {
		return "Collecting nope\nERROR: No matching distribution found for nope\n", os.ErrNotExist
	}
	p.Update(key("i"))
	p.Receive(PackagesMsg{Path: interp, Pkgs: []pkgInfo{{Name: "alpha", Version: "1.0"}}})
	p.Update(key("+"))
	for _, r := range "nope" {
		p.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	cmd := p.Update(key("enter"))
	p.Receive(cmd().(PkgActionMsg))
	if !strings.Contains(p.pkgState, "✗ install nope") || !strings.Contains(p.pkgState, "No matching distribution") {
		t.Fatalf("failure state = %q", p.pkgState)
	}
	if p.pkgBusy != "" {
		t.Fatal("failure must clear the busy marker")
	}
}
