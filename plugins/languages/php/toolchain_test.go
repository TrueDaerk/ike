package langphp

import (
	"errors"
	"testing"
)

// TestInterpreterResolvesShims guards #650: a PATH hit that is a
// version-manager shim is resolved to the real executable.
func TestInterpreterResolvesShims(t *testing.T) {
	prevLook, prevResolve := phpLook, phpResolve
	t.Cleanup(func() { phpLook, phpResolve = prevLook, prevResolve })

	phpLook = func(string) (string, error) { return "/home/u/.local/share/mise/shims/php", nil }
	phpResolve = func(root, p string) string {
		if root != "/proj" {
			t.Errorf("resolve root = %q, want /proj", root)
		}
		return "/home/u/.local/share/mise/installs/php/8.3.8/bin/php"
	}
	if p, ok := (toolchain{}).Interpreter("/proj"); !ok || p != "/home/u/.local/share/mise/installs/php/8.3.8/bin/php" {
		t.Fatalf("Interpreter = %q %v, want resolved shim", p, ok)
	}

	// PATH miss: fallbacks are untouched by the resolver.
	phpLook = func(string) (string, error) { return "", errors.New("not found") }
	phpResolve = func(_, p string) string {
		t.Errorf("resolver must not run for fallback path %q", p)
		return p
	}
	(toolchain{}).Interpreter("/proj") // result depends on the host; only the resolver call matters
}
