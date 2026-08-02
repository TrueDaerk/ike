package editor

import "testing"

// TestSelectionLines guards #1430: the accessor reports the visual line
// range 1-based and normalized, and reports ok=false outside visual mode.
func TestSelectionLines(t *testing.T) {
	m, _ := loaded(t, "one\ntwo\nthree\nfour\n")

	if _, _, ok := m.SelectionLines(); ok {
		t.Fatal("ok = true in normal mode")
	}

	m = typeKeys(m, "Vjj") // linewise select lines 1-3
	start, end, ok := m.SelectionLines()
	if !ok || start != 1 || end != 3 {
		t.Fatalf("Vjj = %d,%d,%v want 1,3,true", start, end, ok)
	}

	// Selecting upwards normalizes: anchor below cursor.
	m, _ = loaded(t, "one\ntwo\nthree\nfour\n")
	m = typeKeys(m, "jjvk") // start at line 3, extend up to 2
	start, end, ok = m.SelectionLines()
	if !ok || start != 2 || end != 3 {
		t.Fatalf("jjvk = %d,%d,%v want 2,3,true", start, end, ok)
	}
}
