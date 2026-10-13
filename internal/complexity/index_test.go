package complexity

// index_test.go covers the project complexity index (#2916) on a temp
// project: the walk lists every supported file, an invalidation re-measures
// only that file, exclude globs remove matches, the master switch drops and
// restores the snapshot, and the generation moves once per settle window.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// indexFixture writes one file per language with a spec, plus files the
// index must leave alone.
var indexFixture = map[string]string{
	"src/a.go":        "package a\n\nfunc A(x int) int {\n\tif x > 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n",
	"src/a_test.go":   "package a\n\nfunc TestA() {}\n",
	"py/b.py":         "def b(x):\n    if x:\n        return 1\n    return 0\n",
	"php/c.php":       "<?php\nfunction c($x) { if ($x) { return 1; } return 0; }\n",
	"ts/d.ts":         "function d(x: number) { if (x) { return 1; } return 0; }\n",
	"js/e.js":         "function e(x) { if (x) { return 1; } return 0; }\n",
	"sh/f.sh":         "f() {\n  if [ -n \"$1\" ]; then\n    echo 1\n  fi\n}\n",
	"vendor/v.go":     "package v\n\nfunc V() {}\n",
	"notes/readme.md": "# nothing to measure\n",
	".hidden/h.go":    "package h\n\nfunc H() {}\n",
}

