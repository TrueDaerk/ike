package vcs

import "testing"

// TestRefreshOutsideRepoIsNilSnapshot: a workspace that is not a git
// repository resolves to SnapshotMsg{Snap: nil}, never an error.
func TestRefreshOutsideRepoIsNilSnapshot(t *testing.T) {
	outside := t.TempDir()
	if msg, ok := Refresh(outside)().(SnapshotMsg); !ok || msg.Snap != nil {
		t.Errorf("Refresh outside a repo = %#v", msg)
	}
}
