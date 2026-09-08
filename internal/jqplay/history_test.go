package jqplay

import (
	"os"
	"path/filepath"
	"testing"
)

// history_test.go covers the program history's persistence (#2536): the list
// is per user and outlives the process, while the zero value stays in memory.

// TestHistoryPersistsPerUser: with a file attached the list survives a fresh
// History over the same file — the restart case — newest first, and a program
// recorded before the first read stays newer than what was on disk.
func TestHistoryPersistsPerUser(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state", "playground-history.json")
	h := NewHistory(file)
	h.Add(".a")
	h.Add(".b")

	again := NewHistory(file)
	if again.Len() != 2 {
		t.Fatalf("reloaded Len = %d, want 2", again.Len())
	}
	if got, _ := again.At(0); got != ".b" {
		t.Errorf("reloaded newest = %q, want .b", got)
	}
	again.Add(".a") // a repeat moves to the front, on disk too
	third := NewHistory(file)
	if got, _ := third.At(0); got != ".a" || third.Len() != 2 {
		t.Errorf("after re-adding: newest = %q, Len = %d; want .a, 2", got, third.Len())
	}

	// A program recorded before the file was first read is newer than the
	// file's entries; the file's entries are still offered after it.
	late := NewHistory(file)
	late.items = []string{".fresh"}
	if got, _ := late.At(0); got != ".fresh" {
		t.Errorf("in-memory entry = %q, want .fresh first", got)
	}
	if got, _ := late.At(1); got != ".a" {
		t.Errorf("entry after it = %q, want the file's newest .a", got)
	}
}

// TestHistoryMalformedFileReadsAsEmpty: persistence never disrupts the
// playground — garbage or a foreign version reads as an empty list, and the
// next Add overwrites it.
func TestHistoryMalformedFileReadsAsEmpty(t *testing.T) {
	file := filepath.Join(t.TempDir(), "playground-history.json")
	for _, body := range []string{"{not json", `{"version":99,"programs":[".x"]}`} {
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		h := NewHistory(file)
		if h.Len() != 0 {
			t.Errorf("%q: Len = %d, want 0", body, h.Len())
		}
		h.Add(".y")
		if got, _ := NewHistory(file).At(0); got != ".y" {
			t.Errorf("%q: after Add, reloaded newest = %q, want .y", body, got)
		}
	}
}

// TestHistoryZeroValueStaysInMemory: the zero value (tests, hand-built
// models) never writes a file, and HistoryFile honours the sandbox override.
func TestHistoryZeroValueStaysInMemory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("IKE_CONFIG_DIR", dir)
	var h History
	h.Add(".a")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("zero-value history wrote %v", entries)
	}
	if got, want := HistoryFile(), filepath.Join(dir, "playground-history.json"); got != want {
		t.Errorf("HistoryFile() = %q, want %q", got, want)
	}
}