func writeIndexFixture(t *testing.T, dir string) {
	t.Helper()
	for name, text := range indexFixture {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func waitScan(t *testing.T, x *Index) {
	t.Helper()
	for start := time.Now(); !x.ScanDone(); {
		if time.Since(start) > 10*time.Second {
			t.Fatal("the walk did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for start := time.Now(); !cond(); {
		if time.Since(start) > 5*time.Second {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newTestIndex(t *testing.T, opts Options) (*Index, string) {
	t.Helper()
	dir := t.TempDir()
	if r, err := filepath.EvalSymlinks(dir); err == nil {
		dir = r
	}
	writeIndexFixture(t, dir)
	x := NewIndex(dir, opts)
	if !x.Available() {
		t.Skip("no syntax tree in this build (cgo off)")
	}
	return x, dir
}

func relPaths(root string, v View) []string {
	var out []string
	for _, p := range v.Paths() {
		r, _ := filepath.Rel(root, p)
		out = append(out, filepath.ToSlash(r))
	}
	return out
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

// TestIndexWalkListsEverySupportedFile: the walk measures every Go, Python,
// PHP, TS, JS and shell file under the root; vendor/, hidden directories and
// files without a spec contribute nothing; the snapshot does not start
// before Start.
func TestIndexWalkListsEverySupportedFile(t *testing.T) {
	x, dir := newTestIndex(t, Options{Enabled: true})
	if !x.ScanDone() {
		t.Fatal("nothing to wait for before Start")
	}
	if v := x.Snapshot(); len(v.Files) != 0 {
		t.Fatalf("snapshot before Start = %v, want empty", v.Paths())
	}
	x.Start()
	waitScan(t, x)
	v := x.Snapshot()
	want := []string{"js/e.js", "php/c.php", "py/b.py", "sh/f.sh", "src/a.go", "src/a_test.go", "ts/d.ts"}
	if got := relPaths(dir, v); !equalStrings(got, want) {
		t.Fatalf("indexed = %v, want %v", got, want)
	}
	for _, p := range []string{"src/a.go", "py/b.py", "php/c.php", "ts/d.ts", "js/e.js", "sh/f.sh"} {
		fns := v.Files[filepath.Join(dir, filepath.FromSlash(p))]
		if len(fns) != 1 || fns[0].CCN != 2 {
			t.Errorf("%s = %+v, want one function with CCN 2", p, fns)
		}
	}
	if s := v.Summary(2, 0, 0); s.Functions != 7 || s.OverThreshold != 6 || s.CCNMax != 2 {
		t.Fatalf("summary = %+v, want 7 functions, 6 at or over CCN 2", s)
	}
	st := x.Stats()
	if !st.Enabled || st.Unavailable || st.Scanning || st.Files != 7 || st.Functions != 7 || st.Truncated {
		t.Fatalf("stats = %+v", st)
	}
	// The snapshot is immutable: a consumer holding it sees no later change.
	held := v
	if err := os.Remove(filepath.Join(dir, "js", "e.js")); err != nil {
		t.Fatal(err)
	}
	x.InvalidateFile(filepath.Join(dir, "js", "e.js"))
	waitFor(t, "the removal", func() bool { return len(x.Snapshot().Files) == 6 })
	if len(held.Files) != 7 {
		t.Fatal("a held View changed under the consumer")
	}
}

// TestIndexInvalidateRemeasuresOneFile: an invalidation re-reads that file
// alone — the other entries keep their identity — and the generation moves
// once for a burst of invalidations inside one settle window.
func TestIndexInvalidateRemeasuresOneFile(t *testing.T) {
	x, dir := newTestIndex(t, Options{Enabled: true})
	var mu sync.Mutex
	var gens []int
	x.SetOnChange(func(gen int) {
		mu.Lock()
		gens = append(gens, gen)
		mu.Unlock()
	})
	x.Start()
	waitScan(t, x)
	waitFor(t, "the scan notification", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(gens) == 1
	})
	before := x.Snapshot()
	if before.Gen != 1 {
		t.Fatalf("Gen after the walk = %d, want 1", before.Gen)
	}
	a := filepath.Join(dir, "src", "a.go")
	if err := os.WriteFile(a, []byte("package a\n\nfunc A(x int) int {\n\tif x > 0 && x < 9 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n\nfunc B() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A burst: the same path twice, a relative path, a file no spec claims
	// and an unrelated existing file.
	x.InvalidateFile(a)
	x.InvalidateFile("src/a.go")
	x.InvalidateFile(filepath.Join(dir, "notes", "readme.md"))
	x.InvalidateFile(filepath.Join(dir, "py", "b.py"))
	waitFor(t, "the re-measure", func() bool {
		fns := x.Snapshot().Files[a]
		return len(fns) == 2 && fns[0].CCN == 3
	})
	after := x.Snapshot()
	if len(after.Files) != 7 {
		t.Fatalf("files after = %v", relPaths(dir, after))
	}
	// Let the settle window close, then count notifications: the walk's
	// one plus one for the burst.
	time.Sleep(changeSettle * 3)
	mu.Lock()
	n := len(gens)
	last := gens[len(gens)-1]
	mu.Unlock()
	if n != 2 || last != 2 {
		t.Fatalf("notifications = %v, want [1 2]: one per settle window", gens)
	}
	if got := x.Snapshot().Gen; got != 2 {
		t.Fatalf("Gen = %d, want 2", got)
	}
}

// TestIndexExcludeGlobs: complexity.exclude removes matching files from the
// walk and from later invalidations; a reconfigured list rewalks.
func TestIndexExcludeGlobs(t *testing.T) {
	x, dir := newTestIndex(t, Options{Enabled: true, Exclude: []string{"vendor/**", "**/*_test.go"}})
	x.Start()
	waitScan(t, x)
	want := []string{"js/e.js", "php/c.php", "py/b.py", "sh/f.sh", "src/a.go", "ts/d.ts"}
	if got := relPaths(dir, x.Snapshot()); !equalStrings(got, want) {
		t.Fatalf("indexed = %v, want %v", got, want)
	}
	// A bare pattern matches the base name at any depth; a new exclude
	// list rebuilds the walk.
	x.Reconfigure(Options{Enabled: true, Exclude: []string{"*.py", "ts/*"}})
	waitScan(t, x)
	waitFor(t, "the rewalk", func() bool { return len(x.Snapshot().Files) == 5 })
	want = []string{"js/e.js", "php/c.php", "sh/f.sh", "src/a.go", "src/a_test.go"}
	if got := relPaths(dir, x.Snapshot()); !equalStrings(got, want) {
		t.Fatalf("after reconfigure = %v, want %v", got, want)
	}
	// An excluded path is not queued on invalidation either.
	x.InvalidateFile(filepath.Join(dir, "py", "b.py"))
	time.Sleep(50 * time.Millisecond)
	if _, ok := x.Snapshot().Files[filepath.Join(dir, "py", "b.py")]; ok {
		t.Fatal("an excluded file came back through InvalidateFile")
	}
	// Identical options are a no-op: the walk is kept.
	v := x.Snapshot()
	x.Reconfigure(Options{Enabled: true, Exclude: []string{"*.py", "ts/*"}})
	if got := x.Snapshot(); got.Gen != v.Gen || len(got.Files) != len(v.Files) {
		t.Fatal("unchanged options rebuilt the walk")
	}
}

// TestExcluded spells out the matching rule.
func TestExcluded(t *testing.T) {
	root := filepath.FromSlash("/proj")
	cases := []struct {
		globs []string
		path  string
		want  bool
	}{
		{[]string{"vendor/**"}, "/proj/vendor/a/b.go", true},
		{[]string{"vendor/**"}, "/proj/src/vendor.go", false},
		{[]string{"**/*_test.go"}, "/proj/a/b/c_test.go", true},
		{[]string{"**/*_test.go"}, "/proj/c_test.go", true},
		{[]string{"**/*_test.go"}, "/proj/c.go", false},
		{[]string{"*_test.go"}, "/proj/a/b/c_test.go", true},
		{[]string{"src/gen/**"}, "/proj/src/gen/x.ts", true},
		{[]string{"src/gen/**"}, "/proj/src/genx/x.ts", false},
		{[]string{"", " "}, "/proj/x.go", false},
		{nil, "/proj/x.go", false},
		{[]string{"*.go"}, "/elsewhere/x.go", true},
	}
	for _, c := range cases {
		if got := Excluded(root, c.globs, filepath.FromSlash(c.path)); got != c.want {
			t.Errorf("Excluded(%v, %s) = %v, want %v", c.globs, c.path, got, c.want)
		}
	}
}

// TestIndexMasterSwitch: disabling drops the snapshot and stops the walk;
// re-enabling restarts it without a new index. An index built disabled
// starts the moment it is enabled, having been started.
func TestIndexMasterSwitch(t *testing.T) {
	x, _ := newTestIndex(t, Options{Enabled: false})
	var mu sync.Mutex
	var gens []int
	x.SetOnChange(func(gen int) {
		mu.Lock()
		gens = append(gens, gen)
		mu.Unlock()
	})
	x.Start()
	if s := x.Stats(); s.Enabled || s.Scanning || s.Files != 0 {
		t.Fatalf("disabled stats = %+v", s)
	}
	if !x.Rebuild() {
		// expected: nothing to rebuild while disabled
	} else {
		t.Fatal("Rebuild must report false while disabled")
	}
	x.Reconfigure(Options{Enabled: true})
	waitScan(t, x)
	waitFor(t, "the walk", func() bool { return len(x.Snapshot().Files) == 7 })
	waitFor(t, "the walk notification", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(gens) >= 1
	})
	x.Reconfigure(Options{Enabled: false})
	if v := x.Snapshot(); len(v.Files) != 0 {
		t.Fatalf("disabled snapshot = %v, want empty", v.Paths())
	}
	if !x.ScanDone() {
		t.Fatal("a disabled index has nothing to wait for")
	}
	x.InvalidateFile("src/a.go") // ignored, no panic
	waitFor(t, "the drop notification", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(gens) >= 2
	})
	x.Reconfigure(Options{Enabled: true})
	waitScan(t, x)
	waitFor(t, "the restart", func() bool { return len(x.Snapshot().Files) == 7 })
	if !x.Rebuild() {
		t.Fatal("Rebuild must report true while enabled")
	}
	waitScan(t, x)
	waitFor(t, "the rebuild", func() bool { return len(x.Snapshot().Files) == 7 })
}

// TestIndexScanCallbackOncePerWalk: SetOnScan fires once for the initial
// walk and once per rebuild, with the stats the walk ended on.
func TestIndexScanCallbackOncePerWalk(t *testing.T) {
	x, _ := newTestIndex(t, Options{Enabled: true})
	var mu sync.Mutex
	var scans []Stats
	x.SetOnScan(func(s Stats) {
		mu.Lock()
		scans = append(scans, s)
		mu.Unlock()
	})
	x.Start()
	waitFor(t, "the scan report", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(scans) == 1
	})
	mu.Lock()
	s := scans[0]
	mu.Unlock()
	if s.Files != 7 || s.Functions != 7 || s.Scanning || s.Truncated || s.Unavailable {
		t.Fatalf("scan stats = %+v", s)
	}
	time.Sleep(changeSettle * 2)
	mu.Lock()
	n := len(scans)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("the walk was reported %d times, want once", n)
	}
	x.Rebuild()
	waitFor(t, "the rebuild report", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(scans) == 2
	})
}

// TestIndexEmptyRoot: an index over no root walks nothing and answers an
// empty snapshot.
func TestIndexEmptyRoot(t *testing.T) {
	x := NewIndex("", Options{Enabled: true})
	x.Start()
	waitScan(t, x)
	if v := x.Snapshot(); len(v.Files) != 0 {
		t.Fatalf("snapshot = %v, want empty", v.Paths())
	}
}
