package main

import (
	"strings"
	"testing"

	"ike/internal/keymap"
	"ike/internal/registry"
)

// TestBindingMatrixFullyResolved is 0081/50's final gate, run against the
// exact plugin set this binary compiles in (main.go's blank imports): every
// default binding row is live with a reachable path, or honestly blocked
// with its dependency recorded. An UNRESOLVED row fails the roadmap.
func TestBindingMatrixFullyResolved(t *testing.T) {
	exists := func(id string) bool {
		_, ok := registry.Global().Command(id)
		return ok
	}
	rows := keymap.StatusMatrix(exists)
	if len(rows) == 0 {
		t.Fatal("matrix should list the default bindings")
	}
	for _, r := range rows {
		if !r.Resolved() {
			t.Errorf("UNRESOLVED %s: primary=%s class=%s live=%v fallback=%q",
				r.Command, r.Primary, r.Class, r.Live, r.Fallback)
		}
	}
	md := keymap.MatrixMarkdown(rows)
	if strings.Contains(md, "UNRESOLVED") {
		t.Error("persisted matrix must not contain unresolved rows")
	}
	if strings.Count(md, "\n") < len(rows) {
		t.Error("markdown should render one line per row")
	}
	t.Logf("matrix rows: %d", len(rows))
}
