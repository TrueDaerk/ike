package settings

import (
	"strings"
	"testing"

	ilsp "ike/internal/lsp"
)

// lsp_silent_test.go covers the known-silent flag on the Language Servers
// page (#3080): a flagged server renders "silent" with an explanation, x
// resets it on disk, and x on an unflagged server is rejected with a notice.

func xEnabled(p *LSPPage) bool {
	for _, a := range p.Actions() {
		if a.Key == "x" {
			return a.Enabled != nil && a.Enabled()
		}
	}
	return false
}

func TestLSPPageSilentFlagShowAndReset(t *testing.T) {
	p, opts, _ := lspPageFixture(t)
	path := ilsp.SilentPath(opts.ProjectRoot)
	if xEnabled(p) {
		t.Fatal("x must be disabled while the server is not flagged")
	}
	if _, err := ilsp.UpdateSilent(path, func(s *ilsp.SilentStore) bool {
		return s.Mark(opts.ProjectRoot, "lsptest") && s.Mark(t.TempDir(), "lsptest")
	}); err != nil {
		t.Fatal(err)
	}

	v := p.View(160, 40)
	if !strings.Contains(v, "silent") || !strings.Contains(v, "known silent in this project") {
		t.Fatalf("a flagged server must render silent with the explanation:\n%s", v)
	}
	if !xEnabled(p) {
		t.Fatal("x must be enabled on a flagged server")
	}

	cmd := p.Update(key("x"))
	if cmd == nil {
		t.Fatal("x on a flagged server must return the reset command")
	}
	msg, ok := cmd().(LSPSilentResetMsg)
	if !ok || msg.ID != "lsptest" || msg.Err != nil {
		t.Fatalf("reset msg = %#v", msg)
	}
	p.Receive(msg)
	s, _ := ilsp.LoadSilent(path)
	if s.Has(opts.ProjectRoot, "lsptest") {
		t.Fatal("reset must clear the flag on disk")
	}
	if len(s.Projects) != 1 {
		t.Errorf("reset must keep the other project's flag, store = %v", s.Projects)
	}
	v = p.View(160, 40)
	if !strings.Contains(v, "ready") || !strings.Contains(v, "silent flag of lsptest reset") {
		t.Fatalf("after the reset the row is ready and the footer says so:\n%s", v)
	}

	// Validation: x on an unflagged server writes nothing and says why.
	if cmd := p.Update(key("x")); cmd != nil {
		t.Fatal("x on an unflagged server must not write")
	}
	if v := p.View(160, 40); !strings.Contains(v, "lsptest is not marked silent") {
		t.Fatalf("x on an unflagged server must explain itself:\n%s", v)
	}
	if p.Update(key("j")); p.notice != "" {
		t.Error("the notice must clear on the next key")
	}
}
