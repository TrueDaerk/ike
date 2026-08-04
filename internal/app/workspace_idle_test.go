package app

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/workspace"
)

// idleHookPlugin records every EventWorkspaceIdle payload it receives.
type idleHookPlugin struct{ roots *[]string }

func (idleHookPlugin) ID() string { return "idletest" }
func (p idleHookPlugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{Hooks: []plugin.Hook{{
		ID:    "idletest.hook",
		Event: plugin.EventWorkspaceIdle,
		Notify: func(h host.API, payload any) tea.Cmd {
			root, _ := payload.(string)
			*p.roots = append(*p.roots, root)
			return nil
		},
	}}}
}

// setBackgroundLSPTimeout swaps the config for the test and restores it.
func setBackgroundLSPTimeout(t *testing.T, raw string) {
	t.Helper()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	var c config.Config
	if old != nil {
		c = *old
	}
	c.Project.BackgroundLSPTimeout = raw
	config.Set(&c)
}

// TestBackgroundLSPTimeoutParsing pins the config resolution (#1521): empty
// and unparsable select the default, off/false/0 and non-positive durations
// disable, valid durations apply.
func TestBackgroundLSPTimeoutParsing(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", defaultBackgroundLSPTimeout},
		{"nonsense", defaultBackgroundLSPTimeout},
		{"off", 0},
		{"false", 0},
		{"0", 0},
		{"-30s", 0},
		{"90s", 90 * time.Second},
		{"5m", 5 * time.Minute},
	}
	for _, tc := range cases {
		setBackgroundLSPTimeout(t, tc.raw)
		if got := backgroundLSPTimeout(); got != tc.want {
			t.Errorf("timeout(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

// TestHandleWorkspaceIdle guards the expiry re-validation (#1521): only a
// workspace still parked for at least a full timeout fires the idle hooks; a
// resumed workspace and one re-parked since the timer was armed are skipped,
// as is everything when the shutdown is disabled.
func TestHandleWorkspaceIdle(t *testing.T) {
	var fired []string
	reg := registry.New()
	reg.Add(idleHookPlugin{roots: &fired})
	m := NewWith(reg, host.MapConfig{})
	setBackgroundLSPTimeout(t, "1m")

	// Park a workspace and backdate its park past the timeout.
	m.ws.SetActive(workspace.New("/tmp/idle-root", m.activeWS().Panes))
	m.ws.Park()
	m.ws.Peek("/tmp/idle-root").ParkedAt = time.Now().Add(-2 * time.Minute)
	m.ws.SetActive(workspace.New("/tmp/other", nil))

	// Freshly re-parked (ParkedAt young): the stale timer must not fire.
	m.ws.Peek("/tmp/idle-root").ParkedAt = time.Now()
	if _, cmd := m.handleWorkspaceIdle(workspaceIdleMsg{root: "/tmp/idle-root"}); cmd != nil {
		cmd()
	}
	if len(fired) != 0 {
		t.Fatalf("young park fired hooks: %v", fired)
	}

	// Parked past the timeout: hooks fire with the root payload.
	m.ws.Peek("/tmp/idle-root").ParkedAt = time.Now().Add(-2 * time.Minute)
	if _, cmd := m.handleWorkspaceIdle(workspaceIdleMsg{root: "/tmp/idle-root"}); cmd != nil {
		cmd()
	}
	if len(fired) != 1 || fired[0] != "/tmp/idle-root" {
		t.Fatalf("idle hooks = %v, want one firing for /tmp/idle-root", fired)
	}

	// Not parked (resumed): nothing fires.
	fired = nil
	if _, cmd := m.handleWorkspaceIdle(workspaceIdleMsg{root: "/tmp/unknown"}); cmd != nil {
		cmd()
	}
	if len(fired) != 0 {
		t.Fatalf("unparked root fired hooks: %v", fired)
	}

	// Disabled: even an expired park stays untouched.
	setBackgroundLSPTimeout(t, "off")
	if _, cmd := m.handleWorkspaceIdle(workspaceIdleMsg{root: "/tmp/idle-root"}); cmd != nil {
		cmd()
	}
	if len(fired) != 0 {
		t.Fatalf("disabled shutdown fired hooks: %v", fired)
	}
}

// TestArmWorkspaceIdle pins the arming guard: no timer without a root or with
// the shutdown disabled, a timer otherwise.
func TestArmWorkspaceIdle(t *testing.T) {
	setBackgroundLSPTimeout(t, "1m")
	if armWorkspaceIdle("") != nil {
		t.Fatal("rootless park must not arm a timer")
	}
	if armWorkspaceIdle("/tmp/x") == nil {
		t.Fatal("enabled shutdown must arm a timer")
	}
	setBackgroundLSPTimeout(t, "off")
	if armWorkspaceIdle("/tmp/x") != nil {
		t.Fatal("disabled shutdown must not arm a timer")
	}
}

// TestParkStampsParkedAt guards the workspace-side seam (#1521): Park stamps
// the park time, Resume clears it.
func TestParkStampsParkedAt(t *testing.T) {
	mgr := workspace.NewManager(workspace.New("/tmp/a", nil))
	mgr.Park()
	w := mgr.Peek("/tmp/a")
	if w == nil || w.ParkedAt.IsZero() {
		t.Fatal("Park must stamp ParkedAt")
	}
	if r := mgr.Resume("/tmp/a"); r == nil || !r.ParkedAt.IsZero() {
		t.Fatal("Resume must clear ParkedAt")
	}
}
