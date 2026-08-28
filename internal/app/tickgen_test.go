package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/perfhud"
)

// tickgen_test.go covers the model generation stamped on every demand-armed
// debounce tick (#2194): a project switch rebuilds the model while a tick
// minted by the departed one may still be sleeping, and the fresh model's
// zeroed armed flag would let that tick arm a second chain on the same clock.

// TestModelGenerationIsUnique: every built model owns a distinct generation,
// so a stale tick can never match a fresh model by accident.
func TestModelGenerationIsUnique(t *testing.T) {
	a := sized(t, 100, 40)
	b := sized(t, 100, 40)
	if a.modelGen == 0 || b.modelGen == 0 {
		t.Fatal("buildModel must mint a non-zero generation")
	}
	if a.modelGen == b.modelGen {
		t.Fatalf("two models share generation %d", a.modelGen)
	}
	if !a.ownsTick(a.modelGen) || a.ownsTick(b.modelGen) {
		t.Fatal("ownsTick must accept only the model's own generation")
	}
}

// TestStaleFollowTickRetires: the follow poll is the tick whose stale chain
// self-sustains — both chains re-arm while a view keeps following. A tick
// from a departed model must be dropped without a poll and without a re-arm.
func TestStaleFollowTickRetires(t *testing.T) {
	m := sized(t, 100, 40)
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := m.openPath(path, false)
	m = out.(Model)
	out, cmd := m.Update(editor.FollowMsg{Path: path, On: true})
	m = out.(Model)
	if cmd == nil || !m.followTickArmed {
		t.Fatal("setup: FollowMsg must arm the follow tick")
	}
	_ = m.routeToEditor(path, editor.ActionMsg{Action: "toggle_follow"})
	if !m.anyFollowing() {
		t.Fatal("setup: the routed toggle must enable follow")
	}

	// A departed model's tick: dropped, and the live chain's armed flag is
	// untouched — arming a second chain here is exactly the #2194 leak.
	m.followTickArmed = true
	out, cmd = m.Update(followTickMsg{gen: m.modelGen - 1})
	m = out.(Model)
	if cmd != nil {
		t.Fatal("a stale follow tick must not poll or re-arm")
	}
	if !m.followTickArmed {
		t.Fatal("a stale follow tick must not clear the live chain's guard")
	}

	// The owned tick still drives the chain.
	out, cmd = m.Update(followTickMsg{gen: m.modelGen})
	m = out.(Model)
	if cmd == nil || !m.followTickArmed {
		t.Fatal("the owned follow tick must poll and re-arm")
	}
}

// TestStaleArmedTicksRetire walks the remaining armed-flag ticks: each drops a
// foreign generation without touching the receiving model's guard.
func TestStaleArmedTicksRetire(t *testing.T) {
	cases := []struct {
		name  string
		msg   func(gen int64) tea.Msg
		arm   func(m *Model)
		armed func(m Model) bool
	}{
		{
			name:  "backup",
			msg:   func(gen int64) tea.Msg { return backupTickMsg{gen: gen} },
			arm:   func(m *Model) { m.backupTickArmed = true },
			armed: func(m Model) bool { return m.backupTickArmed },
		},
		{
			name:  "autosaveIdle",
			msg:   func(gen int64) tea.Msg { return autosaveIdleTickMsg{gen: gen} },
			arm:   func(m *Model) { m.autosaveIdleTickArmed = true },
			armed: func(m Model) bool { return m.autosaveIdleTickArmed },
		},
		{
			name:  "hoverIdle",
			msg:   func(gen int64) tea.Msg { return mouseHoverTickMsg{gen: gen} },
			arm:   func(m *Model) { m.hoverIdleTickArmed = true },
			armed: func(m Model) bool { return m.hoverIdleTickArmed },
		},
		{
			name:  "httpFlight",
			msg:   func(gen int64) tea.Msg { return httpTickMsg{gen: gen} },
			arm:   func(m *Model) { m.httpTickArmed = true },
			armed: func(m Model) bool { return m.httpTickArmed },
		},
		{
			name:  "httpVars",
			msg:   func(gen int64) tea.Msg { return httpVarsTickMsg{gen: gen} },
			arm:   func(m *Model) { m.httpVarsTickArmed = true },
			armed: func(m Model) bool { return m.httpVarsTickArmed },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := sized(t, 100, 40)
			tc.arm(&m)
			out, cmd := m.Update(tc.msg(m.modelGen - 1))
			m = out.(Model)
			if cmd != nil {
				t.Fatal("a stale tick must not schedule work")
			}
			if !tc.armed(m) {
				t.Fatal("a stale tick must not clear the live chain's guard")
			}
		})
	}
}

// TestStalePerfTickRetires: the HUD's sampling tick is generation-stamped too,
// so a departed model's tick neither samples nor re-arms.
func TestStalePerfTickRetires(t *testing.T) {
	m := sized(t, 100, 40)
	if cmd := m.togglePerfHUD(); cmd == nil || !m.perfTickArmed {
		t.Fatal("setup: the HUD must arm its sampling tick")
	}
	t.Cleanup(func() { perfhud.SetEnabled(false) })
	// Drain the "HUD on" toast: its expiry tick would otherwise ride along
	// with the next pass's command and hide what the tick itself returned.
	out, _ := m.Update(struct{}{})
	m = out.(Model)

	out, cmd := m.Update(perfTickMsg{gen: m.modelGen - 1})
	m = out.(Model)
	if cmd != nil || !m.perfTickArmed {
		t.Fatal("a stale perf tick must neither sample nor disarm the live chain")
	}
	out, cmd = m.Update(perfTickMsg{gen: m.modelGen})
	m = out.(Model)
	if cmd == nil || !m.perfTickArmed {
		t.Fatal("the owned perf tick must re-arm while the HUD is open")
	}
}

// TestStaleTermCheckKeepsVerdictQuiet: the audit's other stale-tick symptom —
// a grace tick outliving a project switch judged the fresh model's zero-valued
// caps and popped "keyboard shortcuts will not work" on a Kitty terminal.
func TestStaleTermCheckKeepsVerdictQuiet(t *testing.T) {
	m := sized(t, 100, 40)
	m.caps = termCaps{} // a freshly rebuilt model: nothing probed yet
	out, cmd := m.Update(termCheckMsg{gen: m.modelGen - 1})
	m = out.(Model)
	if cmd != nil {
		t.Fatal("a stale termCheckMsg must not schedule a retry")
	}
	if m.shell.IsOpen() {
		t.Fatal("a stale termCheckMsg must not open the terminal report")
	}
	if m.caps.done {
		t.Fatal("a stale termCheckMsg must not consume the verdict")
	}
}
