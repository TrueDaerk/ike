package keymap

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func sessionKey(t *testing.T, chord string) Key {
	t.Helper()
	k, err := ParseKey(chord)
	if err != nil {
		t.Fatalf("ParseKey(%q): %v", chord, err)
	}
	return k
}

func TestProbeSessionDirectHit(t *testing.T) {
	s := NewProbeSession([]string{"ctrl+p", "cmd+s"})
	s.HandleKey(sessionKey(t, "ctrl+p"))
	if got := s.Hit("ctrl+p"); got != "ctrl+p" {
		t.Fatalf("Hit(ctrl+p) = %q, want direct hit", got)
	}
	if got := s.Hit("cmd+s"); got != "" {
		t.Fatalf("Hit(cmd+s) = %q, want unseen", got)
	}
	if s.Last() != "ctrl+p" {
		t.Fatalf("Last() = %q", s.Last())
	}
}

func TestProbeSessionShiftCollapse(t *testing.T) {
	s := NewProbeSession([]string{"ctrl+shift+z", "ctrl+z"})
	s.HandleKey(sessionKey(t, "ctrl+z"))
	if got := s.Hit("ctrl+z"); got != "ctrl+z" {
		t.Fatalf("direct target: got %q", got)
	}
	if got := s.Hit("ctrl+shift+z"); got != "ctrl+z" {
		t.Fatalf("collapse evidence: got %q, want ctrl+z", got)
	}
	res := s.Results()
	byChord := map[string]ProbeResult{}
	for _, r := range res {
		byChord[r.Chord] = r
	}
	if r := byChord["ctrl+shift+z"]; r.Delivered || r.Got != "ctrl+z" {
		t.Fatalf("collapsed chord must report missing with got=ctrl+z: %+v", r)
	}
	if r := byChord["ctrl+z"]; !r.Delivered || r.Got != "" {
		t.Fatalf("direct chord must report delivered: %+v", r)
	}
}

func TestProbeSessionSkip(t *testing.T) {
	s := NewProbeSession([]string{"cmd+a", "cmd+b"})
	if got := s.SkipNext(); got != "cmd+a" {
		t.Fatalf("SkipNext = %q, want cmd+a", got)
	}
	if !s.Skipped("cmd+a") {
		t.Fatal("cmd+a must be skipped")
	}
	if got := s.Next(); got != "cmd+b" {
		t.Fatalf("Next = %q, want cmd+b", got)
	}
	// A skipped chord yields no verdict at all; the untried one is missing.
	res := s.Results()
	if len(res) != 1 || res[0].Chord != "cmd+b" || res[0].Delivered {
		t.Fatalf("Results = %+v, want only cmd+b missing", res)
	}
	delivered, collapsed, skipped, pending := s.Counts()
	if delivered != 0 || collapsed != 0 || skipped != 1 || pending != 1 {
		t.Fatalf("Counts = %d %d %d %d", delivered, collapsed, skipped, pending)
	}
}

func TestProbeSessionMouseNavButtons(t *testing.T) {
	s := NewProbeSession(ProbeTargets())
	k, ok := FromMouseButton(tea.MouseBackward)
	if !ok {
		t.Fatal("MouseBackward must convert")
	}
	s.HandleKey(k)
	if got := s.Hit(k.String()); got != k.String() {
		t.Fatalf("mouse nav target not recorded: %q", got)
	}
}
