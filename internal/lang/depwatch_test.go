package lang

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// registerDepLangs registers the three shapes the dependency-marker seam has
// to cover (#2613): plain static markers, markers inside a pruned tree, and a
// language mixing a glob with a toolchain-resolved directory and the restart
// flag.
func registerDepLangs(t *testing.T, siteDir string) {
	t.Helper()
	Register(Language{
		ID:   "depgo",
		Deps: &DepWatch{Files: []string{"go.mod", "go.sum"}},
	})
	Register(Language{
		ID:   "depphp",
		Deps: &DepWatch{Files: []string{"composer.json", "vendor/composer/installed.json"}},
	})
	Register(Language{
		ID: "deppy",
		Deps: &DepWatch{
			Files:   []string{"requirements*.txt", "pyproject.toml"},
			Dirs:    func(string) []string { return []string{siteDir} },
			Restart: true,
		},
	})
}

// TestDepWatchPathsPerLanguage guards the per-language marker list: every
// declared static name resolves against the root, a marker inside a pruned
// tree keeps its sub-path, the toolchain-resolved directory joins as an
// absolute path, and glob entries contribute no path at all (they name no
// file to register a watch on).
func TestDepWatchPathsPerLanguage(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	registerDepLangs(t, site)

	got := DepWatchPaths(root)
	want := map[string]string{
		filepath.Join(root, "go.mod"):                               "depgo",
		filepath.Join(root, "go.sum"):                               "depgo",
		filepath.Join(root, "composer.json"):                        "depphp",
		filepath.Join(root, "vendor", "composer", "installed.json"): "depphp",
		filepath.Join(root, "pyproject.toml"):                       "deppy",
		site:                                                        "deppy",
	}
	for p, lang := range want {
		if got[p] != lang {
			t.Errorf("DepWatchPaths[%q] = %q, want %q", p, got[p], lang)
		}
	}
	// The glob entry registers nothing: a pattern names no path.
	if l, ok := got[filepath.Join(root, "requirements*.txt")]; ok {
		t.Errorf("glob entry registered as a path (owner %q)", l)
	}
	// And the registration stays non-recursive: nothing below the marker
	// directory is registered.
	for p := range got {
		if p != site && filepath.Dir(p) == site {
			t.Errorf("path below the marker directory registered: %q", p)
		}
	}
}

// TestDepWatchMatch covers the three matching shapes an event can take: the
// marker itself, an entry of a marker *directory* (how a dist-info install
// reports), and a glob marker resolved against the root.
func TestDepWatchMatch(t *testing.T) {
	root := t.TempDir()
	site := filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	registerDepLangs(t, site)
	registered := DepWatchPaths(root)

	cases := []struct {
		path string
		want string
	}{
		{filepath.Join(root, "go.sum"), "depgo"},
		{filepath.Join(root, "vendor", "composer", "installed.json"), "depphp"},
		{filepath.Join(site, "rich-13.7.0.dist-info"), "deppy"},
		{filepath.Join(root, "requirements.txt"), "deppy"},
		{filepath.Join(root, "requirements-dev.txt"), "deppy"},
		{filepath.Join(root, "main.go"), ""},
		{filepath.Join(root, "src", "requirements.txt"), ""}, // glob never crosses a slash
		{filepath.Join(site, "rich", "console.py"), ""},      // the subtree is not a marker
		{filepath.Join(t.TempDir(), "go.mod"), ""},           // another project's marker
	}
	for _, c := range cases {
		got, ok := DepWatchMatch(root, c.path, registered)
		if c.want == "" {
			if ok {
				t.Errorf("DepWatchMatch(%q) = %q, want no match", c.path, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("DepWatchMatch(%q) = %q,%v; want %q,true", c.path, got, ok, c.want)
		}
	}
}

// TestDepWatchRestartPerLanguage guards the notify-vs-restart decision: it is
// a per-language flag, not a central rule, and defaults to notify-only.
func TestDepWatchRestartPerLanguage(t *testing.T) {
	registerDepLangs(t, t.TempDir())
	Register(Language{ID: "depnone"})

	for lang, want := range map[string]bool{
		"deppy":        true,
		"depgo":        false,
		"depphp":       false,
		"depnone":      false,
		"no-such-lang": false,
	} {
		if got := DepWatchRestart(lang); got != want {
			t.Errorf("DepWatchRestart(%q) = %v, want %v", lang, got, want)
		}
	}
}

// TestRegisteredLanguagesDeclareMarkers pins the marker lists the shipped
// language plugins are expected to carry, without importing them: the
// registry is the contract, and a language declaring Deps must at least name
// its manifest. Only the languages registered in this test binary are seen,
// so this asserts the *shape* rule every declaration has to satisfy.
func TestRegisteredLanguagesDeclareMarkers(t *testing.T) {
	registerDepLangs(t, t.TempDir())
	for _, l := range depWatchLangs() {
		if len(l.Deps.Files) == 0 && l.Deps.Dirs == nil {
			t.Errorf("language %q declares Deps with neither Files nor Dirs", l.ID)
		}
		for _, f := range l.Deps.Files {
			if filepath.IsAbs(f) {
				t.Errorf("language %q: marker %q must be root-relative", l.ID, f)
			}
		}
	}
	// The list is ordered so every derived registration is deterministic.
	var ids []string
	for _, l := range depWatchLangs() {
		ids = append(ids, l.ID)
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(ids, sorted) {
		t.Errorf("depWatchLangs order = %v, want sorted", ids)
	}
}
