package vcs

import "testing"

// TestSnapshotEqual (#2693): two snapshots describing the same repository
// state compare equal, every observable difference breaks it, and nil only
// equals nil.
func TestSnapshotEqual(t *testing.T) {
	base := func() *Snapshot {
		s := NewSnapshotFromEntries("/r",
			FileEntry{Path: "a.go", Status: StatusModified, X: '.', Y: 'M'},
			FileEntry{Path: "b.go", Status: StatusUntracked, X: '?', Y: '?'})
		s.Branch, s.Ahead = "main", 1
		s.ignored = map[string]bool{"build/": true}
		return s
	}
	if !base().Equal(base()) {
		t.Fatal("identical snapshots must be equal")
	}
	var nilSnap *Snapshot
	if !nilSnap.Equal(nil) {
		t.Fatal("nil equals nil")
	}
	if nilSnap.Equal(base()) || base().Equal(nil) {
		t.Fatal("nil never equals a real snapshot")
	}
	cases := map[string]func(*Snapshot){
		"root":     func(s *Snapshot) { s.Root = "/other" },
		"branch":   func(s *Snapshot) { s.Branch = "dev" },
		"detached": func(s *Snapshot) { s.Detached = true },
		"ahead":    func(s *Snapshot) { s.Ahead = 2 },
		"behind":   func(s *Snapshot) { s.Behind = 1 },
		"status":   func(s *Snapshot) { s.Files["a.go"] = StatusAdded },
		"file":     func(s *Snapshot) { s.Files["c.go"] = StatusModified },
		"entry":    func(s *Snapshot) { s.Entries[0].Y = 'D' },
		"entries":  func(s *Snapshot) { s.Entries = s.Entries[:1] },
		"code":     func(s *Snapshot) { s.codes["a.go"] = "AM" },
		"ignored":  func(s *Snapshot) { s.ignored["dist/"] = true },
	}
	for name, mutate := range cases {
		s := base()
		mutate(s)
		if s.Equal(base()) || base().Equal(s) {
			t.Errorf("%s: a differing snapshot must not compare equal", name)
		}
	}
	// The lazily filled path caches are not state.
	s := base()
	s.resolvedRoot, s.rootResolved = "/real/r", true
	s.resolvedDirs = map[string]string{"x": "y"}
	if !s.Equal(base()) {
		t.Fatal("path caches must not take part in the comparison")
	}
}
