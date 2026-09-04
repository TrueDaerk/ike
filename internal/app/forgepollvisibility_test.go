package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/host"
	"ike/internal/pane"
)

// forgepollvisibility_test.go covers the app half of the poll's visibility
// gates (#2488): the terminal's focus reports reaching the poller, the pane
// gate read on the settled pass, and the setting that switches the pause off.

// pendingTick is the deadline the poller currently has armed, so a test can
// deliver it without waiting one out.
func pendingTick(m Model) forge.PollTickMsg {
	p := m.forgePoller()
	return forge.PollTickMsg{Root: p.Root(), Seq: p.Seq()}
}

func TestForgePollBlurPausesAndFocusResumes(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	m.StartForgePoll()
	tick := pendingTick(m)

	out, _ := m.Update(tea.BlurMsg{})
	m = out.(Model)
	if !m.forgePoller().Paused() {
		t.Fatal("tea.BlurMsg must pause the poll")
	}
	// The deadline armed before the blur still lands — its timer kept running
	// — and must not dispatch a fetch.
	out, cmd := m.Update(tick)
	m = out.(Model)
	if cmd != nil {
		t.Fatal("a deadline landing while blurred must not dispatch a fetch")
	}
	if m.forgePoller().InFlight() {
		t.Fatal("no fetch may start while the terminal is blurred")
	}
	if m.armForgePoll() != nil {
		t.Fatal("the settled pass must not re-open the chain while paused")
	}

	out, cmd = m.Update(tea.FocusMsg{})
	m = out.(Model)
	if m.forgePoller().Paused() {
		t.Fatal("tea.FocusMsg must lift the pause")
	}
	if cmd == nil || !m.forgePoller().Armed() {
		t.Fatal("regaining focus must re-arm the poll chain")
	}
}

func TestForgePollPauseSettingOffKeepsPolling(t *testing.T) {
	// forge.poll_pause_on_blur = false is today's behaviour: blur changes
	// nothing at all.
	m := pollApp(t, host.MapConfig{"forge.poll_pause_on_blur": "false"})
	m.StartForgePoll()
	tick := pendingTick(m)

	out, _ := m.Update(tea.BlurMsg{})
	m = out.(Model)
	if m.forgePoller().Paused() {
		t.Fatal("the pause must stay off while the setting is false")
	}
	out, cmd := m.Update(tick)
	m = out.(Model)
	if cmd == nil || !m.forgePoller().InFlight() {
		t.Fatal("with the pause off a blurred terminal must keep fetching")
	}
}

func TestForgePollPauseSettingFromConfig(t *testing.T) {
	if !forgePausePollOnBlur(nil) {
		t.Error("nil config must default to the pause being on")
	}
	if !forgePausePollOnBlur(host.MapConfig{}) {
		t.Error("an unset key must default to the pause being on")
	}
	if forgePausePollOnBlur(host.MapConfig{"forge.poll_pause_on_blur": "false"}) {
		t.Error("false must switch the pause off")
	}
	if !forgePausePollOnBlur(host.MapConfig{"forge.poll_pause_on_blur": "true"}) {
		t.Error("true must switch the pause on")
	}
	// A live reload applies it without a restart.
	m := pollApp(t, host.MapConfig{})
	m.reconfigureForgePoll(host.MapConfig{"forge.poll_pause_on_blur": "false"})
	m.forgePoller().Blur()
	if m.forgePoller().Paused() {
		t.Error("the reload must have switched the pause off")
	}
}

func TestForgePollSlowsDownWhileTheIssuesPaneIsClosed(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	// The settled pass reads the pane gate. No Issues window is open in a
	// freshly sized model, so the cadence stretches.
	if m.armForgePoll() != nil {
		t.Fatal("closing the gate must not return a command — the pass has to settle")
	}
	if m.forgePoller().PaneOpen() {
		t.Fatal("no Issues tool window is open, the gate should say so")
	}
	if got, want := m.forgePoller().Cadence(), forge.SlowPollFactor*forge.DefaultPollInterval; got != want {
		t.Fatalf("cadence = %v, want the slow %v", got, want)
	}

	// Opening the window restores the configured interval and asks the
	// settled pass for the re-arm that supersedes the slow deadline.
	out, _ := m.Update(IssuesToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.IssuesKey) {
		t.Fatal("issues.toggle did not open the tool window")
	}
	// The re-arm rides Update's own settled pass, which already ran.
	if !m.forgePoller().Armed() {
		t.Fatal("opening the pane must re-arm the chain at the fast cadence")
	}
	if got := m.forgePoller().Cadence(); got != forge.DefaultPollInterval {
		t.Fatalf("cadence = %v, want the configured %v", got, forge.DefaultPollInterval)
	}
	// The gate is an edge, not a standing request: the next pass settles.
	if m.armForgePoll() != nil {
		t.Fatal("a further settled pass must not keep re-arming")
	}
}
