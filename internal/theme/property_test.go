package theme

import (
	"strings"
	"testing"
)

// TestBuiltinPropertyColourDistinct guards #3103: the `property` capture
// (dotenv/ini/crontab keys, JSON/YAML/TOML keys) must stand apart from plain
// text, from `variable` (Jinja placeholders in .env.j2) and from `string`
// values in every built-in theme.
func TestBuiltinPropertyColourDistinct(t *testing.T) {
	for _, th := range Builtins() {
		c := th.Captures
		p := strings.ToLower(c["property"])
		if p == "" {
			t.Errorf("%s: property capture has no colour", th.Name)
			continue
		}
		for _, other := range []string{"variable", "string"} {
			if p == strings.ToLower(c[other]) {
				t.Errorf("%s: property %q equals %s", th.Name, p, other)
			}
		}
		if p == strings.ToLower(th.UI.Foreground) || p == "white" {
			t.Errorf("%s: property %q is the default text colour", th.Name, p)
		}
	}
}
