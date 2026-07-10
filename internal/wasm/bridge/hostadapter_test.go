package bridge

import (
	"encoding/json"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/wasm/abi"
)

func TestHostAdapterUnboundIsSafe(t *testing.T) {
	a := NewHostAdapter()
	// Guest calls arriving before SetAPI must be dropped, never panic.
	a.OpenFile("/tmp/x")
	a.Dispatch(abi.DispatchEnvelope{Type: "open_file"})
	a.Notify(abi.Notification{Text: "hi"})
	a.SetStatus("s")
	if _, ok := a.ConfigGet("k"); ok {
		t.Fatal("unbound adapter must report keys absent")
	}
}

func TestHostAdapterSeverityMapping(t *testing.T) {
	h := host.New(host.MapConfig{})
	a := NewHostAdapter()
	a.SetAPI(h)
	a.Notify(abi.Notification{Severity: 0, Text: "i"})
	a.Notify(abi.Notification{Severity: 1, Text: "w"})
	a.Notify(abi.Notification{Severity: 2, Text: "e"})
	a.Notify(abi.Notification{Severity: 99, Text: "x"}) // unknown → info
	got := h.DrainNotifications()
	want := []host.Severity{host.Info, host.Warn, host.Error, host.Info}
	if len(got) != len(want) {
		t.Fatalf("notifications = %+v", got)
	}
	for i := range want {
		if got[i].Severity != want[i] {
			t.Fatalf("severity %d = %v want %v", i, got[i].Severity, want[i])
		}
	}
}

func TestHostAdapterOpenFileAndDispatch(t *testing.T) {
	h := host.New(host.MapConfig{})
	var sent []tea.Msg
	h.SetSender(func(msg tea.Msg) { sent = append(sent, msg) })
	a := NewHostAdapter()
	a.SetAPI(h)

	a.OpenFile("/tmp/direct.txt")
	a.Dispatch(abi.DispatchEnvelope{Type: "open_file", Payload: json.RawMessage(`{"path":"/tmp/enveloped.txt"}`)})
	if len(sent) != 2 {
		t.Fatalf("sent = %+v", sent)
	}
	if req, ok := sent[0].(host.OpenFileRequest); !ok || req.Path != "/tmp/direct.txt" {
		t.Fatalf("sent[0] = %+v", sent[0])
	}
	if req, ok := sent[1].(host.OpenFileRequest); !ok || req.Path != "/tmp/enveloped.txt" {
		t.Fatalf("sent[1] = %+v", sent[1])
	}

	// Unknown dispatch types warn instead of guessing a message shape.
	a.Dispatch(abi.DispatchEnvelope{Type: "launch_missiles"})
	if len(sent) != 2 {
		t.Fatalf("unknown type must not send: %+v", sent)
	}
	notes := h.DrainNotifications()
	if len(notes) != 1 || notes[0].Severity != host.Warn {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestHostAdapterStatusAndConfig(t *testing.T) {
	h := host.New(host.MapConfig{"editor.tab_width": "4"})
	a := NewHostAdapter()
	a.SetAPI(h)
	a.SetStatus("wasm ready")
	if h.Status() != "wasm ready" {
		t.Fatalf("status = %q", h.Status())
	}
	if v, ok := a.ConfigGet("editor.tab_width"); !ok || v != "4" {
		t.Fatalf("config = %q %v", v, ok)
	}
	if _, ok := a.ConfigGet("nope"); ok {
		t.Fatal("absent key must report false")
	}
}
