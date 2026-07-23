package langweb

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWorkspaceTSDKDetection guards #1079: a vendored TypeScript points vtsls
// at the workspace tsdk; without one the server keeps its bundled default.
func TestWorkspaceTSDKDetection(t *testing.T) {
	root := t.TempDir()
	lib := filepath.Join(root, "node_modules", "typescript", "lib")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := (tsToolchain{}.Detect(root)); ok {
		t.Fatal("lib dir without tsserverlibrary.js must not count as a tsdk")
	}
	if err := os.WriteFile(filepath.Join(lib, "tsserverlibrary.js"), []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := tsToolchain{}.Detect(root)
	if !ok {
		t.Fatal("vendored TypeScript must be detected")
	}
	if tsdk := got["typescript"].(map[string]any)["tsdk"]; tsdk != lib {
		t.Fatalf("tsdk = %v want %s", tsdk, lib)
	}
	if _, ok := (tsToolchain{}.Detect(t.TempDir())); ok {
		t.Fatal("no node_modules → nothing to inject")
	}
}
