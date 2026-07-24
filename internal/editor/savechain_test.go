package editor

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// chainFlags enables both on-save toggles for an editor under test.
var chainFlags = host.MapConfig{
	"editor.format_on_save":           "true",
	"editor.organize_imports_on_save": "true",
}

// providerCall records one StartSaveChain invocation the fake provider saw.
type providerCall struct {
	path             string
	organize, format bool
}

// withProvider registers a fake save-chain provider for the test and cleans
// it up afterwards; every call is appended to calls.
func withProvider(t *testing.T, calls *[]providerCall) {
	t.Helper()
	ilsp.SetSaveChain(func(path string, organize, format bool) tea.Cmd {
		*calls = append(*calls, providerCall{path: path, organize: organize, format: format})
		return func() tea.Msg { return nil }
	})
	t.Cleanup(func() { ilsp.SetSaveChain(nil) })
}

// dirtyChained loads content into an editor with both flags on, types an "X"
// so the buffer is dirty, and returns it with its path.
func dirtyChained(t *testing.T, content string) (Model, string) {
	t.Helper()
	m, path := loaded(t, content)
	m.Configure(chainFlags)
	m = send(m, key('i'), key('X'), special(tea.KeyEscape))
	return m, path
}

// runMsg resolves a cmd to its message (nil-safe).
func runMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// TestManualSaveDefersBehindChainAndCompletes: with a provider and a flag on,
// "write" parks the save (file untouched, buffer dirty) until
// CompleteChainedSave performs the deferred write.
func TestManualSaveDefersBehindChainAndCompletes(t *testing.T) {
	var calls []providerCall
	withProvider(t, &calls)
	m, path := dirtyChained(t, "one\n")

	tm, _ := m.Update(ActionMsg{Action: "write"})
	m = tm

	if len(calls) != 1 || !calls[0].organize || !calls[0].format || calls[0].path != path {
		t.Fatalf("provider calls = %#v, want one with both steps for %s", calls, path)
	}
	if !m.SavePending() {
		t.Fatal("manual save with a chain must park the write")
	}
	if data, _ := os.ReadFile(path); string(data) != "one\n" {
		t.Fatalf("file must stay untouched while the chain runs, got %q", data)
	}
	if !m.Dirty() {
		t.Fatal("buffer must stay dirty until the deferred write lands")
	}

	if cmd := m.CompleteChainedSave(); cmd != nil {
		t.Fatalf("plain chained save must not produce a follow-up cmd, got %#v", runMsg(cmd))
	}
	if m.SavePending() || m.Dirty() {
		t.Fatal("completing the chain must write and clear pending + dirty")
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("deferred write must persist the buffer, got %q", data)
	}
}

// TestChainSkippedWhenFlagsOff: a registered provider must not be consulted
// when both toggles are off — the write happens immediately.
func TestChainSkippedWhenFlagsOff(t *testing.T) {
	var calls []providerCall
	withProvider(t, &calls)
	m, path := loaded(t, "one\n")
	m = send(m, key('i'), key('X'), special(tea.KeyEscape))

	tm, _ := m.Update(ActionMsg{Action: "write"})
	m = tm

	if len(calls) != 0 {
		t.Fatalf("flags off must not consult the provider, got %#v", calls)
	}
	if m.SavePending() || m.Dirty() {
		t.Fatal("flags off must write immediately")
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("immediate write missing, got %q", data)
	}
}

// TestChainSkippedWithoutProvider: flags on but no capable server (provider
// answers nil / none registered) must fall back to the plain immediate write.
func TestChainSkippedWithoutProvider(t *testing.T) {
	ilsp.SetSaveChain(nil)
	m, path := dirtyChained(t, "one\n")

	tm, _ := m.Update(ActionMsg{Action: "write"})
	m = tm

	if m.SavePending() || m.Dirty() {
		t.Fatal("no provider must mean an immediate plain write")
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("immediate write missing, got %q", data)
	}
}

