package host

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

func TestFromConfigFlattensTypedSchema(t *testing.T) {
	c, _ := config.Load(config.Options{})
	cfg := FromConfig(c)
	if v, ok := cfg.Get("editor.tab_width"); !ok || v != "4" {
		t.Errorf("editor.tab_width = %q (%v), want 4", v, ok)
	}
	if v, ok := cfg.Get("keymap.preset"); !ok || v != "jetbrains" {
		t.Errorf("keymap.preset = %q (%v), want jetbrains", v, ok)
	}
	if _, ok := cfg.Get("does.not.exist"); ok {
		t.Error("unknown key should report missing")
	}
}

func TestFromConfigNilSafe(t *testing.T) {
	if _, ok := FromConfig(nil).Get("anything"); ok {
		t.Error("nil config should report missing keys")
	}
}

func TestOpenFileRequest(t *testing.T) {
	h := New(nil)
	msg := h.OpenFile("foo.go")()
	r, ok := msg.(OpenFileRequest)
	if !ok || r.Path != "foo.go" {
		t.Fatalf("OpenFile did not produce request, got %#v", msg)
	}
	if r.NewPane {
		t.Fatal("OpenFile should default to Replace (NewPane false)")
	}
	// OpenFileIn carries the explicit open-target intent.
	in := h.OpenFileIn("bar.go", true)().(OpenFileRequest)
	if in.Path != "bar.go" || !in.NewPane {
		t.Fatalf("OpenFileIn request wrong: %#v", in)
	}
}

func TestStatusAndConfig(t *testing.T) {
	h := New(MapConfig{"k": "v"})
	if v, ok := h.Config().Get("k"); !ok || v != "v" {
		t.Fatalf("config get failed: %q %v", v, ok)
	}
	h.SetStatus("hi")
	if h.Status() != "hi" {
		t.Fatalf("status not stored: %q", h.Status())
	}
}

func TestNilConfigSafe(t *testing.T) {
	h := New(nil)
	if _, ok := h.Config().Get("missing"); ok {
		t.Fatal("nil config should report missing keys")
	}
}

