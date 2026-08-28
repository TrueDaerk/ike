package market

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// updateIndex parses a two-entry catalog for the update tests.
func updateIndex(t *testing.T) Index {
	t.Helper()
	idx, diags, err := ParseIndex([]byte(`{"version": 1, "plugins": [
		{"name": "alpha", "version": "1.2.0", "capabilities": ["commands", "notify"],
		 "artifact": {"url": "https://example.com/alpha.wasm", "sha256": "` + hex64 + `"}},
		{"name": "beta", "version": "2.0.0", "capabilities": ["commands"],
		 "artifact": {"url": "https://example.com/beta.wasm", "sha256": "` + hex64 + `"}}
	]}`))
	if err != nil || len(diags) != 0 {
		t.Fatalf("ParseIndex: %v %v", err, diags)
	}
	return idx
}

const hex64 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func v(major, minor, patch int) Version {
	return Version{Major: major, Minor: minor, Patch: patch}
}

// TestFindUpdatesNewerOnly: only installed plugins with an older parsable
// version are offered an update.
func TestFindUpdatesNewerOnly(t *testing.T) {
	idx := updateIndex(t)
	got := FindUpdates(idx, map[string]Installed{
		"alpha": {Name: "alpha", Version: v(1, 0, 0), VersionOK: true, Capabilities: []string{"commands", "notify"}},
		"beta":  {Name: "beta", Version: v(2, 0, 0), VersionOK: true, Capabilities: []string{"commands"}},
	})
	if len(got) != 1 || got[0].Name() != "alpha" {
		t.Fatalf("updates = %+v", got)
	}
	if got[0].From != (v(1, 0, 0)) || got[0].To() != v(1, 2, 0) {
		t.Errorf("versions = %v → %v", got[0].From, got[0].To())
	}
	if got[0].NeedsConfirm() {
		t.Error("unchanged capability list must not need confirmation")
	}
}

// TestFindUpdatesSkipsUnknownAndUninstalled: a plugin that is not installed,
// and one whose manifest version does not parse, are both left alone.
func TestFindUpdatesSkipsUnknownAndUninstalled(t *testing.T) {
	idx := updateIndex(t)
	got := FindUpdates(idx, map[string]Installed{
		"beta": {Name: "beta"}, // hand-installed: no parsable version
	})
	if len(got) != 0 {
		t.Fatalf("updates = %+v", got)
	}
}

// TestFindUpdatesFlagsGrownCapabilities: a newer version asking for a
// capability the installed manifest does not pin needs explicit confirmation.
func TestFindUpdatesFlagsGrownCapabilities(t *testing.T) {
	idx := updateIndex(t)
	got := FindUpdates(idx, map[string]Installed{
		"alpha": {Name: "alpha", Version: v(1, 0, 0), VersionOK: true, Capabilities: []string{"commands"}},
	})
	if len(got) != 1 {
		t.Fatalf("updates = %+v", got)
	}
	if !got[0].NeedsConfirm() {
		t.Fatal("grown capability list must need confirmation")
	}
	if len(got[0].AddedCapabilities) != 1 || got[0].AddedCapabilities[0] != "notify" {
		t.Errorf("added = %v", got[0].AddedCapabilities)
	}
}

// TestAddedCapabilitiesIgnoresDropped: a shorter list can only reduce what the
// runtime allows, so it is not a re-review.
func TestAddedCapabilitiesIgnoresDropped(t *testing.T) {
	idx := updateIndex(t)
	beta := idx.Plugins[1] // capabilities: ["commands"]
	added := AddedCapabilities(beta, Installed{Capabilities: []string{"commands", "notify"}})
	if len(added) != 0 {
		t.Fatalf("added = %v", added)
	}
}

// TestCheckStateDue: the rate limit blocks a second check inside the interval
// and lets one through afterwards.
func TestCheckStateDue(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	if !(CheckState{}).Due(now, DefaultCheckInterval) {
		t.Error("never checked must be due")
	}
	fresh := CheckState{LastCheck: now.Add(-2 * time.Hour)}
	if fresh.Due(now, DefaultCheckInterval) {
		t.Error("checked two hours ago must not be due with a daily interval")
	}
	stale := CheckState{LastCheck: now.Add(-25 * time.Hour)}
	if !stale.Due(now, DefaultCheckInterval) {
		t.Error("checked yesterday must be due")
	}
	// A timestamp in the future (clock moved backwards) must not block forever.
	if !(CheckState{LastCheck: now.Add(time.Hour)}).Due(now, DefaultCheckInterval) {
		t.Error("future timestamp must be treated as due")
	}
}

// TestCheckStateRoundTrip: the timestamp survives save/load, and a missing or
// corrupt file degrades to "never checked" instead of erroring.
func TestCheckStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace-check.json")
	if got := LoadCheckState(path); !got.LastCheck.IsZero() {
		t.Errorf("missing file = %v", got)
	}
	want := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	if err := SaveCheckState(path, CheckState{LastCheck: want}); err != nil {
		t.Fatalf("SaveCheckState: %v", err)
	}
	if got := LoadCheckState(path); !got.LastCheck.Equal(want) {
		t.Errorf("LastCheck = %v, want %v", got.LastCheck, want)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadCheckState(path); !got.LastCheck.IsZero() {
		t.Errorf("corrupt file = %v", got)
	}
}

// TestStatePathRedirects: IKE_CONFIG_DIR relocates the state file like every
// other piece of IKE state.
func TestStatePathRedirects(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	if got, want := StatePath(), filepath.Join(dir, "marketplace-check.json"); got != want {
		t.Errorf("StatePath = %q, want %q", got, want)
	}
}

// TestInstalledReadsCapabilities: the sidecar's capability list comes back
// with the installed state, so the update check can diff it.
func TestInstalledReadsCapabilities(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.wasm"), []byte("\x00asm"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name": "alpha", "version": "1.0.0", "capabilities": ["commands"]}`
	if err := os.WriteFile(filepath.Join(dir, "alpha.manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NewEngine(nil, dir).Installed()
	if err != nil {
		t.Fatalf("Installed: %v", err)
	}
	inst := got["alpha"]
	if !inst.VersionOK || len(inst.Capabilities) != 1 || inst.Capabilities[0] != "commands" {
		t.Fatalf("installed = %+v", inst)
	}
}
