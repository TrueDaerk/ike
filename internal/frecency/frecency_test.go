package frecency

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const testHalfLife = 7 * 24 * time.Hour

// fixedNow returns a clock pinned to base, offset by d.
func fixedNow(base time.Time, d time.Duration) func() time.Time {
	return func() time.Time { return base.Add(d) }
}

func TestPersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "frecency.json")
	base := time.Unix(1_700_000_000, 0)
	f := Load(path, testHalfLife)
	f.SetNow(fixedNow(base, 0))
	f.Record("b.gamma")
	f.Record("b.gamma")
	f.Record("a.alpha")

	re := Load(path, testHalfLife)
	re.SetNow(fixedNow(base, 0))
	if got := re.Score("b.gamma"); got < 1.99 || got > 2.01 {
		t.Fatalf("persisted score = %v, want ~2", got)
	}
	if got := re.Score("c.regen"); got != 0 {
		t.Fatalf("unrecorded score = %v, want 0", got)
	}
	if re.Score("b.gamma") <= re.Score("a.alpha") {
		t.Fatalf("reload lost the ordering: a=%v b=%v", re.Score("a.alpha"), re.Score("b.gamma"))
	}
}

// TestDecaysWithAge guards the decay math: one half-life halves a hit, two
// quarter it, and a clock stepping backwards never inflates a score.
func TestDecaysWithAge(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	f := Load("", testHalfLife)
	f.SetNow(fixedNow(base, 0))
	f.Record("a.alpha")

	for _, c := range []struct {
		age  time.Duration
		want float64
	}{
		{0, 1},
		{testHalfLife, 0.5},
		{2 * testHalfLife, 0.25},
		{10 * testHalfLife, 1.0 / 1024},
	} {
		f.SetNow(fixedNow(base, c.age))
		if got := f.Score("a.alpha"); got < c.want*0.99 || got > c.want*1.01 {
			t.Errorf("score at age %v = %v, want ~%v", c.age, got, c.want)
		}
	}

	f.SetNow(fixedNow(base, -time.Hour))
	if got := f.Score("a.alpha"); got > 1.0001 {
		t.Fatalf("score with a backwards clock = %v, want <= 1", got)
	}
}

// TestHalfLifeIsPerStore guards the shared-package contract: each consumer
// picks how fast its history fades (7 days for commands, 14 for file opens).
func TestHalfLifeIsPerStore(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	fast := Load("", 24*time.Hour)
	slow := Load("", 14*24*time.Hour)
	for _, f := range []*Store{fast, slow} {
		f.SetNow(fixedNow(base, 0))
		f.Record("x")
		f.SetNow(fixedNow(base, 24*time.Hour))
	}
	if got := fast.Score("x"); got < 0.49 || got > 0.51 {
		t.Fatalf("fast store after its half-life = %v, want ~0.5", got)
	}
	if slow.Score("x") <= fast.Score("x") {
		t.Fatalf("the slower half-life must decay less: slow=%v fast=%v", slow.Score("x"), fast.Score("x"))
	}
	// A non-positive half-life falls back to the week default rather than
	// dividing by zero.
	def := Load("", 0)
	def.SetNow(fixedNow(base, 0))
	def.Record("x")
	def.SetNow(fixedNow(base, 7*24*time.Hour))
	if got := def.Score("x"); got < 0.49 || got > 0.51 {
		t.Fatalf("default half-life = %v, want ~0.5 after a week", got)
	}
}

