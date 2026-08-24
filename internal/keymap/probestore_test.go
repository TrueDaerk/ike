package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyprobe.json")
	st := LoadProbeStore(path) // missing file → empty store
	if len(st.TerminalIDs()) != 0 {
		t.Fatalf("fresh store not empty: %v", st.TerminalIDs())
	}
	st.Set("ghostty", "2026-08-24T00:00:00Z", []ProbeResult{
		{Chord: "cmd+p", Delivered: true},
		{Chord: "ctrl+tab", Delivered: false},
		{Chord: "ctrl+shift+z", Delivered: false, Got: "ctrl+z"},
	})
	st.Set("tmux", "", []ProbeResult{{Chord: "ctrl+tab", Delivered: false}})
	if err := st.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back := LoadProbeStore(path)
	if got := back.TerminalIDs(); len(got) != 2 || got[0] != "ghostty" || got[1] != "tmux" {
		t.Fatalf("TerminalIDs = %v", got)
	}
	rs := back.Results("ghostty")
	if len(rs) != 3 {
		t.Fatalf("Results(ghostty) = %+v", rs)
	}
	// Stored sorted by chord for a stable file.
	if rs[0].Chord != "cmd+p" || rs[1].Chord != "ctrl+shift+z" || rs[2].Chord != "ctrl+tab" {
		t.Fatalf("results not sorted: %+v", rs)
	}
	if rs[1].Got != "ctrl+z" {
		t.Fatalf("collapse evidence lost: %+v", rs[1])
	}
	back.Clear("ghostty")
	if err := back.Save(path); err != nil {
		t.Fatalf("Save after Clear: %v", err)
	}
	if got := LoadProbeStore(path).TerminalIDs(); len(got) != 1 || got[0] != "tmux" {
		t.Fatalf("after Clear: %v", got)
	}
}

func TestProbeStoreMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyprobe.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := LoadProbeStore(path)
	if len(st.TerminalIDs()) != 0 {
		t.Fatalf("malformed file must read as empty store: %v", st.TerminalIDs())
	}
}

func TestTerminalID(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	// tmux wins over the outer terminal — its chord rewriting is its own
	// reality, whatever emulator hosts it.
	if got := TerminalID(env(map[string]string{"TMUX": "/tmp/tmux-1", "TERM_PROGRAM": "ghostty", "TERM": "xterm"})); got != "tmux" {
		t.Fatalf("inside tmux: %q", got)
	}
	if got := TerminalID(env(map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-ghostty"})); got != "ghostty" {
		t.Fatalf("TERM_PROGRAM: %q", got)
	}
	if got := TerminalID(env(map[string]string{"TERM": "xterm-256color"})); got != "xterm-256color" {
		t.Fatalf("TERM fallback: %q", got)
	}
	if got := TerminalID(env(map[string]string{})); got != "unknown" {
		t.Fatalf("empty env: %q", got)
	}
}

func TestProbeVerdictPrecedenceOverStaticClasses(t *testing.T) {
	t.Cleanup(func() { SetProbeVerdicts(nil) })
	cmdChord := MustParseChord("cmd+p")
	plainChord := MustParseChord("ctrl+p")
	if Classify(cmdChord) != Fragile {
		t.Fatal("precondition: cmd chords are statically fragile")
	}
	SetProbeVerdicts([]ProbeResult{
		{Chord: "cmd+p", Delivered: true},
		{Chord: "ctrl+p", Delivered: false},
		{Chord: "ctrl+tab", Delivered: true},
	})
	// Probed delivered beats the static Fragile class…
	if got := Classify(cmdChord); got != Delivered {
		t.Fatalf("probed-delivered cmd+p classifies %v, want Delivered", got)
	}
	// …probed missing beats the static Delivered class…
	if got := Classify(plainChord); got != Fragile {
		t.Fatalf("probed-missing ctrl+p classifies %v, want Fragile", got)
	}
	// …and beats the hand-pinned reachabilityOverrides too.
	if got := Classify(MustParseChord("ctrl+tab")); got != Delivered {
		t.Fatalf("probed-delivered ctrl+tab classifies %v, want Delivered", got)
	}
	// Multi-step chords consult per-step verdicts.
	if got := Classify(MustParseChord("cmd+k cmd+p")); got != Fragile {
		// cmd+k has no verdict → static Fragile step keeps the chord Fragile.
		t.Fatalf("multi-step with unprobed fragile step classifies %v, want Fragile", got)
	}
	// Clearing restores the static table.
	SetProbeVerdicts(nil)
	if Classify(cmdChord) != Fragile {
		t.Fatal("clearing verdicts must restore the static class")
	}
}

func TestReachabilityNotePrefersProbe(t *testing.T) {
	t.Cleanup(func() { SetProbeVerdicts(nil) })
	SetProbeVerdicts([]ProbeResult{
		{Chord: "cmd+p", Delivered: true},
		{Chord: "ctrl+p", Delivered: false},
		{Chord: "ctrl+shift+z", Delivered: false, Got: "ctrl+z"},
	})
	if note := ReachabilityNote(MustParseChord("cmd+p")); note != "" {
		t.Fatalf("probed-delivered chord must carry no warning, got %q", note)
	}
	if note := ReachabilityNote(MustParseChord("ctrl+p")); !strings.Contains(note, "probed missing") {
		t.Fatalf("probed-missing note = %q", note)
	}
	if note := ReachabilityNote(MustParseChord("ctrl+shift+z")); !strings.Contains(note, "arrives as ctrl+z") {
		t.Fatalf("collapse evidence missing from note: %q", note)
	}
}

func TestChordProbedMissing(t *testing.T) {
	t.Cleanup(func() { SetProbeVerdicts(nil) })
	SetProbeVerdicts([]ProbeResult{
		{Chord: "cmd+p", Delivered: false, Got: "p"},
		{Chord: "ctrl+s", Delivered: true},
	})
	if got, missing := ChordProbedMissing(MustParseChord("cmd+p")); !missing || got != "p" {
		t.Fatalf("cmd+p: got=%q missing=%v", got, missing)
	}
	// A multi-step chord is dead when any step probed missing.
	if _, missing := ChordProbedMissing(MustParseChord("cmd+p ctrl+s")); !missing {
		t.Fatal("multi-step chord with a missing step must flag")
	}
	if _, missing := ChordProbedMissing(MustParseChord("ctrl+s")); missing {
		t.Fatal("probed-delivered chord must not flag")
	}
	if _, missing := ChordProbedMissing(MustParseChord("ctrl+x")); missing {
		t.Fatal("unprobed chord must not flag")
	}
}

// TestDefaultsFragileFollowsProbe pins the consumption path (#2080): the
// binding table's Fragile flags — what the settings list and help overlay
// render — re-derive from installed verdicts at build time.
func TestDefaultsFragileFollowsProbe(t *testing.T) {
	t.Cleanup(func() { SetProbeVerdicts(nil) })
	fragileOf := func(chord string) (bool, bool) {
		for _, b := range Defaults(PresetJetBrains) {
			if b.Chord.String() == chord {
				return b.Fragile, true
			}
		}
		return false, false
	}
	if f, ok := fragileOf("cmd+p"); !ok || !f {
		t.Fatal("precondition: default cmd+p is statically fragile")
	}
	SetProbeVerdicts([]ProbeResult{{Chord: "cmd+p", Delivered: true}})
	if f, _ := fragileOf("cmd+p"); f {
		t.Fatal("probed-delivered default must rebuild non-fragile")
	}
}
