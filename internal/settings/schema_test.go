package settings

import (
	"testing"

	"ike/internal/config"
)

// TestNoDeadSchemaKeys mirrors the blocked-ledger honesty rule for settings
// pages: every entry must name a key the typed config schema actually exposes
// (via Flat) — a schema entry whose key nothing reads fails here.
func TestNoDeadSchemaKeys(t *testing.T) {
	flat := config.Get().Flat()
	for _, page := range BasePages([]string{"default"}) {
		if len(page.Entries) == 0 {
			t.Errorf("page %q has no entries", page.Title)
		}
		for _, e := range page.Entries {
			if _, ok := flat[e.Key]; !ok {
				t.Errorf("page %q entry %q: key %q not in the typed config schema", page.Title, e.Title, e.Key)
			}
			if e.Description == "" {
				t.Errorf("page %q entry %q: missing description", page.Title, e.Title)
			}
			if e.Type == Enum && len(e.Options) == 0 {
				t.Errorf("page %q entry %q: enum without options", page.Title, e.Title)
			}
		}
	}
}

// TestCorePagesPresent guards the #92 catalog: Editor, Appearance and
// Files & Session exist and cover their spec'd keys.
func TestCorePagesPresent(t *testing.T) {
	pages := BasePages([]string{"default", "tokyo-night"})
	byTitle := map[string][]Entry{}
	for _, p := range pages {
		byTitle[p.Title] = p.Entries
	}
	keys := func(title string) map[string]bool {
		out := map[string]bool{}
		for _, e := range byTitle[title] {
			out[e.Key] = true
		}
		return out
	}
	ed := keys("Editor")
	for _, k := range []string{"editor.tab_width", "editor.use_spaces", "editor.auto_indent", "editor.auto_close_pairs", "editor.trim_trailing_whitespace", "editor.insert_final_newline", "editor.line_numbers", "editor.relative_line_numbers", "editor.scroll_off"} {
		if !ed[k] {
			t.Errorf("Editor page missing %s", k)
		}
	}
	ap := keys("Appearance")
	for _, k := range []string{"theme.name", "ui.menu_bar", "palette.toggle_key"} {
		if !ap[k] {
			t.Errorf("Appearance page missing %s", k)
		}
	}
	fs := keys("Files & Session")
	for _, k := range []string{"project.restore_last", "files.watch"} {
		if !fs[k] {
			t.Errorf("Files & Session page missing %s", k)
		}
	}
	bk := keys("Backup")
	for _, k := range []string{"backup.enable", "backup.debounce_ms", "backup.max_age_days"} {
		if !bk[k] {
			t.Errorf("Backup page missing %s", k)
		}
	}
	// The theme enum carries the registry's theme list.
	for _, e := range byTitle["Appearance"] {
		if e.Key == "theme.name" && len(e.Options) != 2 {
			t.Errorf("theme enum should carry the registry list, got %v", e.Options)
		}
	}
}
