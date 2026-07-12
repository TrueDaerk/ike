package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/lang"
)

func init() {
	// The page lists languages carrying a toolchain or server; register a
	// minimal python so the env actions have their row (the real language
	// package would import-cycle through plugin -> settings).
	lang.Register(lang.Language{
		ID:     "python",
		Server: &lang.ServerSpec{Language: "python", Command: "pyright-langserver"},
	})
}

// fakeEnv builds runner/look fakes: look resolves only the given binaries,
// run records calls and executes an optional side effect.
type fakeEnv struct {
	binaries map[string]string
	calls    []string
	onRun    func(name string, args ...string) string
}

func (f *fakeEnv) look(name string) string { return f.binaries[name] }
func (f *fakeEnv) run(name string, args ...string) string {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.onRun != nil {
		return f.onRun(name, args...)
	}
	return ""
}

func mkInterp(t *testing.T, venv string) string {
	t.Helper()
	bin := filepath.Join(venv, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	interp := filepath.Join(bin, "python")
	if err := os.WriteFile(interp, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return interp
}

func TestCreateEnvPrefersUv(t *testing.T) {
	root := t.TempDir()
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv", "python3": "/bin/python3"}}
	f.onRun = func(name string, args ...string) string {
		mkInterp(t, filepath.Join(root, ".venv"))
		return ""
	}
	msg := createEnv(root, ".venv", f.run, f.look)()
	env := msg.(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	// No pyproject.toml in root: init is attempted first (#548), then venv.
	want := []string{"uv init --bare " + root, "uv venv " + filepath.Join(root, ".venv")}
	if len(f.calls) != 2 || f.calls[0] != want[0] || f.calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	if env.Interpreter != filepath.Join(root, ".venv", "bin", "python") {
		t.Fatalf("interpreter = %q", env.Interpreter)
	}
}

func TestCreateEnvFallsBackToPython(t *testing.T) {
	root := t.TempDir()
	f := &fakeEnv{binaries: map[string]string{"python3": "/bin/python3"}}
	f.onRun = func(string, ...string) string {
		mkInterp(t, filepath.Join(root, ".venv"))
		return ""
	}
	env := createEnv(root, ".venv", f.run, f.look)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	if f.calls[0] != "python3 -m venv "+filepath.Join(root, ".venv") {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestCreateEnvNoToolchain(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{}}
	env := createEnv(t.TempDir(), ".venv", f.run, f.look)().(EnvMsg)
	if env.Err == nil || env.Interpreter != "" {
		t.Fatalf("expected failure, got %+v", env)
	}
}

func TestUvInstallableParsesList(t *testing.T) {
	out := strings.Join([]string{
		"cpython-3.13.1-macos-aarch64-none    <download available>",
		"cpython-3.12.8-macos-aarch64-none    /opt/homebrew/bin/python3.12",
		"cpython-3.11.11-macos-aarch64-none   <download available>",
		"pypy-7.3.17-macos-aarch64-none       <download available>",
		"",
	}, "\n")
	got := uvInstallable(out)
	if len(got) != 2 || got[0] != "3.13.1" || got[1] != "3.11.11" {
		t.Fatalf("versions = %v", got)
	}
}

func TestUvInstallRegistersFoundInterpreter(t *testing.T) {
	dir := t.TempDir()
	managed := filepath.Join(dir, "cpython-3.13.1", "python")
	if err := os.MkdirAll(filepath.Dir(managed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managed, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := &fakeEnv{}
	f.onRun = func(name string, args ...string) string {
		if len(args) > 1 && args[1] == "find" {
			return managed + "\n"
		}
		return ""
	}
	env := uvInstall("3.13.1", f.run)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	if env.Interpreter != managed || !strings.Contains(env.Label, "3.13.1") {
		t.Fatalf("env = %+v", env)
	}
	if f.calls[0] != "uv python install 3.13.1" || f.calls[1] != "uv python find 3.13.1" {
		t.Fatalf("calls = %v", f.calls)
	}
}

// pythonPage builds a toolchain page whose python row is selected, with fakes.
func pythonPage(t *testing.T, f *fakeEnv) *ToolchainPage {
	t.Helper()
	p := NewToolchainPage(config.Options{}, t.TempDir(), nil)
	p.run, p.look = f.run, f.look
	for i, l := range p.languages() {
		if l.ID == "python" {
			p.sel = i
			return p
		}
	}
	t.Skip("no python language registered in this test binary")
	return nil
}

func TestToolchainPageEnvActions(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	f.onRun = func(name string, args ...string) string {
		if len(args) > 1 && args[1] == "list" {
			return "cpython-3.13.1-macos-aarch64-none <download available>\n"
		}
		return ""
	}
	p := pythonPage(t, f)

	// n opens the target input pre-filled with .venv (#547); enter creates.
	if cmd := p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"}); cmd != nil {
		t.Fatal("n should open the target input, not create yet")
	}
	if !p.envInput || p.envPath != ".venv" {
		t.Fatalf("input state = %v %q", p.envInput, p.envPath)
	}
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter should return the async create command")
	}
	if p.envInput || p.envState != envBusy {
		t.Fatalf("state after enter = %v %q", p.envInput, p.envState)
	}

	// u opens the uv picker; enter kicks the install.
	p.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if !p.uvPicking || len(p.uvVersions) != 1 {
		t.Fatalf("picker state = %v %v", p.uvPicking, p.uvVersions)
	}
	if cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter should return the install command")
	}

	// A result lands in the state line via Receive.
	p.Receive(EnvMsg{LangID: "python", Label: "created x", Interpreter: "/x"})
	if !strings.Contains(p.envState, "created x") {
		t.Fatalf("envState = %q", p.envState)
	}

	// Without uv, u reports instead of opening a picker.
	p2 := pythonPage(t, &fakeEnv{binaries: map[string]string{}})
	p2.Update(tea.KeyPressMsg{Code: 'u', Text: "u"})
	if p2.uvPicking || !strings.Contains(p2.envState, "uv not found") {
		t.Fatalf("state = %q picking=%v", p2.envState, p2.uvPicking)
	}
}

// TestCreateEnvCustomTargets guards #547: relative targets resolve against
// the project root, absolute and ~ targets are honored as typed.
func TestCreateEnvCustomTargets(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "envs", "proj")
	home := t.TempDir()
	t.Setenv("HOME", home)

	cases := []struct{ target, want string }{
		{"envs/dev", filepath.Join(root, "envs", "dev")},
		{abs, abs},
		{"~/venvs/x", filepath.Join(home, "venvs", "x")},
	}
	for _, c := range cases {
		f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
		f.onRun = func(string, ...string) string {
			mkInterp(t, c.want)
			return ""
		}
		env := createEnv(root, c.target, f.run, f.look)().(EnvMsg)
		if env.Err != nil {
			t.Fatalf("target %q: %v", c.target, env.Err)
		}
		venvCall := false
		for _, call := range f.calls {
			if call == "uv venv "+c.want {
				venvCall = true
			}
		}
		if !venvCall {
			t.Fatalf("target %q: calls = %v, want uv venv %s", c.target, f.calls, c.want)
		}
		if env.Interpreter != filepath.Join(c.want, "bin", "python") {
			t.Fatalf("target %q: interpreter = %q", c.target, env.Interpreter)
		}
	}
}

// TestEnvInputPathCompletion guards the target input's completion and cancel.
func TestEnvInputPathCompletion(t *testing.T) {
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	p := pythonPage(t, f)
	if err := os.Mkdir(filepath.Join(p.root, "environments"), 0o755); err != nil {
		t.Fatal(err)
	}
	p.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})

	// Replace the prefill with an absolute prefix and complete it.
	for p.envPath != "" {
		p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	for _, r := range filepath.Join(p.root, "env") {
		p.Update(tea.KeyPressMsg{Text: string(r), Code: r})
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if want := filepath.Join(p.root, "environments") + string(filepath.Separator); p.envPath != want {
		t.Fatalf("envPath after tab = %q, want %q", p.envPath, want)
	}

	// Esc cancels without creating.
	p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if p.envInput || p.envState == envBusy || len(f.calls) != 0 {
		t.Fatalf("esc must cancel: input=%v state=%q calls=%v", p.envInput, p.envState, f.calls)
	}
}

// TestCreateEnvScaffoldsUvProject guards #548: on the uv path, a missing
// pyproject.toml is generated (uv init --bare) and a missing uv.lock is
// locked (uv lock); the label names what was created.
func TestCreateEnvScaffoldsUvProject(t *testing.T) {
	root := t.TempDir()
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	f.onRun = func(name string, args ...string) string {
		switch args[0] {
		case "init":
			os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\n"), 0o644)
		case "venv":
			mkInterp(t, filepath.Join(root, ".venv"))
		case "lock":
			os.WriteFile(filepath.Join(root, "uv.lock"), []byte("version = 1\n"), 0o644)
		}
		return ""
	}
	env := createEnv(root, ".venv", f.run, f.look)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	want := []string{
		"uv init --bare " + root,
		"uv venv " + filepath.Join(root, ".venv"),
		"uv lock --directory " + root,
	}
	if len(f.calls) != 3 || f.calls[0] != want[0] || f.calls[1] != want[1] || f.calls[2] != want[2] {
		t.Fatalf("calls = %v, want %v", f.calls, want)
	}
	if !strings.Contains(env.Label, "pyproject.toml") || !strings.Contains(env.Label, "uv.lock") {
		t.Fatalf("label = %q, must name the scaffolded files", env.Label)
	}
}

// TestCreateEnvKeepsExistingManifest: an existing pyproject.toml is never
// re-initialized; only the missing uv.lock is generated.
func TestCreateEnvKeepsExistingManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pyproject.toml"), []byte("[project]\nname='x'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	f.onRun = func(name string, args ...string) string {
		switch args[0] {
		case "venv":
			mkInterp(t, filepath.Join(root, ".venv"))
		case "lock":
			os.WriteFile(filepath.Join(root, "uv.lock"), []byte("version = 1\n"), 0o644)
		}
		return ""
	}
	env := createEnv(root, ".venv", f.run, f.look)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	for _, call := range f.calls {
		if strings.HasPrefix(call, "uv init") {
			t.Fatalf("existing pyproject.toml must not be re-initialized: %v", f.calls)
		}
	}
	if !strings.Contains(env.Label, "uv.lock") || strings.Contains(env.Label, "pyproject.toml") {
		t.Fatalf("label = %q", env.Label)
	}
}

// TestCreateEnvSkipsExistingLock: both files present — only the venv runs.
func TestCreateEnvSkipsExistingLock(t *testing.T) {
	root := t.TempDir()
	for _, file := range []string{"pyproject.toml", "uv.lock"} {
		if err := os.WriteFile(filepath.Join(root, file), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f := &fakeEnv{binaries: map[string]string{"uv": "/bin/uv"}}
	f.onRun = func(name string, args ...string) string {
		mkInterp(t, filepath.Join(root, ".venv"))
		return ""
	}
	env := createEnv(root, ".venv", f.run, f.look)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	if len(f.calls) != 1 || f.calls[0] != "uv venv "+filepath.Join(root, ".venv") {
		t.Fatalf("calls = %v, want only the venv", f.calls)
	}
	if env.Label != "created "+filepath.Join(root, ".venv") {
		t.Fatalf("label = %q", env.Label)
	}
}

// TestCreateEnvFallbackNoScaffold: without uv, no scaffolding happens.
func TestCreateEnvFallbackNoScaffold(t *testing.T) {
	root := t.TempDir()
	f := &fakeEnv{binaries: map[string]string{"python3": "/bin/python3"}}
	f.onRun = func(string, ...string) string {
		mkInterp(t, filepath.Join(root, ".venv"))
		return ""
	}
	env := createEnv(root, ".venv", f.run, f.look)().(EnvMsg)
	if env.Err != nil {
		t.Fatal(env.Err)
	}
	if len(f.calls) != 1 || !strings.HasPrefix(f.calls[0], "python3 -m venv") {
		t.Fatalf("calls = %v", f.calls)
	}
	if fileExists(filepath.Join(root, "pyproject.toml")) {
		t.Fatal("fallback must not scaffold")
	}
}
