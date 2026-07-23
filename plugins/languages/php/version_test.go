package langphp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestComposerConstraintToPHPVersion guards #1079: the require.php constraint
// yields its minimum bound, normalized to major.minor.patch.
func TestComposerConstraintToPHPVersion(t *testing.T) {
	cases := map[string]string{
		"^7.3":         "7.3.0",
		">=7.3 <8.0":   "7.3.0",
		"~8.2.4":       "8.2.4",
		"8.1.*":        "8.1.0",
		"7.4.33":       "7.4.33",
		"*":            "",
		"":             "",
		"dev-main":     "",
		">=8.0.0 <9.0": "8.0.0",
	}
	for constraint, want := range cases {
		if got := firstVersion(constraint); got != want {
			t.Errorf("firstVersion(%q) = %q want %q", constraint, got, want)
		}
	}
}

// TestDetectPrefersComposerOverInterpreter guards #1079's priority: the
// project's composer constraint wins over the interpreter's own version.
func TestDetectPrefersComposerOverInterpreter(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"),
		[]byte(`{"require":{"php":"^7.3","ext-json":"*"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	origOut, origLook := phpVersionOut, phpLook
	phpVersionOut = func(string) string { return "PHP 8.2.1 (cli) (built: Jan  1 2026)" }
	phpLook = func(string) (string, error) { return "/usr/bin/php", nil }
	defer func() { phpVersionOut, phpLook = origOut, origLook }()

	got, ok := toolchain{}.Detect(root)
	if !ok {
		t.Fatal("Detect must inject with a composer constraint present")
	}
	v := got["intelephense"].(map[string]any)["environment"].(map[string]any)["phpVersion"]
	if v != "7.3.0" {
		t.Fatalf("phpVersion = %v want 7.3.0 (composer wins)", v)
	}
}

// TestDetectFallsBackToInterpreterVersion guards #1079: no composer.json →
// the detected interpreter's `php -v` supplies the version.
func TestDetectFallsBackToInterpreterVersion(t *testing.T) {
	origOut, origLook := phpVersionOut, phpLook
	phpVersionOut = func(string) string { return "PHP 8.2.1 (cli) (built: Jan  1 2026)" }
	phpLook = func(string) (string, error) { return "/usr/bin/php", nil }
	defer func() { phpVersionOut, phpLook = origOut, origLook }()

	got, ok := toolchain{}.Detect(t.TempDir())
	if !ok {
		t.Fatal("Detect must fall back to php -v")
	}
	v := got["intelephense"].(map[string]any)["environment"].(map[string]any)["phpVersion"]
	if v != "8.2.1" {
		t.Fatalf("phpVersion = %v want 8.2.1", v)
	}
}

// TestDetectNothingWithoutSources guards #1079: no composer, no interpreter →
// the server keeps its default.
func TestDetectNothingWithoutSources(t *testing.T) {
	origOut, origLook := phpVersionOut, phpLook
	phpVersionOut = func(string) string { return "" }
	phpLook = func(string) (string, error) { return "", os.ErrNotExist }
	defer func() { phpVersionOut, phpLook = origOut, origLook }()

	if _, ok := (toolchain{}.Detect(t.TempDir())); ok {
		t.Fatal("Detect must report nothing without a version source")
	}
}
