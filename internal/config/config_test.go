package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTOML writes content to {dir}/.ike/settings.toml and returns the dir, so a
// caller can hand it to Options as a project root.
func writeProject(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, dotDir)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, fileName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeUser(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), fileName)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefaultsWhenNoFiles(t *testing.T) {
	c, diags := Load(Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if c.Editor.TabWidth != 4 || !c.Editor.UseSpaces {
		t.Errorf("editor defaults wrong: %+v", c.Editor)
	}
	if c.Keymap.Preset != "jetbrains" || c.Theme.Name != "default" {
		t.Errorf("preset/theme defaults wrong")
	}
	if c.Project.MaxHistory != 20 || len(c.Project.History) != 0 {
		t.Errorf("project defaults wrong: %+v", c.Project)
	}
}

func TestPrecedenceProjectWinsScalar(t *testing.T) {
	user := writeUser(t, "[editor]\ntab_width = 2\nscroll_off = 9\n")
	proj := writeProject(t, "[editor]\ntab_width = 8\n")
	c, _ := Load(Options{UserPath: user, ProjectRoot: proj})

	if c.Editor.TabWidth != 8 {
		t.Errorf("project should win tab_width: got %d", c.Editor.TabWidth)
	}
	// user-set, project-absent inherits the user layer, not the default.
	if c.Editor.ScrollOff != 9 {
		t.Errorf("scroll_off should inherit user layer: got %d", c.Editor.ScrollOff)
	}
	// untouched everywhere falls back to default.
	if !c.Editor.LineNumbers {
		t.Errorf("line_numbers should keep default true")
	}
}

func TestTableMergeKeyByKey(t *testing.T) {
	user := writeUser(t, "[explorer.colors]\ngo = \"blue\"\nmd = \"white\"\n")
	proj := writeProject(t, "[explorer.colors]\ngo = \"cyan\"\nrs = \"orange\"\n")
	c, _ := Load(Options{UserPath: user, ProjectRoot: proj})

	want := map[string]string{"go": "cyan", "md": "white", "rs": "orange"}
	for k, v := range want {
		if c.Explorer.Colors[k] != v {
			t.Errorf("colors[%q] = %q, want %q", k, c.Explorer.Colors[k], v)
		}
	}
	if len(c.Explorer.Colors) != 3 {
		t.Errorf("expected 3 merged colors, got %d", len(c.Explorer.Colors))
	}
}

func TestListReplaceNotAppend(t *testing.T) {
	user := writeUser(t, "[project]\nhistory = [\"/a\", \"/b\"]\n")
	proj := writeProject(t, "[project]\nhistory = [\"/c\"]\n")
	c, _ := Load(Options{UserPath: user, ProjectRoot: proj})

	if len(c.Project.History) != 1 || c.Project.History[0] != "/c" {
		t.Errorf("history should be replaced, got %v", c.Project.History)
	}
}

func TestValidateClampAndWarn(t *testing.T) {
	proj := writeProject(t, "[editor]\ntab_width = 0\nscroll_off = -5\n[explorer]\nsort = \"bogus\"\n[lsp]\nlog_level = \"loud\"\n")
	c, diags := Load(Options{ProjectRoot: proj})

	if c.Editor.TabWidth != 1 {
		t.Errorf("tab_width should clamp to 1, got %d", c.Editor.TabWidth)
	}
	if c.Editor.ScrollOff != 0 {
		t.Errorf("scroll_off should clamp to 0, got %d", c.Editor.ScrollOff)
	}
	if c.Explorer.Sort != "name" {
		t.Errorf("bad sort should fall back to name, got %q", c.Explorer.Sort)
	}
	if c.LSP.LogLevel != "warn" {
		t.Errorf("bad log_level should fall back to warn, got %q", c.LSP.LogLevel)
	}
	if len(diags) != 4 {
		t.Fatalf("expected 4 diagnostics, got %d: %v", len(diags), diags)
	}
}

// TestNotificationsSection guards the 0130 config keys: defaults, clamp on the
// timeout and severity fallback (#78).
func TestNotificationsSection(t *testing.T) {
	c, _ := Load(Options{})
	if c.Notifications.TimeoutSeconds != 4 || c.Notifications.MinSeverity != "info" {
		t.Fatalf("unexpected defaults: %+v", c.Notifications)
	}

	proj := writeProject(t, "[notifications]\ntimeout_seconds = 0\nmin_severity = \"whisper\"\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.Notifications.TimeoutSeconds != 1 {
		t.Errorf("timeout_seconds should clamp to 1, got %d", c.Notifications.TimeoutSeconds)
	}
	if c.Notifications.MinSeverity != "info" {
		t.Errorf("bad min_severity should fall back to info, got %q", c.Notifications.MinSeverity)
	}
	if len(diags) != 2 {
		t.Errorf("expected 2 diagnostics, got %v", diags)
	}
	if flat := c.Flat(); flat["notifications.min_severity"] != "info" || flat["notifications.timeout_seconds"] != "1" {
		t.Errorf("notifications keys missing from Flat: %v", flat)
	}
}

func TestHistoryTruncatedToMax(t *testing.T) {
	proj := writeProject(t, "[project]\nmax_history = 2\nhistory = [\"/a\", \"/b\", \"/c\", \"/d\"]\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if len(c.Project.History) != 2 {
		t.Errorf("history should truncate to 2, got %v", c.Project.History)
	}
	if len(diags) != 1 {
		t.Errorf("expected 1 truncation diagnostic, got %v", diags)
	}
}

func TestParseErrorIsolatesLayer(t *testing.T) {
	user := writeUser(t, "[editor]\ntab_width = 7\n")
	proj := writeProject(t, "this is = = not valid toml ][")
	c, diags := Load(Options{UserPath: user, ProjectRoot: proj})

	// Lower (user) layer still applies despite the broken project file.
	if c.Editor.TabWidth != 7 {
		t.Errorf("user layer should survive a broken project file, got %d", c.Editor.TabWidth)
	}
	if len(diags) != 1 || diags[0].Source == "" {
		t.Fatalf("expected one file-sourced parse diagnostic, got %v", diags)
	}
}

func TestMissingFilesAreNotErrors(t *testing.T) {
	_, diags := Load(Options{
		UserPath:    filepath.Join(t.TempDir(), "nope.toml"),
		ProjectRoot: t.TempDir(),
	})
	if len(diags) != 0 {
		t.Errorf("absent files must not produce diagnostics: %v", diags)
	}
}

func TestExtendRegistrationRoundTrip(t *testing.T) {
	t.Cleanup(resetExtensions)
	Register(Extension{
		Name:     "explorer-colors",
		Defaults: func(c *Config) { c.Explorer.Colors["go"] = "default-blue" },
		Validate: func(c *Config) []Diagnostic {
			if c.Explorer.Colors["go"] == "" {
				return []Diagnostic{{Field: "explorer.colors.go", Message: "missing"}}
			}
			return nil
		},
	})

	// With no user override the extension default is present.
	c, diags := Load(Options{})
	if c.Explorer.Colors["go"] != "default-blue" {
		t.Errorf("extension default missing: %v", c.Explorer.Colors)
	}
	if len(diags) != 0 {
		t.Errorf("extension validate should pass, got %v", diags)
	}

	// A user override beats the extension default (extension is lowest layer).
	user := writeUser(t, "[explorer.colors]\ngo = \"user-cyan\"\n")
	c2, _ := Load(Options{UserPath: user})
	if c2.Explorer.Colors["go"] != "user-cyan" {
		t.Errorf("user override should beat extension default, got %q", c2.Explorer.Colors["go"])
	}
}

func TestRegisterIsIdempotentByName(t *testing.T) {
	t.Cleanup(resetExtensions)
	Register(Extension{Name: "x", Defaults: func(c *Config) { c.Theme.Name = "first" }})
	Register(Extension{Name: "x", Defaults: func(c *Config) { c.Theme.Name = "second" }})
	if got := registered(); len(got) != 1 {
		t.Fatalf("expected 1 registered extension, got %d", len(got))
	}
	c, _ := Load(Options{})
	if c.Theme.Name != "second" {
		t.Errorf("re-register should replace, got %q", c.Theme.Name)
	}
}

func TestDiscoverHonorsConfigDirEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	o := Discover("/some/project")
	if o.UserPath != filepath.Join(dir, fileName) {
		t.Errorf("UserPath should use IKE_CONFIG_DIR, got %q", o.UserPath)
	}
	paths := o.layerPaths()
	if len(paths) != 2 || paths[1] != filepath.Join("/some/project", dotDir, fileName) {
		t.Errorf("layer paths wrong: %v", paths)
	}
}

