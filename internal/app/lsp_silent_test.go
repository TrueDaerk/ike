package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/config"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// lsp_silent_test.go covers the known-silent server memory (#3080): a quiet
// warm-up marks its servers silent per project and server id, the next
// switch into the project neither arms the fallback timer nor the notice for
// them (skipped=silent_known), a publish clears the mark, and
// lsp.forgetSilent resets it by hand.

// silentLangs registers two server languages with their own servers and a
// third riding the first one's server.
func silentLangs(t *testing.T) {
	t.Helper()
	registerTestLang(t, lang.Language{ID: "silenta", Extensions: []string{"silenta"}, Server: &lang.ServerSpec{Command: "a-ls"}})
	registerTestLang(t, lang.Language{ID: "silentb", Extensions: []string{"silentb"}, Server: &lang.ServerSpec{Command: "b-ls"}})
	registerTestLang(t, lang.Language{ID: "silentc", Extensions: []string{"silentc"}, ServerLanguage: "silenta"})
}

// silentStore reads the model's project markers from disk.
func silentStore(t *testing.T, m Model) []string {
	t.Helper()
	s, err := ilsp.LoadSilent(ilsp.SilentPath(m.projectRootTag()))
	if err != nil {
		t.Fatal(err)
	}
	return s.IDs(m.projectRootTag())
}

// quietSwitch arms a warm-up for docs the way performSwitch does and lets it
// run into the notice and the quiet fallback; it returns the armed command.
func quietSwitch(t *testing.T, m Model, docs ...serverDoc) (Model, bool) {
	t.Helper()
	known := m.newSwitchLSPWait(time.Now(), docs)
	armed := m.armSwitchLSPWait(known) != nil
	if m.switchLSPWait == nil {
		return m, armed
	}
	return silentTimeout(t, m), armed
}

func TestLSPSilentRememberedAcrossSwitches(t *testing.T) {
	silentLangs(t)
	chdir(t, t.TempDir())
	m := warmupNoticeModel(t, 15000)
	a := serverDoc{lang: "silenta", server: "silenta"}

	m, armed := quietSwitch(t, m, a)
	if !armed {
		t.Fatal("the first switch must arm the warm-up timers")
	}
	if ids := silentStore(t, m); len(ids) != 1 || ids[0] != "silenta" {
		t.Fatalf("a quiet warm-up must mark its server silent, store = %v", ids)
	}

	// A later session (a fresh model reads the file) skips the wait.
	// telemetryModel points IKE_CONFIG_DIR at a fresh directory (its usage
	// log lives there); carry the state store over, as a restart would.
	data, err := os.ReadFile(ilsp.SilentPath(m.projectRootTag()))
	if err != nil {
		t.Fatal(err)
	}
	next := warmupNoticeModel(t, 15000)
	if err := os.WriteFile(ilsp.SilentPath(next.projectRootTag()), data, 0o644); err != nil {
		t.Fatal(err)
	}
	before := countNotices(next)
	next, armed = quietSwitch(t, next, a)
	if armed {
		t.Fatal("a known-silent server must not arm the fallback or notice timer")
	}
	if next.switchLSPWait != nil {
		t.Fatal("the phase must be closed on the spot")
	}
	lsp := lspPhasesOf(t, next)
	if len(lsp) == 0 || lsp[len(lsp)-1].Data["skipped"] != "silent_known" {
		t.Fatalf("lsp phase = %v, want skipped=silent_known", lsp)
	}
	if countNotices(next) != before {
		t.Error("a known-silent server must not raise the notice")
	}
}

