package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/explorer"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/registry"
	"ike/internal/run"
)

// dbgFakeToolchain simulates an adapter runtime gated on a marker file: the
// runtime is "installed" once the marker exists; the install command creates
// it. The adapter argv is /bin/cat — a process that speaks no DAP but starts.
type dbgFakeToolchain struct{}

// dbgMarker is process-global test state (the language registry is global).
var dbgMarker string

func (dbgFakeToolchain) Detect(string) (map[string]any, bool) { return nil, false }
func (dbgFakeToolchain) RunCommand(_ string, spec lang.RunSpec, _ string) ([]string, bool) {
	return []string{"/bin/echo", spec.File}, true
}
func (dbgFakeToolchain) DebugAdapter(string, string) ([]string, bool) {
	return []string{"/bin/cat"}, true
}
func (dbgFakeToolchain) DebugLaunchArgs(string, lang.RunSpec, string, map[string]string) map[string]any {
	return map[string]any{"request": "launch"}
}
func (dbgFakeToolchain) DebugAdapterMissing(string, string) (bool, string) {
	if _, err := os.Stat(dbgMarker); err != nil {
		return true, "fake runtime not installed"
	}
	return false, ""
}
func (dbgFakeToolchain) DebugAdapterInstall(string, string) [][]string {
	return [][]string{{"/usr/bin/touch", dbgMarker}}
}

func init() {
	lang.Register(lang.Language{ID: "dbgfake", Extensions: []string{"dbgfake"}, Toolchain: dbgFakeToolchain{}})
}

// dbgInstallModel opens a .dbgfake file in a fresh sized model.
func dbgInstallModel(t *testing.T) (Model, run.Config, string) {
	t.Helper()
	if testStoreRoot != "" {
		os.Setenv("IKE_CONFIG_DIR", filepath.Join(testStoreRoot, "dbginstall-"+t.Name()))
	}
	dbgMarker = filepath.Join(t.TempDir(), "runtime-installed")
	path := filepath.Join(t.TempDir(), "prog.dbgfake")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewWith(registry.New(), host.MapConfig{})
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	tm, _ = tm.(Model).Update(explorer.OpenFileMsg{Path: path})
	m = tm.(Model)
	root := projectRoot()
	cfg, _, ok := (&run.Store{}).EnsureFor(root, path)
	if !ok {
		t.Fatal("EnsureFor failed for the fake language")
	}
	return m, *cfg, root
}

// TestDebugPreflightInstallsAndRetries drives the auto-install flow (#589):
// a missing runtime defers the launch to the installer, and the install
// result message retries and starts the session.
func TestDebugPreflightInstallsAndRetries(t *testing.T) {
	m, cfg, root := dbgInstallModel(t)
	tm, _ := m.Update(DebugStartMsg{})
	m = tm.(Model)
	if m.dbg != nil {
		t.Fatal("a missing runtime must not start a session yet")
	}
	// The install goroutine creates the marker.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(dbgMarker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("install command never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The result message retries the launch; the runtime now exists, so the
	// session starts (the /bin/cat adapter speaks no DAP — the handshake
	// times out in the background, which is fine for this test).
	tm, _ = m.Update(debugInstallResultMsg{cfg: cfg, root: root})
	m = tm.(Model)
	if m.dbg == nil {
		t.Fatal("a successful install must relaunch the session")
	}
}

// TestDebugStopCancelsInFlightLaunch drives the cancel path (#636): a
// debug.stop while the launch is still in the install window clears the
// launching guard, and the deferred post-install retry must not start a
// session when its result arrives.
func TestDebugStopCancelsInFlightLaunch(t *testing.T) {
	m, cfg, root := dbgInstallModel(t)
	tm, _ := m.Update(DebugStartMsg{})
	m = tm.(Model)
	if !m.dbgLaunching {
		t.Fatal("a missing runtime must leave the model in the launching window")
	}
	gen := m.dbgLaunchGen
	// Stop during the launching window: dbg is still nil.
	tm, _ = m.Update(DebugStopMsg{})
	m = tm.(Model)
	if m.dbgLaunching {
		t.Fatal("debug.stop while launching must clear dbgLaunching")
	}
	// The install goroutine eventually creates the marker; wait so the retry
	// below would succeed if it were (wrongly) allowed to run.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(dbgMarker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("install command never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The install result carries the cancelled launch's generation: dropped.
	tm, _ = m.Update(debugInstallResultMsg{cfg: cfg, root: root, gen: gen})
	m = tm.(Model)
	if m.dbg != nil {
		t.Fatal("a cancelled launch must not start a session after the install resolves")
	}
	if m.dbgLaunching {
		t.Fatal("a dropped retry must not re-arm the launching guard")
	}
	// A fresh debug.start still works after the cancel (runtime now present).
	tm, _ = m.Update(DebugStartMsg{})
	m = tm.(Model)
	if m.dbg == nil {
		t.Fatal("a new debug.start after a cancelled launch must start a session")
	}
}

// TestDebugPreflightNoInstallLoop: still missing after an install surfaces
// an error instead of reinstalling.
func TestDebugPreflightNoInstallLoop(t *testing.T) {
	m, cfg, root := dbgInstallModel(t)
	// Do NOT create the marker: the retry sees the runtime still missing.
	tm, _ := m.Update(debugInstallResultMsg{cfg: cfg, root: root})
	m = tm.(Model)
	if m.dbg != nil {
		t.Fatal("a still-missing runtime must not start a session")
	}
}

// TestRunAdapterInstallFallsThrough tries candidates in order and keeps the
// failure tail.
func TestRunAdapterInstallFallsThrough(t *testing.T) {
	if err := runAdapterInstall([][]string{{"/usr/bin/false"}, {"/usr/bin/true"}}); err != nil {
		t.Fatalf("a later succeeding candidate must win: %v", err)
	}
	err := runAdapterInstall([][]string{{"/bin/sh", "-c", "echo boom >&2; exit 1"}})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failure must carry the output tail, got %v", err)
	}
	if runAdapterInstall(nil) == nil {
		t.Fatal("no candidates must error")
	}
}
