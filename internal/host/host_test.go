package host

import (
	"testing"

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
