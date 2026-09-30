package safego

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// goguard_test.go is the static guard of #2836: every goroutine IKE starts
// goes through safego.Go (or defers safego.Recover), because a bare `go`
// statement's panic kills the process with no crash report. The test parses
// every non-test Go file under internal/, plugins/ and cmd/ike and fails on a
// `go` statement outside the allowlist below. An entry needs a reason — the
// ledger is an audit, not an opt-out list.

// bareGoAllowed maps "<repo-relative file>" or "<file>:<func>" to the reason
// the goroutine may start bare.
var bareGoAllowed = map[string]string{
	"internal/safego/safego.go": "the guard itself: Go's own goroutine is the one that installs the recover",
}

func TestNoBareGoStatements(t *testing.T) {
	root := repoRoot(t)
	var offenders []string
	used := map[string]bool{}
	for _, dir := range []string{"internal", "plugins", "cmd/ike"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if info.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					gs, ok := n.(*ast.GoStmt)
					if !ok {
						return true
					}
					key := rel + ":" + fd.Name.Name
					switch {
					case bareGoAllowed[rel] != "":
						used[rel] = true
					case bareGoAllowed[key] != "":
						used[key] = true
					default:
						offenders = append(offenders, rel+":"+strconvItoa(fset.Position(gs.Pos()).Line)+" in "+fd.Name.Name)
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(offenders)
	for _, o := range offenders {
		t.Errorf("bare go statement: %s — start it with safego.Go(name, fn) or add an allowlist entry with a reason", o)
	}
	for key := range bareGoAllowed {
		if !used[key] {
			t.Errorf("stale allowlist entry %q: no bare go statement there any more", key)
		}
	}
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// repoRoot walks up from the package directory to the module root (go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above " + dir)
		}
		dir = parent
	}
}
