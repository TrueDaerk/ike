package langgo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ike/internal/lang"
)

// project.go implements lang.ProjectScaffolder (#1718): a new Go project is a
// module — `go mod init <dirname>` plus a main.go seed. The single option is
// disabled (never hidden) when no go binary resolves.

// goProjRun is the subprocess seam for tests.
var goProjRun = func(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ProjectOptions implements lang.ProjectScaffolder.
func (t toolchain) ProjectOptions() []lang.ProjectOption {
	bin, ok := t.Interpreter(".")
	return []lang.ProjectOption{{
		ID:        "gomod",
		Label:     "Go module — go.mod + main.go",
		Detail:    bin,
		Available: ok,
		Reason:    "go not found on PATH",
	}}
}

// ScaffoldProject implements lang.ProjectScaffolder.
func (t toolchain) ScaffoldProject(root, option string) error {
	if option != "gomod" {
		return fmt.Errorf("unknown go project option %q", option)
	}
	bin, ok := t.Interpreter(root)
	if !ok {
		return fmt.Errorf("go not found on PATH")
	}
	// The directory name is the module path — enough for a fresh local
	// project; a real import path is a later `go mod edit`.
	if out, err := goProjRun(root, bin, "mod", "init", filepath.Base(root)); err != nil {
		out = strings.TrimSpace(out)
		if out == "" {
			return fmt.Errorf("go mod init failed: %v", err)
		}
		return fmt.Errorf("go mod init failed: %v — %s", err, out)
	}
	return os.WriteFile(filepath.Join(root, "main.go"), []byte(goMain), 0o644)
}

// goMain seeds the module's entry point.
const goMain = `package main

import "fmt"

func main() {
	fmt.Println("Hello from your new project!")
}
`
