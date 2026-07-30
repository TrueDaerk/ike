package langpython

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/format"
)

// writeTool drops an executable printing its own name into dir.
func writeTool(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '" + name + "'\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runPython(t *testing.T) string {
	t.Helper()
	prov, ok := format.Resolve("python", "x.py")
	if !ok {
		t.Fatal("python default must resolve")
	}
	res, err := prov.Format(context.Background(), format.Request{
		Path: "x.py", Language: "python", Lines: []string{"x=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return *res.Text
}

// TestPythonRuffBeforeBlack: ruff wins when both are installed; black serves
// when ruff is missing (#1405).
func TestPythonRuffBeforeBlack(t *testing.T) {
	dir := t.TempDir()
	writeTool(t, dir, "ruff")
	writeTool(t, dir, "black")
	t.Setenv("PATH", dir)
	t.Chdir(t.TempDir()) // no venv candidates

	if got := runPython(t); got != "ruff" {
		t.Fatalf("ruff must win, got %q", got)
	}
	blackOnly := t.TempDir()
	writeTool(t, blackOnly, "black")
	t.Setenv("PATH", blackOnly)
	if got := runPython(t); got != "black" {
		t.Fatalf("black must serve without ruff, got %q", got)
	}
}

// TestPythonVenvWins: a project-local .venv/bin/ruff beats the PATH install
// (the plugin's venv-first resolution, mirroring the interpreter logic).
func TestPythonVenvWins(t *testing.T) {
	path := t.TempDir()
	writeTool(t, path, "ruff")
	t.Setenv("PATH", path)
	project := t.TempDir()
	writeTool(t, filepath.Join(project, ".venv", "bin"), "ruff")
	// the venv copy identifies itself distinctly
	venvRuff := filepath.Join(project, ".venv", "bin", "ruff")
	if err := os.WriteFile(venvRuff, []byte("#!/bin/sh\nprintf 'venv-ruff'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)

	if got := runPython(t); got != "venv-ruff" {
		t.Fatalf("venv ruff must win, got %q", got)
	}
}

// TestPythonNoToolHintOnce: nothing installed — no resolution, one hint.
func TestPythonNoToolHintOnce(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Chdir(t.TempDir())
	var hints []string
	format.SetNotifier(func(text string) { hints = append(hints, text) })
	t.Cleanup(func() { format.SetNotifier(nil) })

	for i := 0; i < 2; i++ {
		if _, ok := format.Resolve("python", "x.py"); ok {
			t.Fatal("must not resolve without any binary")
		}
	}
	if len(hints) != 1 || !strings.Contains(hints[0], "pip install ruff") {
		t.Fatalf("want one ruff hint, got %v", hints)
	}
}