// TestLSPSilentPerServer: the marker is per server id — another server of the
// project still warms up and is marked on its own; a language riding a
// known-silent server's id shares its marker.
func TestLSPSilentPerServer(t *testing.T) {
	silentLangs(t)
	chdir(t, t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m, _ = quietSwitch(t, m, serverDoc{lang: "silenta", server: "silenta"})

	known := m.newSwitchLSPWait(time.Now(), []serverDoc{
		{lang: "silentc", server: "silenta"},
		{lang: "silentb", server: "silentb"},
	})
	if known != 1 || m.switchLSPWait.lang != "silentb" || len(m.switchLSPWait.servers) != 1 || m.switchLSPWait.servers[0] != "silentb" {
		t.Fatalf("wait = %+v (known %d), want only silentb waited for", m.switchLSPWait, known)
	}
	if m.armSwitchLSPWait(known) == nil {
		t.Fatal("the other server must still arm the warm-up")
	}
	m = silentTimeout(t, m)
	if ids := silentStore(t, m); strings.Join(ids, ",") != "silenta,silentb" {
		t.Errorf("store = %v, want both servers", ids)
	}

	// Another project knows nothing of them.
	chdir(t, t.TempDir())
	other := warmupNoticeModel(t, 15000)
	if _, armed := quietSwitch(t, other, serverDoc{lang: "silenta", server: "silenta"}); !armed {
		t.Error("the marker must be per project")
	}
}

// TestLSPSilentClearedByPublish: a known-silent server that publishes after
// all loses its marker, so the next switch waits for it again.
func TestLSPSilentClearedByPublish(t *testing.T) {
	silentLangs(t)
	chdir(t, t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m, _ = quietSwitch(t, m, serverDoc{lang: "silenta", server: "silenta"}, serverDoc{lang: "silentb", server: "silentb"})

	// silentc rides silenta's server: its publish clears silenta.
	out, _ := m.Update(ilsp.DiagnosticsBatchMsg{Items: []ilsp.DiagnosticsMsg{{Path: filepath.Join(m.projectRootTag(), "x.silentc")}}})
	m = out.(Model)
	if ids := silentStore(t, m); strings.Join(ids, ",") != "silentb" {
		t.Fatalf("store = %v after silenta's publish, want only silentb", ids)
	}
	out, _ = m.Update(ilsp.DiagnosticsMsg{Path: filepath.Join(m.projectRootTag(), "y.silentb")})
	m = out.(Model)
	if ids := silentStore(t, m); len(ids) != 0 {
		t.Fatalf("store = %v after silentb's publish, want empty", ids)
	}
	if _, armed := quietSwitch(t, m, serverDoc{lang: "silenta", server: "silenta"}); !armed {
		t.Error("a server that published must be waited for again")
	}
}

// TestLSPSilentQuietWithLSPOff: with language servers switched off a quiet
// warm-up is expected, so nothing is remembered.
func TestLSPSilentQuietWithLSPOff(t *testing.T) {
	silentLangs(t)
	chdir(t, t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m.switchLSPWait = &switchLSPWait{start: time.Now(), servers: []string{"silenta"}}
	lspOff(t)
	m = silentTimeout(t, m)
	if ids := silentStore(t, m); len(ids) != 0 {
		t.Errorf("store = %v with lsp.enabled = false, want empty", ids)
	}
}

// TestLSPForgetSilent: lsp.forgetSilent clears every marker of the project
// and says which; with none it says so.
func TestLSPForgetSilent(t *testing.T) {
	silentLangs(t)
	chdir(t, t.TempDir())
	m := warmupNoticeModel(t, 15000)
	m, _ = quietSwitch(t, m, serverDoc{lang: "silenta", server: "silenta"}, serverDoc{lang: "silentb", server: "silentb"})

	out, _ := m.Update(LSPForgetSilentMsg{})
	m = out.(Model)
	if ids := silentStore(t, m); len(ids) != 0 {
		t.Fatalf("store = %v after lsp.forgetSilent, want empty", ids)
	}
	if !historyHas(m, "silent flag reset for silenta, silentb") {
		t.Errorf("history = %v, want the reset notice", m.history)
	}
	out, _ = m.Update(LSPForgetSilentMsg{})
	if !historyHas(out.(Model), "no language server is marked silent") {
		t.Errorf("history = %v, want the nothing-to-forget notice", out.(Model).history)
	}
}

// historyHas reports whether a notification history entry contains s.
func historyHas(m Model, s string) bool {
	for _, e := range m.history {
		if strings.Contains(e.text, s) {
			return true
		}
	}
	return false
}

// lspOff switches language servers off for the rest of the test.
func lspOff(t *testing.T) {
	t.Helper()
	old := config.Get()
	t.Cleanup(func() { config.Set(old) })
	c := *old
	c.LSP.Enabled = false
	config.Set(&c)
}
