package testdata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandbox redirects the user state dir at the same seam config.Discover uses,
// so the store never touches the developer's ~/.ike.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	return dir
}

// TestLastSpecDefaultsWhenEmpty checks a fresh install starts from the stock
// spec rather than an error or an empty one.
func TestLastSpecDefaultsWhenEmpty(t *testing.T) {
	sandbox(t)
	got := LastSpec()
	if got.Rows != defaultRows || got.DSL != DefaultDSL {
		t.Fatalf("LastSpec on an empty store = %+v, want the default spec", got)
	}
	if got.Validate() != nil {
		t.Fatalf("the default spec must validate: %v", got.Validate())
	}
}

// TestLastSpecRoundTrip is the reopen criterion: the next dialog starts from
// the last generated spec.
func TestLastSpecRoundTrip(t *testing.T) {
	sandbox(t)
	spec := Spec{
		Format: FormatYAML,
		Rows:   7,
		Seed:   99,
		Table:  "people",
		DSL:    "who = full_name()\nsite = url(example.com)\n",
	}
	SaveLast(spec)

	got := LastSpec()
	if got.Format != FormatYAML || got.Rows != 7 || got.Seed != 99 || got.Table != "people" {
		t.Fatalf("LastSpec = %+v, want the saved spec", got)
	}
	if got.DSL != spec.DSL {
		t.Fatalf("DSL lost: %q", got.DSL)
	}
}

// TestLastSpecRejectsUnusableSaved covers a hand-edited or stale store: an
// invalid spec must not reach the dialog. The pre-#2392 per-format schema
// lands here too — it has no "last" key, so it reads as the default.
func TestLastSpecRejectsUnusableSaved(t *testing.T) {
	dir := sandbox(t)
	cases := []string{
		`{"last":{"format":"csv","rows":0,"dsl":"id = id()"}}`,
		`{"last":{"format":"csv","rows":5,"dsl":"id = wat()"}}`,
		`{"specs":{"csv":{"format":"csv","rows":4,"fields":[{"name":"x","kind":"id"}]}}}`, // old schema
	}
	for _, raw := range cases {
		if err := os.WriteFile(filepath.Join(dir, "testdata.json"), []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := LastSpec(); got.DSL != DefaultDSL {
			t.Fatalf("store %s: LastSpec = %+v, want the default spec", raw, got)
		}
	}
}

// TestStoreToleratesGarbage keeps the store non-fatal: an unreadable file is
// an empty store.
func TestStoreToleratesGarbage(t *testing.T) {
	dir := sandbox(t)
	if err := os.WriteFile(filepath.Join(dir, "testdata.json"), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LastSpec(); got.Rows != defaultRows {
		t.Fatalf("LastSpec = %+v, want the default spec", got)
	}
	SaveLast(Default(FormatJSON)) // must not panic over the garbage
	if len(Templates()) < len(BuiltinTemplates()) {
		t.Fatal("Templates() lost the built-ins over a garbage store")
	}
}

// TestStoreFileFollowsConfigDir pins the redirection seam.
func TestStoreFileFollowsConfigDir(t *testing.T) {
	dir := sandbox(t)
	if got, want := StoreFile(), filepath.Join(dir, "testdata.json"); got != want {
		t.Fatalf("StoreFile() = %q, want %q", got, want)
	}
}

// TestSaveLastIgnoresInvalid keeps a bad spec out of the store.
func TestSaveLastIgnoresInvalid(t *testing.T) {
	dir := sandbox(t)
	SaveLast(Spec{Format: "parquet", Rows: 3, DSL: "id = id()"})
	if _, err := os.Stat(filepath.Join(dir, "testdata.json")); !os.IsNotExist(err) {
		t.Fatalf("an invalid spec was persisted (stat err = %v)", err)
	}
}

// TestUserTemplatesRoundTrip covers save, list, load-by-name and delete —
// and that built-ins survive it all.
func TestUserTemplatesRoundTrip(t *testing.T) {
	sandbox(t)
	if err := SaveTemplate("My things", "thing = from_list(a, b)\n"); err != nil {
		t.Fatalf("SaveTemplate: %v", err)
	}
	var found *Template
	for _, tpl := range Templates() {
		tpl := tpl
		if tpl.Name == "My things" {
			found = &tpl
		}
	}
	if found == nil || found.BuiltIn || found.DSL != "thing = from_list(a, b)\n" {
		t.Fatalf("saved template not listed correctly: %+v", found)
	}
	// Built-ins come first, user templates after.
	all := Templates()
	if len(all) != len(BuiltinTemplates())+1 {
		t.Fatalf("Templates() = %d entries, want built-ins + 1", len(all))
	}
	if !all[0].BuiltIn || all[len(all)-1].Name != "My things" {
		t.Fatalf("ordering wrong: %+v", all)
	}
	// Saving again overwrites, not duplicates.
	if err := SaveTemplate("My things", "thing = uuid()\n"); err != nil {
		t.Fatalf("SaveTemplate: %v", err)
	}
	if all := Templates(); len(all) != len(BuiltinTemplates())+1 {
		t.Fatalf("re-saving duplicated the template: %d entries", len(all))
	}

	DeleteTemplate("My things")
	if len(Templates()) != len(BuiltinTemplates()) {
		t.Fatal("DeleteTemplate did not remove the template")
	}
	DeleteTemplate("Person") // built-in: a no-op, not a crash
	if len(Templates()) != len(BuiltinTemplates()) {
		t.Fatal("deleting a built-in changed the list")
	}
}

// TestSaveTemplateRejects: no empty names, no shadowed built-ins, no invalid
// bodies.
func TestSaveTemplateRejects(t *testing.T) {
	sandbox(t)
	if err := SaveTemplate("  ", "id = id()"); err == nil {
		t.Fatal("an empty name must be rejected")
	}
	if err := SaveTemplate("person", "id = id()"); err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("shadowing a built-in must be rejected, got %v", err)
	}
	if err := SaveTemplate("Broken", "id = wat()"); err == nil {
		t.Fatal("an invalid body must be rejected")
	}
	if len(Templates()) != len(BuiltinTemplates()) {
		t.Fatal("a rejected save must not persist anything")
	}
}

// TestTemplatesPersistAcrossLoads simulates a restart: a second read of the
// same store dir sees the template.
func TestTemplatesPersistAcrossLoads(t *testing.T) {
	sandbox(t)
	if err := SaveTemplate("Keep me", "id = id()\n"); err != nil {
		t.Fatalf("SaveTemplate: %v", err)
	}
	// Templates() re-reads the file every time — the same path a restart takes.
	names := []string{}
	for _, tpl := range Templates() {
		names = append(names, tpl.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "Keep me") {
		t.Fatalf("template gone after reload: %v", names)
	}
}
