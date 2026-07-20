package lsp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLatestLogPicksNewest(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "lsp-go.log")
	newer := filepath.Join(dir, "lsp-php.log")
	if err := os.WriteFile(old, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	// Noise that must be ignored.
	_ = os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o644)
	path, others := latestLog(dir)
	if path != newer || others != 1 {
		t.Fatalf("latestLog = %q, %d; want %q, 1", path, others, newer)
	}
}

func TestLatestLogEmptyDir(t *testing.T) {
	if path, others := latestLog(t.TempDir()); path != "" || others != 0 {
		t.Fatalf("empty dir must yield nothing, got %q %d", path, others)
	}
}
