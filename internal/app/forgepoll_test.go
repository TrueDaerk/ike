package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/forge"
	"ike/internal/host"
	"ike/internal/pane"
	"ike/internal/registry"
)

// forgepoll_test.go covers the app half of background forge polling (#2085):
// the tick chain never waiting on the fetch, the events reaching Update as
// typed messages, the single degrade/recover toast, and the interval setting.

func pollApp(t *testing.T, cfg host.Config) Model {
	t.Helper()
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := NewWith(registry.New(), cfg)
	out, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return out.(Model)
}

func TestForgePollIntervalFromConfig(t *testing.T) {
	if got := forgePollInterval(host.MapConfig{}); got != forge.DefaultPollInterval {
		t.Errorf("unset = %v, want the %v default", got, forge.DefaultPollInterval)
	}
	if got := forgePollInterval(host.MapConfig{"forge.poll_interval_seconds": "45"}); got != 45*time.Second {
		t.Errorf("45 = %v, want 45s", got)
	}
	if got := forgePollInterval(host.MapConfig{"forge.poll_interval_seconds": "0"}); got != 0 {
		t.Errorf("0 = %v, want polling off", got)
	}
	if got := forgePollInterval(nil); got != forge.DefaultPollInterval {
		t.Errorf("nil config = %v, want the default", got)
	}
}

func TestStartForgePollOpensTheChain(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	if m.forgePoller().Armed() {
		t.Fatal("an ordinary Update pass must not arm the chain — it would never settle")
	}
	m.StartForgePoll()
	if !m.forgePoller().Armed() {
		t.Fatal("StartForgePoll should open the poll chain")
	}

	off := pollApp(t, host.MapConfig{"forge.poll_interval_seconds": "0"})
	if off.forgePoller().Enabled() {
		t.Fatal("forge.poll_interval_seconds = 0 must disable polling")
	}
	off.StartForgePoll()
	if off.forgePoller().Armed() {
		t.Fatal("a disabled poller must not schedule anything")
	}
}

// TestInitDoesNotArmTheForgePoll guards the regression that timed the package
// out: Init's commands are drained synchronously by the app test helpers
// (sizedWith calls each cmd() in-line), and draining a poll deadline —
// a tea.Tick — blocks the caller for a full interval. The poll rides the
// StartWatcher lifecycle instead, exactly like the file watcher.
func TestInitDoesNotArmTheForgePoll(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	m.Init()
	if m.forgePoller().Armed() {
		t.Fatal("Init must not arm the poll chain — draining its Cmds would block a whole interval")
	}
}

// TestForgePollUpdateSettlesWithoutAPendingTick guards the regression the
// first cut had: arming from every settled pass makes each Update return a
// tick command, so any synchronous command drainer (the `settle` test helper,
// #1795's data-pane restore) spins forever waiting out poll intervals.
func TestForgePollUpdateSettlesWithoutAPendingTick(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	m.StartForgePoll()
	for i := 0; i < 3; i++ {
		out, cmd := m.Update(tea.WindowSizeMsg{Width: 100 + i, Height: 30})
		m = out.(Model)
		if cmd != nil {
			t.Fatalf("pass %d returned a command; the poll chain must not re-arm per message", i)
		}
	}
}

func TestForgePollReopensAfterTheSettingIsTurnedBackOn(t *testing.T) {
	m := pollApp(t, host.MapConfig{"forge.poll_interval_seconds": "0"})
	if m.armForgePoll() != nil {
		t.Fatal("nothing to reopen while polling is off")
	}
	m.reconfigureForgePoll(host.MapConfig{"forge.poll_interval_seconds": "30"})
	if m.armForgePoll() == nil {
		t.Fatal("re-enabling the setting must reopen the chain on the settled pass")
	}
	if m.armForgePoll() != nil {
		t.Fatal("the reopen is a one-shot edge, not a standing request")
	}
}

