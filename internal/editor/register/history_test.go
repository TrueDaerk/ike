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
	for i := 0; i < DefaultHistoryCap+5; i++ {
		s.Yank(0, Entry{Text: string(rune('a' + i))})
	}
	h := s.History()
	if len(h) != DefaultHistoryCap {
		t.Fatalf("history must cap at %d, got %d", DefaultHistoryCap, len(h))
	}
	if h[0].Text != string(rune('a'+DefaultHistoryCap+4)) {
		t.Fatalf("newest must survive the cap, got %q", h[0].Text)
	}
}

// TestHistoryDedupeAnywhere covers #2061: re-copying text already in the ring
// moves it to the front instead of adding a second row, so the picker never
// lists the same payload twice.
func TestHistoryDedupeAnywhere(t *testing.T) {
	s := New()
	s.Yank(0, Entry{Text: "one"})
	s.Yank(0, Entry{Text: "two"})
	s.Yank(0, Entry{Text: "one"}) // not the newest, but already known

	h := s.History()
	if len(h) != 2 {
		t.Fatalf("want 2 entries after the repeat, got %d: %v", len(h), h)
	}
	if h[0].Text != "one" || h[1].Text != "two" {
		t.Fatalf("the repeat must move to the front: %v", h)
	}
}

// TestHistoryCapConfigurable covers #2061's editor.clipboard_history_size: the
// ring resizes, shrinking trims oldest-first, and a nonsense size is ignored
// rather than disabling the picker.
func TestHistoryCapConfigurable(t *testing.T) {
	s := New()
	if s.HistoryCap() != DefaultHistoryCap {
		t.Fatalf("default cap = %d, want %d", s.HistoryCap(), DefaultHistoryCap)
	}
	s.SetHistoryCap(3)
	for _, txt := range []string{"a", "b", "c", "d"} {
		s.Yank(0, Entry{Text: txt})
	}
	if h := s.History(); len(h) != 3 || h[0].Text != "d" || h[2].Text != "b" {
		t.Fatalf("cap 3 must keep the newest three, got %v", h)
	}

	s.SetHistoryCap(2) // shrinking drops the oldest immediately
	if h := s.History(); len(h) != 2 || h[0].Text != "d" || h[1].Text != "c" {
		t.Fatalf("shrink must trim oldest-first, got %v", h)
	}

	s.SetHistoryCap(0) // ignored: an empty ring would make the picker useless
	if s.HistoryCap() != 2 {
		t.Fatalf("cap 0 must be ignored, got %d", s.HistoryCap())
	}

	var zero Store // the zero value falls back to the default cap
	if zero.HistoryCap() != DefaultHistoryCap {
		t.Fatalf("zero-value cap = %d, want %d", zero.HistoryCap(), DefaultHistoryCap)
	}
}

// TestPushHistoryHostCopies covers #2061: pane-side copies that never touch a
// register still reach the picker, and they share the collapsing rules.
func TestPushHistoryHostCopies(t *testing.T) {
	s := New()
	s.Yank(0, Entry{Text: "yanked"})
	s.PushHistory(Entry{Text: "copied\n", Linewise: true})
	s.PushHistory(Entry{Text: ""}) // empty: dropped
	s.PushHistory(Entry{Text: "yanked"})

	h := s.History()
	if len(h) != 2 || h[0].Text != "yanked" || h[1].Text != "copied\n" {
		t.Fatalf("host copies must share the ring, got %v", h)
	}
	if !h[1].Linewise {
		t.Fatal("host copy must keep its linewise flag")
	}
	// Registers stay untouched: a pane copy is not a yank.
	if s.Get(0).Text != "yanked" {
		t.Fatalf("PushHistory must not write the unnamed register, got %q", s.Get(0).Text)
	}
}
