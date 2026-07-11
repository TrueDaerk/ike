package register

import "testing"

func TestHistoryNewestFirstAndDedupe(t *testing.T) {
	s := New()
	s.Yank(0, Entry{Text: "one"})
	s.Delete(0, Entry{Text: "two", Linewise: true})
	s.Yank('a', Entry{Text: "three"}) // named registers record too
	s.Yank(0, Entry{Text: "three"})   // exact repeat of the newest: dropped
	s.Yank(0, Entry{Text: ""})        // empty: dropped

	h := s.History()
	if len(h) != 3 {
		t.Fatalf("want 3 entries, got %d: %v", len(h), h)
	}
	if h[0].Text != "three" || h[1].Text != "two" || h[2].Text != "one" {
		t.Fatalf("order wrong: %v", h)
	}
	if !h[1].Linewise {
		t.Fatal("linewise flag must survive")
	}
	// The returned slice is a copy: mutating it must not corrupt the store.
	h[0].Text = "mutated"
	if s.History()[0].Text != "three" {
		t.Fatal("History must return a copy")
	}
}

func TestHistoryBounded(t *testing.T) {
	s := New()
	for i := 0; i < historyCap+5; i++ {
		s.Yank(0, Entry{Text: string(rune('a' + i))})
	}
	h := s.History()
	if len(h) != historyCap {
		t.Fatalf("history must cap at %d, got %d", historyCap, len(h))
	}
	if h[0].Text != string(rune('a'+historyCap+4)) {
		t.Fatalf("newest must survive the cap, got %q", h[0].Text)
	}
}
