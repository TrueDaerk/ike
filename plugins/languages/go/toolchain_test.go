package langgo

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestInterpreterFallsBackToWellKnownLocations guards #538: when go is not on
// the process PATH (GUI-launched sessions), detection probes the common
// install locations instead of reporting "not found".
func TestInterpreterFallsBackToWellKnownLocations(t *testing.T) {
	prevLook, prevFallbacks := goLook, goFallbacks
	t.Cleanup(func() { goLook, goFallbacks = prevLook, prevFallbacks })

	fake := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(fake, []byte("#!"), 0o755); err != nil {
		t.Fatal(err)
	}
	goLook = func(string) (string, error) { return "", errors.New("not on PATH") }
	goFallbacks = []string{filepath.Join(t.TempDir(), "missing", "go"), fake}

	p, ok := (toolchain{}).Interpreter(".")
	if !ok || p != fake {
		t.Fatalf("Interpreter = %q %v, want fallback %q", p, ok, fake)
	}

	// PATH wins when available.
	goLook = func(string) (string, error) { return "/path/go", nil }
	if p, ok := (toolchain{}).Interpreter("."); !ok || p != "/path/go" {
		t.Fatalf("PATH hit must win, got %q %v", p, ok)
	}

	// Nothing anywhere: not found.
	goLook = func(string) (string, error) { return "", errors.New("nope") }
	goFallbacks = nil
	if _, ok := (toolchain{}).Interpreter("."); ok {
		t.Fatal("no binary anywhere must report not found")
	}
}

// TestInterpreterResolvesShims guards #650: a PATH hit that is a
// version-manager shim is resolved to the real executable; resolution failure
// keeps the shim.
func TestInterpreterResolvesShims(t *testing.T) {
	prevLook, prevResolve := goLook, goResolve
	t.Cleanup(func() { goLook, goResolve = prevLook, prevResolve })

	goLook = func(string) (string, error) { return "/home/u/.asdf/shims/go", nil }
	goResolve = func(root, p string) string {
		if root != "/proj" {
			t.Errorf("resolve root = %q, want /proj", root)
		}
		if p != "/home/u/.asdf/shims/go" {
			t.Errorf("resolve path = %q", p)
		}
		return "/home/u/.asdf/installs/golang/1.22.4/go/bin/go"
	}
	if p, ok := (toolchain{}).Interpreter("/proj"); !ok || p != "/home/u/.asdf/installs/golang/1.22.4/go/bin/go" {
		t.Fatalf("Interpreter = %q %v, want resolved shim", p, ok)
	}

	// Identity resolver (resolution failed): shim path survives.
	goResolve = func(_, p string) string { return p }
	if p, ok := (toolchain{}).Interpreter("/proj"); !ok || p != "/home/u/.asdf/shims/go" {
		t.Fatalf("Interpreter = %q %v, want shim unchanged", p, ok)
	}
}
