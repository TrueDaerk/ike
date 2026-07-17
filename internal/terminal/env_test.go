package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteShimsGeneratesAndSweeps(t *testing.T) {
	dir := t.TempDir()
	ok, err := WriteShims(dir, []Mapping{
		{Lang: "python", Interpreter: "/opt/venv/bin/python"},
		{Lang: "php", Interpreter: "/opt/php83/bin/php"},
	})
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	for name, target := range map[string]string{
		"python":  "/opt/venv/bin/python",
		"python3": "/opt/venv/bin/python",
		"php":     "/opt/php83/bin/php",
	} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.Contains(string(data), "exec \""+target+"\" \"$@\"") {
			t.Fatalf("%s shim = %q", name, data)
		}
		info, _ := os.Stat(filepath.Join(dir, name))
		if info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s must be executable", name)
		}
	}

	// Dropping php regenerates without it (stale sweep).
	ok, err = WriteShims(dir, []Mapping{{Lang: "python", Interpreter: "/usr/bin/python3"}})
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "php")); !os.IsNotExist(err) {
		t.Fatal("stale php shim should be removed")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "python"))
	if !strings.Contains(string(data), "/usr/bin/python3") {
		t.Fatalf("shim should retarget, got %q", data)
	}

	// No mappings at all: everything swept, inactive.
	ok, err = WriteShims(dir, nil)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "python")); !os.IsNotExist(err) {
		t.Fatal("shims should be swept when no mapping remains")
	}
}

// makeVenv builds a virtualenv skeleton (pyvenv.cfg + bin/python{,3}) and
// returns its root and interpreter path.
func makeVenv(t *testing.T) (root, interp string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pyvenv.cfg"), []byte("home = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python", "python3"} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root, filepath.Join(root, "bin", "python3")
}

