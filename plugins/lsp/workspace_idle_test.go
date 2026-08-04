package lsp

import (
	"testing"
	"time"

	"ike/internal/host"
	"ike/internal/lsp/protocol"
)

// TestWorkspaceIdlePrunesRootKeepsGlobalTimers guards #1521: the background
// idle shutdown releases every per-path bridge state under the idle root but
// leaves the global single-shot debounce timers armed — they belong to the
// active workspace, which is a different root by definition.
func TestWorkspaceIdlePrunesRootKeepsGlobalTimers(t *testing.T) {
	b := &bridge{
		diags: map[string][]protocol.Diagnostic{
			"/idle/a.go":   {{Message: "x"}},
			"/active/b.go": {{Message: "y"}},
		},
		pendingChange: map[string]host.EditorEvent{
			"/idle/a.go":   {},
			"/active/b.go": {},
		},
		changeTimer: map[string]*time.Timer{
			"/idle/a.go":   time.NewTimer(time.Hour),
			"/active/b.go": time.NewTimer(time.Hour),
		},
		hlTimer: time.NewTimer(time.Hour),
	}
	defer b.hlTimer.Stop()
	defer b.changeTimer["/active/b.go"].Stop()

	if cmd := b.workspaceIdle("/idle"); cmd != nil {
		t.Fatal("without a manager the close cmd must be nil")
	}
	if _, ok := b.diags["/idle/a.go"]; ok {
		t.Fatal("idle root diags must be pruned")
	}
	if _, ok := b.diags["/active/b.go"]; !ok {
		t.Fatal("other roots' diags must survive")
	}
	if _, ok := b.pendingChange["/idle/a.go"]; ok {
		t.Fatal("idle root pending changes must be pruned")
	}
	if _, ok := b.changeTimer["/idle/a.go"]; ok {
		t.Fatal("idle root change timer must be cancelled")
	}
	if _, ok := b.changeTimer["/active/b.go"]; !ok {
		t.Fatal("other roots' change timers must survive")
	}
	if b.hlTimer == nil {
		t.Fatal("the global highlight debounce must stay armed on idle (unlike workspaceClosed)")
	}
}
