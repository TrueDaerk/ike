package app

import (
	"testing"
	"time"

	"ike/internal/host"
)

// agenttrace_reuse_test.go covers #2884: with the agent trace pane open on
// a transcript that does not change, the one-second poll's tick and read
// passes reuse the previous frame (`view/reuse`), while a new revision, a
// changed session info, a new follow label, a liveness flip or a toast
// still compose one.

// idleTrace opens the live trace and composes the first frame.
func idleTrace(t *testing.T) (Model, string) {
	t.Helper()
	m, _, path := liveTrace(t)
	m.traceBusy = false
	m.View()
	return m, path
}

// traceTick is the tick of the model's live poll chain.
func traceTick(m Model) traceTickMsg { return traceTickMsg{gen: m.traceTickGen} }

// traceRead runs one transcript read synchronously, as the tick launches it.
func traceRead(t *testing.T, m Model) traceReadMsg {
	t.Helper()
	m.traceBusy = false
	cmd := m.traceReadCmd()
	if cmd == nil {
		t.Fatal("no read command")
	}
	msg, ok := cmd().(traceReadMsg)
	if !ok {
		t.Fatal("the read did not answer a traceReadMsg")
	}
	return msg
}

func TestAgentTraceIdlePollReusesFrame(t *testing.T) {
	m, _ := idleTrace(t)
	for i := 0; i < 3; i++ {
		var composed bool
		if m, composed = pass(t, m, traceTick(m)); composed {
			t.Fatalf("idle tick %d composed a frame", i)
		}
		if m, composed = pass(t, m, traceRead(t, m)); composed {
			t.Fatalf("idle read %d composed a frame", i)
		}
	}
}

func TestAgentTraceReadWithChangesRenders(t *testing.T) {
	m, path := idleTrace(t)
	cwd := m.toolPane("watcher").Terminal().Cwd()

	appendTraceTurn(t, path, cwd, 2)
	m, composed := pass(t, m, traceRead(t, m))
	if !composed || m.agentTracePanel().Info().Turns != 2 {
		t.Fatalf("a read with a new revision must render (composed=%v turns=%d)", composed, m.agentTracePanel().Info().Turns)
	}

	msg := traceRead(t, m)
	msg.info.Ended = true
	if _, composed = pass(t, m, msg); !composed {
		t.Fatal("a read with changed session info must render")
	}
}

func TestAgentTraceTickWithNewFollowLabelRenders(t *testing.T) {
	m, _ := idleTrace(t)
	m.agentTracePanel().SetFollowing("someone else (focused)")
	m.View()
	if _, composed := pass(t, m, traceTick(m)); !composed {
		t.Fatal("a tick that changed the follow label must render")
	}
}

func TestAgentTraceLivenessFlipRenders(t *testing.T) {
	m, _ := idleTrace(t)
	p := m.agentTracePanel()
	// No read landed for a while: the header flips to stale on the tick.
	p.SetNow(func() time.Time { return time.Now().Add(time.Minute) })
	m, composed := pass(t, m, traceTick(m))
	if !composed {
		t.Fatal("a tick that flipped the header to stale must render")
	}
	// A read back to live flips it again.
	p.SetNow(time.Now)
	if _, composed = pass(t, m, traceRead(t, m)); !composed {
		t.Fatal("a read that flipped the header back to live must render")
	}
}

func TestAgentTraceIdleTickWithToastRenders(t *testing.T) {
	m, _ := idleTrace(t)
	m.host.Notify(host.Info, "something happened")
	if _, composed := pass(t, m, traceTick(m)); !composed {
		t.Fatal("a queued toast must withdraw the tick's reuse verdict")
	}
}
