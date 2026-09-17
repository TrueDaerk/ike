package langgo

import (
	"testing"

	"ike/internal/lang"
)

// TestGoDeclaresDepMarkers pins the Go dependency markers (#2613): a `go get`
// or `go mod tidy` outside IKE rewrites go.mod/go.sum, and gopls re-resolves
// the module graph from the didChangeWatchedFiles notification alone — so the
// declaration must name both files and must *not* ask for a restart.
func TestGoDeclaresDepMarkers(t *testing.T) {
	l, ok := lang.ByID("go")
	if !ok || l.Deps == nil {
		t.Fatal("go declares no dependency markers")
	}
	want := map[string]bool{"go.mod": true, "go.sum": true}
	for _, f := range l.Deps.Files {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("missing markers %v in %v", want, l.Deps.Files)
	}
	if l.Deps.Restart {
		t.Error("go must notify only: gopls re-indexes on didChangeWatchedFiles")
	}
	if l.Deps.Dirs != nil {
		t.Error("go needs no toolchain-resolved marker directory")
	}
}
