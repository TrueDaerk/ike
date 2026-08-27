package frecency

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// clockAt returns a settable clock starting at t, plus a knob to advance it.
func clockAt(t time.Time) (func() time.Time, func(time.Duration)) {
	now := t
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func near(got, want float64) bool { return math.Abs(got-want) < 1e-9 }

// TestDecayMath guards the decay curve: fresh events count fully, one
// half-life halves them, two quarter them, and a backwards clock never
// amplifies.
func TestDecayMath(t *testing.T) {
	cases := []struct {
		age  time.Duration
		want float64
	}{
		{-time.Hour, 1},
		{0, 1},
		{HalfLife, 0.5},
		{2 * HalfLife, 0.25},
		{10 * HalfLife, 1.0 / 1024},
	}
	for _, c := range cases {
		if got := Decay(c.age); !near(got, c.want) {
			t.Errorf("Decay(%v) = %v, want %v", c.age, got, c.want)
		}
	}
}

// TestBumpAccumulatesAndDecays guards the accumulator: repeated events add up,
// and the stored weight ages between them as well as on read.
func TestBumpAccumulatesAndDecays(t *testing.T) {
	now, advance := clockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := Load("")
	s.SetClock(now)

	s.Bump("a")
	s.Bump("a")
	if got := s.Score("a"); !near(got, 2) {
		t.Fatalf("two immediate bumps: score = %v, want 2", got)
	}

	advance(HalfLife)
	if got := s.Score("a"); !near(got, 1) {
		t.Fatalf("after one half-life: score = %v, want 1", got)
	}

	// A bump ages the old weight first, then adds one: 1 + 1 = 2.
	s.Bump("a")
	if got := s.Score("a"); !near(got, 2) {
		t.Fatalf("bump after decay: score = %v, want 2", got)
	}

	if got := s.Score("never-touched"); got != 0 {
		t.Fatalf("unknown key: score = %v, want 0", got)
	}
}

// TestFrequencyBeatsStaleRecency guards the ranking intent: a file opened many
// times recently outranks one opened once a moment ago, while a single ancient
// open loses to a single fresh one.
func TestFrequencyBeatsStaleRecency(t *testing.T) {
	now, advance := clockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := Load("")
	s.SetClock(now)

	s.Bump("old")
	advance(4 * HalfLife)
	for i := 0; i < 5; i++ {
		s.Bump("hot")
		advance(time.Hour)
	}
	s.Bump("fresh")

	if s.Score("hot") <= s.Score("fresh") {
		t.Fatalf("frequent file lost to a single fresh open: hot=%v fresh=%v", s.Score("hot"), s.Score("fresh"))
	}
	if s.Score("fresh") <= s.Score("old") {
		t.Fatalf("stale file did not decay below a fresh one: fresh=%v old=%v", s.Score("fresh"), s.Score("old"))
	}
}

// TestPersistenceRoundTrip guards that events survive a reload of the store
// file, keeping their relative order.
func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "frecency.json")
	now, _ := clockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := Load(path)
	s.SetClock(now)
	s.Bump("a")
	s.Bump("b")
	s.Bump("b")

	back := Load(path)
	back.SetClock(now)
	if got := back.Score("b"); !near(got, 2) {
		t.Fatalf("reloaded score = %v, want 2", got)
	}
	if back.Score("b") <= back.Score("a") {
		t.Fatalf("reload lost the ordering: a=%v b=%v", back.Score("a"), back.Score("b"))
	}
}

// TestCorruptStoreTolerated guards the acceptance criterion: garbage, a
// truncated file, and impossible weights degrade to empty/ignored rather than
// disrupting the caller.
func TestCorruptStoreTolerated(t *testing.T) {
	dir := t.TempDir()
	for i, body := range []string{"not json at all", "{", `{"a":{"w":`, `[1,2,3]`, `null`} {
		path := filepath.Join(dir, "corrupt"+strconv.Itoa(i)+".json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s := Load(path)
		if s.Len() != 0 || s.Score("a") != 0 {
			t.Fatalf("body %q: want empty store, got len=%d", body, s.Len())
		}
		// A corrupt store still works from here on.
		s.Bump("a")
		if s.Score("a") <= 0 {
			t.Fatalf("body %q: bump after corrupt load had no effect", body)
		}
	}

	bad := filepath.Join(dir, "impossible.json")
	if err := os.WriteFile(bad, []byte(`{"a":{"w":-5,"t":"2026-01-01T00:00:00Z"},"":{"w":5,"t":"2026-01-01T00:00:00Z"},"ok":{"w":3,"t":"2026-01-01T00:00:00Z"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := Load(bad)
	if s.Len() != 1 || s.Score("a") != 0 {
		t.Fatalf("impossible weights kept: len=%d score(a)=%v", s.Len(), s.Score("a"))
	}
}

// TestCapDropsColdestEntries guards the cap: the store stays bounded and keeps
// the hot keys.
func TestCapDropsColdestEntries(t *testing.T) {
	now, advance := clockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	s := Load("")
	s.SetClock(now)

	s.Bump("hot")
	for i := 0; i < 3; i++ {
		s.Bump("hot")
	}
	advance(time.Minute)
	for i := 0; i < MaxEntries+50; i++ {
		s.Bump("cold" + strconv.Itoa(i))
	}
	if s.Len() > MaxEntries {
		t.Fatalf("store grew past the cap: len=%d", s.Len())
	}
	if s.Score("hot") == 0 {
		t.Fatalf("cap dropped the hottest key")
	}
}

// TestNilStoreInert guards the nil-safety contract callers rely on.
func TestNilStoreInert(t *testing.T) {
	var s *Store
	s.SetClock(time.Now)
	s.Bump("a")
	if s.Score("a") != 0 || s.Len() != 0 {
		t.Fatalf("nil store is not inert")
	}
}
