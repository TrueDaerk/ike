package editor

import (
	"path/filepath"
	"testing"

	"ike/internal/lang"
)

func init() {
	lang.Register(lang.Language{
		ID:         "edtmpl",
		Extensions: []string{"edtmpl"},
		Template:   "seed ${NAME}\n",
	})
}

// NewFile seeds the buffer with the path's language template (#170) but keeps
// it clean: nothing user-authored is lost by quitting without saving.
func TestNewFileSeedsTemplate(t *testing.T) {
	m := New()
	m.NewFile("/proj/thing.edtmpl")
	if got := m.Text(); got != "seed thing" { // Text() has no trailing newline
		t.Fatalf("NewFile text = %q, want %q", got, "seed thing")
	}
	if m.dirty {
		t.Fatal("template-seeded buffer must not start dirty")
	}
	// No matching language: an empty buffer, as before.
	m.NewFile("/proj/plain.no-such-ext")
	if got := m.Text(); got != "" {
		t.Fatalf("NewFile without template = %q, want empty", got)
	}
}

// :e on a nonexistent path opens it as an unsaved template-seeded buffer
// instead of silently doing nothing (#170).
func TestExEditNewPathSeedsTemplate(t *testing.T) {
	m, _ := loaded(t, "old\n")
	target := filepath.Join(t.TempDir(), "fresh.edtmpl")
	m = runEx(m, "e "+target)
	if m.path != target {
		t.Fatalf("path = %q, want %q", m.path, target)
	}
	if got := m.Text(); got != "seed fresh" {
		t.Fatalf(":e new path text = %q, want %q", got, "seed fresh")
	}
	if m.dirty {
		t.Fatal(":e new path must not start dirty")
	}
}
