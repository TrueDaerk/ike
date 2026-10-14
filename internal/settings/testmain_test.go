package settings

import (
	"os"
	"testing"

	"ike/internal/keymap"
)

// TestMain pins the keymap's target platform to darwin for the whole package
// (#3000). The keymap page tests feed and expect cmd chords, the way the
// suite is written on macOS; on a Linux build host the Cmd→Ctrl fold would
// capture "ctrl+…" where the test looks for "cmd+…" and miss the macOS-only
// default rows. A test of the fold itself sets keymap.GOOS and restores it.
func TestMain(m *testing.M) {
	keymap.GOOS = "darwin"
	os.Exit(m.Run())
}
