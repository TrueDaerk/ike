package palette

import (
	"path/filepath"
	"testing"
	"time"

	"ike/internal/plugin"
	"ike/internal/registry"
)

// The store itself (persistence, decay, corrupt files, caps) is tested in
// internal/frecency; what belongs here is the palette's *blend policy* — how a
// decayed count turns into ranking.

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

// TestFileFrecencyHalfLifeIsSlower guards the two stores' distinct decay
// (#2155): a file open a week old still counts nearly fully, while a command
// execution of the same age is worth half.
func TestFileFrecencyHalfLifeIsSlower(t *testing.T) {
	base := time.Unix(1_700_000_000, 0)
	week := 7 * 24 * time.Hour
	cmd := LoadFrecency(filepath.Join(t.TempDir(), "cmdfrecency.json"))
	file := LoadFileFrecency(filepath.Join(t.TempDir(), "filefrecency.json"))
	for _, f := range []*Frecency{cmd, file} {
		f.SetNow(fixedNow(base, 0))
		f.Record("x")
		f.SetNow(fixedNow(base, week))
	}
	if got := cmd.Score("x"); got < 0.49 || got > 0.51 {
		t.Fatalf("command score after a week = %v, want ~0.5", got)
	}
	if file.Score("x") <= cmd.Score("x") {
		t.Fatalf("file history must decay slower: file=%v cmd=%v", file.Score("x"), cmd.Score("x"))
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
