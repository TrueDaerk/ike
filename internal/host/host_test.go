package host

import "testing"

func TestOpenFileRequest(t *testing.T) {
	h := New(nil)
	msg := h.OpenFile("foo.go")()
	r, ok := msg.(OpenFileRequest)
	if !ok || r.Path != "foo.go" {
		t.Fatalf("OpenFile did not produce request, got %#v", msg)
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
