package keymap

import (
	"os"
	"testing"
)

// TestMain pins the table's target platform to darwin for the whole package
// (#3000). The suite is written against the macOS default keymap: the
// fixtures feed cmd chords and expect the macOS-only rows (darwinRows), so
// on a Linux build host the Cmd→Ctrl fold would turn dozens of them into
// misses that say nothing about the code under test. Tests of the fold
// itself set GOOS to "linux" explicitly and restore it.
func TestMain(m *testing.M) {
	GOOS = "darwin"
	os.Exit(m.Run())
}
