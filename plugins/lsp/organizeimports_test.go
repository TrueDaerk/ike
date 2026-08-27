package lsp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// TestSaveChainFormatAmendsOrganizeChange is the one-undo-unit contract
// (#2253): when both steps rewrite the buffer, the format delivery carries
// Amend so the editor merges it into the organize-imports change.
func TestSaveChainFormatAmendsOrganizeChange(t *testing.T) {
	reqs := make(chan string, 8)
	b, path, msgs := chainBridge(t, chainConnector(chainCaps(), reqs, nil))

	cmd := b.saveChainCmd(chainReq(path, true, true))
	if cmd == nil {
		t.Fatal("chain must start")
	}
	_ = cmd()

	m1 := recvMsg(t, msgs, "organize edits").(ilsp.FormatEditsMsg)
	if m1.Amend {
		t.Fatal("the first step opens the undo unit, it must not amend")
	}
	m1.Applied()

	m2 := recvMsg(t, msgs, "format edits").(ilsp.FormatEditsMsg)
	if !m2.Amend {
		t.Fatal("format after an applied organize step must amend its change")
	}
	m2.Applied()
	if _, ok := recvMsg(t, msgs, "chain done").(ilsp.SaveChainDoneMsg); !ok {
		t.Fatal("chain must finish")
	}
}

// TestSaveChainFormatAloneDoesNotAmend: without a preceding organize rewrite
// the format edits are the undo unit's own start — amending would fold them
// into whatever the user did last.
func TestSaveChainFormatAloneDoesNotAmend(t *testing.T) {
	reqs := make(chan string, 8)
	b, path, msgs := chainBridge(t, chainConnector(chainCaps(), reqs, nil))

	cmd := b.saveChainCmd(chainReq(path, false, true))
	if cmd == nil {
		t.Fatal("chain must start")
	}
	_ = cmd()

	m := recvMsg(t, msgs, "format edits").(ilsp.FormatEditsMsg)
	if m.Amend {
		t.Fatal("format-only save must push its own undo unit")
	}
	m.Applied()
}

// TestSaveChainOrganizeTimeoutDoesNotAmend: a dead organize step must leave
// the format step self-contained — there is no change to merge into.
func TestSaveChainOrganizeTimeoutDoesNotAmend(t *testing.T) {
	old := saveChainStepTimeout
	saveChainStepTimeout = 200 * time.Millisecond
	defer func() { saveChainStepTimeout = old }()

	reqs := make(chan string, 8)
	b, path, msgs := chainBridge(t, chainConnector(chainCaps(), reqs, map[string]bool{"textDocument/codeAction": true}))

	cmd := b.saveChainCmd(chainReq(path, true, true))
	if cmd == nil {
		t.Fatal("chain must start")
	}
	_ = cmd()

	m := recvMsg(t, msgs, "format edits").(ilsp.FormatEditsMsg)
	if m.Amend {
		t.Fatal("a timed-out organize step leaves nothing to amend")
	}
	m.Applied()
	if _, ok := recvMsg(t, msgs, "chain done").(ilsp.SaveChainDoneMsg); !ok {
		t.Fatal("chain must finish despite the dead step")
	}
}

// TestOrganizeImportsCommandApplies drives the on-demand palette command
// (lsp.organizeImports): it applies the server's action to the focused buffer
// without any save behind it.
func TestOrganizeImportsCommandApplies(t *testing.T) {
	reqs := make(chan string, 8)
	b, path, msgs := chainBridge(t, chainConnector(chainCaps(), reqs, nil))
	b.mu.Lock()
	b.curPath = path
	b.mu.Unlock()

	if cmd := b.organizeImports(b.h); cmd != nil {
		t.Fatal("the command delivers through the host, it returns no tea.Cmd")
	}
	m, ok := recvMsg(t, msgs, "organize edits").(ilsp.FormatEditsMsg)
	if !ok || len(m.Edits) == 0 || m.Edits[0].Text != "organized " {
		t.Fatalf("command must apply the organize-imports edits, got %#v", m)
	}
	m.Applied()
	if got := <-reqs; got != "textDocument/codeAction" {
		t.Fatalf("server request = %s, want textDocument/codeAction", got)
	}
}

// TestOrganizeImportsCommandUnsupportedNotifies: a server without the kind
// must produce a spoken-out toast instead of a silent no-op, and must not be
// asked for code actions at all.
func TestOrganizeImportsCommandUnsupportedNotifies(t *testing.T) {
	caps := protocol.ServerCapabilities{
		TextDocumentSync:   json.RawMessage(`1`),
		CodeActionProvider: json.RawMessage(`{"codeActionKinds":["quickfix"]}`),
	}
	reqs := make(chan string, 8)
	b, path, _ := chainBridge(t, chainConnector(caps, reqs, nil))
	b.mu.Lock()
	b.curPath = path
	b.mu.Unlock()

	_ = b.organizeImports(b.h)
	hh, ok := b.h.(*host.Host)
	if !ok {
		t.Fatalf("test host type = %T", b.h)
	}
	notes := hh.DrainNotifications()
	if len(notes) != 1 || !strings.Contains(notes[0].Text, "organize imports") {
		t.Fatalf("unsupported organize must notify, got %#v", notes)
	}
	select {
	case m := <-reqs:
		if m == "textDocument/codeAction" {
			t.Fatal("an unsupported server must not be asked for code actions")
		}
	case <-time.After(100 * time.Millisecond):
	}
}
