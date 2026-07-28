package config

import (
	"os"
	"path/filepath"
	"strings"
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
	if c.Files.LargeFileKB != 1024 || c.Files.LargeFileLines != 100_000 {
		t.Errorf("large-file defaults wrong: %+v", c.Files)
	}
	if c.LSP.InlayHints || !c.LSP.SignatureAuto {
		t.Errorf("lsp defaults wrong (#523): %+v", c.LSP)
	}
	if c.Palette.ToggleKey != "" {
		t.Errorf("palette toggle key should default empty (#523), got %q", c.Palette.ToggleKey)
	}
	flat := c.Flat()
	if flat["files.large_file_kb"] != "1024" || flat["files.large_file_lines"] != "100000" {
		t.Errorf("large-file keys not flattened: kb=%q lines=%q",
			flat["files.large_file_kb"], flat["files.large_file_lines"])
	}
	if flat["lsp.inlay_hints"] != "false" || flat["lsp.signature_auto"] != "true" {
		t.Errorf("lsp keys not flattened: inlay=%q sig=%q",
			flat["lsp.inlay_hints"], flat["lsp.signature_auto"])
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
	user := writeUser(t, "[[project.history]]\npath = \"/a\"\n[[project.history]]\npath = \"/b\"\n")
	proj := writeProject(t, "[[project.history]]\npath = \"/c\"\nname = \"c\"\nlast_opened = \"2026-06-19T10:00:00Z\"\n")
	c, _ := Load(Options{UserPath: user, ProjectRoot: proj})

	if len(c.Project.History) != 1 || c.Project.History[0].Path != "/c" {
		t.Errorf("history should be replaced, got %v", c.Project.History)
	}
	if e := c.Project.History[0]; e.Name != "c" || e.LastOpened != "2026-06-19T10:00:00Z" {
		t.Errorf("entry fields should decode, got %+v", e)
	}
}

// TestUnknownKeyProducesDiagnostic (0380, #793): a key no schema field
// absorbs is ignored with a warning diagnostic, never silently inert.
func TestUnknownKeyProducesDiagnostic(t *testing.T) {
	proj := writeProject(t, "[editor]\ntab_wdth = 8\n[nosuchsection]\nfoo = 1\n")
	c, diags := Load(Options{ProjectRoot: proj})

	if c.Editor.TabWidth == 8 {
		t.Fatal("the typoed key must not land anywhere")
	}
	found := map[string]bool{}
	for _, d := range diags {
		found[d.Field] = strings.Contains(d.Message, "unknown setting")
	}
	if !found["editor.tab_wdth"] || !found["nosuchsection.foo"] {
		t.Fatalf("unknown keys must warn, diags = %+v", diags)
	}
}

// TestKnownSlotKeysProduceNoUnknownDiagnostic guards the Undecoded scan
// against false positives on slot maps and list tables.
func TestKnownSlotKeysProduceNoUnknownDiagnostic(t *testing.T) {
	proj := writeProject(t,
		"[explorer.colors]\ngo = \"cyan\"\n[keymap.bindings]\n\"ctrl+q\" = \"editor.save\"\n[[tools.custom]]\nname = \"htop\"\ncommand = \"htop\"\n")
	_, diags := Load(Options{ProjectRoot: proj})
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown setting") {
			t.Fatalf("slot keys must not warn as unknown: %+v", d)
		}
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
	proj := writeProject(t, "[project]\nmax_history = 2\n[[project.history]]\npath = \"/a\"\n[[project.history]]\npath = \"/b\"\n[[project.history]]\npath = \"/c\"\n[[project.history]]\npath = \"/d\"\n")
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
	proj := writeProject(t, "[editor]\ntab_width = 6\n[explorer.colors]\ngo = \"blue\"\n[[project.history]]\npath = \"/a\"\n[[project.history]]\npath = \"/b\"\n")
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

func TestPluginsSectionFlatAndDecode(t *testing.T) {
	proj := writeProject(t, "[plugins.example]\nenabled = false\n")
	c, _ := Load(Options{ProjectRoot: proj})
	if v, ok := c.Plugins["example"]["enabled"].(bool); !ok || v {
		t.Fatalf("plugins section should decode, got %+v", c.Plugins)
	}
	if f := c.Flat(); f["plugins.example.enabled"] != "false" {
		t.Fatalf("flat should expose plugin toggles, got %q", f["plugins.example.enabled"])
	}
}

// TestViewOptionKeys guards the #64 config keys: the show_whitespace enum
// (with the pre-#64 boolean spelling accepted), rulers validation, and the
// flattened editor.rulers list.
func TestViewOptionKeys(t *testing.T) {
	proj := writeProject(t, "[editor]\nshow_whitespace = \"bogus\"\nrulers = [80, -3, 120]\n")
	c, diags := Load(Options{ProjectRoot: proj})
	if c.Editor.ShowWhitespace != "none" {
		t.Errorf("bad show_whitespace should fall back to none, got %q", c.Editor.ShowWhitespace)
	}
	if len(c.Editor.Rulers) != 3 || c.Editor.Rulers[1] != 1 {
		t.Errorf("negative ruler should clamp to 1, got %v", c.Editor.Rulers)
	}
	if len(diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %v", len(diags), diags)
	}
	if flat := c.Flat(); flat["editor.rulers"] != "80,1,120" {
		t.Errorf("rulers not flattened: %q", flat["editor.rulers"])
	}

	proj = writeProject(t, "[editor]\nshow_whitespace = \"true\"\nindent_guides = true\n")
	c, diags = Load(Options{ProjectRoot: proj})
	if c.Editor.ShowWhitespace != "all" {
		t.Errorf("legacy \"true\" should map to all, got %q", c.Editor.ShowWhitespace)
	}
	if !c.Editor.IndentGuides {
		t.Error("indent_guides not decoded")
	}
	if len(diags) != 0 {
		t.Errorf("legacy boolean spelling must not warn: %v", diags)
	}
}

// --- per-capture colour overrides (#1318) ---

// TestThemeCapturesDecodeAndFlatten: [theme.captures] is a slot map like
// explorer.colors — it decodes, merges key by key, and reaches Flat() under
// its dotted name, including capture names that themselves contain dots.
func TestThemeCapturesDecodeAndFlatten(t *testing.T) {
	user := writeUser(t, "[theme.captures]\nkeyword = \"#ff8800\"\n\"constant.builtin\" = \"orange\"\n")
	proj := writeProject(t, "[theme.captures]\nkeyword = \"cyan\"\n\"rainbow.0\" = \"42\"\n")
	c, diags := Load(Options{UserPath: user, ProjectRoot: proj})

	for _, d := range diags {
		if strings.Contains(d.Message, "unknown setting") {
			t.Fatalf("theme.captures must be a known slot map, got %v", d)
		}
	}
	want := map[string]string{"keyword": "cyan", "constant.builtin": "orange", "rainbow.0": "42"}
	for k, v := range want {
		if got := c.Theme.Captures[k]; got != v {
			t.Errorf("captures[%q] = %q, want %q", k, got, v)
		}
	}
	flat := c.Flat()
	for k, v := range want {
		if got := flat["theme.captures."+k]; got != v {
			t.Errorf("Flat()[theme.captures.%s] = %q, want %q", k, got, v)
		}
	}
}

// TestInvalidCaptureColourDropped: lipgloss renders an unparseable token as
// the terminal default, so a typo must be reported and dropped rather than
// silently un-styling the capture.
func TestInvalidCaptureColourDropped(t *testing.T) {
	proj := writeProject(t, "[theme.captures]\nkeyword = \"nosuchcolour\"\nstring = \"#00ff00\"\n")
	c, diags := Load(Options{ProjectRoot: proj})

	if _, still := c.Theme.Captures["keyword"]; still {
		t.Fatal("an unparseable colour must not survive validation")
	}
	if c.Theme.Captures["string"] != "#00ff00" {
		t.Fatal("a valid colour beside it must survive")
	}
	var reported bool
	for _, d := range diags {
		if d.Field == "theme.captures.keyword" && strings.Contains(d.Message, "nosuchcolour") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("the dropped colour must be reported, diags = %+v", diags)
	}
}

// TestSlotKeyLeavesRoundTrip guards the dotted-leaf write path: a slot-map key
// whose leaf contains dots must survive write → read → decode instead of
// being split into nested tables.
func TestSlotKeyLeavesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	opts := Options{UserPath: filepath.Join(dir, "settings.toml")}
	keys := map[string]string{
		"theme.captures.constant.builtin": "orange",
		"theme.captures.rainbow.0":        "42",
		"explorer.colors.go":              "cyan",
	}
	for k, v := range keys {
		if err := WriteKey(opts, UserScope, k, v); err != nil {
			t.Fatalf("WriteKey(%s): %v", k, err)
		}
	}
	c, diags := Load(opts)
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown setting") {
			t.Fatalf("a written slot key must decode again, got %v", d)
		}
	}
	flat := c.Flat()
	for k, v := range keys {
		if got := flat[k]; got != v {
			t.Errorf("after round-trip Flat()[%s] = %q, want %q", k, got, v)
		}
		if got := Origin(opts, k); got != "user" {
			t.Errorf("Origin(%s) = %q, want user", k, got)
		}
	}
	// Removing one leaf leaves its siblings alone.
	if err := RemoveKey(opts, UserScope, "theme.captures.constant.builtin"); err != nil {
		t.Fatal(err)
	}
	c, _ = Load(opts)
	if _, still := c.Theme.Captures["constant.builtin"]; still {
		t.Fatal("RemoveKey must drop the leaf")
	}
	if c.Theme.Captures["rainbow.0"] != "42" {
		t.Fatal("removing one leaf must not disturb its siblings")
	}
}

