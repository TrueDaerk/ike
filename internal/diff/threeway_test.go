package diff

import (
	"strings"
	"testing"
)

func kinds(chunks []Chunk3) []Kind3 {
	var ks []Kind3
	for _, c := range chunks {
		ks = append(ks, c.Kind)
	}
	return ks
}

// TestCompute3Classification guards the chunk classification: ours-only,
// theirs-only, identical-on-both and conflicting changes.
func TestCompute3Classification(t *testing.T) {
	base := "a\nb\nc\nd\ne\n"
	ours := "a\nB1\nc\nd\ne\n"  // ours changes b
	theirs := "a\nb\nc\nD2\ne\n" // theirs changes d
	got := kinds(Compute3(base, ours, theirs))
	want := []Kind3{Chunk3Same, Chunk3Ours, Chunk3Same, Chunk3Theirs, Chunk3Same}
	if len(got) != len(want) {
		t.Fatalf("chunks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunks = %v, want %v", got, want)
		}
	}
}

// TestCompute3Conflict classifies the same region changed differently on
// both sides as a conflict carrying all three segments.
func TestCompute3Conflict(t *testing.T) {
	chunks := Compute3("a\nx\nz\n", "a\nours\nz\n", "a\ntheirs\nz\n")
	var conflict *Chunk3
	for i := range chunks {
		if chunks[i].Kind == Chunk3Conflict {
			conflict = &chunks[i]
		}
	}
	if conflict == nil {
		t.Fatalf("no conflict chunk in %v", kinds(chunks))
	}
	if len(conflict.Base) != 1 || conflict.Base[0] != "x" ||
		len(conflict.Ours) != 1 || conflict.Ours[0] != "ours" ||
		len(conflict.Theirs) != 1 || conflict.Theirs[0] != "theirs" {
		t.Fatalf("conflict segments = %+v", *conflict)
	}
}

// TestCompute3BothSame treats an identical change on both sides as
// auto-resolvable, not a conflict.
func TestCompute3BothSame(t *testing.T) {
	chunks := Compute3("a\nx\nz\n", "a\nsame\nz\n", "a\nsame\nz\n")
	for _, c := range chunks {
		if c.Kind == Chunk3Conflict {
			t.Fatalf("identical both-side change must not conflict: %v", kinds(chunks))
		}
		if c.Kind == Chunk3Both {
			return
		}
	}
	t.Fatalf("expected a Chunk3Both, got %v", kinds(chunks))
}

// TestMerge3AutoResolves merges non-overlapping changes from both sides
// without any markers.
func TestMerge3AutoResolves(t *testing.T) {
	merged, n := Merge3("a\nb\nc\nd\ne\n", "a\nB1\nc\nd\ne\n", "a\nb\nc\nD2\ne\n")
	if n != 0 {
		t.Fatalf("conflicts = %d, want 0", n)
	}
	if merged != "a\nB1\nc\nD2\ne\n" {
		t.Fatalf("merged = %q", merged)
	}
}

// TestMerge3ConflictMarkers emits one diff3 marker block per conflict.
func TestMerge3ConflictMarkers(t *testing.T) {
	merged, n := Merge3("a\nx\nz\n", "a\nours\nz\n", "a\ntheirs\nz\n")
	if n != 1 {
		t.Fatalf("conflicts = %d, want 1", n)
	}
	want := "a\n<<<<<<< ours\nours\n||||||| base\nx\n=======\ntheirs\n>>>>>>> theirs\nz\n"
	if merged != want {
		t.Fatalf("merged =\n%q\nwant\n%q", merged, want)
	}
}

// TestMerge3Insertions merges pure insertions from both sides at different
// stable anchors.
func TestMerge3Insertions(t *testing.T) {
	merged, n := Merge3("a\nb\nc\n", "a\nx\nb\nc\n", "a\nb\ny\nc\n")
	if n != 0 {
		t.Fatalf("conflicts = %d (%q)", n, merged)
	}
	if merged != "a\nx\nb\ny\nc\n" {
		t.Fatalf("merged = %q", merged)
	}
}

// TestMerge3DeletionVsEdit conflicts a deletion against an edit of the same
// region.
func TestMerge3DeletionVsEdit(t *testing.T) {
	merged, n := Merge3("a\nx\nz\n", "a\nz\n", "a\nedited\nz\n")
	if n != 1 {
		t.Fatalf("conflicts = %d (%q)", n, merged)
	}
	if !strings.Contains(merged, "<<<<<<< ours\n||||||| base\nx\n=======\nedited\n>>>>>>>") {
		t.Fatalf("merged = %q", merged)
	}
}

// TestMerge3EmptyBase treats a both-added file (no :1 stage) as a conflict
// when the sides differ and as clean when they agree.
func TestMerge3EmptyBase(t *testing.T) {
	if _, n := Merge3("", "same\n", "same\n"); n != 0 {
		t.Fatalf("identical additions must not conflict, n=%d", n)
	}
	if _, n := Merge3("", "ours\n", "theirs\n"); n != 1 {
		t.Fatalf("differing additions must conflict, n=%d", n)
	}
}
