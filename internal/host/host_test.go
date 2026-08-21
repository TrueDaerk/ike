package host

import (
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
