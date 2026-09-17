package langweb

import (
	"testing"

	"ike/internal/lang"
)

// TestTypeScriptDeclaresDepMarkers pins the npm dependency markers (#2613).
// The three lock-file flavours cover npm, yarn and pnpm; node_modules/
// .package-lock.json is the one inside the pruned tree, and it is what an
// `npm install` rewrites when package.json itself did not change. tsserver
// re-reads module resolution on the notification, so no restart.
func TestTypeScriptDeclaresDepMarkers(t *testing.T) {
	l, ok := lang.ByID("typescript")
	if !ok || l.Deps == nil {
		t.Fatal("typescript declares no dependency markers")
	}
	want := map[string]bool{
		"package.json":                    true,
		"package-lock.json":               true,
		"yarn.lock":                       true,
		"pnpm-lock.yaml":                  true,
		"node_modules/.package-lock.json": true,
	}
	for _, f := range l.Deps.Files {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("missing markers %v in %v", want, l.Deps.Files)
	}
	if l.Deps.Restart {
		t.Error("typescript must notify only: tsserver re-reads module resolution on didChangeWatchedFiles")
	}
}
