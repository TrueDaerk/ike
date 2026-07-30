package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFilesAssociationsDecode: [files.associations] is a slot map (#1365) —
// pattern keys with dots and globs decode as single map keys, and the nested
// sub-table spelling flattens to the same dotted key.
func TestFilesAssociationsDecode(t *testing.T) {
	proj := writeProject(t, "[files.associations]\n\"*.mytool\" = \"toml\"\n\"Dockerfile.dev\" = \"dockerfile\"\n")
	c, diags := Load(Options{ProjectRoot: proj})
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %+v", d)
	}
	want := map[string]string{"*.mytool": "toml", "Dockerfile.dev": "dockerfile"}
	for k, v := range want {
		if c.Files.Associations[k] != v {
			t.Fatalf("Associations[%q] = %q, want %q (map: %v)", k, c.Files.Associations[k], v, c.Files.Associations)
		}
	}
	// Flat exposes the entries under the slot-map prefix.
	if got := c.Flat()["files.associations.*.mytool"]; got != "toml" {
		t.Fatalf("Flat()[files.associations.*.mytool] = %q", got)
	}
}

// TestFilesAssociationsNestedSpelling: a dotted key written as a sub-table
// ([files.associations.Dockerfile] dev = ...) flattens back to one map key.
func TestFilesAssociationsNestedSpelling(t *testing.T) {
	proj := writeProject(t, "[files.associations.Dockerfile]\ndev = \"dockerfile\"\n")
	c, diags := Load(Options{ProjectRoot: proj})
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %+v", d)
	}
	if got := c.Files.Associations["Dockerfile.dev"]; got != "dockerfile" {
		t.Fatalf("Associations = %v, want Dockerfile.dev -> dockerfile", c.Files.Associations)
	}
}

// TestFilesAssociationsWriteRoundTrip: WriteKey treats the pattern as one
// slot-map leaf — the glob/dotted key survives the write-back and reload.
func TestFilesAssociationsWriteRoundTrip(t *testing.T) {
	opts := Options{UserPath: filepath.Join(t.TempDir(), fileName)}
	if err := WriteKey(opts, UserScope, "files.associations.*.mytool", "toml"); err != nil {
		t.Fatal(err)
	}
	c, _ := Load(opts)
	if got := c.Files.Associations["*.mytool"]; got != "toml" {
		t.Fatalf("reload sees %v, want *.mytool -> toml", c.Files.Associations)
	}
	if err := RemoveKey(opts, UserScope, "files.associations.*.mytool"); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(opts)
	if _, ok := c.Files.Associations["*.mytool"]; ok {
		t.Fatalf("remove left %v", c.Files.Associations)
	}
	data, _ := os.ReadFile(opts.UserPath)
	if string(data) != "" && len(c.Files.Associations) != 0 {
		t.Fatalf("leftover file content: %s", data)
	}
}
