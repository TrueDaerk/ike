package project

import (
	"os"
	"testing"

	"ike/internal/gittest"
)

// TestMain: the git fixtures commit into t.TempDir trees, so no commit may
// leave a maintenance daemon behind to race the cleanup (#3018).
func TestMain(m *testing.M) {
	gittest.DisableAutoMaintenance()
	os.Exit(m.Run())
}
