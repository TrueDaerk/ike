package transport

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// resolve_test.go covers the post-install binary resolution (#370): PATH wins,
// well-known toolchain install directories are probed when PATH fails, and a
// binary that is nowhere keeps failing with the LookPath error.

// stubEnv routes toolchain queries to fixed answers and restores the real
// runner on cleanup.
func stubEnv(t *testing.T, gobin, gopath, npmPrefix string) {
	t.Helper()
	old := envOutput
	envOutput = func(name string, args ...string) (string, error) {
		switch {
		case name == "go" && len(args) == 2 && args[1] == "GOBIN":
			return gobin, nil
		case name == "go" && len(args) == 2 && args[1] == "GOPATH":
			return gopath, nil
		case name == "npm":
			return npmPrefix, nil
		}
		return "", errors.New("unexpected toolchain query")
	}
	t.Cleanup(func() { envOutput = old })
}

// writeBin drops an executable stub named command into dir.
func writeBin(t *testing.T, dir, command string) string {
	t.Helper()
	p := filepath.Join(dir, command)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolvePrefersPath(t *testing.T) {
	stubEnv(t, t.TempDir(), "", "")
	want, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("no cat on PATH")
	}
	got, err := Resolve("cat")
	if err != nil || got != want {
		t.Fatalf("a PATH hit must win untouched, got %q, %v (want %q)", got, err, want)
	}
}

func TestResolveFindsBinaryInGoBin(t *testing.T) {
	dir := t.TempDir()
	want := writeBin(t, dir, "fake-server-370")
	stubEnv(t, dir, "", "")
	got, err := Resolve("fake-server-370")
	if err != nil || got != want {
		t.Fatalf("a GOBIN binary off PATH must resolve by absolute path, got %q, %v", got, err)
	}
}

func TestResolveFindsBinaryInGopathBin(t *testing.T) {
	gopath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gopath, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeBin(t, filepath.Join(gopath, "bin"), "fake-server-370")
	stubEnv(t, "", gopath, "")
	got, err := Resolve("fake-server-370")
	if err != nil || got != want {
		t.Fatalf("with GOBIN unset, GOPATH/bin must be probed, got %q, %v", got, err)
	}
}

func TestResolveFindsBinaryInNpmPrefix(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("no npm on PATH — FallbackDirs skips its prefix")
	}
	prefix := t.TempDir()
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := writeBin(t, filepath.Join(prefix, "bin"), "fake-server-370")
	stubEnv(t, t.TempDir(), "", prefix)
	got, err := Resolve("fake-server-370")
	if err != nil || got != want {
		t.Fatalf("the npm global bin must be probed, got %q, %v", got, err)
	}
}

func TestResolveSkipsNonExecutable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fake-server-370"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	stubEnv(t, dir, "", "")
	if got, err := Resolve("fake-server-370"); err == nil {
		t.Fatalf("a non-executable file must not resolve, got %q", got)
	}
}

func TestResolveNotFoundKeepsLookPathError(t *testing.T) {
	stubEnv(t, t.TempDir(), "", "")
	_, err := Resolve("definitely-not-a-binary-370")
	if err == nil {
		t.Fatal("an unresolvable command must return an error")
	}
	var execErr *exec.Error
	if !errors.As(err, &execErr) {
		t.Fatalf("the LookPath error must be returned unchanged, got %v", err)
	}
}
