package concealfilter

import (
	"reflect"
	"testing"
)

// concealfilter_test.go covers the two things that decide behaviour: how a
// pattern matches a path, and how the two levels compose (#1704).

func TestMatchPath(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		// A separator-free pattern matches the base name at any depth.
		{"*.py", "main.py", true},
		{"*.py", "a/b/main.py", true},
		{"*.py", "main.pyc", false},
		{"Makefile", "src/Makefile", true},
		{"Makefile", "src/Makefile.am", false},
		{"*.{yml,yaml}", "ci/deploy.yaml", true},
		// Case folding, both directions.
		{"*.PY", "main.py", true},
		{"*.py", "MAIN.PY", true},
		// A pattern with a separator matches the path, anchored anywhere.
		{"**/vendor/**", "app/vendor/pkg/x.go", true},
		{"**/vendor/**", "vendor/x.go", true},
		{"**/vendor/**", "app/pkg/x.go", false},
		{"vendor/**", "app/vendor/pkg/x.go", true},
		{"vendor/**", "vendor/x.go", true},
		{"vendor/**", "app/x.go", false},
		{"testdata/*.env", "internal/x/testdata/a.env", true},
		{"testdata/*.env", "internal/x/testdata/deep/a.env", false},
		// A rooted pattern is not re-anchored.
		{"/etc/**", "/etc/hosts", true},
		{"/etc/**", "home/etc/hosts", false},
		// Windows separators normalise.
		{"vendor/**", `app\vendor\x.go`, true},
	}
	for _, c := range cases {
		if got := matchPath(c.pattern, c.path); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestZeroRulesAllowEverything(t *testing.T) {
	var r Rules
	if !r.Empty() {
		t.Error("zero Rules must be Empty")
	}
	if !r.Allows(SecretMasking, "a/.env") {
		t.Error("zero Rules must allow every family everywhere")
	}
}

func TestGlobalIncludeExclude(t *testing.T) {
	cases := []struct {
		name             string
		include, exclude []string
		path             string
		want             bool
	}{
		{"no rules allow", nil, nil, "main.py", true},
		{"include hit", []string{"*.py"}, nil, "main.py", true},
		{"include miss blocks", []string{"*.py"}, nil, "main.go", false},
		{"exclude hit blocks", nil, []string{"*.log"}, "app.log", false},
		{"exclude miss allows", nil, []string{"*.log"}, "main.py", true},
		// Exclude wins over include, both at the same level and on the same file.
		{"exclude beats include", []string{"*.py"}, []string{"*_test.py"}, "x_test.py", false},
		{"include still applies", []string{"*.py"}, []string{"*_test.py"}, "x.py", true},
		// Blank entries never count as a list.
		{"blank include is no include", []string{"", "  "}, nil, "main.go", true},
		// A pathless buffer is always allowed.
		{"untitled buffer", []string{"*.py"}, nil, "", true},
	}
	for _, c := range cases {
		r := Compile(c.include, c.exclude, nil)
		if got := r.Allows(SecretMasking, c.path); got != c.want {
			t.Errorf("%s: Allows(%q) = %v, want %v", c.name, c.path, got, c.want)
		}
	}
}

func TestPerFamilyOverride(t *testing.T) {
	// Globally: conceal only in config files. Secret masking additionally
	// stays out of the fixtures, and cron hints opt back in to *.log, which
	// the global include would otherwise have blocked.
	r := Compile(
		[]string{"*.yaml", "*.env"},
		nil,
		[]string{"secret_masking=-testdata/**", "cron_hints=*.log"},
	)
	cases := []struct {
		family, path string
		want         bool
	}{
		{SecretMasking, "app/config.yaml", true},     // global include hit
		{SecretMasking, "app/main.go", false},        // global include miss
		{SecretMasking, "testdata/a.env", false},     // family exclude beats the global include
		{TimestampDecoding, "testdata/a.env", true},  // ...for that family only
		{CronHints, "app.log", true},                 // family include overrides the global include
		{CronHints, "app/config.yaml", false},        // a family include list is exhaustive
		{TimestampDecoding, "app/config.yaml", true}, // families without rules follow the global level
		{TimestampDecoding, "app/notes.md", false},   //
	}
	for _, c := range cases {
		if got := r.Allows(c.family, c.path); got != c.want {
			t.Errorf("Allows(%q, %q) = %v, want %v", c.family, c.path, got, c.want)
		}
	}
}

func TestFamilyExcludeBeatsFamilyInclude(t *testing.T) {
	r := Compile(nil, nil, []string{"radix_hints=*.go", "radix_hints=-*_test.go"})
	if !r.Allows(RadixHints, "pkg/x.go") {
		t.Error("family include must allow a matching file")
	}
	if r.Allows(RadixHints, "pkg/x_test.go") {
		t.Error("family exclude must beat the family include")
	}
	if !r.Allows(RadixHints, "") {
		t.Error("a pathless buffer stays allowed")
	}
	if !r.Allows(ByteSizeHints, "pkg/x_test.go") {
		t.Error("a rule must not leak into another family")
	}
}

func TestParseRule(t *testing.T) {
	cases := []struct {
		raw             string
		family, pattern string
		exclude, ok     bool
	}{
		{"secret_masking=-*.log", SecretMasking, "*.log", true, true},
		{"secret_masking=!*.log", SecretMasking, "*.log", true, true},
		{"secret_masking=+*.log", SecretMasking, "*.log", false, true},
		{"secret_masking=*.log", SecretMasking, "*.log", false, true},
		{" SECRET_MASKING = - *.log ", SecretMasking, "*.log", true, true},
		{"editor.secret_masking=-*.log", SecretMasking, "*.log", true, true},
		// Rejected: unknown family, missing pattern, missing separator.
		{"color_preview=-*.css", "", "", false, false},
		{"nonsense=-*.log", "", "", false, false},
		{"secret_masking=", "", "", false, false},
		{"secret_masking=-", "", "", false, false},
		{"*.log", "", "", false, false},
		{"", "", "", false, false},
	}
	for _, c := range cases {
		fam, pat, exc, ok := parseRule(c.raw)
		if fam != c.family || pat != c.pattern || exc != c.exclude || ok != c.ok {
			t.Errorf("parseRule(%q) = (%q, %q, %v, %v), want (%q, %q, %v, %v)",
				c.raw, fam, pat, exc, ok, c.family, c.pattern, c.exclude, c.ok)
		}
	}
}

func TestInvalidRules(t *testing.T) {
	got := Invalid([]string{"secret_masking=-*.log", "", "  ", "nope=-*.log", "*.log"})
	want := []string{"nope=-*.log", "*.log"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Invalid = %q, want %q", got, want)
	}
	// An invalid rule is dropped, not fatal: the valid one still applies.
	r := Compile(nil, nil, []string{"nope=-*.log", "secret_masking=-*.log"})
	if r.Allows(SecretMasking, "app.log") {
		t.Error("the valid rule must survive an invalid neighbour")
	}
	if !r.Allows(CronHints, "app.log") {
		t.Error("the invalid rule must not gate anything")
	}
}

func TestFamiliesSorted(t *testing.T) {
	f := Families()
	if len(f) != len(families) {
		t.Fatalf("Families() = %d entries, want %d", len(f), len(families))
	}
	for i := 1; i < len(f); i++ {
		if f[i-1] >= f[i] {
			t.Fatalf("Families() not sorted at %d: %q, %q", i, f[i-1], f[i])
		}
	}
}
