package langphp

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"ike/internal/lang"
)

// phpunitProject lays out a project root with a vendored phpunit and a test
// file, returning the root and the test file's absolute path.
func phpunitProject(t *testing.T) (root, file string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vendor", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "vendor", "bin", "phpunit")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(root, "tests", "CalculatorTest.php")
	if err := os.WriteFile(file, []byte(calculatorTest), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, file
}

const calculatorTest = `<?php

namespace App\Tests;

use PHPUnit\Framework\TestCase;

class CalculatorTest extends TestCase
{
    public function testAdd(): void
    {
    }

    public static function sumProvider(): array
    {
        return [];
    }

    public function testSum(int $a, int $b): void
    {
    }

    private function helperNotATest(): void
    {
    }
}
`

func TestPHPUnitDetectsTestMethods(t *testing.T) {
	_, file := phpunitProject(t)
	if !lang.HasTests(file) {
		t.Fatal("*Test.php is not recognized as a test file")
	}
	if lang.HasTests(filepath.Join(filepath.Dir(file), "Calculator.php")) {
		t.Error("a plain .php file is recognized as a test file")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range lang.TestsInFile(file, strings.Split(string(raw), "\n")) {
		names = append(names, m.Name)
	}
	want := []string{"testAdd", "testSum"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("detected %v, want %v", names, want)
	}
}

func TestPHPUnitArgv(t *testing.T) {
	root, file := phpunitProject(t)
	bin := filepath.Join(root, "vendor", "bin", "phpunit")

	// Whole file, structured: the vendored binary, the root-relative file
	// path and the machine-readable format.
	got, ok := lang.TestStructuredArgv(root, file, nil, "")
	if !ok {
		t.Fatal("no structured argv for a PHPUnit file")
	}
	want := []string{bin, "tests/CalculatorTest.php", "--teamcity"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("file argv = %v, want %v", got, want)
	}

	// One test: an anchored ::method filter that still admits its data sets.
	got, ok = lang.TestStructuredArgv(root, file, &lang.TestMatch{Name: "testSum"}, "")
	if !ok {
		t.Fatal("no structured argv for a single test")
	}
	want = []string{bin, "--filter", `/::testSum( |$)/`, "tests/CalculatorTest.php", "--teamcity"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("single-test argv = %v, want %v", got, want)
	}

	// Re-run failed: one filter alternating the failed method names.
	got, ok = lang.TestFailedArgv(root, file, []string{"testAdd", "testSum"}, "")
	if !ok {
		t.Fatal("no failed-argv for a PHPUnit file")
	}
	want = []string{bin, "--filter", `/::(testAdd|testSum)( |$)/`, "tests/CalculatorTest.php", "--teamcity"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("failed argv = %v, want %v", got, want)
	}
}

// A PHPUnit run happens at the project root — phpunit.xml and the composer
// autoloader live there, not in tests/.
func TestPHPUnitRunsAtRoot(t *testing.T) {
	_, file := phpunitProject(t)
	if !lang.TestRunsAtRoot(file) {
		t.Error("PHPUnit tests do not run at the project root")
	}
}

func TestPHPUnitRunnerResolution(t *testing.T) {
	root, _ := phpunitProject(t)
	if got := phpunitRunner(root); !reflect.DeepEqual(got, []string{filepath.Join(root, "vendor", "bin", "phpunit")}) {
		t.Errorf("runner = %v, want the vendored binary", got)
	}

	// No vendored binary, but a phpunit.xml.dist: the global binary stands in.
	bare := t.TempDir()
	if err := os.WriteFile(filepath.Join(bare, "phpunit.xml.dist"), []byte("<phpunit/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := phpunitLook
	phpunitLook = func(string) (string, error) { return "/usr/local/bin/phpunit", nil }
	defer func() { phpunitLook = restore }()
	if got := phpunitRunner(bare); !reflect.DeepEqual(got, []string{"/usr/local/bin/phpunit"}) {
		t.Errorf("runner = %v, want the global binary", got)
	}

	// Neither: the seam falls back to the spec's plain tool name, so the
	// argv stays readable instead of naming an unrelated binary.
	if got := phpunitRunner(t.TempDir()); got != nil {
		t.Errorf("runner = %v, want nil for a non-PHPUnit project", got)
	}
}
