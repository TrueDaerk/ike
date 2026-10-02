package phpindex

// cache_test.go covers the persisted index (#2885): a warm start over an
// unchanged tree parses nothing and knows what a cold scan knows, a file
// changed, added or removed between sessions is re-extracted or dropped, a
// cache of another format version is ignored, and php.index.cache switches
// the whole thing off.

import (
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func cacheOpts() Options {
	o := defaultOpts()
	o.Cache = true
	return o
}

// cachedFixture is fixture with the cache on and its file under the project
// (IKE_CONFIG_DIR cleared, so CacheFile lands in <root>/.ike).
func cachedFixture(t *testing.T) (*Index, string) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", "")
	return fixture(t, cacheOpts())
}

// reopen is the next session over the same project: a fresh index, scan
// finished.
func reopen(t *testing.T, dir string, opts Options) *Index {
	t.Helper()
	x := New(dir, opts)
	waitScan(t, x)
	return x
}

// dump renders the walk's extractions canonically, path by path, so a cold
// and a warm index compare as text (nil and empty slices print alike).
func dump(x *Index) string {
	x.mu.Lock()
	p := x.project
	x.mu.Unlock()
	var rows []string
	p.Each([]string{"php"}, func(path string, v fileDecls) {
		rows = append(rows, fmt.Sprintf("%s %+v", path, v))
	})
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

func TestCacheWarmStartScansNothing(t *testing.T) {
	cold, dir := cachedFixture(t)
	cs := cold.Stats()
	if cs.Cached != 0 || cs.Files == 0 {
		t.Fatalf("cold stats = %+v, want files and nothing cached", cs)
	}
	file := filepath.Join(dir, ".ike", cacheName)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("the finished scan must write %s: %v", file, err)
	}

	warm := reopen(t, dir, cacheOpts())
	ws := warm.Stats()
	if ws.Cached != ws.Files || ws.Files != cs.Files {
		t.Fatalf("warm stats = %+v, want every one of %d files from the cache", ws, cs.Files)
	}
	if ws.Declarations != cs.Declarations || ws.Edges != cs.Edges {
		t.Fatalf("warm %+v and cold %+v disagree", ws, cs)
	}
	if got, want := dump(warm), dump(cold); got != want {
		t.Fatalf("warm extractions differ from the cold scan:\nwarm:\n%s\ncold:\n%s", got, want)
	}
	// The restored paths are real: a lookup answers with the file it lives in.
	if ds := warm.DeclarationsNamed(classB); len(ds) != 1 || ds[0].Path != filepath.Join(dir, "src", "Models", "B.php") {
		t.Fatalf("warm lookup of B = %+v", ds)
	}
	for _, d := range warm.DeclarationsNamed(classB) {
		for _, m := range d.Members {
			if m.Path != d.Path {
				t.Fatalf("member %s path = %q, want %q", m.Name, m.Path, d.Path)
			}
		}
	}
}

