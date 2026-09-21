package phpindex

// hostview_rename_test.go covers the rename half of the host view (#2672):
// the extend side contributes only the rows inside trait bodies, the index
// side answers inside a trait body with the member's whole scope (methods,
// properties with their `$`, constants), a trait-use alias keeps its own
// name, an ambiguous target is refused naming the declarations, the gates
// hold, and the telemetry callback sees the applied rename.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/host"
)

// renameProject writes the reference fixture plus extra files and returns
// an index over it with the scan finished.
func renameProject(t *testing.T, extra map[string]string) (*Index, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"src/Traits/A.php":   refsTraitA,
		"src/Traits/C.php":   refsTraitC,
		"src/Traits/X.php":   refsTraitX,
		"src/Models/B.php":   refsClassB,
		"src/Models/Sub.php": refsSubclasses,
		"src/Other/Z.php":    refsUnrelated,
	}
	for rel, content := range extra {
		files[rel] = content
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

// renameView returns a view over the project plus a file's path and lines.
func renameView(t *testing.T, dir, rel string) (string, []string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, strings.Split(string(data), "\n")
}

// editKeys describes plan edits as "file:line:col=text" for assertions.
func editKeys(edits []host.TraitRenameEdit) map[string]host.TraitRenameEdit {
	out := map[string]host.TraitRenameEdit{}
	for _, e := range edits {
		out[filepath.Base(e.Path)+":"+itoa(e.Line)+":"+itoa(e.Col)] = e
	}
	return out
}

func wantEdit(t *testing.T, dir string, keys map[string]host.TraitRenameEdit, s site, text string, decl bool) {
	t.Helper()
	p, l, c := s.want(t, dir)
	e, ok := keys[filepath.Base(p)+":"+itoa(l)+":"+itoa(c)]
	if !ok {
		t.Errorf("missing edit %s %q", s.rel, s.needle)
		return
	}
	if e.Text != text || e.Decl != decl || e.EndCol != c+len([]rune(text)) {
		t.Errorf("edit %s %q = %+v, want text %q decl %v", s.rel, s.needle, e, text, decl)
	}
}

// TestHostViewRenameExtendSide: renaming `abc` on class B, the plan holds
// only the rows inside the traits B consumes — the calls in A and C — and
// neither B's own declaration, the subclasses nor the unrelated Z.
func TestHostViewRenameExtendSide(t *testing.T) {
	x, dir := renameProject(t, nil)
	v := NewHostView(x)
	path, lines := renameView(t, dir, "src/Models/B.php")
	line, col := at(t, lines, "public function abc", 17)
	plan, ok := v.TraitRenameAt(host.TraitRenameExtend, path, lines, line, col)
	if !ok || plan.OldName != "abc" {
		t.Fatalf("plan = %+v ok=%v, want the abc extension", plan, ok)
	}
	if len(plan.Edits) != 2 {
		t.Fatalf("edits = %+v, want the calls in A and C only", plan.Edits)
	}
	keys := editKeys(plan.Edits)
	wantEdit(t, dir, keys, site{"src/Traits/A.php", "$this->abc()", 7, 0}, "abc", false)
	wantEdit(t, dir, keys, site{"src/Traits/C.php", "$this->abc()", 7, 0}, "abc", false)
	for _, e := range plan.Edits {
		if !e.InTrait || e.DeclName == "" {
			t.Errorf("extend-side edit must sit in a trait with its name: %+v", e)
		}
	}
	// The same plan from a call inside B.
	line, col = at(t, lines, "self::K", 0)
	if _, ok := v.TraitRenameAt(host.TraitRenameExtend, path, lines, line, col-2); ok {
		t.Fatal("a position off any member must plan nothing")
	}
}

// TestHostViewRenameIndexSide: inside trait A, `$this->abc()` plans the
// whole scope — the declaration in B, the calls in A, C and the subclasses
// — and excludes `$other->abc()`, the property-shaped `$this->abc` and Z.
func TestHostViewRenameIndexSide(t *testing.T) {
	x, dir := renameProject(t, nil)
	v := NewHostView(x)
	path, lines := renameView(t, dir, "src/Traits/A.php")
	line, col := at(t, lines, "$this->abc()", 8)
	plan, ok := v.TraitRenameAt(host.TraitRenameIndex, path, lines, line, col)
	if !ok || plan.OldName != "abc" || len(plan.Ambiguous) != 0 {
		t.Fatalf("plan = %+v ok=%v", plan, ok)
	}
	if len(plan.Edits) != 5 {
		t.Fatalf("edits = %+v, want A, B (decl), C, Sub, SubSub", plan.Edits)
	}
	keys := editKeys(plan.Edits)
	wantEdit(t, dir, keys, site{"src/Traits/A.php", "$this->abc()", 7, 0}, "abc", false)
	wantEdit(t, dir, keys, site{"src/Models/B.php", "public function abc", 16, 0}, "abc", true)
	wantEdit(t, dir, keys, site{"src/Traits/C.php", "$this->abc()", 7, 0}, "abc", false)
	wantEdit(t, dir, keys, site{"src/Models/Sub.php", "$this->abc()", 7, 0}, "abc", false)
	wantEdit(t, dir, keys, site{"src/Models/Sub.php", "$this->abc()", 7, 1}, "abc", false)
	for i := 1; i < len(plan.Edits); i++ {
		a, b := plan.Edits[i-1], plan.Edits[i]
		if a.Path > b.Path || (a.Path == b.Path && (a.Line > b.Line || (a.Line == b.Line && a.Col >= b.Col))) {
			t.Fatalf("edits must be sorted, %+v before %+v", a, b)
		}
	}
}

// TestHostViewRenameProperty: a property rename keeps the `$` where it is
// written — the declaration and `self::$x` carry it, `$this->x` does not.
func TestHostViewRenameProperty(t *testing.T) {
	x, dir := renameProject(t, nil)
	v := NewHostView(x)
	path, lines := renameView(t, dir, "src/Traits/A.php")
	line, col := at(t, lines, "$this->x = ", 7)
	plan, ok := v.TraitRenameAt(host.TraitRenameIndex, path, lines, line, col)
	if !ok || plan.OldName != "x" {
		t.Fatalf("plan = %+v ok=%v", plan, ok)
	}
	if len(plan.Edits) != 3 {
		t.Fatalf("edits = %+v, want the declaration, $this->x and self::$x", plan.Edits)
	}
	keys := editKeys(plan.Edits)
	wantEdit(t, dir, keys, site{"src/Traits/C.php", "protected $x", 10, 0}, "$x", true)
	wantEdit(t, dir, keys, site{"src/Traits/A.php", "$this->x = ", 7, 0}, "x", false)
	wantEdit(t, dir, keys, site{"src/Traits/A.php", "self::$x", 6, 0}, "$x", false)
}

// TestHostViewRenameAliasKeepsItsName: renaming X::foo from inside X
// rewrites the declaration, the call in X and the `foo` of `foo as bar` —
// never `bar` itself, which is the consumer's own name for it.
func TestHostViewRenameAliasKeepsItsName(t *testing.T) {
	x, dir := renameProject(t, nil)
	v := NewHostView(x)
	path, lines := renameView(t, dir, "src/Traits/X.php")
	line, col := at(t, lines, "$this->foo()", 8)
	plan, ok := v.TraitRenameAt(host.TraitRenameIndex, path, lines, line, col)
	if !ok || plan.OldName != "foo" {
		t.Fatalf("plan = %+v ok=%v", plan, ok)
	}
	if len(plan.Edits) != 3 {
		t.Fatalf("edits = %+v, want the declaration, the call in X and the alias clause's foo", plan.Edits)
	}
	keys := editKeys(plan.Edits)
	wantEdit(t, dir, keys, site{"src/Traits/X.php", "public function foo", 16, 0}, "foo", true)
	wantEdit(t, dir, keys, site{"src/Traits/X.php", "$this->foo()", 7, 0}, "foo", false)
	wantEdit(t, dir, keys, site{"src/Models/B.php", "foo as bar", 0, 0}, "foo", true)
	for _, e := range plan.Edits {
		if e.Text == "bar" {
			t.Fatalf("the alias name must keep: %+v", e)
		}
	}
}

const (
	// renameB2 is a second, unrelated consumer of A declaring abc with a
	// different signature — the ambiguous target.
	renameB2Differs = `<?php

namespace App\Models;

use App\Traits\A;

class B2
{
    use A;

    public function abc(): void
    {
    }
}
`
	// renameB2Alike declares abc exactly like B does: two consumers
	// implementing one contract, renamed together.
	renameB2Alike = `<?php

namespace App\Models;

use App\Traits\A;

class B2
{
    use A;

    public function abc(int $times = 1): string
    {
        return $this->abc();
    }
}
`
)

// TestHostViewRenameAmbiguous: two unrelated consumers declaring abc
// differently make the index-side rename refuse, naming both; declared
// alike, both are renamed.
func TestHostViewRenameAmbiguous(t *testing.T) {
	x, dir := renameProject(t, map[string]string{"src/Models/B2.php": renameB2Differs})
	v := NewHostView(x)
	path, lines := renameView(t, dir, "src/Traits/A.php")
	line, col := at(t, lines, "$this->abc()", 8)
	plan, ok := v.TraitRenameAt(host.TraitRenameIndex, path, lines, line, col)
	if !ok || len(plan.Edits) != 0 || len(plan.Ambiguous) != 2 {
		t.Fatalf("plan = %+v ok=%v, want two ambiguous declarations and no edits", plan, ok)
	}
	names := map[string]bool{}
	for _, m := range plan.Ambiguous {
		names[m.DeclKind+" "+m.DeclName] = true
		if m.Path == "" || m.Signature == "" {
			t.Fatalf("ambiguous declaration must be locatable: %+v", m)
		}
	}
	if !names["class B"] || !names["class B2"] {
		t.Fatalf("ambiguous = %v, want class B and class B2", names)
	}
	// The extend side never refuses: the server chose the target.
	bPath := filepath.Join(dir, "src", "Models", "B.php")
	bData, _ := os.ReadFile(bPath)
	bLines := strings.Split(string(bData), "\n")
	bl, bc := at(t, bLines, "public function abc", 17)
	if plan, ok := v.TraitRenameAt(host.TraitRenameExtend, bPath, bLines, bl, bc); !ok || len(plan.Ambiguous) != 0 || len(plan.Edits) == 0 {
		t.Fatalf("extend plan = %+v ok=%v", plan, ok)
	}

	x2, dir2 := renameProject(t, map[string]string{"src/Models/B2.php": renameB2Alike})
	v2 := NewHostView(x2)
	path2, lines2 := renameView(t, dir2, "src/Traits/A.php")
	line, col = at(t, lines2, "$this->abc()", 8)
	plan, ok = v2.TraitRenameAt(host.TraitRenameIndex, path2, lines2, line, col)
	if !ok || len(plan.Ambiguous) != 0 {
		t.Fatalf("alike declarations must not be ambiguous: %+v ok=%v", plan, ok)
	}
	decls := 0
	for _, e := range plan.Edits {
		if e.Decl {
			decls++
		}
	}
	if decls != 2 {
		t.Fatalf("edits = %+v, want both declarations renamed", plan.Edits)
	}
}

// TestHostViewRenameGates: the master switch off, a class body on the index
// side, a non-PHP path and a position outside any declaration all plan
// nothing; the telemetry callback sees an applied rename and never a
// zero-edit one.
func TestHostViewRenameGates(t *testing.T) {
	x, dir := renameProject(t, nil)
	v := NewHostView(x)
	aPath, aLines := renameView(t, dir, "src/Traits/A.php")
	line, col := at(t, aLines, "$this->abc()", 8)

	x.Reconfigure(Options{Enabled: false, ParentDepth: 3, MaxFiles: 1000})
	if _, ok := v.TraitRenameAt(host.TraitRenameIndex, aPath, aLines, line, col); ok {
		t.Fatal("php.trait_index = false must plan nothing")
	}
	x.Reconfigure(defaultOpts())
	waitScan(t, x)
	if _, ok := v.TraitRenameAt(host.TraitRenameIndex, aPath, aLines, line, col); !ok {
		t.Fatal("re-enabled index must plan again")
	}

	bPath, bLines := renameView(t, dir, "src/Models/B.php")
	bl, bc := at(t, bLines, "public function abc", 17)
	if _, ok := v.TraitRenameAt(host.TraitRenameIndex, bPath, bLines, bl, bc); ok {
		t.Fatal("the index side answers only inside a trait body")
	}
	if _, ok := v.TraitRenameAt(host.TraitRenameIndex, filepath.Join(dir, "notes.txt"), aLines, line, col); ok {
		t.Fatal("a non-PHP path must plan nothing")
	}
	if _, ok := v.TraitRenameAt(host.TraitRenameIndex, aPath, aLines, 0, 0); ok {
		t.Fatal("a position outside any declaration must plan nothing")
	}

	var gotSide host.TraitRenameSide
	gotEdits := -1
	v.SetRenameTelemetry(func(side host.TraitRenameSide, edits int) { gotSide, gotEdits = side, edits })
	v.TraitRenameApplied(host.TraitRenameExtend, 0)
	if gotEdits != -1 {
		t.Fatal("a rename the index added nothing to must record nothing")
	}
	v.TraitRenameApplied(host.TraitRenameIndex, 4)
	if gotSide != host.TraitRenameIndex || gotEdits != 4 {
		t.Fatalf("telemetry = (%v, %d), want (index, 4)", gotSide, gotEdits)
	}
	if host.TraitRenameExtend.String() != "extended" || host.TraitRenameIndex.String() != "index" {
		t.Fatal("side spellings are the telemetry path values")
	}
}
