package app

import (
	"testing"
	"time"

	"ike/internal/config"
)

// lsp_warmup_notice_once_test.go covers the silent-server notice's dedupe and
// opt-out (#2886): it warns once per project per session, and a project the
// user muted never warns at all.

// armSilentWait arms a warm-up wait for the model's current project, the way
// performSwitch does for a switch that opened a server-language document.
func armSilentWait(m Model) Model {
	m.switchLSPWait = &switchLSPWait{start: time.Now(), lang: "go", root: m.projectRootTag()}
	return m
}

// silentTimeout runs the notice and the quiet fallback of the armed wait.
func silentTimeout(t *testing.T, m Model) Model {
	t.Helper()
	w := m.switchLSPWait
	out, _ := m.Update(switchLSPNoticeMsg{wait: w})
	out, _ = out.(Model).Update(switchLSPQuietMsg{wait: w})
	return out.(Model)
}

// TestLSPWarmupNoticeOncePerSession is the #2886 criterion: switching
// A→B→A→B with B's server silent warns exactly once — the set of warned roots
// rides across the switches — and every later quiet phase says the notice was
// held back (notified=false, suppressed=session) rather than omitting it.
func TestLSPWarmupNoticeOncePerSession(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	t.Chdir(a)
	m := warmupNoticeModel(t, 15000)
	for _, root := range []string{b, a, b, a, b} {
		tm, _ := m.performSwitch(root)
		m = tm.(Model)
		if root == b {
			m = silentTimeout(t, armSilentWait(m))
		}
	}
	if n := countNotices(m); n != 1 {
		t.Fatalf("notices = %d after three silent switches into B, want 1", n)
	}
	var quiet []map[string]string
	for _, ev := range lspPhasesOf(t, m) {
		if ev.Data["skipped"] == "quiet" {
			quiet = append(quiet, ev.Data)
		}
	}
	if len(quiet) != 3 {
		t.Fatalf("quiet phases = %v, want 3", quiet)
	}
	if quiet[0]["notified"] != "true" {
		t.Errorf("first quiet phase = %v, want notified=true", quiet[0])
	}
	for _, d := range quiet[1:] {
		if d["notified"] != "false" || d["suppressed"] != "session" {
			t.Errorf("later quiet phase = %v, want notified=false suppressed=session", d)
		}
	}

	// A new ike session starts with a clean slate: B warns once again.
	t.Chdir(b)
	fresh := silentTimeout(t, armSilentWait(warmupNoticeModel(t, 15000)))
	if n := countNotices(fresh); n != 1 {
		t.Errorf("a restarted session must warn again once, got %d notices", n)
	}
}

// TestLSPWarmupNoticeMutedRoot: a root in lsp.warmup_notice_muted_roots never
// warns, and the quiet phase records why (notified=false, suppressed=muted).
func TestLSPWarmupNoticeMutedRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)
	c := *config.Get()
	c.LSP.WarmupNoticeMutedRoots = []string{m.projectRootTag() + "/"}
	config.Set(&c)

	out := silentTimeout(t, armSilentWait(m))
	if n := countNotices(out); n != 0 {
		t.Fatalf("a muted project warned %d times", n)
	}
	lsp := lspPhasesOf(t, out)
	if len(lsp) != 1 || lsp[0].Data["notified"] != "false" || lsp[0].Data["suppressed"] != "muted" {
		t.Errorf("quiet phase = %v, want notified=false suppressed=muted", lsp)
	}
}

// TestLSPWarmupNoticeMuteAction: the notice offers "Don't warn for this
// project", and running it persists the active root into the setting, so the
// next session stays quiet for it.
func TestLSPWarmupNoticeMuteAction(t *testing.T) {
	t.Chdir(t.TempDir())
	m := warmupNoticeModel(t, 15000)
	out := silentTimeout(t, armSilentWait(m))
	e := warmupNotice(out)
	if e == nil {
		t.Fatal("no silent-server notice")
	}
	var offered bool
	for _, a := range e.actions {
		if a.Command == "lsp.muteWarmupNotice" && a.Label == "Don't warn for this project" {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("notice actions = %v, want the per-project opt-out", e.actions)
	}

	tm, cmd := out.Update(LSPMuteWarmupNoticeMsg{})
	if cmd == nil {
		t.Fatal("muting must write the setting")
	}
	// Update wraps the write in its own batch; the write itself is the
	// handler's command.
	reloaded, ok := out.muteWarmupNotice()().(config.ConfigReloadedMsg)
	if !ok {
		t.Fatal("muting must write and reload the config")
	}
	root := tm.(Model).projectRootTag()
	if !reloaded.Config.LSP.WarmupNoticeMuted(root) {
		t.Fatalf("muted roots = %v, want %s", reloaded.Config.LSP.WarmupNoticeMutedRoots, root)
	}
	tm, _ = tm.(Model).Update(reloaded)
	if !config.Get().LSP.WarmupNoticeMuted(root) {
		t.Fatal("the reloaded config must carry the muted root")
	}
	// Muting twice does not duplicate the entry.
	if cmd := tm.(Model).muteWarmupNotice(); cmd != nil {
		t.Error("an already muted root must not be written again")
	}
}
