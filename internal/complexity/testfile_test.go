package complexity

import (
	"path/filepath"
	"testing"

	_ "ike/plugins/languages/go"
	_ "ike/plugins/languages/php"
	_ "ike/plugins/languages/python"
)

func TestIsTestFile(t *testing.T) {
	root := filepath.FromSlash("/proj")
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"a_test.go", true},
		{"test_x.py", true},
		{"x_test.py", true},
		{"FooTest.php", true},
		{"foo.spec.ts", true},
		{"foo.test.js", true},
		{"src/__tests__/foo.ts", true},
		{"tests/foo.sh", true},
		{"test/unit/x.go", true},
		{"spec/x.rb", true},
		{"run_test.sh", true},
		{"suite.bats", true},
		{"testing.go", false},
		{"contest.py", false},
		{"src/testdata/x.go", false},
		{"latest.ts", false},
		{"main.go", false},
		{"Test.go", false},
	} {
		abs := filepath.Join(root, filepath.FromSlash(tc.path))
		if got := IsTestFile(root, abs); got != tc.want {
			t.Errorf("IsTestFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
	// The directory rule reads the project-relative path: a project living
	// below a "test" directory is not test code throughout.
	if IsTestFile("/home/test/proj", "/home/test/proj/main.go") {
		t.Error("a test directory above the root must not count")
	}
}
