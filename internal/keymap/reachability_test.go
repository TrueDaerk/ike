package keymap

import (
	"strings"
	"testing"
)

func TestClassifyRules(t *testing.T) {
	cases := map[string]Reachability{
		"ctrl+s":       Delivered,
		"f6":           Delivered,
		"shift+f6":     Delivered,
		"ctrl+z":       Delivered,
		"cmd+s":        Fragile,
		"cmd+shift+o":  Fragile,
		"alt+f7":       Fragile,
		"alt+shift+p":  Fragile,
		"ctrl+shift+z": Fragile,
		"ctrl+tab":     Fragile,
		"shift shift":  Undetectable,
		"cmd+k cmd+c":  Fragile, // worst step wins
		"alt+enter":    Fragile,
	}
	for chord, want := range cases {
		if got := Classify(MustParseChord(chord)); got != want {
			t.Errorf("Classify(%q) = %v, want %v", chord, got, want)
		}
	}
}

func TestReachabilityNotes(t *testing.T) {
	for chord, wantSub := range map[string]string{
		"shift shift":  "key-up",
		"ctrl+tab":     "terminal-eaten",
		"cmd+s":        "Kitty",
		"alt+f7":       "option-as-meta",
		"ctrl+shift+z": "collapses",
	} {
		if note := ReachabilityNote(MustParseChord(chord)); !strings.Contains(note, wantSub) {
			t.Errorf("note(%q) = %q, want substring %q", chord, note, wantSub)
		}
	}
	if note := ReachabilityNote(MustParseChord("ctrl+s")); note != "" {
		t.Errorf("delivered chords need no note, got %q", note)
	}
}

func TestReachabilityReportCoversDefaults(t *testing.T) {
	report := ReachabilityReport()
	if len(report) == 0 {
		t.Fatal("report should list the default chords")
	}
	seen := map[string]bool{}
	for _, r := range report {
		if seen[r.Chord] {
			t.Fatalf("duplicate chord %q", r.Chord)
		}
		seen[r.Chord] = true
		if r.Class != Delivered && r.Note == "" {
			t.Errorf("%s is %s but carries no note", r.Chord, r.Class)
		}
	}
	for _, b := range Defaults(PresetJetBrains) {
		if !seen[b.Chord.String()] {
			t.Errorf("default chord %q missing from the report", b.Chord)
		}
	}
}

func TestProbeReportRoundTrip(t *testing.T) {
	lines := []string{
		"ike key probe — UI noise",
		FormatProbeResult(ProbeResult{Chord: "ctrl+s", Delivered: true}),
		FormatProbeResult(ProbeResult{Chord: "ctrl+tab", Delivered: false}),
		FormatProbeResult(ProbeResult{Chord: "ctrl+shift+z", Delivered: false, Got: "ctrl+z"}),
		"trailing noise",
	}
	got, err := ParseProbeReport(strings.NewReader(strings.Join(lines, "\n")))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("results = %+v", got)
	}
	// Sorted by chord: ctrl+s, ctrl+shift+z, ctrl+tab.
	if !got[0].Delivered || got[0].Chord != "ctrl+s" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Chord != "ctrl+shift+z" || got[1].Delivered || got[1].Got != "ctrl+z" {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[2].Chord != "ctrl+tab" || got[2].Delivered {
		t.Errorf("got[2] = %+v", got[2])
	}

	if _, err := ParseProbeReport(strings.NewReader("PROBE\tbroken")); err == nil {
		t.Fatal("malformed line should error")
	}
}

func TestProbeTargetsSkipBareModifiers(t *testing.T) {
	for _, t2 := range ProbeTargets() {
		if bareModifiers[t2] {
			t.Fatalf("bare modifier %q must not be probed", t2)
		}
	}
	// The classic fragile suspects are present.
	want := map[string]bool{"ctrl+tab": false, "cmd+s": false, "alt+f7": false}
	for _, t2 := range ProbeTargets() {
		if _, ok := want[t2]; ok {
			want[t2] = true
		}
	}
	for k, ok := range want {
		if !ok {
			t.Errorf("target %q missing", k)
		}
	}
}
