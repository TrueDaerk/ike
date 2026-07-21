package langtoml

import (
	"testing"

	"ike/internal/lang"
)

// TestTOMLRegistered guards #895: .toml resolves to the toml language with the
// taplo server and # line comments.
func TestTOMLRegistered(t *testing.T) {
	l, ok := lang.ByPath("/p/Cargo.toml")
	if !ok {
		t.Fatal("no language registered for .toml")
	}
	if l.ID != "toml" {
		t.Errorf("id = %s, want toml", l.ID)
	}
	if l.Server == nil || l.Server.Command != "taplo" {
		t.Errorf("server = %+v, want taplo", l.Server)
	}
	line, _, ok := lang.Comments("/p/config.toml")
	if !ok || line != "#" {
		t.Errorf("line comment = %q/%v, want #", line, ok)
	}
}
