package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// groups_validate_test.go covers the [[project.groups]] validator (0510,
// #2570): structurally broken entries are dropped with a diagnostic, a root
// that is merely gone is reported but kept — the open chain skips missing
// members without editing the stored group.

func diagFor(diags []Diagnostic, field string) (Diagnostic, bool) {
	for _, d := range diags {
		if d.Field == field {
			return d, true
		}
	}
	return Diagnostic{}, false
}

func TestValidateProjectGroupsDropsBrokenEntries(t *testing.T) {
	dir := t.TempDir()
	c := &Config{}
	c.Project.Groups = []ProjectGroup{
		{Name: "web", Roots: []string{dir}},
		{Name: "", Roots: []string{dir}},
		{Name: "a/b", Roots: []string{dir}},
		{Name: "WEB", Roots: []string{dir}},
		{Name: "empty"},
	}
	diags := validateProjectGroups(c)

	if len(c.Project.Groups) != 1 || c.Project.Groups[0].Name != "web" {
		t.Fatalf("only the sound entry should survive, got %+v", c.Project.Groups)
	}
	for field, want := range map[string]string{
		"project.groups[1]": "name is empty",
		"project.groups[2]": "path separator",
		"project.groups[3]": "duplicate group name",
		"project.groups[4]": "no roots",
	} {
		d, ok := diagFor(diags, field)
		if !ok || !strings.Contains(d.Message, want) {
			t.Errorf("%s: expected a diagnostic containing %q, got %+v", field, want, d)
		}
	}
}

func TestValidateProjectGroupsReportsButKeepsMissingRoots(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Config{}
	c.Project.Groups = []ProjectGroup{
		{Name: "a", Roots: []string{dir}},
		{Name: "b", Roots: []string{dir}},
		{Name: "web", Roots: []string{dir, file}},
	}
	diags := validateProjectGroups(c)

	if len(c.Project.Groups) != 3 || len(c.Project.Groups[2].Roots) != 2 {
		t.Fatalf("a bad root must not drop the entry, got %+v", c.Project.Groups)
	}
	d, ok := diagFor(diags, "project.groups[2]")
	if !ok || !strings.Contains(d.Message, "is not a directory") || !strings.Contains(d.Message, file) {
		t.Errorf("expected the not-a-directory message naming the root, got %+v", d)
	}
}

func TestValidateProjectGroupsNormalisesRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	code := filepath.Join(home, "code")
	if err := os.Mkdir(code, 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Config{}
	c.Project.Groups = []ProjectGroup{{Name: " web ", Roots: []string{"~/code", code, "  "}}}

	if diags := validateProjectGroups(c); len(diags) != 0 {
		t.Fatalf("a sound group should report nothing, got %+v", diags)
	}
	g := c.Project.Groups[0]
	if g.Name != "web" {
		t.Errorf("name should be trimmed, got %q", g.Name)
	}
	if len(g.Roots) != 1 || g.Roots[0] != code {
		t.Errorf("~ should expand and the duplicate collapse, got %v", g.Roots)
	}
}

func TestProjectGroupsRoundTripThroughLoad(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(t.TempDir(), "settings.toml")
	toml := "[project]\nactive_group = \"web\"\n\n[[project.groups]]\nname = \"web\"\nroots = [\"" +
		filepath.ToSlash(dir) + "\"]\ncreated = \"2026-09-08T12:00:00Z\"\n"
	if err := os.WriteFile(user, []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, diags := Load(Options{UserPath: user})
	for _, d := range diags {
		if strings.HasPrefix(d.Field, "project.") {
			t.Errorf("unexpected diagnostic: %+v", d)
		}
	}
	if cfg.Project.ActiveGroup != "web" {
		t.Errorf("active_group should load, got %q", cfg.Project.ActiveGroup)
	}
	if len(cfg.Project.Groups) != 1 || cfg.Project.Groups[0].Created != "2026-09-08T12:00:00Z" {
		t.Fatalf("group should load intact, got %+v", cfg.Project.Groups)
	}
}