func TestFlatExposesScalarsAndSlots(t *testing.T) {
	proj := writeProject(t, "[editor]\ntab_width = 6\n[explorer.colors]\ngo = \"blue\"\n[project]\nhistory = [\"/a\", \"/b\"]\n")
	c, _ := Load(Options{ProjectRoot: proj})
	f := c.Flat()

	if f["editor.tab_width"] != "6" {
		t.Errorf("flat tab_width = %q", f["editor.tab_width"])
	}
	if f["editor.use_spaces"] != "true" {
		t.Errorf("flat use_spaces = %q", f["editor.use_spaces"])
	}
	if f["explorer.colors.go"] != "blue" {
		t.Errorf("flat color slot = %q", f["explorer.colors.go"])
	}
	if f["project.history"] != "/a,/b" {
		t.Errorf("flat history = %q", f["project.history"])
	}
}

func TestGetReturnsDefaultsBeforeSet(t *testing.T) {
	mu.Lock()
	loaded = nil
	mu.Unlock()
	if Get().Editor.TabWidth != 4 {
		t.Errorf("Get before Set should return defaults")
	}
	c, _ := Load(Options{})
	c.Theme.Name = "marker"
	Set(c)
	if Get().Theme.Name != "marker" {
		t.Errorf("Get after Set should return installed config")
	}
}

