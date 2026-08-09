package langphp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProjectOptionAlwaysAvailable guards the wizard row: plain PHP needs no
// binary, so it never disables.
func TestProjectOptionAlwaysAvailable(t *testing.T) {
	opts := toolchain{}.ProjectOptions()
	if len(opts) != 1 || opts[0].ID != "plain" || !opts[0].Available {
		t.Fatalf("options = %+v", opts)
	}
}

// TestScaffoldSeedsIndexPHP guards the scaffold output.
func TestScaffoldSeedsIndexPHP(t *testing.T) {
	root := t.TempDir()
	if err := (toolchain{}).ScaffoldProject(root, "plain"); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "index.php")); err != nil {
		t.Fatalf("index.php not seeded: %v", err)
	}
	if err := (toolchain{}).ScaffoldProject(root, "composer"); err == nil {
		t.Fatal("unknown option must refuse")
	}
}