// firstPathHit mimics `which`: the first PATH entry holding name.
func firstPathHit(name, path string) string {
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// pathOf extracts the PATH value from an overlay.
func pathOf(t *testing.T, ov []string) string {
	t.Helper()
	for _, kv := range ov {
		if strings.HasPrefix(kv, "PATH=") {
			return kv[len("PATH="):]
		}
	}
	t.Fatalf("overlay carries no PATH: %v", ov)
	return ""
}

// TestPlanVenvActivation (#652): a venv mapping activates like `source
// bin/activate` — venv bin first on PATH, VIRTUAL_ENV set, no shim — and a
// which-equivalent lookup resolves both python3 and python inside the venv.
func TestPlanVenvActivation(t *testing.T) {
	venv, interp := makeVenv(t)
	m := []Mapping{{Lang: "python", Interpreter: interp, Source: "detected"}}
	p := PlanActivation(m, "/usr/bin")
	if len(p.Shims) != 0 {
		t.Fatalf("venv must not shim, got %v", p.Shims)
	}
	if len(p.Vars) != 1 || p.Vars[0] != "VIRTUAL_ENV="+venv {
		t.Fatalf("vars = %v", p.Vars)
	}
	if len(p.Active) != 1 {
		t.Fatalf("venv mapping should be active, got %v", p.Active)
	}
	ov := p.Overlay("/shims", "/usr/bin")
	path := pathOf(t, ov)
	if !strings.HasPrefix(path, filepath.Join(venv, "bin")+string(os.PathListSeparator)) {
		t.Fatalf("PATH must start with the venv bin, got %q", path)
	}
	if strings.Contains(path, "/shims") {
		t.Fatalf("no shim dir expected on PATH, got %q", path)
	}
	for _, name := range []string{"python3", "python"} {
		if hit := firstPathHit(name, path); hit != filepath.Join(venv, "bin", name) {
			t.Fatalf("which %s = %q, want the venv one", name, hit)
		}
	}
}

// TestPlanPrivateDirPrepend (#652): an interpreter in a private toolchain
// directory (pyenv-style versions dir) prepends that dir, no shim — for
// explicit and for detected-but-not-on-PATH alike.
func TestPlanPrivateDirPrepend(t *testing.T) {
	base := t.TempDir()
	bin := filepath.Join(base, ".pyenv", "versions", "3.12.1", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	interp := filepath.Join(bin, "python")
	if err := os.WriteFile(interp, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"config", "detected"} {
		p := PlanActivation([]Mapping{{Lang: "python", Interpreter: interp, Source: source}}, "/usr/bin")
		if len(p.Shims) != 0 {
			t.Fatalf("%s: private dir must not shim, got %v", source, p.Shims)
		}
		if len(p.Prepend) != 1 || p.Prepend[0] != bin {
			t.Fatalf("%s: prepend = %v, want [%s]", source, p.Prepend, bin)
		}
		path := pathOf(t, p.Overlay("/shims", "/usr/bin"))
		if !strings.HasPrefix(path, bin+string(os.PathListSeparator)) {
			t.Fatalf("%s: PATH = %q", source, path)
		}
	}
}

// TestPlanSharedDirModes (#652): a shared system dir keeps the shim fallback
// for explicit choices and injects nothing for detected ones.
func TestPlanSharedDirModes(t *testing.T) {
	// Explicit: shim mode, shim dir on PATH.
	p := PlanActivation([]Mapping{{Lang: "php", Interpreter: "/usr/bin/php", Source: "config"}}, "/opt/homebrew/bin:/usr/bin")
	if len(p.Shims) != 1 || p.Shims[0].Lang != "php" {
		t.Fatalf("explicit shared dir should shim, got %+v", p)
	}
	if len(p.Prepend) != 0 {
		t.Fatalf("shared dirs must never be prepended, got %v", p.Prepend)
	}
	path := pathOf(t, p.Overlay("/shims", "/opt/homebrew/bin:/usr/bin"))
	if !strings.HasPrefix(path, "/shims"+string(os.PathListSeparator)+"/opt/homebrew/bin") {
		t.Fatalf("shim dir should lead, base PATH order untouched: %q", path)
	}

	// Detected: no injection at all.
	p = PlanActivation([]Mapping{{Lang: "php", Interpreter: "/usr/bin/php", Source: "detected"}}, "/usr/bin")
	if ov := p.Overlay("/shims", "/usr/bin"); ov != nil {
		t.Fatalf("detected shared-dir interpreter must not inject, got %v", ov)
	}
	if len(p.Active) != 0 {
		t.Fatalf("nothing should be active, got %v", p.Active)
	}
}

// TestPlanDetectedAlreadyWinning (#652): a detected interpreter whose private
// dir already wins the base PATH lookup is a no-op — the environment is what
// PATH gives anyway.
func TestPlanDetectedAlreadyWinning(t *testing.T) {
	bin := t.TempDir()
	interp := filepath.Join(bin, "go")
	if err := os.WriteFile(interp, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	base := bin + string(os.PathListSeparator) + "/usr/bin"
	p := PlanActivation([]Mapping{{Lang: "go", Interpreter: interp, Source: "detected"}}, base)
	if ov := p.Overlay("/shims", base); ov != nil {
		t.Fatalf("already-winning detected interpreter must not inject, got %v", ov)
	}
	// The same path chosen explicitly still prepends (deterministic order).
	p = PlanActivation([]Mapping{{Lang: "go", Interpreter: interp, Source: "config"}}, base)
	if len(p.Prepend) != 1 || p.Prepend[0] != bin {
		t.Fatalf("explicit choice should prepend, got %v", p.Prepend)
	}
}

// TestPlanEmpty: no mappings → nil overlay, environment untouched.
func TestPlanEmpty(t *testing.T) {
	p := PlanActivation(nil, "/usr/bin")
	if ov := p.Overlay("/shims", "/usr/bin"); ov != nil {
		t.Fatalf("empty plan must not touch the env, got %v", ov)
	}
}

// TestShimSweptOnModeChange (#652): a language moving from shim mode to
// venv/prepend activation loses its stale shim files.
func TestShimSweptOnModeChange(t *testing.T) {
	dir := t.TempDir()
	p := PlanActivation([]Mapping{{Lang: "python", Interpreter: "/usr/bin/python3", Source: "config"}}, "/usr/bin")
	if ok, err := WriteShims(dir, p.Shims); err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "python3")); err != nil {
		t.Fatal("shim expected before the mode change")
	}

	// Settings now point into a venv: shim mode gone, files swept.
	_, interp := makeVenv(t)
	p = PlanActivation([]Mapping{{Lang: "python", Interpreter: interp, Source: "config"}}, "/usr/bin")
	if len(p.Shims) != 0 {
		t.Fatalf("venv must not shim, got %v", p.Shims)
	}
	if _, err := WriteShims(dir, p.Shims); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"python", "python3"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("stale %s shim should be swept on mode change", name)
		}
	}
}

func TestMergeEnvOverrides(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/home/x", "TERM=xterm"}
	got := MergeEnv(base, []string{"PATH=/shims:/usr/bin", "VIRTUAL_ENV=/venv"})
	joined := strings.Join(got, "\n")
	if strings.Count(joined, "PATH=") != 1 || !strings.Contains(joined, "PATH=/shims:/usr/bin") {
		t.Fatalf("merged = %v", got)
	}
	if !strings.Contains(joined, "HOME=/home/x") || !strings.Contains(joined, "VIRTUAL_ENV=/venv") {
		t.Fatalf("merged = %v", got)
	}
	if out := MergeEnv(base, nil); len(out) != len(base) {
		t.Fatal("nil overlay should keep the base as-is")
	}
}

// TestSessionEnvInjection: a spawned shell sees the overlay (end to end).
func TestSessionEnvInjection(t *testing.T) {
	c := &collector{}
	s, err := StartSession("terminal", "/bin/sh", t.TempDir(), 80, 24,
		[]string{"IKE_SHIM_MARK=active"}, c.send)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, r := range "echo mark=$IKE_SHIM_MARK\r" {
		s.SendKey(keyFor(r))
	}
	waitFor(t, "env echo", func() bool {
		return strings.Contains(plainView(s), "mark=active")
	})
}
