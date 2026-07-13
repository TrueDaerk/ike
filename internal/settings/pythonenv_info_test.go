package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// mkVenv builds a venv-shaped directory with a pyvenv.cfg and returns the
// interpreter path.
func mkVenv(t *testing.T, cfg string) string {
	t.Helper()
	venv := t.TempDir()
	interp := mkInterp(t, venv)
	if err := os.WriteFile(filepath.Join(venv, "pyvenv.cfg"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return interp
}

func TestEnvProvenance(t *testing.T) {
	uvVenv := mkVenv(t, "home = /usr/bin\nversion = 3.12.8\nuv = 0.5.9\n")
	plain := mkVenv(t, "home = /usr/bin\nversion = 3.12.8\n")
	cases := []struct{ interp, want string }{
		{"", ""},
		{uvVenv, "uv venv"},
		{plain, "venv"},
		{"/Users/x/.local/share/uv/python/cpython-3.13.1/bin/python", "uv managed"},
		{"/Users/x/.pyenv/shims/python", "pyenv"},
		{"/usr/local/bin/python3.13", "system"},
	}
	for _, c := range cases {
		if got := envProvenance(c.interp); got != c.want {
			t.Errorf("envProvenance(%q) = %q, want %q", c.interp, got, c.want)
		}
	}
}

func TestParseFreeze(t *testing.T) {
	out := strings.Join([]string{
		"# comment",
		"pip==24.2",
		"pycairo==1.27.0",
		"mytool @ file:///Users/x/dev/mytool",
		"-e /Users/x/dev/other",
		"",
	}, "\n")
	pkgs := parseFreeze(out)
	if len(pkgs) != 3 {
		t.Fatalf("pkgs = %v", pkgs)
	}
	if pkgs[0].Name != "pip" || pkgs[0].Version != "24.2" {
		t.Fatalf("pkgs[0] = %v", pkgs[0])
	}
	if pkgs[2].Name != "mytool" || pkgs[2].Version != "(local)" {
		t.Fatalf("pkgs[2] = %v", pkgs[2])
	}
}

func TestListPackagesPrefersUv(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	f.onRun = func(name string, args ...string) string {
		if name == "uv" {
			return "pip==24.2\nwheel==0.44.0\n"
		}
		return ""
	}
	msg := listPackages("/venv/bin/python", f.run, f.look)().(PackagesMsg)
	if msg.Err != nil || len(msg.Pkgs) != 2 {
		t.Fatalf("msg = %+v", msg)
	}
	if f.calls[0] != "uv pip list --python /venv/bin/python --format freeze" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestListPackagesFallsBackToPip(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{}}
	f.onRun = func(name string, args ...string) string {
		if name == "/venv/bin/python" {
			return "pip==24.2\n"
		}
		return ""
	}
	msg := listPackages("/venv/bin/python", f.run, f.look)().(PackagesMsg)
	if msg.Err != nil || len(msg.Pkgs) != 1 {
		t.Fatalf("msg = %+v", msg)
	}
	if f.calls[0] != "/venv/bin/python -m pip list --format=freeze" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestListPackagesReportsEmpty(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{}}
	msg := listPackages("/venv/bin/python", f.run, f.look)().(PackagesMsg)
	if msg.Err == nil {
		t.Fatal("empty listing must surface an error")
	}
}

func TestUvVersionsAllDedupes(t *testing.T) {
	out := strings.Join([]string{
		"cpython-3.13.1-macos-aarch64-none    <download available>",
		"cpython-3.13.1-macos-x86_64-none     <download available>",
		"cpython-3.12.8-macos-aarch64-none    /opt/homebrew/bin/python3.12",
		"pypy-7.3.17-macos-aarch64-none       <download available>",
	}, "\n")
	got := uvVersionsAll(out)
	if len(got) != 2 || got[0] != "3.13.1" || got[1] != "3.12.8" {
		t.Fatalf("versions = %v", got)
	}
}

// TestCreateEnvWithUvVersion: the wizard's uv version choice lands as
// --python.
func TestCreateEnvWithUvVersion(t *testing.T) {
	root := t.TempDir()
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	f.onRun = func(name string, args ...string) string {
		if args[0] == "venv" {
			mkInterp(t, filepath.Join(root, ".venv"))
		}
		return ""
	}
	env := createEnvWith(root, envSpec{Tool: "uv", Python: "3.12.8", Target: ".venv"}, f.run, f.look)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	want := "uv venv --python 3.12.8 " + filepath.Join(root, ".venv")
	found := false
	for _, call := range f.calls {
		if call == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("calls = %v, want %q", f.calls, want)
	}
	if !strings.Contains(env.Label, "python 3.12.8") {
		t.Fatalf("label = %q", env.Label)
	}
}

// TestCreateEnvWithBaseInterpreter: the wizard's venv base choice runs
// <base> -m venv.
func TestCreateEnvWithBaseInterpreter(t *testing.T) {
	root := t.TempDir()
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}} // uv present but venv chosen
	f.onRun = func(string, ...string) string {
		mkInterp(t, filepath.Join(root, ".venv"))
		return ""
	}
	env := createEnvWith(root, envSpec{Tool: "venv", Python: "/opt/homebrew/bin/python3.13", Target: ".venv"}, f.run, f.look)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	if len(f.calls) != 1 || f.calls[0] != "/opt/homebrew/bin/python3.13 -m venv "+filepath.Join(root, ".venv") {
		t.Fatalf("calls = %v", f.calls)
	}
	if !strings.Contains(env.Label, "python3.13 -m venv") {
		t.Fatalf("label = %q", env.Label)
	}
}

