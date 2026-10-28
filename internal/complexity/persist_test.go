package complexity

// persist_test.go covers the persisted project index (#2967): a warm start
// over an unchanged tree measures nothing and knows what a cold walk knows,
// a file changed or removed between sessions is re-measured or dropped, a
// cache of another spec version is ignored, a damaged one is reported once
// and rewritten, and a rebuild or a changed exclude list walks cold.

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

// persistedIndex is a started, finished index over a fresh fixture with its
// cache under the project (IKE_CONFIG_DIR cleared, so CacheFile lands in
// <root>/.ike).
func persistedIndex(t *testing.T) (*Index, string) {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", "")
	x, dir := newTestIndex(t, Options{Enabled: true})
	x.Start()
	waitScan(t, x)
	return x, dir
}

// nextSession is a fresh index over dir, walk finished.
func nextSession(t *testing.T, dir string, opts Options) *Index {
	t.Helper()
	x := NewIndex(dir, opts)
	x.Start()
	waitScan(t, x)
	return x
}

// dumpView renders a snapshot canonically, path by path, so a cold and a
// warm index compare as text.
func dumpView(v View) string {
	var rows []string
	for p, fns := range v.Files {
		rows = append(rows, fmt.Sprintf("%s %+v %+v", p, fns, v.Tokens[p]))
	}
	sort.Strings(rows)
	return strings.Join(rows, "\n")
}

func TestCacheWarmStartMeasuresNothing(t *testing.T) {
	cold, dir := persistedIndex(t)
	cs := cold.Stats()
	if cs.Cached != 0 || cs.Files == 0 || cs.CacheCorrupt {
		t.Fatalf("cold stats = %+v, want files and nothing cached", cs)
	}
	if _, err := os.Stat(filepath.Join(dir, ".ike", cacheName)); err != nil {
		t.Fatalf("the finished walk must write the cache: %v", err)
	}
	warm := nextSession(t, dir, Options{Enabled: true})
	ws := warm.Stats()
	if ws.Cached != ws.Files || ws.Files != cs.Files || ws.Functions != cs.Functions {
		t.Fatalf("warm stats = %+v, want every one of %d files from the cache", ws, cs.Files)
	}
	if got, want := dumpView(warm.Snapshot()), dumpView(cold.Snapshot()); got != want {
		t.Fatalf("warm measurements differ from the cold walk:\nwarm:\n%s\ncold:\n%s", got, want)
	}
}

