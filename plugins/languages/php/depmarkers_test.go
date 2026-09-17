package langphp

import (
	"testing"

	"ike/internal/lang"
)

// TestPHPDeclaresDepMarkers pins the Composer dependency markers (#2613).
// vendor/composer/installed.json is the load-bearing one: it is the file a
// `composer update` rewrites last, and it sits inside the vendor tree the
// recursive project watch prunes — without its own marker watch no event for
// it exists at all. Intelephense re-indexes on the notification; no restart.
func TestPHPDeclaresDepMarkers(t *testing.T) {
	l, ok := lang.ByID("php")
	if !ok || l.Deps == nil {
		t.Fatal("php declares no dependency markers")
	}
	want := map[string]bool{
		"composer.json":                  true,
		"composer.lock":                  true,
		"vendor/composer/installed.json": true,
	}
	for _, f := range l.Deps.Files {
		delete(want, f)
	}
	if len(want) != 0 {
		t.Errorf("missing markers %v in %v", want, l.Deps.Files)
	}
	if l.Deps.Restart {
		t.Error("php must notify only: Intelephense re-indexes on didChangeWatchedFiles")
	}
}
