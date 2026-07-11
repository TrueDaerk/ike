package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lang"
)

func init() {
	// A private test language so the per-language command test never depends
	// on the compiled-in language plugins (not imported by this package).
	lang.Register(lang.Language{ID: "sctest", Extensions: []string{"sct"}})
}

func TestScratchCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"scratch.new", "scratch.new.sctest"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must be registered", id)
		}
	}
}

func TestNewScratchCreatesAndFocusesBuffer(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()

	m = dispatch(t, m, NewScratchMsg{Ext: "sct"})

	ed := m.panes.FocusedInstance().Editor()
	if !ed.HasFile() {
		t.Fatal("scratch must open as the focused buffer")
	}
	path := ed.Path()
	if filepath.Base(path) != "scratch-1.sct" || !strings.Contains(path, "scratches") {
		t.Fatalf("path = %q, want scratch-1.sct under the store dir", path)
	}
	if ed.Text() != "" || ed.Dirty() {
		t.Fatalf("fresh scratch must be empty and clean (text %q dirty %v)", ed.Text(), ed.Dirty())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("scratch must exist on disk: %v", err)
	}

	// Plain scratch.new defaults to txt and allocates independently.
	m = dispatch(t, m, NewScratchMsg{})
	if got := filepath.Base(m.panes.FocusedInstance().Editor().Path()); got != "scratch-1.txt" {
		t.Fatalf("default scratch = %q, want scratch-1.txt", got)
	}
}

func TestLangTitle(t *testing.T) {
	for id, want := range map[string]string{"go": "GO", "php": "PHP", "python": "Python"} {
		if got := langTitle(id); got != want {
			t.Errorf("langTitle(%q) = %q, want %q", id, got, want)
		}
	}
}
