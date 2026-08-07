package settings

import (
	"reflect"
	"strings"
	"testing"

	"ike/internal/config"
)

// coverage_test.go is the schema/UI drift guard (#1663). The audit that opened
// the issue was a one-off reflection dump of config.Config against the settings
// schema; this test makes it permanent. Every TOML leaf key the typed schema
// declares must either be editable in the settings panel or be listed below
// with the reason it is not — a new config field can no longer land file-only
// by accident.

// pagedKeys are the config keys whose surface is a dedicated interactive page
// (a custom PageModel), not a schema Entry. Values are free-form tables or
// table arrays whose editors live in their own files; the settings schema
// deliberately holds no Entry for them.
var pagedKeys = map[string]string{
	"lsp.servers":             "Language Servers page (lsp_page.go)",
	"keymap.bindings":         "Keymap page (keymap_page.go)",
	"keymap.preset":           "Keymap page (keymap_page.go)",
	"theme.captures":          "Syntax Colors page (colors_page.go)",
	"files.associations":      "File Associations page (associations_page.go)",
	"lang":                    "Toolchain page (toolchain_page.go)",
	"plugins":                 "Plugins page (plugins_page.go)",
	"format":                  "Formatters page (format_page.go)",
	"tools.custom":            "Tools page (tools_page.go)",
	"debug.php.path_mappings": "PHP Debug Mappings page (debugmap_page.go)",
	"snippets":                "live templates, edited as TOML (#1152)",
}

// internalKeys are the keys intentionally kept out of the UI: state IKE writes
// about itself, not settings a user chooses.
var internalKeys = map[string]string{
	"lsp.onboarded":   "records that the first-start install dialog had its say",
	"ui.onboarded":    "records that the welcome tour was shown",
	"project.history": "the recent-projects list, maintained by internal/project",
}

// uncoveredKeys are leaf keys with no settings surface yet, each with the
// reason. Shrinking this map is the point of the guard; growing it is a
// deliberate decision, not an oversight.
var uncoveredKeys = map[string]string{
	"explorer.colors": "per-filetype colour slot map; needs a colour picker page like Syntax Colors (#1663 follow-up)",
	"theme.terminal":  "terminal ANSI palette slot map; same (#1663 follow-up)",
}

// TestEveryConfigKeyHasASurface walks config.Config by reflection and fails on
// a TOML leaf key that is neither in the settings schema nor explicitly
// excused above.
func TestEveryConfigKeyHasASurface(t *testing.T) {
	inSchema := map[string]bool{}
	for _, p := range BasePages([]string{"default"}, []string{"intellij-light"}, []string{"default"}) {
		for _, e := range p.Entries {
			inSchema[e.Key] = true
		}
	}
	for _, key := range tomlLeafKeys(reflect.TypeOf(config.Config{}), "") {
		switch {
		case inSchema[key], pagedKeys[key] != "", internalKeys[key] != "", uncoveredKeys[key] != "":
			continue
		}
		t.Errorf("config key %q has no settings-UI surface: add an Entry to BasePages, or list it in pagedKeys / internalKeys / uncoveredKeys with a reason", key)
	}
}

// TestExcusesAreLive keeps the excuse maps honest: an entry naming a key the
// typed schema no longer has, or one the schema now covers, is stale.
func TestExcusesAreLive(t *testing.T) {
	known := map[string]bool{}
	for _, key := range tomlLeafKeys(reflect.TypeOf(config.Config{}), "") {
		known[key] = true
	}
	inSchema := map[string]bool{}
	for _, p := range BasePages([]string{"default"}, nil, nil) {
		for _, e := range p.Entries {
			inSchema[e.Key] = true
		}
	}
	for _, m := range []map[string]string{pagedKeys, internalKeys, uncoveredKeys} {
		for key := range m {
			if !known[key] {
				t.Errorf("excused key %q is not in the typed config schema any more", key)
			}
			if inSchema[key] {
				t.Errorf("excused key %q is covered by a schema Entry now — drop the excuse", key)
			}
		}
	}
}

// tomlLeafKeys returns the dotted TOML keys of t: one per scalar or scalar-slice
// field, plus one per map or table-array field (those are edited wholesale, so
// the container itself is the key a surface answers for).
func tomlLeafKeys(t reflect.Type, prefix string) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("toml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		key := tag
		if prefix != "" {
			key = prefix + "." + tag
		}
		switch ft := f.Type; ft.Kind() {
		case reflect.Struct:
			// A nested section is not a key of its own; only its leaves are.
			out = append(out, tomlLeafKeys(ft, key)...)
		case reflect.Map, reflect.Slice:
			// Maps (slot tables) and slices (scalar arrays like editor.rulers,
			// table arrays like [[tools.custom]]) are edited whole, so the
			// container name is the key a surface answers for.
			out = append(out, key)
		default:
			out = append(out, key)
		}
	}
	return out
}
