package tour

import (
	"os"
	"testing"

	"ike/internal/keymap"
)

// TestMain pins the keymap's target platform to darwin for the whole package
// (#3000): the curated tour text is checked against the macOS chords it was
// written with. TestLinuxShowsCtrlChords covers the Cmd→Ctrl rendering by
// setting keymap.GOOS to "linux" itself.
func TestMain(m *testing.M) {
	keymap.GOOS = "darwin"
	os.Exit(m.Run())
}
