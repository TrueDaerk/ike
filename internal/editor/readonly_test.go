package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// readOnlyBuffer installs content under a virtual archive-entry path, the
// shape the archive viewer produces (#1762).
func readOnlyBuffer(t *testing.T, name, content string) Model {
	t.Helper()
	m := New()
	m.SetSize(80, 20)
	m.SetFocused(true)
	m.ShowReadOnly(filepath.Join(t.TempDir(), "src.tar")+"!"+name, content)
	return m
}

// TestShowReadOnlyInstallsContent: the buffer holds the text, is clean, knows
// its virtual path and reports itself read-only.
func TestShowReadOnlyInstallsContent(t *testing.T) {
	m := readOnlyBuffer(t, "cmd/main.go", "package main\n\nfunc main() {}\n")
	if !m.ReadOnly() {
		t.Fatal("ShowReadOnly must lock the buffer")
	}
	if got := m.Text(); got != "package main\n\nfunc main() {}" {
		t.Fatalf("text = %q", got)
	}
	if m.Dirty() {
		t.Fatal("a freshly shown preview is not dirty")
	}
	if !m.HasFile() || !strings.HasSuffix(m.Path(), "main.go") {
		t.Fatalf("path = %q", m.Path())
	}
	// The path ends in the entry's own file name, which is what every
	// path-keyed consumer (language lookup, highlighting, tab title) reads.
	// The registry-backed language assertion lives in the app tests, where
	// the language plugins are compiled in.
}

// TestReadOnlyRefusesEdits: every edit class leaves the buffer untouched and
// says so on the ex line, with no dependency-guard prompt in sight.
func TestReadOnlyRefusesEdits(t *testing.T) {
	for _, k := range []rune{'x', 'i', 'o', 'p', 'J', '~', 'D', 'a', 'O', 's'} {
		m := readOnlyBuffer(t, "a.txt", "abc\ndef\n")
		before := m.Text()
		m = send(m, keys("yy")...) // prime the register so p has something to put
		m, _ = m.Update(key(k))
		if !strings.Contains(m.cmdMsg, "read-only") {
			t.Errorf("%q left no refusal on the ex line: %q", k, m.cmdMsg)
		}
		// Whatever mode the key may have reached, typing lands nothing.
		m = send(m, keys("zz")...)
		if m.Text() != before {
			t.Errorf("%q mutated a read-only buffer: %q", k, m.Text())
		}
		if m.Dirty() {
			t.Errorf("%q dirtied a read-only buffer", k)
		}
		if m.ModeName() != Normal {
			t.Errorf("%q left a read-only buffer in mode %v", k, m.ModeName())
		}
	}
}

// TestReadOnlyRefusesWrite: :w reports the refusal and creates nothing on
// disk — the virtual path must never become a file.
func TestReadOnlyRefusesWrite(t *testing.T) {
	dir := t.TempDir()
	vpath := filepath.Join(dir, "src.tar") + "!notes.txt"
	m := New()
	m.SetSize(80, 20)
	m.ShowReadOnly(vpath, "hello\n")
	if err := m.save(); err == nil {
		t.Fatal("save on a read-only buffer must fail")
	}
	if _, err := os.Stat(vpath); err == nil {
		t.Fatalf("the write created %q", vpath)
	}
	// The ex command surfaces it rather than silently doing nothing.
	m, _ = m.Update(key(':'))
	m = send(m, keys("w")...)
	m, _ = m.Update(special(tea.KeyEnter))
	if !strings.Contains(m.cmdMsg, "read-only") {
		t.Fatalf("ex line = %q", m.cmdMsg)
	}
	if _, err := os.Stat(vpath); err == nil {
		t.Fatalf(":w created %q", vpath)
	}
}

// TestReadOnlyWriteToOtherPathRefused: ":w elsewhere" is a write too — the
// content is a preview, not a document to fork off.
func TestReadOnlyWriteToOtherPathRefused(t *testing.T) {
	dir := t.TempDir()
	m := readOnlyBuffer(t, "a.txt", "hello\n")
	out := filepath.Join(dir, "copy.txt")
	if err := m.saveAs(out); err == nil {
		t.Fatal("saveAs on a read-only buffer must fail")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("saveAs wrote the file anyway")
	}
}

// TestLoadClearsReadOnly: reusing the view for a real file unlocks it.
func TestLoadClearsReadOnly(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := readOnlyBuffer(t, "a.txt", "hello\n")
	if err := m.Load(real); err != nil {
		t.Fatal(err)
	}
	if m.ReadOnly() {
		t.Fatal("loading a real file must clear the read-only flag")
	}
	m, _ = m.Update(key('x'))
	if m.Text() != "bc" {
		t.Fatalf("the unlocked buffer must edit: %q", m.Text())
	}
	// NewFile unlocks too.
	m.ShowReadOnly("x.tar!a.txt", "hello\n")
	m.NewFile(filepath.Join(dir, "fresh.txt"))
	if m.ReadOnly() {
		t.Fatal("NewFile must clear the read-only flag")
	}
}

// TestReadOnlyAutosaveIsNoOp: the focus-leave autosave never writes a preview.
func TestReadOnlyAutosaveIsNoOp(t *testing.T) {
	m := readOnlyBuffer(t, "a.txt", "hello\n")
	if m.Autosave() {
		t.Fatal("autosave must not write a read-only buffer")
	}
}
