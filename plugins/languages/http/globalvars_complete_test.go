package langhttp

import (
	"testing"

	"ike/internal/config"
)

// withGlobalVars installs a config carrying the given global variables for
// the test.
func withGlobalVars(t *testing.T, vars ...config.HTTPVariable) {
	t.Helper()
	prev := config.Get()
	c, _ := config.Load(config.Options{})
	c.HTTP.Variables = vars
	config.Set(c)
	t.Cleanup(func() { config.Set(prev) })
}

// TestCompletePlaceholderGlobalVars: "{{" offers the global variables of
// both scopes, each labelled with its own origin; a file definition keeps a
// shared name, and a project variable wins over a user one.
func TestCompletePlaceholderGlobalVars(t *testing.T) {
	withGlobalVars(t,
		config.HTTPVariable{Name: "apiBase", Value: "https://p.test", Scope: config.ProjectScope},
		config.HTTPVariable{Name: "apiBase", Value: "https://u.test", Scope: config.UserScope},
		config.HTTPVariable{Name: "apiToken", Value: "t", Scope: config.UserScope},
		config.HTTPVariable{Name: "apiHost", Value: "h", Scope: config.UserScope},
	)
	items := completeAt(t, "@apiHost=file\nGET {{api|\n")
	for label, want := range map[string]string{
		"apiBase":  "global project · https://p.test",
		"apiToken": "global user · ••••",
		"apiHost":  "file · file",
	} {
		if got := detailFor(items, label); got != want {
			t.Errorf("%s detail = %q, want %q (items %v)", label, got, want, labels(items))
		}
	}
}
