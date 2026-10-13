package main

import (
	"os/exec"
	"strings"
	"testing"
)

// complexityOnlyForbidden are the packages the complexity-only build
// (#2939) must not link: the UI runtime, the IDE model and host, the WASM
// plugin runtime and the IDE-only plugins. A prefix match, so a subpackage
// (ike/internal/wasm/abi, charm.land/bubbletea/v2) counts too.
var complexityOnlyForbidden = []string{
	"charm.land/bubbletea",
	"charm.land/bubbles",
	"ike/internal/app",
	"ike/internal/host",
	"ike/internal/wasm",
	"ike/internal/plugin",
	"ike/internal/settings",
	"ike/plugins/lsp",
	"ike/plugins/tasks",
	"ike/plugins/format",
}

// TestComplexityOnlyBuildDeps asserts the complexity-only build's dependency
// graph by asking the go tool itself, so it guards the tagged build from
// either build: `go list -deps -tags ike_complexity_only ./cmd/ike` must name
// none of complexityOnlyForbidden, and must name the engine it exists for.
func TestComplexityOnlyBuildDeps(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go tool not on PATH")
	}
	out, err := exec.Command(goBin, "list", "-deps", "-tags", "ike_complexity_only", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	deps := strings.Fields(string(out))
	seen := map[string]bool{}
	for _, d := range deps {
		seen[d] = true
		for _, bad := range complexityOnlyForbidden {
			if d == bad || strings.HasPrefix(d, bad+"/") {
				t.Errorf("complexity-only build links %s (forbidden: %s)", d, bad)
			}
		}
	}
	for _, need := range []string{"ike/internal/complexitycli", "ike/internal/complexity", "ike/plugins/languages/go"} {
		if !seen[need] {
			t.Errorf("complexity-only build does not link %s", need)
		}
	}
}
