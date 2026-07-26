package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/lang"
	"ike/internal/palette"
)

func init() {
	// A private test language so the per-language command test never depends
	// on the compiled-in language plugins (not imported by this package).
	lang.Register(lang.Language{ID: "sctest", Extensions: []string{"sct"}})
}

func TestScratchCommandsRegistered(t *testing.T) {
	m := newSized()
	for _, id := range []string{"scratch.new", "scratch.new.text", "scratch.new.sctest"} {
		if _, ok := m.reg.Command(id); !ok {
			t.Fatalf("command %s must be registered", id)
		}
	}
}

func TestNewScratchCreatesAndFocusesBuffer(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()

	m = dispatch(t, m, NewScratchMsg{Ext: "sct"})

	ed := m.activeWS().Panes.FocusedInstance().Editor()
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
	if got := filepath.Base(m.activeWS().Panes.FocusedInstance().Editor().Path()); got != "scratch-1.txt" {
		t.Fatalf("default scratch = %q, want scratch-1.txt", got)
	}
}

func TestScratchListOpensLockedPalette(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	if _, ok := m.reg.Command("scratch.list"); !ok {
		t.Fatal("scratch.list must be registered")
	}
	m = dispatch(t, m, ShowScratchFilesMsg{})
	if !m.palette.IsOpen() {
		t.Fatal("scratch.list must open the palette")
	}
}

// TestNewScratchOpensLanguagePicker covers #1223: scratch.new — the command
// behind cmd+shift+n and the File menu — asks for the language instead of
// silently creating a .txt file.
func TestNewScratchOpensLanguagePicker(t *testing.T) {
	t.Setenv("IKE_CONFIG_DIR", t.TempDir())
	m := newSized()
	m = dispatch(t, m, ShowNewScratchMsg{})
	if !m.palette.IsOpen() {
		t.Fatal("scratch.new must open the language picker")
	}

	items := scratchNewMode{}.Results("", palette.Context{})
	if len(items) == 0 || items[0].Title != "Plain Text" {
		t.Fatalf("plain text must head the unfiltered list, got %+v", items)
	}
	if items[0].Msg != (palette.RunCommandMsg{ID: "scratch.new.text"}) {
		t.Fatalf("plain-text row msg = %+v, want the scratch.new.text command", items[0].Msg)
	}
	var found bool
	for _, it := range items {
		if it.Msg == (palette.RunCommandMsg{ID: "scratch.new.sctest"}) {
			found = true
			if it.Detail != ".sct" {
				t.Errorf("detail = %q, want the language extension", it.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("every registered language must be offered, got %+v", items)
	}

	// Filtering matches the language title.
	got := scratchNewMode{}.Results("sctest", palette.Context{})
	if len(got) != 1 || got[0].Title != "Sctest" {
		t.Fatalf("query must filter to the language, got %+v", got)
	}
}

func TestLangTitle(t *testing.T) {
	for id, want := range map[string]string{"go": "GO", "php": "PHP", "python": "Python"} {
		if got := langTitle(id); got != want {
			t.Errorf("langTitle(%q) = %q, want %q", id, got, want)
		}
	}
}