// TestSendNeverBlocksTheCaller guards the #2027 freeze: bubbletea's Send
// blocks until the event loop receives the message, so a Send issued from
// inside Update (alt+enter in a buffer with no file, where the LSP bridge
// answers on the spot) deadlocked the program against itself. Send must
// return while the program is still busy — here a sender that stays blocked
// until the test releases it stands in for an Update in flight.
func TestSendNeverBlocksTheCaller(t *testing.T) {
	h := New(nil)
	busy, got := make(chan struct{}), make(chan tea.Msg, 3)
	h.SetSender(func(msg tea.Msg) {
		<-busy // the event loop is inside Update and cannot receive yet
		got <- msg
	})

	done := make(chan struct{})
	go func() {
		h.Send("a")
		h.Send("b")
		h.Send("c")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Send blocked on a busy program — the #2027 deadlock is back")
	}

	// Update returns: the queued messages arrive, in Send order.
	close(busy)
	for _, want := range []string{"a", "b", "c"} {
		select {
		case msg := <-got:
			if msg != want {
				t.Fatalf("delivery out of order: got %v, want %v", msg, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("queued message %q never arrived", want)
		}
	}
}

// TestSendWithoutProgramIsNoop: before main wires the program's Send there is
// nothing to deliver to, and Send must not queue or panic.
func TestSendWithoutProgramIsNoop(t *testing.T) {
	h := New(nil)
	h.Send("dropped")
	h.sendMu.Lock()
	defer h.sendMu.Unlock()
	if len(h.outbox) != 0 {
		t.Fatalf("outbox = %+v, want nothing queued without a program", h.outbox)
	}
}

// snapMsg is a Coalescable test message: an idempotent snapshot keyed by key.
type snapMsg struct{ key, val string }

func (s snapMsg) CoalesceKey() string { return s.key }

// parkedHost returns a host whose pump sits blocked inside the sender —
// standing in for an Update loop that cannot receive — with the first message
// ("head") already popped in flight. Messages sent afterwards queue in the
// outbox until release is closed; deliveries arrive on got.
func parkedHost(t *testing.T, buf int) (h *Host, release chan struct{}, got chan tea.Msg) {
	t.Helper()
	h = New(nil)
	release, got = make(chan struct{}), make(chan tea.Msg, buf)
	first := make(chan struct{})
	var once sync.Once
	h.SetSender(func(msg tea.Msg) {
		once.Do(func() { close(first) })
		<-release
		got <- msg
	})
	h.Send("head")
	<-first // the pump holds "head" in flight; the outbox is empty
	return h, release, got
}

// expectMsgs asserts the next deliveries on got, in order.
func expectMsgs(t *testing.T, got chan tea.Msg, want []tea.Msg) {
	t.Helper()
	for _, w := range want {
		select {
		case msg := <-got:
			if msg != w {
				t.Fatalf("got %#v, want %#v", msg, w)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("message %#v never arrived", w)
		}
	}
}

// TestSendCoalescesKeyedSnapshots (#2169): while an earlier Coalescable
// message with the same non-empty key waits in the outbox, a newer one
// replaces it in place — same queue position, latest payload — instead of
// growing the queue. An empty key never coalesces.
func TestSendCoalescesKeyedSnapshots(t *testing.T) {
	h, release, got := parkedHost(t, 16)
	h.Send(snapMsg{key: "a", val: "1"})
	h.Send("plain")
	h.Send(snapMsg{key: "b", val: "1"})
	h.Send(snapMsg{key: "a", val: "2"})
	h.Send(snapMsg{key: "a", val: "3"})
	h.Send(snapMsg{key: "b", val: "2"})
	h.Send(snapMsg{key: "", val: "x"})
	h.Send(snapMsg{key: "", val: "y"})
	close(release)
	expectMsgs(t, got, []tea.Msg{
		"head",
		snapMsg{key: "a", val: "3"},
		"plain",
		snapMsg{key: "b", val: "2"},
		snapMsg{key: "", val: "x"},
		snapMsg{key: "", val: "y"},
	})
	if n := h.SendDrops(); n != 0 {
		t.Fatalf("SendDrops = %d, coalescing must not count as dropping", n)
	}
}

// TestSendOutboxBounded (#2169): once the outbox holds maxOutbox entries, a
// further non-coalescing message is dropped (newest loses), counted, and
// reported once to the diag logger — while a keyed snapshot still replaces
// its queued entry, since a replace never grows the queue.
func TestSendOutboxBounded(t *testing.T) {
	h, release, got := parkedHost(t, maxOutbox+2)
	var logLines []string
	h.SetDiagLog(func(line string) { logLines = append(logLines, line) })

	h.Send(snapMsg{key: "k", val: "old"})
	for i := 0; i < maxOutbox-1; i++ {
		h.Send(i) // fills the outbox to exactly maxOutbox
	}
	const extra = 7
	for i := 0; i < extra; i++ {
		h.Send("overflow")
	}
	if n := h.SendDrops(); n != extra {
		t.Fatalf("SendDrops = %d, want %d", n, extra)
	}
	if len(logLines) != 1 || !strings.Contains(logLines[0], "outbox full") {
		t.Fatalf("want exactly one diag line on the first drop, got %q", logLines)
	}
	// The keyed snapshot coalesces even at the cap: replaced, not dropped.
	h.Send(snapMsg{key: "k", val: "new"})
	if n := h.SendDrops(); n != extra {
		t.Fatalf("SendDrops = %d after keyed replace at cap, want %d", n, extra)
	}

	close(release)
	expectMsgs(t, got, []tea.Msg{"head", snapMsg{key: "k", val: "new"}})
	for i := 0; i < maxOutbox-1; i++ {
		select {
		case msg := <-got:
			if msg != i {
				t.Fatalf("delivery %d out of order: got %#v", i, msg)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("queued message %d never arrived", i)
		}
	}
	select {
	case msg := <-got:
		t.Fatalf("dropped message delivered anyway: %#v", msg)
	case <-time.After(100 * time.Millisecond):
	}
}
