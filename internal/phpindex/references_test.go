package phpindex

// references_test.go covers the index-derived reference scanner (#2671):
// the scope walk (declaring type, traits, subclasses within depth, and for
// a trait member its consumers and sibling traits), the tree-based access
// matching on `$this` / `self` / `static` for every member kind, alias
// names, buffer text over disk, and what must stay out — a same-named
// member on an unrelated class, an access on another receiver, a
// mismatched shape.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"ike/internal/host"

	_ "ike/plugins/languages/php"
)

const (
	refsTraitA = `<?php

namespace App\Traits;

trait A
{
    public function run(): void
    {
        $this->abc();
        $this->abc;
        self::K;
        static::make();
        $this->x = self::$x;
        $this->x();
        $other->abc();
    }
}
`
	refsTraitC = `<?php

namespace App\Traits;

trait C
{
    protected $x;

    const K = 1;

    public static function make(): static
    {
        return new static();
    }

    public function fromC(): void
    {
        $this->abc();
    }
}
`
	refsTraitX = `<?php

namespace App\Traits;

trait X
{
    public function foo(): void
    {
        $this->foo();
    }
}
`
	refsClassB = `<?php

namespace App\Models;

use App\Traits\A;
use App\Traits\C;
use App\Traits\X;

class B
{
    use A, C;
    use X {
        foo as bar;
    }

    public function abc(int $times = 1): string
    {
        $this->bar();
        return (string) self::K;
    }
}
`
	refsSubclasses = `<?php

namespace App\Models;

class Sub extends B
{
    public function go(): void
    {
        $this->abc();
    }
}

class SubSub extends Sub
{
    public function deeper(): void
    {
        $this->abc();
    }
}
`
	refsUnrelated = `<?php

namespace App\Other;

class Z
{
    public function abc(): void
    {
        $this->abc();
        self::K;
    }
}
`
)

// refsProject writes the reference fixture into a temp dir and returns an
// index over it with the scan finished.
func refsProject(t *testing.T) (*Index, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"src/Traits/A.php":     refsTraitA,
		"src/Traits/C.php":     refsTraitC,
		"src/Traits/X.php":     refsTraitX,
		"src/Models/B.php":     refsClassB,
		"src/Models/Sub.php":   refsSubclasses,
		"src/Other/Z.php":      refsUnrelated,
		"src/Other/notes.txt":  "abc",
		"src/Other/Ignored.md": "$this->abc()",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	x := New(dir, defaultOpts())
	if !x.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	waitScan(t, x)
	return x, dir
}

// memberOf finds a declared member of the type by kind and name.
func memberOf(t *testing.T, x *Index, fqn string, kind MemberKind, name string) Member {
	t.Helper()
	for _, d := range x.DeclarationsNamed(fqn) {
		if d.FQN != fqn {
			continue
		}
		for _, m := range d.Members {
			if m.Kind == kind && m.Name == name {
				return m
			}
		}
	}
	t.Fatalf("%s has no %s %s", fqn, kind, name)
	return Member{}
}

// site is one expected reference: the file, the line holding the nth
// occurrence of needle (0-based, one per line) and the column of needle
// plus within.
type site struct {
	rel    string
	needle string
	within int
	nth    int
}

// want resolves a site to (path, line, col) in the project.
func (s site) want(t *testing.T, dir string) (string, int, int) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(s.rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for i, line := range strings.Split(string(data), "\n") {
		if c := strings.Index(line, s.needle); c >= 0 {
			if seen == s.nth {
				return path, i, len([]rune(line[:c])) + s.within
			}
			seen++
		}
	}
	t.Fatalf("occurrence %d of %q not found in %s", s.nth, s.needle, s.rel)
	return "", 0, 0
}

