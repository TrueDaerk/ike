package nav

import "testing"

func loc(path string, line int) Location { return Location{Position: Position{Path: path, Line: line}} }

func TestEditRingRecordDedupesByProximity(t *testing.T) {
	var r EditRing
	r.Record(Location{}) // pathless: ignored
	r.Record(loc("a.go", 10))
	r.Record(loc("a.go", 12)) // within 3 lines: replaces a:10
	r.Record(loc("b.go", 12)) // other file: separate
	r.Record(loc("a.go", 20)) // far away: separate
	got := r.Recent()
	want := []Location{loc("a.go", 20), loc("b.go", 12), loc("a.go", 12)}
	if len(got) != len(want) {
		t.Fatalf("recent = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recent[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
	// Editing near an older entry moves it to the front (newest first).
	r.Record(loc("b.go", 13))
	if got := r.Recent(); got[0] != loc("b.go", 13) || len(got) != 3 {
		t.Fatalf("after re-edit: %+v", got)
	}
}

func TestEditRingCap(t *testing.T) {
	var r EditRing
	for i := 0; i < maxEdits+10; i++ {
		r.Record(loc("a.go", i*10)) // 10 lines apart: no dedupe
	}
	if r.Len() != maxEdits {
		t.Fatalf("len = %d, want %d", r.Len(), maxEdits)
	}
	rec := r.Recent()
	if rec[0].Line != (maxEdits+9)*10 || rec[len(rec)-1].Line != 100 {
		t.Fatalf("cap kept the wrong end: newest %d oldest %d", rec[0].Line, rec[len(rec)-1].Line)
	}
}

func TestEditRingStepWalksBack(t *testing.T) {
	var r EditRing
	r.Record(loc("a.go", 10))
	r.Record(loc("b.go", 20))
	r.Record(loc("c.go", 30))

	// From elsewhere: the newest edit.
	got, ok := r.Step(pos("d.go", 0))
	if !ok || got != loc("c.go", 30) {
		t.Fatalf("step1 = %+v ok=%v", got, ok)
	}
	// Repeating from the landing spot walks back.
	got, ok = r.Step(pos("c.go", 30))
	if !ok || got != loc("b.go", 20) {
		t.Fatalf("step2 = %+v ok=%v", got, ok)
	}
	got, ok = r.Step(pos("b.go", 20))
	if !ok || got != loc("a.go", 10) {
		t.Fatalf("step3 = %+v ok=%v", got, ok)
	}
	if _, ok = r.Step(pos("a.go", 10)); ok {
		t.Fatal("exhausted ring must report false")
	}
	if _, ok = r.Step(pos("a.go", 10)); ok {
		t.Fatal("repeat while exhausted stays exhausted")
	}
	// Moving elsewhere restarts from the newest.
	got, ok = r.Step(pos("z.go", 1))
	if !ok || got != loc("c.go", 30) {
		t.Fatalf("restart = %+v ok=%v", got, ok)
	}
	// A fresh edit resets the walk.
	r.Step(pos("c.go", 30))
	r.Record(loc("e.go", 5))
	got, ok = r.Step(pos("b.go", 20))
	if !ok || got != loc("e.go", 5) {
		t.Fatalf("after edit = %+v ok=%v", got, ok)
	}
}

func TestEditRingStepSkipsCurrentLine(t *testing.T) {
	var r EditRing
	r.Record(loc("a.go", 10))
	r.Record(loc("b.go", 20))
	// Invoked at the newest edit site: the previous edit, not the same line.
	got, ok := r.Step(Position{Path: "b.go", Line: 20, Col: 7})
	if !ok || got != loc("a.go", 10) {
		t.Fatalf("step = %+v ok=%v", got, ok)
	}
	var empty EditRing
	if _, ok := empty.Step(pos("a.go", 0)); ok {
		t.Fatal("empty ring must report false")
	}
}

func TestHistoryRecentOrdersNewestFirst(t *testing.T) {
	var h History
	h.RecordJump(pos("a.go", 1))
	h.RecordJump(pos("b.go", 2))
	h.RecordJump(pos("c.go", 3))
	h.Back(pos("d.go", 4)) // d lands on the forward stack, c is returned
	got := h.Recent()
	want := []Position{pos("b.go", 2), pos("a.go", 1), pos("d.go", 4)}
	if len(got) != len(want) {
		t.Fatalf("recent = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("recent[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