// TestNestedSlotTableFlattensToDottedKey (0460, #1312): a context-qualified
// binding may be written as a sub-table — TOML's own spelling for a dotted map
// key — and must decode to exactly the same map entry as the quoted form.
func TestNestedSlotTableFlattensToDottedKey(t *testing.T) {
	proj := writeProject(t, "[keymap.bindings.editor]\n\"ctrl+g\" = \"editor.cmd\"\n")
	c, diags := Load(Options{ProjectRoot: proj})
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown setting") {
			t.Fatalf("the nested spelling must not warn: %+v", d)
		}
	}
	if got := c.Keymap.Bindings["editor.ctrl+g"]; got != "editor.cmd" {
		t.Fatalf("bindings = %v, want editor.ctrl+g → editor.cmd", c.Keymap.Bindings)
	}
	// It is exposed flat under the same key the write-back layer uses.
	if got := c.Flat()["keymap.bindings.editor.ctrl+g"]; got != "editor.cmd" {
		t.Fatalf("flat key = %q", got)
	}
}

// TestNestedAndDottedBindingSpellingsMerge: the two spellings are the same key,
// so one layer may use either and still merge key by key with the other.
func TestNestedAndDottedBindingSpellingsMerge(t *testing.T) {
	user := writeUser(t, "[keymap.bindings.editor]\n\"ctrl+g\" = \"editor.one\"\n\"ctrl+h\" = \"editor.two\"\n")
	proj := writeProject(t, "[keymap.bindings]\n\"editor.ctrl+g\" = \"editor.override\"\n")
	c, _ := Load(Options{UserPath: user, ProjectRoot: proj})

	if got := c.Keymap.Bindings["editor.ctrl+g"]; got != "editor.override" {
		t.Errorf("project layer should win: %q", got)
	}
	if got := c.Keymap.Bindings["editor.ctrl+h"]; got != "editor.two" {
		t.Errorf("the user-only key should survive: %q", got)
	}
}
