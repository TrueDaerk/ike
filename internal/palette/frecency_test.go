package palette

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// frecSource is a stub CommandSource whose titles differ in match quality for
// the query "gamma": "Gamma" matches at the very start, "Regenerate Gamma"
// only mid-title.
type frecSource struct{}

func (frecSource) Commands() []registry.OwnedCommand {
	mk := func(id, title string) registry.OwnedCommand {
		return registry.OwnedCommand{Owner: "test", Command: plugin.Command{
			ID: id, Title: title, Scope: plugin.Scope{Global: true},
		}}
	}
	return []registry.OwnedCommand{
		mk("a.alpha", "Alpha"),
		mk("b.gamma", "Gamma"),
		mk("c.regen", "Regenerate Gamma"),
	}
}

// fixedNow returns a clock pinned to base, offset by d.
func fixedNow(base time.Time, d time.Duration) func() time.Time {
	return func() time.Time { return base.Add(d) }
}

func TestFrecencyPersistsAcrossLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdfrecency.json")
	base := time.Unix(1_700_000_000, 0)
	f := LoadFrecency(path)
	f.SetNow(fixedNow(base, 0))
	f.Record("b.gamma")
	f.Record("b.gamma")
	f.Record("a.alpha")

	re := LoadFrecency(path)
	re.SetNow(fixedNow(base, 0))
	if got := re.Score("b.gamma"); got < 1.99 || got > 2.01 {
		t.Fatalf("persisted score = %v, want ~2", got)
	}
	if got := re.Score("c.regen"); got != 0 {
		t.Fatalf("unrecorded score = %v, want 0", got)
	}
	// Nil receiver and empty id stay inert.
	var nilF *Frecency
	nilF.Record("x")
	if nilF.Score("x") != 0 {
		t.Fatal("nil Frecency must score 0")
	}
}

func TestFrecencyDecaysWithAge(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	f := LoadFrecency(filepath.Join(t.TempDir(), "cmdfrecency.json"))
	f.SetNow(fixedNow(base, 0))
	f.Record("a.alpha")

	// One half-life later the single hit is worth half an execution.
	f.SetNow(fixedNow(base, frecencyHalfLife))
	if got := f.Score("a.alpha"); got < 0.49 || got > 0.51 {
		t.Fatalf("score after one half-life = %v, want ~0.5", got)
	}
	// A clock that steps backwards must not inflate the score.
	f.SetNow(fixedNow(base, -time.Hour))
	if got := f.Score("a.alpha"); got > 1.0001 {
		t.Fatalf("score with a backwards clock = %v, want <= 1", got)
	}
}

func TestFrecencyToleratesCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdfrecency.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := LoadFrecency(path)
	if got := f.Score("a.alpha"); got != 0 {
		t.Fatalf("corrupt file must load empty, score = %v", got)
	}
	// The store stays usable and repairs the file on the next write.
	f.Record("a.alpha")
	if got := LoadFrecency(path).Score("a.alpha"); got <= 0 {
		t.Fatalf("record after corrupt load did not persist, score = %v", got)
	}
	// A missing file is equally tolerated.
	if got := LoadFrecency(filepath.Join(t.TempDir(), "none.json")).Score("x"); got != 0 {
		t.Fatalf("missing file must load empty, score = %v", got)
	}
}

func TestFrecencyCapsHistory(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	f := LoadFrecency(filepath.Join(t.TempDir(), "cmdfrecency.json"))
	f.SetNow(fixedNow(base, 0))
	for i := 0; i < frecencyMaxHits*3; i++ {
		f.Record("a.alpha")
	}
	if got := len(f.hits["a.alpha"]); got != frecencyMaxHits {
		t.Fatalf("hits per command = %d, want %d", got, frecencyMaxHits)
	}
	// Many distinct commands: the store caps the id count, keeping the
	// highest-scoring entries.
	for i := 0; i < frecencyMaxIDs+50; i++ {
		f.Record(fmt.Sprintf("gen.%03d", i))
	}
	if got := len(f.hits); got != frecencyMaxIDs {
		t.Fatalf("tracked commands = %d, want %d", got, frecencyMaxIDs)
	}
	if f.Score("a.alpha") == 0 {
		t.Fatal("the most-executed command must survive pruning")
	}
}

func TestCommandModeEmptyQueryRanksByFrecency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdfrecency.json")
	base := time.Unix(1_700_000_000, 0)
	f := LoadFrecency(path)
	f.SetNow(fixedNow(base, 0))
	f.Record("c.regen")
	f.Record("c.regen")

	c := NewCommandMode(frecSource{}, nil, false)
	c.SetFrecency(f)
	items := c.Results("", Context{})
	if len(items) != 3 || items[0].Title != "Regenerate Gamma" {
		t.Fatalf("empty-query order wrong: %+v", titles(items))
	}

	// The boost survives a restart: a freshly loaded store ranks the same.
	re := LoadFrecency(path)
	re.SetNow(fixedNow(base, time.Hour))
	c2 := NewCommandMode(frecSource{}, nil, false)
	c2.SetFrecency(re)
	if got := titles(c2.Results("", Context{})); got[0] != "Regenerate Gamma" {
		t.Fatalf("boost did not persist across a reload: %+v", got)
	}
}

func TestCommandModeLongQueryFuzzyDominatesFrecency(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	f := LoadFrecency(filepath.Join(t.TempDir(), "cmdfrecency.json"))
	f.SetNow(fixedNow(base, 0))
	for i := 0; i < 10; i++ {
		f.Record("c.regen") // heavy history on the weaker match
	}
	c := NewCommandMode(frecSource{}, nil, false)
	c.SetFrecency(f)

	// One rune: the fuzzy scores tie, so history decides.
	if got := titles(c.Results("g", Context{})); got[0] != "Regenerate Gamma" {
		t.Fatalf("short query should follow history: %+v", got)
	}
	// Five runes: the far better match wins despite ten recent executions.
	if got := titles(c.Results("gamma", Context{})); got[0] != "Gamma" {
		t.Fatalf("long query must be dominated by match quality: %+v", got)
	}
}

// titles extracts result titles in order, for order assertions.
func titles(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}
