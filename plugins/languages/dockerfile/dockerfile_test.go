package langdockerfile

import (
	"testing"

	"ike/internal/lang"
)

// TestDockerfileRegistered guards #896: exact base names (Dockerfile,
// Containerfile — the first Filenames user besides templates, exercising the
// nameIx path) and the .dockerfile extension both resolve, with the
// docker-langserver attached and # line comments.
func TestDockerfileRegistered(t *testing.T) {
	for _, path := range []string{
		"/p/Dockerfile",
		"/p/Containerfile",
		"/p/api.dockerfile",
	} {
		l, ok := lang.ByPath(path)
		if !ok {
			t.Errorf("%s: no language registered", path)
			continue
		}
		if l.ID != "dockerfile" {
			t.Errorf("%s → %s, want dockerfile", path, l.ID)
		}
	}

	// A base name that merely contains "Dockerfile" must not match the
	// exact-name index; the extension path is the only fallback.
	if l, ok := lang.ByPath("/p/Dockerfile.bak"); ok && l.ID == "dockerfile" {
		t.Error("Dockerfile.bak must not resolve to dockerfile")
	}

	l, _ := lang.ByID("dockerfile")
	if l.Server == nil || l.Server.Command != "docker-langserver" {
		t.Errorf("server = %+v, want docker-langserver", l.Server)
	}
	line, _, ok := lang.Comments("/p/Dockerfile")
	if !ok || line != "#" {
		t.Errorf("line comment = %q/%v, want #", line, ok)
	}
}
