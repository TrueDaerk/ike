package lsp

import (
	"strings"
	"testing"

	"ike/internal/host"
	"ike/internal/largefile"
)

// TestLargeFileGated covers the didOpen gate (#149): a document over the
// configured thresholds is not opened with the server unless the per-path
// override is set.
func TestLargeFileGated(t *testing.T) {
	defer largefile.Reset()
	cfg := host.MapConfig{
		"files.large_file_kb":    "1",
		"files.large_file_lines": "5",
	}

	small := []byte("package x\n")
	if largeFileGated(cfg, "/p/small.go", small) {
		t.Fatal("small file must pass the gate")
	}

	big := []byte(strings.Repeat("x", 2048))
	if !largeFileGated(cfg, "/p/big.go", big) {
		t.Fatal("2 KB over a 1 KB threshold must be gated")
	}

	manyLines := []byte(strings.Repeat("a\n", 10))
	if !largeFileGated(cfg, "/p/lines.go", manyLines) {
		t.Fatal("10 lines over a 5-line guard must be gated")
	}

	// The editor.forceCodeInsight override punches through.
	largefile.Force("/p/big.go")
	if largeFileGated(cfg, "/p/big.go", big) {
		t.Fatal("forced path must pass the gate")
	}

	// No config falls back to the defaults, which never flag a tiny file.
	if largeFileGated(nil, "/p/small.go", small) {
		t.Fatal("defaults must not gate a tiny file")
	}
}
