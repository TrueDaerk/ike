package main

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/theme"
)

// TestRegisteredLanguagesHaveFileColors is #1366's drift guard, run against
// the exact plugin set this binary compiles in (main.go's blank imports):
// every extension and filename a language plugin registers must have a Files
// entry in every built-in theme, so a newly added language cannot silently
// render untinted in the explorer. Fails point at the theme helper's
// fileGroups table in internal/theme/files.go.
func TestRegisteredLanguagesHaveFileColors(t *testing.T) {
	langs := lang.All()
	if len(langs) == 0 {
		t.Fatal("no languages registered — blank imports missing?")
	}
	for _, th := range theme.Builtins() {
		t.Run(th.Name, func(t *testing.T) {
			for _, l := range langs {
				for _, ext := range l.Extensions {
					if c, ok := th.Files[ext]; !ok || c == "" {
						t.Errorf("language %s: extension %q has no Files color", l.ID, ext)
					}
				}
				for _, name := range l.Filenames {
					if c, ok := th.Files[name]; !ok || c == "" {
						t.Errorf("language %s: filename %q has no Files color", l.ID, name)
					}
				}
			}
			for _, key := range []string{"dir", "default", "lock"} {
				if c, ok := th.Files[key]; !ok || c == "" {
					t.Errorf("required fallback key %q missing", key)
				}
			}
		})
	}
}
