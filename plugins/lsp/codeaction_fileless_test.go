package lsp

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// TestCodeActionFilelessAnswersWithCommand guards the #2027 freeze at its
// source: alt+enter in a buffer with no file has nothing to ask a server, and
// the bridge answers on the spot — on the Update goroutine. Answering with
// h.Send there blocked bubbletea's event loop against itself and froze the
// whole IDE, so the offer must come back as the tea.Cmd Run already returns.
func TestCodeActionFilelessAnswersWithCommand(t *testing.T) {
	sent := make(chan tea.Msg, 4)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { sent <- m })

	b := &bridge{h: h}
	b.setCur("", 0, 0) // an untitled buffer: no path to request actions for

	cmd := b.codeAction(h)
	if cmd == nil {
		t.Fatal("a fileless buffer must still get an offer — the built-in intentions merge into it")
	}
	msg, ok := cmd().(ilsp.CodeActionsMsg)
	if !ok {
		t.Fatalf("command produced %#v, want a CodeActionsMsg", cmd())
	}
	if msg.Path != "" || !msg.Intentions {
		t.Fatalf("offer = %+v, want the empty path with Intentions set", msg)
	}
	if len(msg.Actions) != 0 {
		t.Fatalf("no server was asked, so no actions may be offered: %+v", msg.Actions)
	}
}