// expect checks that locs are exactly the sites (as a set of starts).
func expect(t *testing.T, dir string, locs []Location, sites ...site) {
	t.Helper()
	got := map[string]bool{}
	for _, l := range locs {
		k := l.Path + ":" + itoa(l.Range.Start.Line) + ":" + itoa(l.Range.Start.Col)
		if got[k] {
			t.Errorf("duplicate location %s", k)
		}
		got[k] = true
	}
	for _, s := range sites {
		p, l, c := s.want(t, dir)
		k := p + ":" + itoa(l) + ":" + itoa(c)
		if !got[k] {
			t.Errorf("missing %s (%s %q+%d)", k, s.rel, s.needle, s.within)
		}
		delete(got, k)
	}
	for k := range got {
		t.Errorf("unexpected location %s", k)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestReferencesMethodAcrossTraitScope: a consumer's method is referenced
// from the trait bodies it consumes (A and C), the declaration, and the
// subclasses within depth — never from an unrelated class with a
// same-named method, another receiver, or a property-shaped access.
func TestReferencesMethodAcrossTraitScope(t *testing.T) {
	x, dir := refsProject(t)
	abc := memberOf(t, x, `App\Models\B`, MemberMethod, "abc")
	locs := x.References(abc)
	expect(t, dir, locs,
		site{"src/Traits/A.php", "$this->abc()", 7, 0},
		site{"src/Traits/C.php", "$this->abc()", 7, 0},
		site{"src/Models/B.php", "public function abc", 16, 0},
		site{"src/Models/Sub.php", "$this->abc()", 7, 0},
		site{"src/Models/Sub.php", "$this->abc()", 7, 1},
	)
	var inTrait, decls int
	for _, l := range locs {
		if l.InTrait {
			inTrait++
		}
		if l.Decl {
			decls++
			if l.Declaring != `App\Models\B` {
				t.Fatalf("declaration on %s", l.Declaring)
			}
		}
	}
	if inTrait != 2 || decls != 1 {
		t.Fatalf("in-trait rows = %d, declarations = %d; want 2 and 1", inTrait, decls)
	}
}

// TestReferencesParentDepth: subclasses beyond php.index.parent_depth are
// out of scope.
func TestReferencesParentDepth(t *testing.T) {
	x, dir := refsProject(t)
	opts := defaultOpts()
	opts.ParentDepth = 1
	x.Reconfigure(opts)
	abc := memberOf(t, x, `App\Models\B`, MemberMethod, "abc")
	locs := x.References(abc)
	subs := filepath.Join(dir, "src", "Models", "Sub.php")
	n := 0
	for _, l := range locs {
		if l.Path == subs {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("depth 1 must reach Sub but not SubSub, got %d rows in Sub.php", n)
	}
}

// TestReferencesStaticConstAndProperty covers `self::K`, `static::make()`,
// `$this->x` and `self::$x`: each matches only accesses of its own shape.
func TestReferencesStaticConstAndProperty(t *testing.T) {
	x, dir := refsProject(t)

	k := memberOf(t, x, `App\Traits\C`, MemberConst, "K")
	expect(t, dir, x.References(k),
		site{"src/Traits/C.php", "const K", 6, 0},
		site{"src/Traits/A.php", "self::K", 6, 0},
		site{"src/Models/B.php", "self::K", 6, 0},
	)

	make := memberOf(t, x, `App\Traits\C`, MemberMethod, "make")
	expect(t, dir, x.References(make),
		site{"src/Traits/C.php", "public static function make", 23, 0},
		site{"src/Traits/A.php", "static::make()", 8, 0},
	)

	prop := memberOf(t, x, `App\Traits\C`, MemberProperty, "$x")
	expect(t, dir, x.References(prop),
		site{"src/Traits/C.php", "protected $x", 10, 0},
		site{"src/Traits/A.php", "$this->x = self::$x", 7, 0},
		site{"src/Traits/A.php", "self::$x", 6, 0},
	)
}

// TestReferencesAlias: `use X { foo as bar; }` makes `bar` a name of X::foo
// — usages under either name are found from either member, and the alias
// clause itself counts as a declaration under both names.
func TestReferencesAlias(t *testing.T) {
	x, dir := refsProject(t)
	foo := memberOf(t, x, `App\Traits\X`, MemberMethod, "foo")
	fromFoo := x.References(foo)
	expect(t, dir, fromFoo,
		site{"src/Traits/X.php", "public function foo", 16, 0},
		site{"src/Traits/X.php", "$this->foo()", 7, 0},
		site{"src/Models/B.php", "foo as bar", 0, 0},
		site{"src/Models/B.php", "foo as bar", 7, 0},
		site{"src/Models/B.php", "$this->bar()", 7, 0},
	)
	bar := memberOf(t, x, `App\Models\B`, MemberMethod, "bar")
	if bar.AliasOf != "foo" {
		t.Fatalf("bar.AliasOf = %q", bar.AliasOf)
	}
	fromBar := x.References(bar)
	expect(t, dir, fromBar,
		site{"src/Traits/X.php", "public function foo", 16, 0},
		site{"src/Traits/X.php", "$this->foo()", 7, 0},
		site{"src/Models/B.php", "foo as bar", 0, 0},
		site{"src/Models/B.php", "foo as bar", 7, 0},
		site{"src/Models/B.php", "$this->bar()", 7, 0},
	)
}

// TestReferencesUsesBufferText: an open buffer is scanned as observed, so a
// call typed but not saved is listed and the on-disk text is not.
func TestReferencesUsesBufferText(t *testing.T) {
	x, dir := refsProject(t)
	cPath := filepath.Join(dir, "src", "Traits", "C.php")
	edited := strings.Replace(refsTraitC, "        $this->abc();\n", "        $this->abc();\n        $this->abc();\n", 1)
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: cPath, Text: edited})
	x.Flush()
	abc := memberOf(t, x, `App\Models\B`, MemberMethod, "abc")
	n := 0
	for _, l := range x.References(abc) {
		if l.Path == cPath {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("buffer text must be scanned: got %d rows in C.php, want 2", n)
	}
}

// TestReferencesInRestrictsToFile is the document-highlight case.
func TestReferencesInRestrictsToFile(t *testing.T) {
	x, dir := refsProject(t)
	abc := memberOf(t, x, `App\Models\B`, MemberMethod, "abc")
	aPath := filepath.Join(dir, "src", "Traits", "A.php")
	locs := x.ReferencesIn(abc, aPath)
	expect(t, dir, locs, site{"src/Traits/A.php", "$this->abc()", 7, 0})
	if !locs[0].InTrait || locs[0].Decl {
		t.Fatalf("row = %+v, want an in-trait access", locs[0])
	}
}

// TestMembersAt resolves the member under a position in every body shape:
// the declaration identifier, a `$this` access in a class, in a subclass,
// and in a trait (the consumer scope).
func TestMembersAt(t *testing.T) {
	x, dir := refsProject(t)
	cases := []struct {
		name string
		s    site
	}{
		{"declaration name", site{"src/Models/B.php", "public function abc", 17, 0}},
		{"class access", site{"src/Models/Sub.php", "$this->abc()", 8, 0}},
		{"trait access", site{"src/Traits/A.php", "$this->abc()", 8, 0}},
		{"sibling trait access", site{"src/Traits/C.php", "$this->abc()", 8, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, line, col := tc.s.want(t, dir)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			ms := x.MembersAt(path, string(data), Pos{line, col})
			if len(ms) != 1 || ms[0].Declaring != `App\Models\B` || ms[0].Name != "abc" {
				t.Fatalf("members = %+v, want B::abc", ms)
			}
		})
	}
	t.Run("unrelated class resolves its own", func(t *testing.T) {
		path, line, col := site{"src/Other/Z.php", "$this->abc()", 8, 0}.want(t, dir)
		data, _ := os.ReadFile(path)
		ms := x.MembersAt(path, string(data), Pos{line, col})
		if len(ms) != 1 || ms[0].Declaring != `App\Other\Z` {
			t.Fatalf("members = %+v, want Z::abc", ms)
		}
	})
}
