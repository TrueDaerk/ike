package toolcatalog

import (
	"errors"
	"strings"
	"testing"
)

// stubLookPath fakes PATH resolution: names in present resolve, everything
// else fails. Restored on cleanup.
func stubLookPath(t *testing.T, present ...string) {
	t.Helper()
	orig := LookPath
	set := make(map[string]bool, len(present))
	for _, p := range present {
		set[p] = true
	}
	LookPath = func(name string) (string, error) {
		if set[name] {
			return "/fake/bin/" + name, nil
		}
		return "", errors.New(name + " not found")
	}
	t.Cleanup(func() { LookPath = orig })
}

func find(t *testing.T, entries []Entry, name string) Entry {
	t.Helper()
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("entry %q not in %v", name, entries)
	return Entry{}
}

func TestCatalogEntriesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range All() {
		if e.Name == "" || e.Command == "" || e.Description == "" {
			t.Errorf("entry %+v missing name/command/description", e)
		}
		if seen[e.Name] {
			t.Errorf("duplicate catalog name %q", e.Name)
		}
		seen[e.Name] = true
		if len(e.Recipes) == 0 {
			t.Errorf("entry %s has no install recipe", e.Name)
		}
		for _, r := range e.Recipes {
			if len(r) == 0 {
				t.Errorf("entry %s has an empty recipe", e.Name)
			}
		}
		switch e.Placement {
		case "", "bottom", "right":
		default:
			t.Errorf("entry %s has invalid placement %q", e.Name, e.Placement)
		}
	}
	for _, want := range []string{"lazygit", "lazydocker", "sqlit"} {
		if !seen[want] {
			t.Errorf("catalog is missing %s", want)
		}
	}
}

func TestOfferedHidesGatedEntries(t *testing.T) {
	stubLookPath(t) // nothing on PATH: docker and kubectl absent
	for _, e := range Offered() {
		if e.Requires != "" {
			t.Errorf("gated entry %s offered without %s", e.Name, e.Requires)
		}
	}

	stubLookPath(t, "docker")
	offered := Offered()
	find(t, offered, "lazydocker")
	for _, e := range offered {
		if e.Name == "k9s" {
			t.Error("k9s offered without kubectl")
		}
	}
}

func TestInstalledAndInstallArgv(t *testing.T) {
	stubLookPath(t, "go")
	e := find(t, All(), "lazygit")
	if e.Installed() {
		t.Error("lazygit reported installed with empty PATH")
	}
	argv, ok := e.InstallArgv()
	if !ok || argv[0] != "go" {
		t.Errorf("expected go recipe, got %v ok=%v", argv, ok)
	}

	stubLookPath(t, "brew", "go", "lazygit")
	if !e.Installed() {
		t.Error("lazygit not reported installed")
	}
	argv, _ = e.InstallArgv()
	if argv[0] != "brew" {
		t.Errorf("brew should win over go, got %v", argv)
	}
}

func TestInstallRunsRecipeAndReverifies(t *testing.T) {
	e := find(t, All(), "lazygit")

	// Success: recipe runs, binary appears afterwards.
	stubLookPath(t, "brew")
	var ran []string
	origRun := RunInstall
	RunInstall = func(argv []string) ([]byte, error) {
		ran = argv
		stubLookPath(t, "brew", "lazygit") // install "puts it on PATH"
		return []byte("ok"), nil
	}
	t.Cleanup(func() { RunInstall = origRun })
	msg := Install(e)().(InstallResultMsg)
	if msg.Err != nil {
		t.Fatalf("install failed: %v", msg.Err)
	}
	if strings.Join(ran, " ") != "brew install lazygit" {
		t.Errorf("ran %v", ran)
	}

	// Failure: installer errors, output tail lands in Detail.
	stubLookPath(t, "brew")
	RunInstall = func([]string) ([]byte, error) {
		return []byte("line1\nline2\nboom: no bottle"), errors.New("exit 1")
	}
	msg = Install(e)().(InstallResultMsg)
	if msg.Err == nil || !strings.Contains(msg.Detail, "boom: no bottle") {
		t.Errorf("want failure with output tail, got err=%v detail=%q", msg.Err, msg.Detail)
	}

	// Exit 0 but the binary still missing is a failure (#370 semantics).
	stubLookPath(t, "brew")
	RunInstall = func([]string) ([]byte, error) { return nil, nil }
	msg = Install(e)().(InstallResultMsg)
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "still not on PATH") {
		t.Errorf("want still-not-on-PATH failure, got %v", msg.Err)
	}

	// No installer available at all.
	stubLookPath(t)
	msg = Install(e)().(InstallResultMsg)
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "no supported installer") {
		t.Errorf("want no-installer failure, got %v", msg.Err)
	}

	// Already installed short-circuits without running anything.
	stubLookPath(t, "lazygit")
	RunInstall = func([]string) ([]byte, error) {
		t.Error("RunInstall called for an installed tool")
		return nil, nil
	}
	if msg = Install(e)().(InstallResultMsg); msg.Err != nil {
		t.Errorf("installed tool should succeed immediately: %v", msg.Err)
	}
}
