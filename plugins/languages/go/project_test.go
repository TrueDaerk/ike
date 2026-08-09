package langgo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectOptionsReflectGoBinary guards the single wizard row.
func TestProjectOptionsReflectGoBinary(t *testing.T) {
	prevLook, prevResolve := goLook, goResolve
	t.Cleanup(func() { goLook, goResolve = prevLook, prevResolve })
	goLook = func(string) (string, error) { return "/usr/local/bin/go", nil }
	goResolve = func(_, p string) string { return p }

	opts := toolchain{}.ProjectOptions()
	if len(opts) != 1 || opts[0].ID != "gomod" || !opts[0].Available || opts[0].Detail != "/usr/local/bin/go" {
		t.Fatalf("options = %+v", opts)
	}
}

// TestScaffoldInitsModuleAndMain guards the scaffold: go mod init with the
// directory name, plus the main.go seed.
func TestScaffoldInitsModuleAndMain(t *testing.T) {
	prevLook, prevResolve, prevRun := goLook, goResolve, goProjRun
	t.Cleanup(func() { goLook, goResolve, goProjRun = prevLook, prevResolve, prevRun })
	goLook = func(string) (string, error) { return "/usr/local/bin/go", nil }
	goResolve = func(_, p string) string { return p }

	root := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []string
	goProjRun = func(dir, name string, args ...string) (string, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return "", nil
	}

	if err := (toolchain{}).ScaffoldProject(root, "gomod"); err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	if len(calls) != 1 || calls[0] != "/usr/local/bin/go mod init myproj" {
		t.Fatalf("calls = %v", calls)
	}
	if _, err := os.Stat(filepath.Join(root, "main.go")); err != nil {
		t.Fatalf("main.go not seeded: %v", err)
	}
	if err := (toolchain{}).ScaffoldProject(root, "cargo"); err == nil {
		t.Fatal("unknown option must refuse")
	}
}
