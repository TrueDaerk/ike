package ui

import "testing"

// TestKeyVerdict pins the claim rule (#2889): a key is unanswered until a
// path claims it, so a branch that forgets to claim keeps the chord logged
// rather than hiding a missing keybind.
func TestKeyVerdict(t *testing.T) {
	var v KeyVerdict
	v.BeginKey()
	if v.HandledLastKey() {
		t.Fatal("a fresh key must start unanswered")
	}
	v.HitKey()
	if !v.HandledLastKey() {
		t.Fatal("HitKey must claim the key")
	}
	v.MissKey()
	if v.HandledLastKey() {
		t.Fatal("MissKey must take the claim back")
	}
	if v.KeyAnswered(false) || v.HandledLastKey() {
		t.Fatal("an unhandled block result must leave the key unanswered")
	}
	if !v.KeyAnswered(true) || !v.HandledLastKey() {
		t.Fatal("a handled block result must claim the key")
	}
	if v.KeyAnswered(false); !v.HandledLastKey() {
		t.Fatal("a later unhandled block result must not undo an earlier claim")
	}
	v.BeginKey()
	if v.HandledLastKey() {
		t.Fatal("BeginKey must reset the verdict for the next key")
	}
}