func TestCacheRefreshesChangedAddedRemoved(t *testing.T) {
	cold, dir := cachedFixture(t)
	before := cold.Stats().Files

	bPath := filepath.Join(dir, "src", "Models", "B.php")
	text, err := os.ReadFile(bPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(text[:len(text)-len("}\n")]) + " public function betweenSessions() {} }\n"
	if err := os.WriteFile(bPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	added := filepath.Join(dir, "src", "Models", "Added.php")
	if err := os.WriteFile(added, []byte("<?php\nnamespace App\\Models;\nclass Added {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed := firstOtherPHP(t, dir, bPath)
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}

	warm := reopen(t, dir, cacheOpts())
	s := warm.Stats()
	if s.Files != before || s.Cached != before-2 {
		t.Fatalf("warm stats = %+v, want %d files, %d of them cached (B changed, Added new, one removed)", s, before, before-2)
	}
	if !hasDeclMember(warm, classB, "betweenSessions") {
		t.Fatal("the changed file must be re-extracted")
	}
	if got := warm.DeclarationsNamed(`App\Models\Added`); len(got) != 1 {
		t.Fatalf("the new file must be indexed: %v", got)
	}
	warm.mu.Lock()
	p := warm.project
	warm.mu.Unlock()
	p.Each([]string{"php"}, func(path string, _ fileDecls) {
		if path == removed {
			t.Errorf("the removed file %s came back from the cache", path)
		}
	})
}

// firstOtherPHP is a PHP file of the fixture other than skip.
func firstOtherPHP(t *testing.T, dir, skip string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(filepath.Join(dir, "src"), func(p string, d os.DirEntry, err error) error {
		if err == nil && found == "" && !d.IsDir() && strings.HasSuffix(p, ".php") && p != skip {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("fixture has no second PHP file")
	}
	return found
}

func TestCacheOtherVersionIgnored(t *testing.T) {
	_, dir := cachedFixture(t)
	file := filepath.Join(dir, ".ike", cacheName)
	seed := loadCache(file, dir)
	if len(seed) == 0 {
		t.Fatal("setup: the cold scan should have written a loadable cache")
	}
	// Rewrite it under an older version, with a declaration no file holds:
	// were it trusted, Ghost would surface.
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	enc := gob.NewEncoder(f)
	if err := enc.Encode(cacheHeader{Version: cacheVersion - 1, Root: dir, Files: len(seed)}); err != nil {
		t.Fatal(err)
	}
	for path, s := range seed {
		s.Value.Decls = append(s.Value.Decls, Decl{Name: "Ghost", FQN: "Ghost"})
		if err := enc.Encode(cacheRecord{Path: path, Stamp: s.Stamp, Decls: s.Value}); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	warm := reopen(t, dir, cacheOpts())
	if s := warm.Stats(); s.Cached != 0 {
		t.Fatalf("an outdated cache must not seed the walk: %+v", s)
	}
	if got := warm.DeclarationsNamed("Ghost"); len(got) != 0 {
		t.Fatalf("outdated cache content leaked: %v", got)
	}
	// The cold walk replaced it with a current one.
	if got := loadCache(file, dir); len(got) != len(seed) {
		t.Fatalf("the rebuilt cache holds %d files, want %d", len(got), len(seed))
	}
	// A cache for another root (IKE_CONFIG_DIR shared between projects) is
	// ignored the same way.
	if got := loadCache(file, filepath.Join(dir, "elsewhere")); got != nil {
		t.Fatalf("a foreign root's cache must not load: %d entries", len(got))
	}
	// So is a damaged one.
	if err := os.WriteFile(file, []byte("not a cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadCache(file, dir); got != nil {
		t.Fatalf("a damaged cache must not load: %d entries", len(got))
	}
}

func TestCacheSwitchOff(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "")
	x, dir := fixture(t, defaultOpts()) // Cache: false
	file := filepath.Join(dir, ".ike", cacheName)
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("with php.index.cache off nothing is written: %v", err)
	}
	// Turning it on persists the finished walk…
	x.Reconfigure(cacheOpts())
	waitFor(t, "the cache write", func() bool { _, err := os.Stat(file); return err == nil })
	// …and the next session with it off ignores it.
	if s := reopen(t, dir, defaultOpts()).Stats(); s.Cached != 0 {
		t.Fatalf("cache off must not seed the walk: %+v", s)
	}
	// Turning it off deletes it.
	x.Reconfigure(defaultOpts())
	waitFor(t, "the cache removal", func() bool { _, err := os.Stat(file); return os.IsNotExist(err) })
}

func TestCacheRebuildRunsCold(t *testing.T) {
	_, dir := cachedFixture(t)
	warm := reopen(t, dir, cacheOpts())
	if warm.Stats().Cached == 0 {
		t.Fatal("setup: the second session should start warm")
	}
	if !warm.Rebuild() {
		t.Fatal("Rebuild should start a scan")
	}
	waitScan(t, warm)
	if s := warm.Stats(); s.Cached != 0 || s.Files == 0 {
		t.Fatalf("a rebuild must parse every file: %+v", s)
	}
}

func TestCacheNotWrittenForProjectWithoutPHP(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	x := New(dir, cacheOpts())
	if !x.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	waitScan(t, x)
	if _, err := os.Stat(filepath.Join(dir, ".ike")); !os.IsNotExist(err) {
		t.Fatalf("a project without PHP must not grow a .ike cache: %v", err)
	}
}

func TestCacheFileHonoursConfigDir(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "")
	if got, want := CacheFile("/p"), filepath.Join("/p", ".ike", cacheName); got != want {
		t.Fatalf("CacheFile = %q, want %q", got, want)
	}
	if got := CacheFile(""); got != "" {
		t.Fatalf("CacheFile(\"\") = %q, want none", got)
	}
	t.Setenv("IKE_CONFIG_DIR", "/state")
	if got, want := CacheFile("/p"), filepath.Join("/state", cacheName); got != want {
		t.Fatalf("CacheFile with IKE_CONFIG_DIR = %q, want %q", got, want)
	}
}

// cacheShape is the persisted format's type shape. When it changes, bump
// cacheVersion and update the golden below — a cache written in the old
// shape would otherwise decode into the new one with fields silently zero.
const cacheShape = `cacheHeader{Version:int Root:string Files:int} ` +
	`cacheRecord{Path:string Stamp:Stamp{Size:int64 ModTime:int64} Decls:fileDecls{Decls:[]Decl{Kind:int Name:string FQN:string Extends:[]string Implements:[]string Traits:[]string Abstract:bool Final:bool ` +
	`Members:[]Member{Kind:int Name:string Static:bool Visibility:string Params:string ReturnType:string Type:string Doc:string AliasOf:string Declaring:string Path:string ` +
	`Range:Range{Start:Pos{Line:int Col:int} End:Pos{Line:int Col:int}} NameRange:Range{Start:Pos{Line:int Col:int} End:Pos{Line:int Col:int}}} ` +
	`Path:string Range:Range{Start:Pos{Line:int Col:int} End:Pos{Line:int Col:int}} NameRange:Range{Start:Pos{Line:int Col:int} End:Pos{Line:int Col:int}}} Refs:[]string}}`

func TestCacheVersionTracksFormat(t *testing.T) {
	got := shape(reflect.TypeOf(cacheHeader{})) + " " + shape(reflect.TypeOf(cacheRecord{}))
	if got != cacheShape || cacheVersion != 1 {
		t.Fatalf("the cache format changed — bump cacheVersion (now %d) and update cacheShape:\n%s", cacheVersion, got)
	}
}

// shape renders t's structure: struct fields by name and kind, recursively.
func shape(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Struct:
		parts := make([]string, t.NumField())
		for i := range parts {
			f := t.Field(i)
			parts[i] = f.Name + ":" + shape(f.Type)
		}
		return t.Name() + "{" + strings.Join(parts, " ") + "}"
	case reflect.Slice:
		return "[]" + shape(t.Elem())
	case reflect.Map:
		return "map[" + shape(t.Key()) + "]" + shape(t.Elem())
	}
	return t.Kind().String()
}
