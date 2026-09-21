package phpindex

// phpindex_test.go covers the index's promises (#2667) over the fixture
// project in testdata: the trait/consumer edges, the consumer scope, the
// scope lookup by position, the buffer and watcher freshness paths, cycle
// safety, the settings switches and the no-grammar fallback.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"ike/internal/host"

	_ "ike/plugins/languages/php"
)

const (
	traitA = `App\Traits\A`
	traitC = `App\Traits\C`
	traitX = `App\Traits\X`
	classB = `App\Models\B`
)

func defaultOpts() Options {
	return Options{Enabled: true, ParentDepth: 3, MaxFiles: 20000}
}

// fixture copies testdata/project into a temp dir (tests write to it) and
// returns an index over it with the scan finished.
func fixture(t *testing.T, opts Options) (*Index, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join("testdata", "project")
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	x := New(dir, opts)
	if !x.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	waitScan(t, x)
	return x, dir
}

func waitScan(t *testing.T, x *Index) {
	t.Helper()
	for start := time.Now(); !x.ScanDone(); {
		if time.Since(start) > 10*time.Second {
			t.Fatal("scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for start := time.Now(); !cond(); {
		if time.Since(start) > 5*time.Second {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func memberNames(ms []Member) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestEdges(t *testing.T) {
	x, _ := fixture(t, defaultOpts())
	if got := x.ConsumersOf(traitA); !equalStrings(got, []string{classB}) {
		t.Fatalf("ConsumersOf(A) = %v, want [B]", got)
	}
	if got := x.SiblingTraitsOf(traitA); !equalStrings(got, []string{traitC, traitX}) {
		t.Fatalf("SiblingTraitsOf(A) = %v, want [C X]", got)
	}
	// X is used by B, D, the enum and, through the aliased group import, Report.
	want := []string{`App\Enums\Status`, classB, `App\Models\D`, `App\Services\Report`}
	if got := x.ConsumersOf(traitX); !equalStrings(got, want) {
		t.Fatalf("ConsumersOf(X) = %v, want %v", got, want)
	}
	// Report's parent is B through the `as Model` alias; the chain is capped.
	if got := x.ParentChain(`App\Services\Report`, -1); !equalStrings(got, []string{classB, `App\Base\ParentModel`, `App\Base\GrandParent`}) {
		t.Fatalf("ParentChain(Report) = %v", got)
	}
	if got := x.ParentChain(`App\Services\Report`, 10); len(got) != 5 || got[4] != `App\Base\Ancestor` {
		t.Fatalf("ParentChain(Report, 10) = %v", got)
	}
	s := x.Stats()
	if s.Files != 10 || s.Declarations < 12 || s.Edges == 0 || s.Truncated || s.Scanning || !s.Enabled || s.Unavailable {
		t.Fatalf("stats = %+v", s)
	}
}

func TestVisibleMembers(t *testing.T) {
	x, _ := fixture(t, defaultOpts())
	ms := x.VisibleMembers(traitA)
	names := memberNames(ms)
	for _, want := range []string{"abc", "fromC", "$x", "K", "make", "run", "bar", "foo", "jsonSerialize",
		"$id", "$label", "find", "save", "touch"} {
		if !has(names, want) {
			t.Errorf("VisibleMembers(A) lacks %q: %v", want, names)
		}
	}
	// Ancestor sits four levels up: beyond php.index.parent_depth = 3.
	if has(names, "beyond") {
		t.Errorf("VisibleMembers(A) reaches past the parent depth: %v", names)
	}
	// Dedup by (kind, name): X::foo and Y::foo collapse to one.
	seen := map[string]int{}
	for _, m := range ms {
		seen[m.Kind.String()+" "+m.Name]++
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("member %q listed %d times", k, n)
		}
	}
	byName := map[string]Member{}
	for _, m := range ms {
		byName[m.Name] = m
	}
	if m := byName["abc"]; m.Declaring != classB || m.Doc != "Does the abc thing." || m.Params != "(int $times = 1)" || m.ReturnType != "string" || m.Visibility != "public" || !strings.HasSuffix(m.Path, "B.php") {
		t.Errorf("abc = %+v", m)
	}
	if m := byName["abc"]; m.Signature() != "public function abc(int $times = 1): string" || m.Range.Start.Line != 20 || m.NameRange.Start.Col != 20 {
		t.Errorf("abc signature/position = %q %+v", m.Signature(), m.Range)
	}
	if m := byName["$x"]; m.Kind != MemberProperty || m.Visibility != "protected" || m.Declaring != traitC {
		t.Errorf("$x = %+v", m)
	}
	if m := byName["K"]; m.Kind != MemberConst || m.Signature() != "const K" {
		t.Errorf("K = %+v", m)
	}
	if m := byName["make"]; !m.Static || m.Signature() != "public static function make(): static" {
		t.Errorf("make = %+v", m)
	}
	if m := byName["fromC"]; m.Doc != "From C." || m.ReturnType != "?int" {
		t.Errorf("fromC = %+v", m)
	}
	if m := byName["bar"]; m.AliasOf != "foo" || m.Declaring != classB || m.Kind != MemberMethod {
		t.Errorf("alias bar = %+v", m)
	}
	if m := byName["$label"]; m.Visibility != "private" || m.Type != "string" || m.Declaring != `App\Base\ParentModel` {
		t.Errorf("promoted $label = %+v", m)
	}
	// Lookup returns the declaring member; a property matches without its $.
	if got := x.Lookup(traitA, MemberProperty, "x"); len(got) != 1 || got[0].Declaring != traitC {
		t.Errorf("Lookup($x) = %+v", got)
	}
	if got := x.Lookup(traitA, MemberMethod, "nope"); len(got) != 0 {
		t.Errorf("Lookup(nope) = %+v", got)
	}
}

func TestScopeAtAndDeclarations(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	aPath := filepath.Join(dir, "src", "Traits", "A.php")
	d, ok := x.ScopeAt(aPath, Pos{Line: 11, Col: 8})
	if !ok || !d.IsTrait() || d.FQN != traitA || d.Name != "A" {
		t.Fatalf("ScopeAt(A body) = %+v %v", d, ok)
	}
	if _, ok := x.ScopeAt(aPath, Pos{Line: 2, Col: 0}); ok {
		t.Fatal("the namespace line is outside every declaration")
	}
	// Two declarations in one file: the position picks the right one.
	base := filepath.Join(dir, "src", "Base", "ParentModel.php")
	if d, ok := x.ScopeAt(base, Pos{Line: 21, Col: 4}); !ok || d.FQN != `App\Base\GrandParent` || d.IsTrait() {
		t.Fatalf("ScopeAt(GrandParent) = %+v", d)
	}
	if d, ok := x.ScopeAt(base, Pos{Line: 4, Col: 0}); !ok || !d.Abstract || d.Kind != KindClass {
		t.Fatalf("ScopeAt(ParentModel) = %+v", d)
	}

	ds := x.DeclarationsNamed("B")
	if len(ds) != 1 || ds[0].FQN != classB || len(ds[0].Implements) != 1 || ds[0].Implements[0] != "JsonSerializable" || ds[0].Extends[0] != `App\Base\ParentModel` {
		t.Fatalf("DeclarationsNamed(B) = %+v", ds)
	}
	if ds := x.DeclarationsNamed(`\App\Models\D`); len(ds) != 1 || !ds[0].Final {
		t.Fatalf("DeclarationsNamed(\\App\\Models\\D) = %+v", ds)
	}
	if ds := x.DeclarationsNamed("Status"); len(ds) != 1 || ds[0].Kind != KindEnum {
		t.Fatalf("DeclarationsNamed(Status) = %+v", ds)
	} else {
		names := memberNames(ds[0].Members)
		if !has(names, "Active") || !has(names, "DEFAULT") || !has(names, "label") {
			t.Fatalf("enum members = %v", names)
		}
	}
	// insteadof adds no member; the aliased Y::foo becomes yFoo.
	if ds := x.DeclarationsNamed("D"); len(ds) != 1 {
		t.Fatal("D missing")
	} else if names := memberNames(ds[0].Members); !equalStrings(names, []string{"yFoo", "VERSION"}) {
		t.Fatalf("D members = %v", names)
	} else if ds[0].Members[0].AliasOf != "Y::foo" || ds[0].Members[0].Visibility != "protected" {
		t.Fatalf("yFoo = %+v", ds[0].Members[0])
	}

	refs := x.FilesReferencing(traitX)
	var rel []string
	for _, p := range refs {
		r, _ := filepath.Rel(dir, p)
		rel = append(rel, filepath.ToSlash(r))
	}
	sort.Strings(rel)
	if !equalStrings(rel, []string{"src/Enums/Status.php", "src/Models/B.php", "src/Models/D.php", "src/Services/Report.php"}) {
		t.Fatalf("FilesReferencing(X) = %v", rel)
	}
}

// TestLegacyShortNameFallback: a file without namespace or imports writes
// `use LegacyTrait;`, which resolves to the global name; the snapshot falls
// back to the short name and finds the namespaced declaration.
func TestLegacyShortNameFallback(t *testing.T) {
	x, _ := fixture(t, defaultOpts())
	if got := x.ConsumersOf(`Legacy\Support\LegacyTrait`); !equalStrings(got, []string{"OldConsumer"}) {
		t.Fatalf("ConsumersOf(LegacyTrait) = %v", got)
	}
	names := memberNames(x.VisibleMembers(`Legacy\Support\LegacyTrait`))
	if !has(names, "legacy") || !has(names, "$legacyProp") || !has(names, "helper") {
		t.Fatalf("legacy scope = %v", names)
	}
	if ds := x.DeclarationsNamed("OldContract"); len(ds) != 1 || ds[0].Kind != KindInterface || ds[0].FQN != "OldContract" {
		t.Fatalf("OldContract = %+v", ds)
	}
}

// TestBufferEditUpdatesScope: an edit in an open consumer buffer reaches
// the scope through the observer path, with nothing written to disk.
func TestBufferEditUpdatesScope(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	bPath := filepath.Join(dir, "src", "Models", "B.php")
	data, _ := os.ReadFile(bPath)
	edited := strings.Replace(string(data), "public function abc(", "public function fresh(): void {}\n    public function abc(", 1)
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: bPath, Text: edited})
	x.Flush()
	if names := memberNames(x.VisibleMembers(traitA)); !has(names, "fresh") {
		t.Fatalf("buffer edit not visible: %v", names)
	}
	if disk, _ := os.ReadFile(bPath); string(disk) != string(data) {
		t.Fatal("the buffer edit must not touch the disk")
	}
	// The debounce timer flushes on its own too.
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: bPath, Text: strings.Replace(edited, "fresh", "later", 1)})
	waitFor(t, "debounced buffer extraction", func() bool { return has(memberNames(x.VisibleMembers(traitA)), "later") })
	// A large-file event drops the override: the disk text is back.
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: bPath, Large: true})
	if names := memberNames(x.VisibleMembers(traitA)); has(names, "later") || !has(names, "abc") {
		t.Fatalf("large-file event did not drop the buffer: %v", names)
	}
	// Non-PHP buffers are ignored.
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: filepath.Join(dir, "x.go"), Text: "package x"})
	if x.Stats().Files != 10 {
		t.Fatalf("a Go buffer was indexed: %+v", x.Stats())
	}
}

