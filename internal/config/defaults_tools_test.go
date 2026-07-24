package config

import (
	"errors"
	"testing"
)

// stubLazygit overrides the PATH probe for the test's duration.
func stubLazygit(t *testing.T, onPath bool) {
	t.Helper()
	orig := lookPath
	lookPath = func(name string) (string, error) {
		if name == "lazygit" && onPath {
			return "/usr/local/bin/lazygit", nil
		}
		return "", errors.New("not found")
	}
	resetLazygitProbe()
	t.Cleanup(func() {
		lookPath = orig
		resetLazygitProbe()
	})
}

// TestLazygitPreconfiguredWhenOnPath guards #750: with lazygit on PATH the
// default config ships it as the preconfigured example tool pane, so the
// tool.lazygit command exists without any user configuration.
func TestLazygitPreconfiguredWhenOnPath(t *testing.T) {
	stubLazygit(t, true)
	c := defaults()
	if len(c.Tools.Custom) != 1 {
		t.Fatalf("default tools = %+v, want the lazygit example entry", c.Tools.Custom)
	}
	e := c.Tools.Custom[0]
	if e.Name != "lazygit" || e.Command != "lazygit" || e.Placement != "bottom" {
		t.Fatalf("lazygit default entry = %+v", e)
	}
}

// TestLazygitOmittedWhenMissing guards the no-hard-dependency half of #750:
// without the binary the default entry is omitted (tools.setup offers the
// install instead).
func TestLazygitOmittedWhenMissing(t *testing.T) {
	stubLazygit(t, false)
	if c := defaults(); len(c.Tools.Custom) != 0 {
		t.Fatalf("default tools = %+v, want none without lazygit on PATH", c.Tools.Custom)
	}
}

// TestUserToolsOverrideDefault: a user-defined tools.custom list replaces the
// default lazygit entry wholesale, like any other setting layer.
func TestUserToolsOverrideDefault(t *testing.T) {
	stubLazygit(t, true)
	user := writeUser(t, `
[[tools.custom]]
name = "htop"
command = "htop"
`)
	c, _ := Load(Options{UserPath: user})
	if len(c.Tools.Custom) != 1 || c.Tools.Custom[0].Name != "htop" {
		t.Fatalf("merged tools = %+v, want only the user's htop", c.Tools.Custom)
	}
}
