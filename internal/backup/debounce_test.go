package backup

import (
	"reflect"
	"testing"
	"time"
)

func TestDebounceFiresAfterQuietInterval(t *testing.T) {
	t0 := time.Unix(1000, 0)
	b := NewDebouncer(2 * time.Second)
	b.Mark("a", t0)
	if due := b.Due(t0.Add(time.Second)); len(due) != 0 {
		t.Fatalf("not due before the interval, got %v", due)
	}
	if due := b.Due(t0.Add(2 * time.Second)); !reflect.DeepEqual(due, []string{"a"}) {
		t.Fatalf("due at the interval = %v", due)
	}
	// Once fired, it is cleared until marked again.
	if due := b.Due(t0.Add(10 * time.Second)); len(due) != 0 {
		t.Fatalf("should not fire twice, got %v", due)
	}
}

func TestDebounceReMarkExtendsDeadline(t *testing.T) {
	t0 := time.Unix(0, 0)
	b := NewDebouncer(2 * time.Second)
	b.Mark("a", t0)
	b.Mark("a", t0.Add(time.Second)) // a later edit pushes the deadline out
	if due := b.Due(t0.Add(2 * time.Second)); len(due) != 0 {
		t.Fatalf("re-mark should delay firing, got %v", due)
	}
	if due := b.Due(t0.Add(3 * time.Second)); !reflect.DeepEqual(due, []string{"a"}) {
		t.Fatalf("should fire 2s after the last mark, got %v", due)
	}
}

func TestDebounceCancel(t *testing.T) {
	t0 := time.Unix(0, 0)
	b := NewDebouncer(time.Second)
	b.Mark("a", t0)
	b.Cancel("a")
	if due := b.Due(t0.Add(time.Hour)); len(due) != 0 {
		t.Fatalf("cancelled key must not fire, got %v", due)
	}
	if b.Pending() != 0 {
		t.Fatalf("pending = %d, want 0", b.Pending())
	}
}

func TestDebounceDueSortedMultiple(t *testing.T) {
	t0 := time.Unix(0, 0)
	b := NewDebouncer(time.Second)
	b.Mark("b", t0)
	b.Mark("a", t0)
	b.Mark("c", t0.Add(5 * time.Second)) // not yet due
	due := b.Due(t0.Add(time.Second))
	if !reflect.DeepEqual(due, []string{"a", "b"}) {
		t.Fatalf("due = %v, want [a b] (sorted, c pending)", due)
	}
	if b.Pending() != 1 {
		t.Fatalf("pending = %d, want 1 (c)", b.Pending())
	}
}

func TestDebounceNext(t *testing.T) {
	t0 := time.Unix(0, 0)
	b := NewDebouncer(time.Second)
	if _, ok := b.Next(); ok {
		t.Fatal("empty debouncer should have no next")
	}
	b.Mark("late", t0.Add(10*time.Second))
	b.Mark("soon", t0.Add(time.Second))
	next, ok := b.Next()
	if !ok || !next.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("next = %v (ok=%v), want %v", next, ok, t0.Add(2*time.Second))
	}
}
