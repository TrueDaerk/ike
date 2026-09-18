package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// lsp_warmup_notice_test.go covers the silent-server notice (#2629): a project
// switch whose language server never publishes says so instead of leaving the
// editor quietly diagnostic-blind until the two-minute telemetry fallback.

// warmupNoticeModel is a telemetry model with a known lsp.warmup_notice_ms.
func warmupNoticeModel(t *testing.T, noticeMs int) Model {
	t.Helper()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	c.LSP.WarmupNoticeMs = noticeMs
	config.Set(&c)
	return telemetryModel(t, host.MapConfig{})
}

// warmupNotice returns the newest history entry about a silent server, or nil.
func warmupNotice(m Model) *histEntry {
	for i := range m.history {
		if strings.Contains(m.history[i].text, "has not responded since the switch") {
			return &m.history[i]
		}
	}
	return nil
}

// TestLSPWarmupNoticeFires is the #2629 criterion: an armed warm-up wait whose
// threshold passes without a publish records exactly one notification naming
// the language, and that notification offers lsp.restart as a follow-up.
func TestLSPWarmupNoticeFires(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m.switchLSPWait = &switchLSPWait{start: time.Now(), lang: "go"}

	out, _ := m.Update(switchLSPNoticeMsg{wait: m.switchLSPWait})
	notified := out.(Model)

	e := warmupNotice(notified)
	if e == nil {
		t.Fatalf("no silent-server notice in the history: %v", notified.history)
	}
	if want := "Language server for go has not responded since the switch"; e.text != want {
		t.Errorf("notice text = %q, want %q", e.text, want)
	}
	if e.sev != host.Warn {
		t.Errorf("notice severity = %v, want warn", e.sev)
	}
	var cmds []string
	for _, a := range e.actions {
		cmds = append(cmds, a.Command)
		if a.Label == "" {
			t.Errorf("action %q has no label", a.Command)
		}
	}
	if len(cmds) == 0 || cmds[0] != "lsp.restart" {
		t.Errorf("notice actions = %v, want lsp.restart first", cmds)
	}
	if !contains(cmds, "lsp.doctor") {
		t.Errorf("notice actions = %v, want the LSP Doctor among them", cmds)
	}
	// The wait stays armed: a late publish is still a measurement (#2492).
	if notified.switchLSPWait == nil {
		t.Error("the notice must not disarm the warm-up wait")
	}
	// A second timer (or a repeated message) must not double-notify.
	out, _ = notified.Update(switchLSPNoticeMsg{wait: notified.switchLSPWait})
	if n := countNotices(out.(Model)); n != 1 {
		t.Errorf("notices recorded = %d, want exactly 1", n)
	}
	// The quiet fallback that eventually closes the wait says the user was
	// told, so the export can separate a silent failure from a reported one.
	out, _ = out.(Model).Update(switchLSPQuietMsg{wait: out.(Model).switchLSPWait})
	lsp := lspPhasesOf(t, out.(Model))
	if len(lsp) != 1 || lsp[0].Data["skipped"] != "quiet" {
		t.Fatalf("want one lsp phase with skipped=quiet, got %v", lsp)
	}
	if lsp[0].Data["notified"] != "true" {
		t.Errorf("quiet phase = %v, want notified=true", lsp[0])
	}
}

// countNotices counts the silent-server entries in the history ring.
func countNotices(m Model) int {
	n := 0
	for _, e := range m.history {
		if strings.Contains(e.text, "has not responded since the switch") {
			n++
		}
	}
	return n
}

// contains is strings.Contains for a slice of ids.
func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestLSPWarmupNoticePublishBeforeThreshold: a publish disarms the wait, so the
// notice timer that fires afterwards finds nothing to report.
func TestLSPWarmupNoticePublishBeforeThreshold(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m.switchLSPWait = &switchLSPWait{start: time.Now(), lang: "go"}
	armed := m.switchLSPWait

	out, _ := m.Update(ilsp.DiagnosticsMsg{Path: "/somewhere/a.go"})
	out, _ = out.(Model).Update(switchLSPNoticeMsg{wait: armed})

	if e := warmupNotice(out.(Model)); e != nil {
		t.Errorf("a warmed-up switch notified anyway: %q", e.text)
	}
}

// TestLSPWarmupNoticeNoServerDocs: a switch that opens no server-language
// document never arms a wait, so it neither schedules nor raises a notice.
func TestLSPWarmupNoticeNoServerDocs(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)

	tm, _ := m.performSwitch(t.TempDir())
	fresh := tm.(Model)
	if fresh.switchLSPWait != nil {
		t.Fatal("a docless switch must not arm the warm-up wait")
	}
	// Even a stray timer from somewhere else finds no wait to report on.
	out, _ := fresh.Update(switchLSPNoticeMsg{wait: &switchLSPWait{lang: "go"}})
	if e := warmupNotice(out.(Model)); e != nil {
		t.Errorf("no_server_docs notified anyway: %q", e.text)
	}
}

