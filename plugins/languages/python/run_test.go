package langpython

import (
	"os"
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

// pkgTree lays out root/pkg/sub with __init__.py files and returns root.
func pkgTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"pkg", "pkg/sub", "plain"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"pkg/__init__.py", "pkg/sub/__init__.py", "pkg/sub/mod.py", "pkg/__main__.py", "plain/script.py", "top.py"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("pass\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestModuleDetection verifies the `-m` spelling rules: full package chains
// resolve dotted modules, __main__.py maps to its package, everything else
// runs as a file.
func TestModuleDetection(t *testing.T) {
	root := pkgTree(t)
	tc := toolchain{}
	cases := []struct {
		file, want string
		ok         bool
	}{
		{"pkg/sub/mod.py", "pkg.sub.mod", true},
		{"pkg/__main__.py", "pkg", true},
		{"pkg/__init__.py", "", false},
		{"plain/script.py", "", false}, // no __init__.py in plain/
		{"top.py", "", false},          // top-level scripts run as files
	}
	for _, c := range cases {
		got, ok := tc.Module(root, filepath.Join(root, c.file))
		if ok != c.ok || got != c.want {
			t.Errorf("Module(%s) = %q/%v, want %q/%v", c.file, got, ok, c.want, c.ok)
		}
	}
	if _, ok := tc.Module(root, "/elsewhere/x.py"); ok {
		t.Error("files outside the root have no module form")
	}
}

// TestRunCommandForms verifies file and module invocations.
func TestRunCommandForms(t *testing.T) {
	tc := toolchain{}
	argv, ok := tc.RunCommand("/r", lang.RunSpec{File: "/r/x.py", Args: []string{"-v"}}, "/venv/bin/python")
	if !ok || len(argv) != 3 || argv[0] != "/venv/bin/python" || argv[1] != "/r/x.py" || argv[2] != "-v" {
		t.Fatalf("file form = %v", argv)
	}
	argv, _ = tc.RunCommand("/r", lang.RunSpec{File: "/r/pkg/m.py", Module: "pkg.m"}, "")
	if argv[0] != "python3" || argv[1] != "-m" || argv[2] != "pkg.m" {
		t.Fatalf("module form = %v", argv)
	}
}
