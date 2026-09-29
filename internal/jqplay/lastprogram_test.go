package jqplay

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// lastprogram_test.go covers the per-source last-program store's persistence
// (#2774): a restart resumes the program last run on a given path/dialect
// pair, the count is capped with the oldest entry dropped, and a malformed
// file reads as empty rather than disrupting the playground.

// TestLastProgramsPersistsPerSource: with a file attached the store survives
// a fresh LastPrograms over the same file, keyed independently and moving a
// re-set key to the front like the history list does.
func TestLastProgramsPersistsPerSource(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state", "playground-last.json")
	l := NewLastPrograms(file)
	l.Set("jq:file:/a.json", ".items[0]")
	l.Set("yq:file:/a.yaml", ".spec.name")

	again := NewLastPrograms(file)
	if got, ok := again.Get("jq:file:/a.json"); !ok || got != ".items[0]" {
		t.Errorf("jq entry = %q, %v; want .items[0], true", got, ok)
	}
	if got, ok := again.Get("yq:file:/a.yaml"); !ok || got != ".spec.name" {
		t.Errorf("yq entry = %q, %v; want .spec.name, true", got, ok)
	}
	if again.Len() != 2 {
		t.Fatalf("reloaded Len = %d, want 2", again.Len())
	}

	again.Set("jq:file:/a.json", ".items[1]")
	third := NewLastPrograms(file)
	if got, _ := third.Get("jq:file:/a.json"); got != ".items[1]" {
		t.Errorf("after re-set, reloaded = %q, want .items[1]", got)
	}
}

// TestLastProgramsSameDialectDifferentPath keeps a jq entry and a yq entry
// over paths that differ only in extension separate — the dialect is part of
// the key, mirroring playDocKey.
func TestLastProgramsSameDialectDifferentPath(t *testing.T) {
	l := NewLastPrograms("")
	l.Set("jq:file:/a.json", ".a")
	l.Set("yq:file:/a.yaml", ".b")
	if got, _ := l.Get("jq:file:/a.json"); got != ".a" {
		t.Errorf("jq entry = %q, want .a", got)
	}
	if got, _ := l.Get("yq:file:/a.yaml"); got != ".b" {
		t.Errorf("yq entry = %q, want .b", got)
	}
}

// TestLastProgramsCapEnforced: once the store holds LastProgramLimit entries,
// the next Set drops the oldest (least recently set) one.
func TestLastProgramsCapEnforced(t *testing.T) {
	file := filepath.Join(t.TempDir(), "playground-last.json")
	l := NewLastPrograms(file)
	for i := 0; i < LastProgramLimit; i++ {
		l.Set(fmt.Sprintf("jq:file:/%d.json", i), ".x")
	}
	if l.Len() != LastProgramLimit {
		t.Fatalf("Len = %d, want %d", l.Len(), LastProgramLimit)
	}
	l.Set("jq:file:/overflow.json", ".x")
	if l.Len() != LastProgramLimit {
		t.Fatalf("after overflow, Len = %d, want %d", l.Len(), LastProgramLimit)
	}
	if _, ok := l.Get("jq:file:/0.json"); ok {
		t.Error("oldest entry survived the cap")
	}
	if _, ok := l.Get("jq:file:/overflow.json"); !ok {
		t.Error("newest entry was dropped instead")
	}

	reloaded := NewLastPrograms(file)
	if reloaded.Len() != LastProgramLimit {
		t.Fatalf("reloaded Len = %d, want %d", reloaded.Len(), LastProgramLimit)
	}
}

// TestLastProgramsMalformedFileReadsAsEmpty: persistence never disrupts the
// playground — garbage or a foreign version reads as an empty store, and the
// next Set overwrites it.
func TestLastProgramsMalformedFileReadsAsEmpty(t *testing.T) {
	file := filepath.Join(t.TempDir(), "playground-last.json")
	for _, body := range []string{"{not json", `{"version":99,"entries":[{"key":"jq:file:/a.json","program":".x"}]}`} {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		l := NewLastPrograms(file)
		if l.Len() != 0 {
			t.Errorf("%q: Len = %d, want 0", body, l.Len())
		}
		l.Set("jq:file:/y.json", ".y")
		if got, _ := NewLastPrograms(file).Get("jq:file:/y.json"); got != ".y" {
			t.Errorf("%q: after Set, reloaded = %q, want .y", body, got)
		}
	}
}

// TestLastProgramsZeroValueStaysInMemory: the zero value (tests, hand-built
// models) never writes a file, and LastProgramFile honours the sandbox
// override.
func TestLastProgramsZeroValueStaysInMemory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	l := NewLastPrograms("")
	l.Set("jq:file:/a.json", ".a")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("in-memory-only store wrote %v", entries)
	}
	if got, want := LastProgramFile(), filepath.Join(dir, "playground-last.json"); got != want {
		t.Errorf("LastProgramFile() = %q, want %q", got, want)
	}
}