// TestLSPWarmupNoticeSuperseded: the next switch replaces the armed wait, so
// the previous switch's pending notice timer is recognised as stale and drops.
func TestLSPWarmupNoticeSuperseded(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m.switchLSPWait = &switchLSPWait{start: time.Now(), lang: "go"}
	stale := m.switchLSPWait

	tm, _ := m.performSwitch(t.TempDir())
	out, _ := tm.(Model).Update(switchLSPNoticeMsg{wait: stale})

	if e := warmupNotice(out.(Model)); e != nil {
		t.Errorf("a superseded switch notified anyway: %q", e.text)
	}
}

// TestLSPWarmupNoticeDisabled: 0 turns the notice off — no timer is scheduled.
func TestLSPWarmupNoticeDisabled(t *testing.T) {
	t.Chdir(t.TempDir())
	warmupNoticeModel(t, 0)
	if cmd := armSwitchLSPNotice(&switchLSPWait{start: time.Now()}); cmd != nil {
		t.Error("lsp.warmup_notice_ms = 0 must schedule no notice timer")
	}
	old := config.Get()
	c := *old
	c.LSP.WarmupNoticeMs = 15000
	config.Set(&c)
	if cmd := armSwitchLSPNotice(&switchLSPWait{start: time.Now()}); cmd == nil {
		t.Error("a positive threshold must schedule a notice timer")
	}
}

// TestLSPWarmupNoticeOffWithoutLSP: with the whole subsystem switched off no
// server is meant to answer, so the notice schedules nothing.
func TestLSPWarmupNoticeOffWithoutLSP(t *testing.T) {
	t.Chdir(t.TempDir())
	warmupNoticeModel(t, 15000)
	old := config.Get()
	c := *old
	c.LSP.Enabled = false
	config.Set(&c)
	if cmd := armSwitchLSPNotice(&switchLSPWait{start: time.Now()}); cmd != nil {
		t.Error("lsp.enabled = false must schedule no notice timer")
	}
}

// TestLSPWarmupNoticeUnnamedLanguage: a wait armed without a language name
// still produces a readable sentence rather than a hole in the text.
func TestLSPWarmupNoticeUnnamedLanguage(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m.switchLSPWait = &switchLSPWait{start: time.Now()}

	out, _ := m.Update(switchLSPNoticeMsg{wait: m.switchLSPWait})
	e := warmupNotice(out.(Model))
	if e == nil || strings.Contains(e.text, "for  ") {
		t.Fatalf("unnamed language notice = %v", e)
	}
}

// TestNotifCenterRunsAction (#2629): the numbered follow-ups of a notification
// are listed in the center and the matching digit runs the command, closing
// the center. A digit past the last action stays the shell's.
func TestNotifCenterRunsAction(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m.switchLSPWait = &switchLSPWait{start: time.Now(), lang: "go"}
	out, _ := m.Update(switchLSPNoticeMsg{wait: m.switchLSPWait})
	notified := out.(Model)

	notified.openNotifCenter()
	view := notified.historyView()
	if !strings.Contains(view, "[1] Restart Language Servers") {
		t.Errorf("history view lists no numbered action:\n%s", view)
	}
	if !strings.Contains(view, "[2] Open LSP Doctor") {
		t.Errorf("history view misses the doctor action:\n%s", view)
	}

	if _, _, handled := notified.updateNotifCenter(tea.KeyPressMsg{Code: '9', Text: "9"}); handled {
		t.Error("a digit past the last action must fall through to the shell")
	}
	ran, _, handled := notified.updateNotifCenter(tea.KeyPressMsg{Code: '1', Text: "1"})
	if !handled {
		t.Fatal("the action digit must be consumed by the center")
	}
	if ran.shell.IsOpen() {
		t.Error("running an action must close the notification center")
	}
	// The dispatch itself, against a command this test registry really has:
	// an unregistered id is the funnel's own failure mode (RunCommandFrom
	// records it), not the center's.
	notified.history[0].actions[0].Command = "tm.fire"
	if _, cmd, _ := notified.updateNotifCenter(tea.KeyPressMsg{Code: '1', Text: "1"}); cmd == nil {
		t.Error("the action digit must dispatch its command")
	}
	// Clear-all still works and takes the actions with it.
	cleared, _, handled := ran.updateNotifCenter(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if !handled || len(cleared.history) != 0 || len(cleared.notifActions()) != 0 {
		t.Errorf("clear-all left %d entries / %d actions", len(cleared.history), len(cleared.notifActions()))
	}
}