func TestCacheRemeasuresTouchedAndDropsDeleted(t *testing.T) {
	cold, dir := persistedIndex(t)
	files := cold.Stats().Files
	touched := filepath.Join(dir, "py", "b.py")
	if err := os.WriteFile(touched, []byte("def b(x):\n    return 0\n\ndef b2():\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "js", "e.js")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	warm := nextSession(t, dir, Options{Enabled: true})
	ws := warm.Stats()
	if ws.Files != files-1 || ws.Cached != files-2 {
		t.Fatalf("warm stats = %+v, want %d files, %d of them cached", ws, files-1, files-2)
	}
	v := warm.Snapshot()
	if _, ok := v.Files[gone]; ok {
		t.Fatal("a deleted file came back from the cache")
	}
	if fns := v.Files[touched]; len(fns) != 2 {
		t.Fatalf("touched file = %+v, want its two new functions", fns)
	}
}

func TestCacheOtherSpecVersionIsIgnored(t *testing.T) {
	_, dir := persistedIndex(t)
	file := filepath.Join(dir, ".ike", cacheName)
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	enc := gob.NewEncoder(f)
	_ = enc.Encode(cacheHeader{Version: SpecVersion + 1, Root: dir, Files: 1})
	_ = enc.Encode(cacheRecord{Path: filepath.Join(dir, "src", "a.go")})
	f.Close()
	warm := nextSession(t, dir, Options{Enabled: true})
	if s := warm.Stats(); s.Cached != 0 || s.Files == 0 || s.CacheCorrupt {
		t.Fatalf("stats = %+v, want a cold walk and no corruption reported", s)
	}
	if seed, _ := loadCache(file, dir); len(seed) == 0 {
		t.Fatal("the walk must rewrite the outdated cache under the current version")
	}
}

func TestCacheCorruptIsReportedAndRewritten(t *testing.T) {
	_, dir := persistedIndex(t)
	file := filepath.Join(dir, ".ike", cacheName)
	if err := os.WriteFile(file, []byte("not a gob stream"), 0o644); err != nil {
		t.Fatal(err)
	}
	warm := nextSession(t, dir, Options{Enabled: true})
	s := warm.Stats()
	if s.Cached != 0 || s.Files == 0 || !s.CacheCorrupt {
		t.Fatalf("stats = %+v, want a cold walk reporting the corrupt cache", s)
	}
	seed, corrupt := loadCache(file, dir)
	if corrupt || len(seed) != s.Files {
		t.Fatalf("rewritten cache: %d files, corrupt %v; want %d", len(seed), corrupt, s.Files)
	}
	// A rebuild walks a fresh project: the report belongs to the old one.
	if !warm.Rebuild() {
		t.Fatal("rebuild refused")
	}
	waitScan(t, warm)
	if warm.Stats().CacheCorrupt {
		t.Fatal("the rebuilt walk still reports the corrupt cache")
	}
}

func TestCacheRebuildAndExcludeChangeWalkCold(t *testing.T) {
	_, dir := persistedIndex(t)
	x := nextSession(t, dir, Options{Enabled: true})
	if s := x.Stats(); s.Cached != s.Files {
		t.Fatalf("warm stats = %+v, want everything cached", s)
	}
	if !x.Rebuild() {
		t.Fatal("rebuild refused")
	}
	waitScan(t, x)
	if s := x.Stats(); s.Cached != 0 || s.Files == 0 {
		t.Fatalf("rebuilt stats = %+v, want a cold walk", s)
	}
	x.Reconfigure(Options{Enabled: true, Exclude: []string{"*.sh"}})
	waitScan(t, x)
	if s := x.Stats(); s.Cached != 0 || s.Files == 0 {
		t.Fatalf("re-excluded stats = %+v, want a cold walk", s)
	}
	// Switching the index back on seeds again.
	x.Reconfigure(Options{Enabled: false, Exclude: []string{"*.sh"}})
	x.Reconfigure(Options{Enabled: true, Exclude: []string{"*.sh"}})
	waitScan(t, x)
	if s := x.Stats(); s.Cached != s.Files || s.Files == 0 {
		t.Fatalf("re-enabled stats = %+v, want everything cached", s)
	}
}

// TestRescanIsIncremental pins #3085: Rescan re-walks from the current
// measurements — an unchanged tree measures nothing, a touched file is the
// only one measured, a deleted one drops out — and still writes the cache.
func TestRescanIsIncremental(t *testing.T) {
	x, dir := persistedIndex(t)
	files := x.Stats().Files
	if !x.Rescan() {
		t.Fatal("rescan refused")
	}
	waitScan(t, x)
	if s := x.Stats(); s.Files != files || s.Cached != files {
		t.Fatalf("rescan of an unchanged tree = %+v, want all %d files cached", s, files)
	}
	touched := filepath.Join(dir, "py", "b.py")
	if err := os.WriteFile(touched, []byte("def b(x):\n    return 0\n\ndef b2():\n    pass\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, "js", "e.js")
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if !x.Rescan() {
		t.Fatal("rescan refused")
	}
	waitScan(t, x)
	if s := x.Stats(); s.Files != files-1 || s.Cached != files-2 {
		t.Fatalf("rescan = %+v, want %d files, %d of them cached", s, files-1, files-2)
	}
	v := x.Snapshot()
	if _, ok := v.Files[gone]; ok {
		t.Fatal("a deleted file came back from the seed")
	}
	if fns := v.Files[touched]; len(fns) != 2 {
		t.Fatalf("touched file = %+v, want its two new functions", fns)
	}
	if seed, _ := loadCache(filepath.Join(dir, ".ike", cacheName), dir); len(seed) != files-1 {
		t.Fatalf("the rescan must rewrite the cache: %d records, want %d", len(seed), files-1)
	}
	x.Reconfigure(Options{Enabled: false})
	if x.Rescan() {
		t.Fatal("a disabled index must refuse the rescan")
	}
}

// TestReconfigureIgnoresRespelledExclude: a reload that only re-spells the
// exclude list (spaces, blank entries) is no change, so no cold walk (#3085).
func TestReconfigureIgnoresRespelledExclude(t *testing.T) {
	a := Options{Enabled: true, Exclude: []string{"vendor/**", "*.sh"}}
	if !a.equal(Options{Enabled: true, Exclude: []string{" vendor/**", "", "*.sh "}}) {
		t.Fatal("a re-spelled exclude list must compare equal")
	}
	if a.equal(Options{Enabled: true, Exclude: []string{"vendor/**"}}) {
		t.Fatal("a shorter exclude list must differ")
	}
	if !(Options{Enabled: true}).equal(Options{Enabled: true, Exclude: []string{}}) {
		t.Fatal("nil and empty exclude lists must compare equal")
	}
}

func TestCacheFileFollowsConfigDir(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", "")
	if got, want := CacheFile("/p"), filepath.Join("/p", ".ike", cacheName); got != want {
		t.Fatalf("CacheFile = %q, want %q", got, want)
	}
	if CacheFile("") != "" {
		t.Fatal("an empty root persists nothing")
	}
	t.Setenv("IKE_CONFIG_DIR", "/cfg")
	if got, want := CacheFile("/p"), filepath.Join("/cfg", cacheName); got != want {
		t.Fatalf("CacheFile = %q, want %q", got, want)
	}
}

func TestCacheDropsBaselineStatus(t *testing.T) {
	fns := []Function{{Name: "a", Baseline: BaselineAccepted}}
	out := withoutBaseline(fns)
	if out[0].Baseline != BaselineNew || fns[0].Baseline != BaselineAccepted {
		t.Fatalf("withoutBaseline = %+v (live %+v), want a cleared copy", out, fns)
	}
}

// cacheShape pins the persisted format; see TestCacheVersionTracksFormat.
const cacheShape = "cacheHeader{Version:int Root:string Files:int} " +
	"cacheRecord{Path:string Stamp:Stamp{Size:int64 ModTime:int64} Functions:[]Function{" +
	"Name:string Class:string Kind:string Line:int EndLine:int NLOC:int Params:int CCN:int Cognitive:int Depth:int " +
	"CommentLines:int CommentDensity:float64 " +
	"Sites:[]Site{Line:int Col:int EndLine:int Kind:string CCN:int Cognitive:int Nesting:int Level:int} " +
	"Candidates:[]Candidate{Line:int EndLine:int Kind:string CogSaving:int NewCCN:int NewCog:int RestCCN:int RestCog:int} " +
	"Baseline:int} Tokens:Tokens{Lang:string Hash:[]uint32 Line:[]int32 Seg:[]int32}}"

// TestCacheVersionTracksFormat fails when the persisted shape moves: bump
// SpecVersion and update cacheShape together.
func TestCacheVersionTracksFormat(t *testing.T) {
	got := cacheTypeShape(reflect.TypeOf(cacheHeader{})) + " " + cacheTypeShape(reflect.TypeOf(cacheRecord{}))
	if got != cacheShape || SpecVersion != 2 {
		t.Fatalf("the cache format changed — bump SpecVersion (now %d) and update cacheShape:\n%s", SpecVersion, got)
	}
}

// cacheTypeShape renders t's structure: struct fields by name and kind,
// recursively.
func cacheTypeShape(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Struct:
		parts := make([]string, t.NumField())
		for i := range parts {
			f := t.Field(i)
			parts[i] = f.Name + ":" + cacheTypeShape(f.Type)
		}
		return t.Name() + "{" + strings.Join(parts, " ") + "}"
	case reflect.Slice:
		return "[]" + cacheTypeShape(t.Elem())
	case reflect.Map:
		return "map[" + cacheTypeShape(t.Key()) + "]" + cacheTypeShape(t.Elem())
	}
	return t.Kind().String()
}