// TestForgePollTickDoesNotBlockUpdate is the no-UI-stall check: an
// artificially slow fetch may not hold the Update loop. The tick handler only
// hands back a Cmd, so dispatching one takes microseconds however long the
// forge takes to answer.
func TestForgePollTickDoesNotBlockUpdate(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	root := m.forgeRoot()

	start := time.Now()
	out, cmd := m.Update(forge.PollTickMsg{Root: root})
	took := time.Since(start)
	m = out.(Model)
	if cmd == nil {
		t.Fatal("the tick must dispatch a fetch command")
	}
	if took > 100*time.Millisecond {
		t.Fatalf("the tick handler took %v — it waited on the fetch", took)
	}
	if !m.forgePoller().InFlight() {
		t.Fatal("the poller should record the fetch as in flight")
	}
	// A tick arriving while that fetch is still running is dropped, so a forge
	// slower than the interval cannot pile up subprocesses.
	out, cmd = m.Update(forge.PollTickMsg{Root: root})
	m = out.(Model)
	if cmd != nil {
		t.Fatal("a tick during an in-flight fetch must not dispatch a second one")
	}

	// Stand in for the real fetch with one far slower than any acceptable
	// frame. It resolves in its own goroutine — exactly what the runtime does
	// with the dispatched Cmd — while the loop keeps handling messages.
	slow := make(chan tea.Msg, 1)
	go func() {
		time.Sleep(300 * time.Millisecond)
		slow <- forge.IssuesMsg{Poll: true, Issues: []forge.Issue{{Number: 1, Title: "one"}}}
	}()
	busy := time.Now()
	out, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = out.(Model)
	if elapsed := time.Since(busy); elapsed > 100*time.Millisecond {
		t.Fatalf("an unrelated message took %v while the fetch was running — the UI stalled", elapsed)
	}
	if m.forgePoller().Snapshot() != nil {
		t.Fatal("the snapshot must not exist before the fetch answered")
	}

	out, _ = m.Update(<-slow)
	m = out.(Model)
	if m.forgePoller().InFlight() {
		t.Fatal("the finished fetch should clear the in-flight flag")
	}
	if m.forgePoller().Snapshot() == nil {
		t.Fatal("the slow fetch's result never reached the poller")
	}
}

func TestForgePollTickForAnotherRootIsDropped(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	_, cmd := m.Update(forge.PollTickMsg{Root: "/some/other/project"})
	if cmd != nil {
		t.Fatal("a tick left over from the project switched away from must be dropped")
	}
	if m.forgePoller().InFlight() {
		t.Fatal("a foreign tick must not start a fetch")
	}
}

func TestForgePollSeedsSilentlyThenEmitsEvents(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	seed := forge.IssuesMsg{Poll: true, Issues: []forge.Issue{{Number: 1, Title: "one"}}}
	out, cmd := m.Update(seed)
	m = out.(Model)
	if evs := collectEvents(cmd); len(evs) != 0 {
		t.Fatalf("seeding emitted %d events, want none", len(evs))
	}

	fresh := forge.IssuesMsg{Poll: true, Issues: []forge.Issue{
		{Number: 2, Title: "two", URL: "https://e/2"},
		{Number: 1, Title: "one"},
	}}
	out, cmd = m.Update(fresh)
	m = out.(Model)
	evs := collectEvents(cmd)
	if len(evs) != 1 || evs[0].Kind != forge.IssueOpened || evs[0].Number != 2 {
		t.Fatalf("events = %+v, want one IssueOpened for issue 2", evs)
	}
	// The typed message must be handled by the app, not forwarded to a pane.
	if _, cmd := m.Update(forge.EventsMsg{Root: m.forgeRoot(), Events: evs}); cmd != nil {
		t.Fatal("forge.EventsMsg should be consumed without side effects for now")
	}
}

// collectEvents runs cmd (and one level of batch) and returns the events of
// the first forge.EventsMsg it produces.
func collectEvents(cmd tea.Cmd) []forge.Event {
	for _, msg := range drainForgeCmd(cmd) {
		if ev, ok := msg.(forge.EventsMsg); ok {
			return ev.Events
		}
	}
	return nil
}

// drainForgeCmd resolves cmd into the messages it produces, flattening one
// batch. Every command is run off the test goroutine with a short deadline:
// the batch also carries the chain's re-arm tick, which would otherwise block
// the test for a whole poll interval. The event command resolves instantly.
func drainForgeCmd(cmd tea.Cmd) []tea.Msg {
	msg, ok := runBriefly(cmd)
	if !ok {
		return nil
	}
	batch, isBatch := msg.(tea.BatchMsg)
	if !isBatch {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		if m, ok := runBriefly(c); ok {
			out = append(out, m)
		}
	}
	return out
}

