package history

import (
	"testing"

	"ike/internal/editor/buffer"
)

// push applies one append-to-line-0 edit and records it, returning the seq.
func push(t *testing.T, h *History, b *buffer.Buffer, text string) int {
	t.Helper()
	rec := NewRecorder(b, buffer.Position{})
	end := rec.Apply(buffer.Insert(buffer.Position{Line: 0, Col: b.RuneLen(0)}, text))
	h.Push(rec.Commit(end))
	return h.CurrentSeq()
}

func TestContentAtReconstructsWithoutMutating(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	s1 := push(t, h, b, "b") // "ab"
	s2 := push(t, h, b, "c") // "abc"

	for _, c := range []struct {
		seq  int
		want string
	}{
		{0, "a"}, {s1, "ab"}, {s2, "abc"},
	} {
		got, ok := h.ContentAt(b, c.seq)
		if !ok || got != c.want {
			t.Errorf("ContentAt(%d) = %q,%v; want %q,true", c.seq, got, ok, c.want)
		}
	}
	if b.String() != "abc" || h.CurrentSeq() != s2 {
		t.Fatalf("ContentAt must not move the buffer/history: %q at seq %d",
			b.String(), h.CurrentSeq())
	}
}

// TestContentAtAcrossBranches walks over the branch point: the abandoned
// sibling must reconstruct even though it is not on the current path.
func TestContentAtAcrossBranches(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	s1 := push(t, h, b, "b") // "ab"
	s2 := push(t, h, b, "c") // "abc"
	h.Undo(b)                // back to "ab"
	s3 := push(t, h, b, "X") // "abX", a sibling of s2

	if got, ok := h.ContentAt(b, s2); !ok || got != "abc" {
		t.Errorf("abandoned branch: ContentAt(%d) = %q,%v; want \"abc\",true", s2, got, ok)
	}
	if got, ok := h.ContentAt(b, s1); !ok || got != "ab" {
		t.Errorf("branch point: ContentAt(%d) = %q,%v; want \"ab\",true", s1, got, ok)
	}
	if got, ok := h.ContentAt(b, s3); !ok || got != "abX" {
		t.Errorf("current: ContentAt(%d) = %q,%v; want \"abX\",true", s3, got, ok)
	}
	if b.String() != "abX" {
		t.Fatalf("buffer moved: %q", b.String())
	}
}

func TestContentAtUnknownState(t *testing.T) {
	b := buffer.FromString("a")
	h := New()
	if _, ok := h.ContentAt(b, 42); ok {
		t.Error("an unknown seq must report false")
	}
	if _, ok := h.ContentAt(nil, 0); ok {
		t.Error("a nil buffer must report false")
	}
}
