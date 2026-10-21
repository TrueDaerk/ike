package toolcatalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prettier_test.go covers the prettier install entry and the install
// tracking every Install shares (#3023).

func TestPrettierInstallsGloballyViaNpm(t *testing.T) {
	stubLookPath(t, "npm")
	argv, ok := Prettier.InstallArgv()
	if !ok || strings.Join(argv, " ") != PrettierInstall {
		t.Fatalf("prettier installs with %q, got %v (%v)", PrettierInstall, argv, ok)
	}
	if !NpmAvailable() {
		t.Fatal("npm is on the faked PATH")
	}
	stubLookPath(t)
	if NpmAvailable() {
		t.Fatal("npm is not on the faked PATH")
	}
	for _, e := range All() {
		if e.Name == Prettier.Name {
			t.Fatal("prettier is a formatter, not a tool-pane catalog entry")
		}
	}
}

func TestInstallTracksRunningAndLastResult(t *testing.T) {
	ResetInstallsForTest()
	t.Cleanup(ResetInstallsForTest)
	stubLookPath(t, "npm")
	orig := RunInstall
	t.Cleanup(func() { RunInstall = orig })
	RunInstall = func(argv []string) ([]byte, error) { return []byte("ok"), nil }

	cmd := Install(Prettier)
	if cmd == nil || !Installing(Prettier.Name) {
		t.Fatal("a started install must be tracked as running")
	}
	if Install(Prettier) != nil {
		t.Fatal("a second install while one runs must not start")
	}
	msg := cmd().(InstallResultMsg)
	if Installing(Prettier.Name) {
		t.Fatal("a finished install is no longer running")
	}
	last, ok := LastInstall(Prettier.Name)
	if !ok || last != msg {
		t.Fatalf("the finished result must be recorded, got %+v (%v)", last, ok)
	}
	// The faked install leaves prettier off PATH: a verified failure.
	if msg.Err == nil || !strings.Contains(msg.Err.Error(), "still not on PATH") {
		t.Fatalf("want the post-install PATH check to fail, got %v", msg.Err)
	}
	if Install(Prettier) == nil {
		t.Fatal("after it finished, a new install may start")
	}
}

func TestPrettierLocal(t *testing.T) {
	root := t.TempDir()
	if PrettierLocal(root) || PrettierLocal("") {
		t.Fatal("no node_modules, no local prettier")
	}
	bin := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "prettier"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if !PrettierLocal(root) {
		t.Fatal("node_modules/.bin/prettier is a project-local prettier")
	}
}
