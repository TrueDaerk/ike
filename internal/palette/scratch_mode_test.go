package palette

import "testing"

func TestScratchModeListsNewestFirst(t *testing.T) {
	m := NewScratchMode(func() []string {
		return []string{"/s/scratch-3.py", "/s/scratch-1.txt", "/s/scratch-2.go"}
	})
	items := m.Results("", Context{})
	if len(items) != 3 {
		t.Fatalf("want 3 items, got %d", len(items))
	}
	// Empty query keeps the store's newest-first order; titles are base names.
	for i, want := range []string{"scratch-3.py", "scratch-1.txt", "scratch-2.go"} {
		if items[i].Title != want {
			t.Fatalf("item %d = %q, want %q", i, items[i].Title, want)
		}
	}
	if msg, ok := items[0].Msg.(OpenFileMsg); !ok || msg.Path != "/s/scratch-3.py" {
		t.Fatalf("enter must open the scratch, msg = %#v", items[0].Msg)
	}
}

func TestScratchModeFuzzyFilter(t *testing.T) {
	m := NewScratchMode(func() []string {
		return []string{"/s/scratch-1.py", "/s/scratch-2.go", "/s/notes.txt"}
	})
	items := m.Results("py", Context{})
	if len(items) != 1 || items[0].Title != "scratch-1.py" {
		t.Fatalf("filter 'py' = %v", items)
	}
	// A non-matching query yields no rows (and no inert hint).
	if items := m.Results("zzz", Context{}); len(items) != 0 {
		t.Fatalf("non-matching query must be empty, got %v", items)
	}
}

func TestScratchModeEmptyStoreHint(t *testing.T) {
	m := NewScratchMode(func() []string { return nil })
	items := m.Results("", Context{})
	if len(items) != 1 || items[0].Msg != nil {
		t.Fatalf("empty store must render one inert hint row, got %v", items)
	}
}
