package palette

import (
	"path/filepath"
	"testing"
	"time"

	"ike/internal/frecency"
)

// frecStore builds an in-memory file-open history on a fixed clock and records
// each listed project-relative path the given number of times.
func frecStore(root string, opens map[string]int) *Frecency {
	s := LoadFileFrecency("")
	base := time.Unix(1_700_000_000, 0)
	s.SetNow(fixedNow(base, 0))
	for rel, n := range opens {
		for i := 0; i < n; i++ {
			s.Record(frecency.Key(filepath.Join(root, rel)))
		}
	}
	return s
}

// TestFileModeFrecencyLeadsOnEmptyQuery guards #2155: with no query typed the
// finder opens on the files one actually works on, not on the alphabetical
// head of the tree — and a hotter file outranks a merely warm one.
func TestFileModeFrecencyLeadsOnEmptyQuery(t *testing.T) {
	f := fileMode("a.go", "b.go", "c.go")
	cx := Context{Root: "/proj"}
	f.SetFrecency(frecStore(cx.Root, map[string]int{"c.go": 3, "b.go": 1}))

	if got := titles(f.Results("", cx)); got[0] != "c.go" || got[1] != "b.go" || got[2] != "a.go" {
		t.Fatalf("empty query order = %v, want [c.go b.go a.go]", got)
	}
}

// TestFileModeFrecencyLeadsOnShortQuery guards #2155's blend order below the
// threshold: at one or two characters the fuzzy score barely discriminates, so
// a hot file wins even against a strictly better match.
func TestFileModeFrecencyLeadsOnShortQuery(t *testing.T) {
	f := fileMode("no.go", "xxnote.go")
	cx := Context{Root: "/proj"}
	f.SetFrecency(frecStore(cx.Root, map[string]int{"xxnote.go": 2}))

	// Sanity: without frecency the better match ("no.go", start anchor) leads.
	cold := fileMode("no.go", "xxnote.go")
	if got := titles(cold.Results("no", cx)); got[0] != "no.go" {
		t.Fatalf("cold short query = %v, want no.go first", got)
	}
	if got := titles(f.Results("no", cx)); got[0] != "xxnote.go" {
		t.Fatalf("short query (2 chars) = %v, want the hot file first", got)
	}
}

// TestFileModeScoreLeadsOnLongQuery guards the other side of the blend: from
// the third character the typed text is a real signal, so match quality wins
// and frecency no longer drags a worse match to the top.
func TestFileModeScoreLeadsOnLongQuery(t *testing.T) {
	f := fileMode("note.go", "xxnote.go")
	cx := Context{Root: "/proj"}
	f.SetFrecency(frecStore(cx.Root, map[string]int{"xxnote.go": 10}))

	if got := titles(f.Results("note", cx)); got[0] != "note.go" {
		t.Fatalf("long query = %v, want the better match (note.go) first", got)
	}
}

// TestFileModeFrecencyBreaksScoreTies guards the acceptance criterion: at
// equal fuzzy score the recently/frequently opened file ranks above the cold
// one — and above the path tiebreak that would otherwise decide.
func TestFileModeFrecencyBreaksScoreTies(t *testing.T) {
	f := fileMode("aa/note.go", "bb/note.go")
	cx := Context{Root: "/proj"}

	cold := fileMode("aa/note.go", "bb/note.go")
	got := cold.Results("note", cx)
	if len(got) != 2 || got[0].Score != got[1].Score {
		t.Fatalf("test setup: want two equally scored matches, got %v", got)
	}
	if titles(got)[0] != "aa/note.go" {
		t.Fatalf("cold tie = %v, want the path tiebreak (aa/note.go) first", titles(got))
	}

	f.SetFrecency(frecStore(cx.Root, map[string]int{"bb/note.go": 1}))
	if got := titles(f.Results("note", cx)); got[0] != "bb/note.go" {
		t.Fatalf("frecency tie-break = %v, want bb/note.go first", got)
	}
}

// TestFileModeFrecencyBeatsUsage guards the precedence between the two ranking
// aids at equal fuzzy score: frecency (every open, decayed) outranks the raw
// palette-confirmation counter.
func TestFileModeFrecencyBeatsUsage(t *testing.T) {
	f := fileMode("aa/note.go", "bb/note.go")
	cx := Context{Root: "/proj"}
	u := &Usage{}
	u.Bump(filepath.Join(cx.Root, "aa/note.go"))
	u.Bump(filepath.Join(cx.Root, "aa/note.go"))
	f.SetUsage(u)
	f.SetFrecency(frecStore(cx.Root, map[string]int{"bb/note.go": 1}))

	if got := titles(f.Results("note", cx)); got[0] != "bb/note.go" {
		t.Fatalf("frecency vs usage = %v, want bb/note.go first", got)
	}
}

// TestFileModeFrecencyKeyMatchesEmittedPath guards the key contract: bumping
// the store with the path the activated OpenFileMsg carries — what the root
// model does on every open — is what lifts the row next time.
func TestFileModeFrecencyKeyMatchesEmittedPath(t *testing.T) {
	f := fileMode("a.go", "b.go")
	cx := Context{Root: "/proj"}
	items := f.Results("", cx)

	s := LoadFileFrecency("")
	s.Record(frecency.Key(items[1].Msg.(OpenFileMsg).Path)) // opened b.go
	f.SetFrecency(s)

	if got := titles(f.Results("", cx)); got[0] != "b.go" {
		t.Fatalf("bump via the emitted path had no effect: %v", got)
	}
}

// TestFileModeWithoutFrecencyUnchanged guards the nil-safe path: a finder
// built without a store ranks exactly as before (#1419 order preserved).
func TestFileModeWithoutFrecencyUnchanged(t *testing.T) {
	f := fileMode("ax.go", "bax.go")
	cx := Context{Root: "/proj"}
	u := &Usage{}
	u.Bump(filepath.Join(cx.Root, "bax.go"))
	f.SetUsage(u)

	if got := titles(f.Results("", cx)); got[0] != "bax.go" {
		t.Fatalf("usage tie-break lost without a frecency store: %v", got)
	}
}