// TestWriteQuitClosesAfterChain: ":wq" with a chain keeps the pane open until
// the chain completes; the deferred write then yields the CloseMsg.
func TestWriteQuitClosesAfterChain(t *testing.T) {
	var calls []providerCall
	withProvider(t, &calls)
	m, path := dirtyChained(t, "one\n")

	tm, cmd := m.Update(ActionMsg{Action: "write_quit"})
	m = tm
	if !m.SavePending() {
		t.Fatal("write_quit must park the save behind the chain")
	}
	if msg := runMsg(cmd); msg != nil {
		if _, isClose := msg.(CloseMsg); isClose {
			t.Fatal("the pane must not close before the chained write landed")
		}
	}

	cmd = m.CompleteChainedSave()
	if _, ok := runMsg(cmd).(CloseMsg); !ok {
		t.Fatal("completing a write_quit chain must close the pane")
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("write_quit chain must write before closing, got %q", data)
	}
}

// TestSecondSaveCoalescesAndLatchesClose: a save while the chain is pending
// must not start a second chain, and a ":wq" issued meanwhile latches the
// close intent onto the pending save.
func TestSecondSaveCoalescesAndLatchesClose(t *testing.T) {
	var calls []providerCall
	withProvider(t, &calls)
	m, _ := dirtyChained(t, "one\n")

	tm, _ := m.Update(ActionMsg{Action: "write"})
	m = tm
	tm, _ = m.Update(ActionMsg{Action: "write_quit"})
	m = tm

	if len(calls) != 1 {
		t.Fatalf("re-entrant saves must coalesce into one chain, provider saw %d", len(calls))
	}
	cmd := m.CompleteChainedSave()
	if _, ok := runMsg(cmd).(CloseMsg); !ok {
		t.Fatal("a coalesced write_quit must still close after the chained write")
	}
}

// TestWriteRawBypassesChain guards the shutdown/switch flows: "write_raw"
// writes synchronously even with flags on and a provider registered.
func TestWriteRawBypassesChain(t *testing.T) {
	var calls []providerCall
	withProvider(t, &calls)
	m, path := dirtyChained(t, "one\n")

	tm, _ := m.Update(ActionMsg{Action: "write_raw"})
	m = tm

	if len(calls) != 0 || m.SavePending() || m.Dirty() {
		t.Fatal("write_raw must bypass the chain and write immediately")
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("raw write missing, got %q", data)
	}
}

// TestAutosaveBypassesChain: autosave (focus/idle) stays raw by design — no
// chain, immediate write, even with flags on and a provider registered.
func TestAutosaveBypassesChain(t *testing.T) {
	var calls []providerCall
	withProvider(t, &calls)
	m, path := dirtyChained(t, "one\n")

	if !m.Autosave() {
		t.Fatal("Autosave must write the dirty buffer")
	}
	if len(calls) != 0 || m.SavePending() {
		t.Fatal("autosave must never run the save chain")
	}
	if data, _ := os.ReadFile(path); !strings.HasPrefix(string(data), "Xone") {
		t.Fatalf("autosave write missing, got %q", data)
	}
}

// TestSaveAllStyleChainsPerBuffer mirrors editor.saveAll's per-buffer walk:
// each dirty buffer gets its own chained save and its own completion.
func TestSaveAllStyleChainsPerBuffer(t *testing.T) {
	var calls []providerCall
	withProvider(t, &calls)
	m1, path1 := dirtyChained(t, "one\n")
	m2, path2 := dirtyChained(t, "two\n")

	tm, _ := m1.Update(ActionMsg{Action: "write"})
	m1 = tm
	tm, _ = m2.Update(ActionMsg{Action: "write"})
	m2 = tm

	if len(calls) != 2 || calls[0].path != path1 || calls[1].path != path2 {
		t.Fatalf("save-all must chain once per buffer, provider saw %#v", calls)
	}
	m1.CompleteChainedSave()
	m2.CompleteChainedSave()
	for _, p := range []string{path1, path2} {
		if data, _ := os.ReadFile(p); !strings.HasPrefix(string(data), "X") {
			t.Fatalf("buffer %s must be written after its chain, got %q", p, data)
		}
	}
	if m1.Dirty() || m2.Dirty() {
		t.Fatal("both buffers must be clean after their chained writes")
	}
}
