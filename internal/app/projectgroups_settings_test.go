package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/config"
)

// groupOpsConfig pins a config whose project directory is dir, so the relative
// root rule is exercised against a known place.
func groupOpsConfig(t *testing.T, dir string) {
	t.Helper()
	prev := config.Get()
	c, _ := config.Load(config.Options{})
	c.Project.Directory = dir
	c.Project.Groups = nil
	config.Set(c)
	t.Cleanup(func() { config.Set(prev) })
}

// TestProjectGroupOpsResolveRoot guards the settings page's real wiring
// (#2573): a bare name is a project inside the project directory, "~" expands,
// and a missing directory is refused with a message the form can show.
func TestProjectGroupOpsResolveRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	groupOpsConfig(t, dir)
	ops := projectGroupOps(config.Options{UserPath: filepath.Join(t.TempDir(), "settings.toml")})

	got, err := ops.ResolveRoot("api")
	if err != nil || got != filepath.Join(dir, "api") {
		t.Fatalf("relative root = %q, %v", got, err)
	}
	if home, herr := os.UserHomeDir(); herr == nil {
		if got, err := ops.ResolveRoot("~"); err != nil || got != filepath.Clean(home) {
			t.Fatalf("~ root = %q, %v", got, err)
		}
	}
	if _, err := ops.ResolveRoot("gone"); err == nil {
		t.Fatal("a missing directory must be refused")
	}
}

// TestProjectGroupOpsWriteAndRemove: the page's write path persists the whole
// list at user scope and the remove path drops one entry.
func TestProjectGroupOpsWriteAndRemove(t *testing.T) {
	dir := t.TempDir()
	groupOpsConfig(t, dir)
	opts := config.Options{
		UserPath:    filepath.Join(t.TempDir(), "settings.toml"),
		ProjectRoot: t.TempDir(),
	}
	ops := projectGroupOps(opts)

	a, b := t.TempDir(), t.TempDir()
	if err := ops.Write([]config.ProjectGroup{
		{Name: "web", Roots: []string{a, b}},
		{Name: "docs", Roots: []string{a}},
	}); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(opts.UserPath)
	if err != nil || !strings.Contains(string(data), "web") || !strings.Contains(string(data), "docs") {
		t.Fatalf("user settings = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(opts.ProjectRoot, ".ike", "settings.toml")); !os.IsNotExist(err) {
		t.Fatalf("groups must never reach the project layer (stat err %v)", err)
	}

	c, _ := config.Load(opts)
	config.Set(c)
	if got := config.Get().Project.Groups; len(got) != 2 {
		t.Fatalf("groups after write = %+v", got)
	}
	if err := ops.Remove("WEB"); err != nil { // names match case-insensitively
		t.Fatalf("remove: %v", err)
	}
	c, _ = config.Load(opts)
	if got := c.Project.Groups; len(got) != 1 || got[0].Name != "docs" {
		t.Fatalf("groups after remove = %+v", got)
	}

	// An invalid entry leaves the stored list untouched.
	if err := ops.Write([]config.ProjectGroup{{Name: "", Roots: []string{a}}}); err == nil {
		t.Fatal("an unnamed group must be refused")
	}
	if _, _, err := ops.Validate("a/b", []string{a}); err == nil {
		t.Fatal("a path separator in the name must be refused")
	}
}