func TestPushHistoryBoundedAndDeduped(t *testing.T) {
	c := defaults()
	c.Project.MaxHistory = 3
	c.PushHistory("/a").PushHistory("/b").PushHistory("/a").PushHistory("/c").PushHistory("/d")
	want := []string{"/d", "/c", "/a", "/b"}[:3]
	if len(c.Project.History) != 3 {
		t.Fatalf("history should bound to 3, got %v", c.Project.History)
	}
	for i, v := range want {
		if c.Project.History[i] != v {
			t.Errorf("history[%d] = %q, want %q (full %v)", i, c.Project.History[i], v, c.Project.History)
		}
	}
}

func TestBackupDefaults(t *testing.T) {
	c, diags := Load(Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !c.Backup.Enable || c.Backup.DebounceMs != 2000 || c.Backup.MaxAgeDays != 7 {
		t.Errorf("backup defaults wrong: %+v", c.Backup)
	}
}

func TestBackupClampAndOverride(t *testing.T) {
	proj := writeProject(t, "[backup]\nenable = false\ndebounce_ms = 5\nmax_age_days = 0\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.Backup.Enable {
		t.Errorf("enable = false should stick")
	}
	if c.Backup.DebounceMs != 100 {
		t.Errorf("debounce_ms should clamp to 100, got %d", c.Backup.DebounceMs)
	}
	if c.Backup.MaxAgeDays != 1 {
		t.Errorf("max_age_days should clamp to 1, got %d", c.Backup.MaxAgeDays)
	}
	if len(diags) != 2 {
		t.Errorf("expected one diagnostic per clamp, got %v", diags)
	}
}
