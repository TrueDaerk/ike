package ui

import "testing"

func TestCopyChord(t *testing.T) {
	for _, key := range []string{"cmd+c", "super+c"} {
		if !CopyChord(key) {
			t.Errorf("CopyChord(%q) = false, want true", key)
		}
	}
	// ctrl+c stays the global quit; bare keys are each panel's own business.
	for _, key := range []string{"ctrl+c", "c", "y", "cmd+v"} {
		if CopyChord(key) {
			t.Errorf("CopyChord(%q) = true, want false", key)
		}
	}
}
