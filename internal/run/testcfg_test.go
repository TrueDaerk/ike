package run

import (
	"path/filepath"
	"reflect"
	"testing"

	"ike/internal/lang"
)

func init() {
	lang.Register(lang.Language{
		ID:         "runtst",
		Extensions: []string{"rt"},
		Test: &lang.TestSpec{
			FilePattern: `_test\.rt$`,
			Pattern:     `^func (?P<name>(?P<kind>Test)\w*)\s*\(`,
			Kinds:       map[string][]string{"Test": {"{interpreter}", "test", "-run", "^{name}$"}},
			FileArgv:    []string{"{interpreter}", "test"},
			Tool:        "rtool",
		},
	})
}

// TestTestConfigSingle pins the synthesized single-test configuration:
// project-relative file, cwd = the file's directory, a stable name folding
// repeated runs of the same test into one Upsert target.
func TestTestConfigSingle(t *testing.T) {
	root := "/proj"
	file := filepath.Join(root, "pkg", "a_test.rt")
	cfg, ok := TestConfig(root, file, &lang.TestMatch{Name: "TestX", Kind: "Test"})
	if !ok {
		t.Fatal("TestConfig must synthesize for a test file")
	}
	if !cfg.Tests || cfg.TestName != "TestX" || cfg.TestKind != "Test" {
		t.Fatalf("test fields wrong: %+v", cfg)
	}
	if cfg.File != filepath.Join("pkg", "a_test.rt") || cfg.Cwd != "pkg" {
		t.Fatalf("file/cwd wrong: %+v", cfg)
	}
	if cfg.Name != "TestX (pkg)" {
		t.Fatalf("name = %q, want %q", cfg.Name, "TestX (pkg)")
	}
	if got := cfg.Dir(root); got != filepath.Join(root, "pkg") {
		t.Fatalf("Dir = %q", got)
	}
}

// TestTestConfigFileScope pins the whole-file-scope form.
func TestTestConfigFileScope(t *testing.T) {
	root := "/proj"
	cfg, ok := TestConfig(root, filepath.Join(root, "pkg", "a_test.rt"), nil)
	if !ok || !cfg.Tests || cfg.TestName != "" {
		t.Fatalf("file-scope config wrong: %+v, ok=%v", cfg, ok)
	}
	if cfg.Name != "tests: pkg" {
		t.Fatalf("name = %q", cfg.Name)
	}
}

// TestTestConfigRejectsNonTestFile: no runner without a matching TestSpec.
func TestTestConfigRejectsNonTestFile(t *testing.T) {
	if _, ok := TestConfig("/proj", "/proj/pkg/a.rt", nil); ok {
		t.Fatal("a non-test file must not synthesize a test config")
	}
	if _, ok := TestConfig("/proj", "/proj/pkg/a.unknowable", nil); ok {
		t.Fatal("an unknown file type must not synthesize a test config")
	}
}

// TestArgvTestBranch: Argv routes test-scope configs through the TestSpec
// templates — the rerun path (store.Last -> Argv) reproduces the exact test.
func TestArgvTestBranch(t *testing.T) {
	root := "/proj"
	cfg, _ := TestConfig(root, filepath.Join(root, "pkg", "a_test.rt"), &lang.TestMatch{Name: "TestX", Kind: "Test"})
	argv, ok := Argv(root, cfg, "")
	if !ok || !reflect.DeepEqual(argv, []string{"rtool", "test", "-run", "^TestX$"}) {
		t.Fatalf("single-test argv = %v, ok=%v", argv, ok)
	}
	fileCfg, _ := TestConfig(root, filepath.Join(root, "pkg", "a_test.rt"), nil)
	argv, ok = Argv(root, fileCfg, "")
	if !ok || !reflect.DeepEqual(argv, []string{"rtool", "test"}) {
		t.Fatalf("file-scope argv = %v, ok=%v", argv, ok)
	}
}

// TestTestConfigRerunMemory: touching the upserted config makes it the
// rerun-last target, exactly like an ordinary run.
func TestTestConfigRerunMemory(t *testing.T) {
	root := "/proj"
	var s Store
	cfg, _ := TestConfig(root, filepath.Join(root, "pkg", "a_test.rt"), &lang.TestMatch{Name: "TestX", Kind: "Test"})
	stored := s.Upsert(cfg)
	s.Touch(stored.Name)
	if last := s.Last(); last == nil || !last.Tests || last.TestName != "TestX" {
		t.Fatalf("Last = %+v, want the test config", s.Last())
	}
	// A second run of the same test folds into the same config.
	again, _ := TestConfig(root, filepath.Join(root, "pkg", "a_test.rt"), &lang.TestMatch{Name: "TestX", Kind: "Test"})
	s.Upsert(again)
	if len(s.Configs) != 1 {
		t.Fatalf("configs = %d, want 1", len(s.Configs))
	}
}