// TestFrequencyBeatsStaleRecency guards the ranking intent: many recent events
// outrank a single fresh one, and a stale event decays below a fresh one.
func TestFrequencyBeatsStaleRecency(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	f := Load("", testHalfLife)
	f.SetNow(fixedNow(base, 0))
	f.Record("old")
	for i := 0; i < 5; i++ {
		f.SetNow(fixedNow(base, 4*testHalfLife+time.Duration(i)*time.Hour))
		f.Record("hot")
	}
	f.SetNow(fixedNow(base, 4*testHalfLife+5*time.Hour))
	f.Record("fresh")

	if f.Score("hot") <= f.Score("fresh") {
		t.Fatalf("frequent key lost to a single fresh event: hot=%v fresh=%v", f.Score("hot"), f.Score("fresh"))
	}
	if f.Score("fresh") <= f.Score("old") {
		t.Fatalf("stale key did not decay below a fresh one: fresh=%v old=%v", f.Score("fresh"), f.Score("old"))
	}
}

// TestToleratesCorruptFile guards the acceptance criterion: garbage, a
// truncated file and structurally broken entries load as empty history and the
// store stays usable.
func TestToleratesCorruptFile(t *testing.T) {
	dir := t.TempDir()
	for i, body := range []string{"{not json", "{", `{"a":[`, `[1,2,3]`, `null`, `{"a":[],"":[1]}`} {
		path := filepath.Join(dir, "corrupt"+strconv.Itoa(i)+".json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		f := Load(path, testHalfLife)
		if f.Len() != 0 || f.Score("a") != 0 {
			t.Fatalf("body %q: want empty history, got len=%d", body, f.Len())
		}
		// The store stays usable and repairs the file on the next write.
		f.Record("a")
		if got := Load(path, testHalfLife).Score("a"); got <= 0 {
			t.Fatalf("body %q: record after corrupt load did not persist (%v)", body, got)
		}
	}
	// A missing file is equally tolerated.
	if got := Load(filepath.Join(dir, "none.json"), testHalfLife).Score("x"); got != 0 {
		t.Fatalf("missing file must load empty, score = %v", got)
	}
}

// TestCapsHistory guards both caps: timestamps per key and tracked keys, the
// coldest pruned first.
func TestCapsHistory(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	f := Load("", testHalfLife)
	f.SetNow(fixedNow(base, 0))
	for i := 0; i < MaxHits*3; i++ {
		f.Record("a.alpha")
	}
	if got := len(f.hits["a.alpha"]); got != MaxHits {
		t.Fatalf("hits per key = %d, want %d", got, MaxHits)
	}
	for i := 0; i < MaxKeys+50; i++ {
		f.Record(fmt.Sprintf("gen.%03d", i))
	}
	if got := f.Len(); got != MaxKeys {
		t.Fatalf("tracked keys = %d, want %d", got, MaxKeys)
	}
	if f.Score("a.alpha") == 0 {
		t.Fatal("the most-recorded key must survive pruning")
	}
}

// TestKeyNormalizesPaths guards the file-open key contract (#2155): both sides
// of the '@' finder must spell the same file the same way.
func TestKeyNormalizesPaths(t *testing.T) {
	if Key("") != "" {
		t.Fatalf("empty path must stay empty")
	}
	if got := Key("/tmp/a/../b.go"); got != filepath.Clean("/tmp/b.go") {
		t.Fatalf("Key(/tmp/a/../b.go) = %q", got)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Skip("no working directory")
	}
	if got, want := Key("rel/file.go"), filepath.Join(cwd, "rel/file.go"); got != want {
		t.Fatalf("Key(rel/file.go) = %q, want %q", got, want)
	}
	if got := Key(filepath.Join(cwd, "rel/file.go")); got != Key("rel/file.go") {
		t.Fatalf("relative and absolute spellings must agree: %q", got)
	}
}

// TestNilStoreInert guards the nil-safety contract callers rely on.
func TestNilStoreInert(t *testing.T) {
	var f *Store
	f.SetNow(time.Now)
	f.Record("a")
	if f.Score("a") != 0 || f.Len() != 0 {
		t.Fatalf("nil store is not inert")
	}
	// An empty key records nothing.
	s := Load("", testHalfLife)
	s.Record("")
	if s.Len() != 0 || s.Score("") != 0 {
		t.Fatalf("empty key must be ignored")
	}
}
