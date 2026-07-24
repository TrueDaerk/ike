package pane

import "testing"

// TestCachedBoxReusesUntilSigChanges verifies the box cache runs compute only on
// a signature change (#612): an unchanged pane reuses its composed box, a changed
// one recomputes.
func TestCachedBoxReusesUntilSigChanges(t *testing.T) {
	var inst Instance
	calls := 0
	compute := func() string {
		calls++
		return "box-v" + string(rune('0'+calls))
	}

	sig := BoxSig{ContentHash: 1, Title: "t", W: 10, H: 5, Border: [4]uint32{1, 2, 3, 4}}
	a := inst.CachedBox(sig, compute)
	b := inst.CachedBox(sig, compute) // same sig → cached, no recompute
	if calls != 1 || a != b {
		t.Fatalf("expected 1 compute and equal results, got calls=%d a=%q b=%q", calls, a, b)
	}

	// A different content hash (the pane's rendered output changed) recomputes.
	sig2 := sig
	sig2.ContentHash = 2
	c := inst.CachedBox(sig2, compute)
	if calls != 2 || c == a {
		t.Fatalf("content change should recompute: calls=%d c=%q a=%q", calls, c, a)
	}

	// A chrome change (focus border) also recomputes.
	sig3 := sig2
	sig3.Border = [4]uint32{9, 9, 9, 9}
	_ = inst.CachedBox(sig3, compute)
	if calls != 3 {
		t.Fatalf("chrome change should recompute: calls=%d", calls)
	}
}