// TestWizardToolStep: with uv and python3 both present, n asks for the tool
// first; picking venv leads to the base-interpreter step.
func TestWizardToolStep(t *testing.T) {
	// Discovery stats candidate paths, so the fake binaries must exist.
	py3 := mkInterp(t, t.TempDir())
	py := mkInterp(t, t.TempDir())
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv", "python3": py3, "python": py}}
	p := pythonPage(t, f)
	p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if p.wizStep != 1 || len(p.wizTools) != 2 || p.wizTools[0] != "uv" || p.wizTools[1] != "venv" {
		t.Fatalf("wizard = step %d tools %v", p.wizStep, p.wizTools)
	}
	// Pick venv: candidates come from discovery (PATH python3 + python).
	p.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.wizTool != "venv" || p.wizStep != 2 || len(p.wizPys) != 2 {
		t.Fatalf("wizard = tool %q step %d pys %v", p.wizTool, p.wizStep, p.wizPys)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !p.envInput || p.wizPython != py3 {
		t.Fatalf("input=%v python=%q", p.envInput, p.wizPython)
	}
	// Esc from the wizard resets everything.
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.envInput || p.wizStep != 0 || p.Capturing() {
		t.Fatalf("esc must reset: input=%v step=%d", p.envInput, p.wizStep)
	}
}

// TestWizardNoToolchain: nothing on PATH — n reports instead of opening.
func TestWizardNoToolchain(t *testing.T) {
	p := pythonPage(t, &fakeEnv{binaries: map[string]string{}})
	p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if p.wizStep != 0 || p.envInput || !strings.Contains(p.envState, "neither uv nor python") {
		t.Fatalf("state = step %d input %v %q", p.wizStep, p.envInput, p.envState)
	}
}

// TestPackageView: i fetches the effective interpreter's packages, the
// result renders, j scrolls, esc closes.
func TestPackageView(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{"python3": "/bin/python3"}}
	p := pythonPage(t, f)
	cmd := p.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if cmd == nil && p.pkgViewing {
		t.Fatal("i with an interpreter must fetch async")
	}
	if !p.pkgViewing {
		// No effective interpreter in this sandbox — simulate the open.
		p.pkgViewing = true
	}
	p.Receive(PackagesMsg{Path: "/x", Pkgs: []pkgInfo{{Name: "pip", Version: "24.2"}, {Name: "wheel", Version: "0.44.0"}}})
	if len(p.pkgs) != 2 || !p.Capturing() {
		t.Fatalf("pkgs = %v capturing=%v", p.pkgs, p.Capturing())
	}
	view := strings.Join(p.renderPackages(), "\n")
	if !strings.Contains(view, "pip") || !strings.Contains(view, "24.2") {
		t.Fatalf("view = %q", view)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.pkgViewing {
		t.Fatal("esc must close the package view")
	}
}
