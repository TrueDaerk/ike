package testdata

import (
	"os"
	"path/filepath"
	"testing"
)

// sandbox redirects the user state dir at the same seam config.Discover uses,
// so the preset store never touches the developer's ~/.ike.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	return dir
}

// TestPresetDefaultsWhenEmpty checks a fresh install starts from the stock
// spec rather than an error or an empty one.
func TestPresetDefaultsWhenEmpty(t *testing.T) {
	sandbox(t)
	got := Preset(FormatCSV)
	if got.Format != FormatCSV || got.Rows != defaultRows || len(got.Fields) == 0 {
		t.Fatalf("Preset on an empty store = %+v, want the default spec", got)
	}
}

// TestPresetRoundTrip is the reusable-presets criterion: the last spec used
// for a format comes back, and only for that format.
func TestPresetRoundTrip(t *testing.T) {
	sandbox(t)
	spec := Spec{
		Format: FormatYAML,
		Rows:   7,
		Seed:   99,
		Table:  "people",
		Fields: []Field{{Name: "who", Kind: KindFullName}, {Name: "site", Kind: KindURL, Param: "example.com"}},
	}
	SavePreset(spec)

	got := Preset(FormatYAML)
	if got.Rows != 7 || got.Seed != 99 || got.Table != "people" || len(got.Fields) != 2 {
		t.Fatalf("Preset = %+v, want the saved spec", got)
	}
	if got.Fields[1].Param != "example.com" {
		t.Fatalf("field parameter lost: %+v", got.Fields[1])
	}
	if other := Preset(FormatCSV); other.Rows != defaultRows {
		t.Fatalf("saving a YAML preset changed the CSV one: %+v", other)
	}
}

// TestPresetRejectsUnusableSaved covers a hand-edited or stale store: an
// invalid spec must not reach the wizard.
func TestPresetRejectsUnusableSaved(t *testing.T) {
	dir := sandbox(t)
	if err := os.WriteFile(filepath.Join(dir, "testdata.json"),
		[]byte(`{"specs":{"csv":{"format":"csv","rows":0,"fields":[{"name":"x","kind":"nope"}]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Preset(FormatCSV)
	if got.Rows != defaultRows || got.Fields[0].Kind != KindID {
		t.Fatalf("Preset = %+v, want the default spec", got)
	}
}

// TestPresetToleratesGarbage keeps the store non-fatal: an unreadable file is
// an empty store.
func TestPresetToleratesGarbage(t *testing.T) {
	dir := sandbox(t)
	if err := os.WriteFile(filepath.Join(dir, "testdata.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Preset(FormatJSON); got.Rows != defaultRows {
		t.Fatalf("Preset = %+v, want the default spec", got)
	}
	SavePreset(Default(FormatJSON)) // must not panic over the garbage
}

// TestStoreFileFollowsConfigDir pins the redirection seam.
func TestStoreFileFollowsConfigDir(t *testing.T) {
	dir := sandbox(t)
	if got, want := StoreFile(), filepath.Join(dir, "testdata.json"); got != want {
		t.Fatalf("StoreFile() = %q, want %q", got, want)
	}
}

// TestSavePresetIgnoresUnknownFormat keeps a bad format out of the store.
func TestSavePresetIgnoresUnknownFormat(t *testing.T) {
	dir := sandbox(t)
	SavePreset(Spec{Format: "parquet", Rows: 3})
	if _, err := os.Stat(filepath.Join(dir, "testdata.json")); !os.IsNotExist(err) {
		t.Fatalf("an unknown format was persisted (stat err = %v)", err)
	}
}