// TestDiskChangeAndRemoval: the watcher path re-extracts a changed file and
// drops a removed one.
func TestDiskChangeAndRemoval(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	cPath := filepath.Join(dir, "src", "Traits", "C.php")
	data, _ := os.ReadFile(cPath)
	if err := os.WriteFile(cPath, []byte(strings.Replace(string(data), "fromC", "fromCRenamed", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	x.InvalidateFile(cPath)
	waitFor(t, "re-extraction", func() bool { return has(memberNames(x.VisibleMembers(traitA)), "fromCRenamed") })
	if err := os.Remove(cPath); err != nil {
		t.Fatal(err)
	}
	x.InvalidateFile(cPath)
	waitFor(t, "removal", func() bool { return len(x.DeclarationsNamed("C")) == 0 })
	if got := x.SiblingTraitsOf(traitA); !equalStrings(got, []string{traitX}) {
		t.Fatalf("after removing C, siblings = %v", got)
	}
	// A file added later is picked up through the same path.
	nPath := filepath.Join(dir, "src", "New.php")
	if err := os.WriteFile(nPath, []byte("<?php\nnamespace App;\nuse App\\Traits\\A;\nclass Newcomer { use A; public function added() {} }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	x.InvalidateFile(nPath)
	waitFor(t, "new file", func() bool { return has(x.ConsumersOf(traitA), `App\Newcomer`) })
}

// TestTraitCycleTerminates: `trait A { use C; } trait C { use A; }` must
// not loop in any transitive query.
func TestTraitCycleTerminates(t *testing.T) {
	dir := t.TempDir()
	src := "<?php\nnamespace Cyc;\ntrait A { use C; public function fromA() {} }\ntrait C { use A; public function fromC() {} }\nclass K { use A; public function fromK() {} }\n"
	if err := os.WriteFile(filepath.Join(dir, "cyc.php"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	x := New(dir, defaultOpts())
	if !x.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	waitScan(t, x)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if got := x.ConsumersOf(`Cyc\A`); !equalStrings(got, []string{`Cyc\C`, `Cyc\K`}) {
			t.Errorf("ConsumersOf(A) = %v", got)
		}
		if got := x.SiblingTraitsOf(`Cyc\A`); len(got) != 0 {
			t.Errorf("SiblingTraitsOf(A) = %v, want none (C is a consumer)", got)
		}
		names := memberNames(x.VisibleMembers(`Cyc\A`))
		if !has(names, "fromA") || !has(names, "fromC") || !has(names, "fromK") {
			t.Errorf("cycle scope = %v", names)
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cycle query did not terminate")
	}
}

func TestVendorAndCaps(t *testing.T) {
	x, _ := fixture(t, defaultOpts())
	if ds := x.DeclarationsNamed("VendorTrait"); len(ds) != 0 {
		t.Fatalf("vendor/ indexed without include_vendor: %+v", ds)
	}
	opts := defaultOpts()
	opts.IncludeVendor = true
	x.Reconfigure(opts)
	waitScan(t, x)
	if got := x.ConsumersOf(`Acme\Lib\VendorTrait`); !equalStrings(got, []string{`Acme\Lib\VendorConsumer`}) {
		t.Fatalf("vendor edge = %v", got)
	}
	opts.MaxFiles = 3
	x.Reconfigure(opts)
	waitScan(t, x)
	if s := x.Stats(); !s.Truncated || s.Files != 3 {
		t.Fatalf("capped stats = %+v", s)
	}
	// Parent depth applies without a rescan.
	opts.MaxFiles = 20000
	opts.ParentDepth = 1
	x.Reconfigure(opts)
	waitScan(t, x)
	names := memberNames(x.VisibleMembers(traitA))
	if !has(names, "find") || has(names, "save") {
		t.Fatalf("parent depth 1 scope = %v", names)
	}
	opts.ParentDepth = 0
	x.Reconfigure(opts)
	if names := memberNames(x.VisibleMembers(traitA)); has(names, "find") {
		t.Fatalf("parent depth 0 scope still has a parent member: %v", names)
	}
}

func TestMasterSwitchDropsAndRebuilds(t *testing.T) {
	x, dir := fixture(t, defaultOpts())
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: filepath.Join(dir, "src", "Traits", "A.php"), Text: "<?php trait A {}"})
	x.Reconfigure(Options{Enabled: false, ParentDepth: 3, MaxFiles: 20000})
	if s := x.Stats(); s.Enabled || s.Files != 0 || s.Declarations != 0 {
		t.Fatalf("disabled stats = %+v", s)
	}
	if got := x.ConsumersOf(traitA); len(got) != 0 {
		t.Fatalf("disabled index answered %v", got)
	}
	if _, ok := x.ScopeAt(filepath.Join(dir, "src", "Traits", "A.php"), Pos{Line: 8, Col: 0}); ok {
		t.Fatal("disabled index has a scope")
	}
	// Events while disabled are dropped without work.
	x.Observe(host.EditorEvent{Kind: host.EditorChange, Path: filepath.Join(dir, "src", "Traits", "A.php"), Text: "<?php trait A {}"})
	x.InvalidateFile(filepath.Join(dir, "src", "Traits", "A.php"))
	x.Flush()
	x.Reconfigure(defaultOpts())
	waitScan(t, x)
	if got := x.ConsumersOf(traitA); !equalStrings(got, []string{classB}) {
		t.Fatalf("rebuilt index: ConsumersOf(A) = %v", got)
	}
}

func TestEmptyRoot(t *testing.T) {
	x := New("", defaultOpts())
	if !x.ScanDone() {
		t.Fatal("an empty root is scanned at once")
	}
	if got := x.VisibleMembers("Nothing"); len(got) != 0 {
		t.Fatalf("empty root answered %v", got)
	}
	if s := x.Stats(); s.Files != 0 || s.Scanning {
		t.Fatalf("empty stats = %+v", s)
	}
}

func TestDocSummaryAndResolve(t *testing.T) {
	cases := map[string]string{
		"/** Does x. */": "Does x.",
		"/**\n * First line.\n *\n * @return int\n */": "First line.",
		"/**\n * @param int $a\n * Later prose.\n */":  "Later prose.",
		"// plain":                        "",
		"/* block */":                     "",
		"/**\n * @internal\n */":          "",
		"/**   \n   *   Indented.  \n */": "Indented.",
	}
	for in, want := range cases {
		if got := docSummary(in); got != want {
			t.Errorf("docSummary(%q) = %q, want %q", in, got, want)
		}
	}
	e := &extractor{ns: `App\Models`, imports: map[string]string{"model": `App\Base\Model`, "traits": `App\Traits`}}
	for in, want := range map[string]string{
		`\Foo\Bar`:      `Foo\Bar`,
		`Model`:         `App\Base\Model`,
		`model`:         `App\Base\Model`,
		`Traits\A`:      `App\Traits\A`,
		`Local`:         `App\Models\Local`,
		`namespace\Sub`: `App\Models\Sub`,
		``:              ``,
	} {
		if got := e.resolve(in); got != want {
			t.Errorf("resolve(%q) = %q, want %q", in, got, want)
		}
	}
	if got := collapseSpace("(\n    int $a,\n    string $b = ''\n)"); got != "(int $a, string $b = '')" {
		t.Errorf("collapseSpace = %q", got)
	}
}
