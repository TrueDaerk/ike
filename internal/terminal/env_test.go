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

func TestEnvOverlayComposition(t *testing.T) {
	// No mappings: untouched environment.
	if ov := EnvOverlay("/shims", nil, "/usr/bin"); ov != nil {
		t.Fatalf("no mappings must not touch the env, got %v", ov)
	}

	// Plain interpreter (no venv): shim dir precedes the base PATH only.
	ov := EnvOverlay("/shims", []Mapping{{Lang: "php", Interpreter: "/opt/php"}}, "/usr/bin:/bin")
	if len(ov) != 1 || ov[0] != "PATH=/shims:/usr/bin:/bin" {
		t.Fatalf("overlay = %v", ov)
	}

	// venv python: VIRTUAL_ENV plus the venv bin on PATH.
	venv := t.TempDir()
	if err := os.MkdirAll(filepath.Join(venv, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(venv, "pyvenv.cfg"), []byte("home = x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	interp := filepath.Join(venv, "bin", "python")
	ov = EnvOverlay("/shims", []Mapping{{Lang: "python", Interpreter: interp}}, "/usr/bin")
	want := "PATH=/shims" + string(os.PathListSeparator) + filepath.Join(venv, "bin") + string(os.PathListSeparator) + "/usr/bin"
	if len(ov) != 2 || ov[0] != "VIRTUAL_ENV="+venv || ov[1] != want {
		t.Fatalf("overlay = %v", ov)
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