// runBriefly runs cmd with a deadline, reporting false when it did not answer
// in time (a pending tick) or was nil.
func runBriefly(cmd tea.Cmd) (tea.Msg, bool) {
	if cmd == nil {
		return nil, false
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case m := <-done:
		return m, m != nil
	case <-time.After(200 * time.Millisecond):
		return nil, false
	}
}

func TestForgePollDegradeAndRecoverToastOnce(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	out, _ := m.Update(forge.IssuesMsg{Poll: true, Issues: []forge.Issue{{Number: 1, Title: "one"}}})
	m = out.(Model)
	m.toasts = nil

	boom := forge.IssuesMsg{Poll: true, Err: errors.New("network down")}
	out, _ = m.Update(boom)
	m = out.(Model)
	if len(m.toasts) != 1 || !strings.Contains(m.toasts[0].text, "degraded") {
		t.Fatalf("toasts = %+v, want one degrade notice", m.toasts)
	}
	for i := 0; i < 4; i++ {
		out, _ = m.Update(boom)
		m = out.(Model)
	}
	if len(m.toasts) != 1 {
		t.Fatalf("toasts = %d after 5 failures, want the single degrade notice", len(m.toasts))
	}
	if m.forgePoller().Failures() != 5 {
		t.Errorf("failures = %d, want 5", m.forgePoller().Failures())
	}

	out, _ = m.Update(forge.IssuesMsg{Poll: true, Issues: []forge.Issue{{Number: 1, Title: "one"}}})
	m = out.(Model)
	if len(m.toasts) != 2 || !strings.Contains(m.toasts[0].text, "recovered") {
		t.Fatalf("toasts = %+v, want a recovery notice on top", m.toasts)
	}
	if m.forgePoller().Failures() != 0 {
		t.Error("a success must reset the backoff")
	}
	out, _ = m.Update(forge.IssuesMsg{Poll: true, Issues: []forge.Issue{{Number: 1, Title: "one"}}})
	m = out.(Model)
	if len(m.toasts) != 2 {
		t.Fatalf("toasts = %d, want no second recovery notice", len(m.toasts))
	}
}

func TestForgePollStopsOnSetupProblemAndResumesOnRefresh(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	out, _ := m.Update(forge.IssuesMsg{Poll: true, Setup: "gh not found"})
	m = out.(Model)
	if m.forgePoller().Enabled() {
		t.Fatal("an unavailable forge must switch polling off")
	}
	m.StartForgePoll()
	if m.forgePoller().Armed() {
		t.Fatal("a stopped poller must not schedule another deadline")
	}
	if len(m.toasts) != 0 {
		t.Fatalf("toasts = %+v, want the setup state reported in the pane only", m.toasts)
	}
	// A manual refresh that found the forge is the recovery path, and it has
	// to reopen the chain the setup stop closed.
	out, cmd := m.Update(forge.IssuesMsg{Issues: []forge.Issue{{Number: 1, Title: "one"}}})
	m = out.(Model)
	if !m.forgePoller().Enabled() {
		t.Fatal("a successful foreground refresh should restart polling")
	}
	if cmd == nil || !m.forgePoller().Armed() {
		t.Fatal("the resumed chain needs a fresh deadline, or polling never runs again")
	}
}

func TestForgePollFeedsTheIssuesPane(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	out, _ := m.Update(IssuesToggleMsg{})
	m = out.(Model)
	if !m.activeWS().Panes.Has(pane.IssuesKey) {
		t.Fatal("setup: the issues pane should be open")
	}
	out, _ = m.Update(forge.IssuesMsg{Poll: true, Issues: []forge.Issue{
		{Number: 1, Title: "one"}, {Number: 2, Title: "two"},
	}})
	m = out.(Model)
	if got := m.issuesPanel().Visible(); got != 2 {
		t.Fatalf("pane shows %d issues, want the 2 the poll brought", got)
	}
}

func TestReconfigureForgePollAppliesLive(t *testing.T) {
	m := pollApp(t, host.MapConfig{})
	m.reconfigureForgePoll(host.MapConfig{"forge.poll_interval_seconds": "0"})
	if m.forgePoller().Enabled() {
		t.Fatal("setting the interval to 0 must disable polling")
	}
	m.reconfigureForgePoll(host.MapConfig{"forge.poll_interval_seconds": "90"})
	if got := m.forgePoller().Interval(); got != 90*time.Second {
		t.Fatalf("interval = %v, want 90s", got)
	}
}
