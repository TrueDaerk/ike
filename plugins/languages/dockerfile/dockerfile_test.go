package langdockerfile

import (
	"testing"

	"ike/internal/lang"
	"ike/internal/permhint"
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

// TestDockerfilePermissionHints (#1656): the Spans hook is wired, so the
// --chmod= flag of COPY carries its symbolic form.
func TestDockerfilePermissionHints(t *testing.T) {
	l, ok := lang.ByPath("/p/Dockerfile")
	if !ok || l.Spans == nil {
		t.Fatal("dockerfile: no Spans producer registered")
	}
	spans := l.Spans([]string{"FROM alpine", "COPY --chmod=0755 entrypoint.sh /"})
	if len(spans) != 1 || spans[0].Line != 1 {
		t.Fatalf("spans = %+v, want one on line 1", spans)
	}
	if want := "0755" + permhint.Gap + "rwxr-xr-x"; spans[0].Replace != want {
		t.Errorf("Replace = %q, want %q", spans[0].Replace, want)
	}
}
