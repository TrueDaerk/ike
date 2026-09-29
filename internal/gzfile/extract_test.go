package gzfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestExtractName: the .gz suffix is stripped; a suffix-less gzip gains .out.
func TestExtractName(t *testing.T) {
	for in, want := range map[string]string{
		"/x/app.log.gz": "app.log",
		"/x/dump.GZIP":  "dump",
		"/x/data":       "data.out",
		"/x/.gz":        ".gz.out",
	} {
		if got := ExtractName(in); got != want {
			t.Errorf("ExtractName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExtractWritesFile: app.log.gz decompresses beside itself (#2805).
func TestExtractWritesFile(t *testing.T) {
	src := writeGz(t, "app.log.gz", "", []byte("hello\n"))
	dest := filepath.Join(filepath.Dir(src), ExtractName(src))
	n, err := Extract(src, dest, 0)
	if err != nil || n != 6 {
		t.Fatalf("Extract = %d, %v", n, err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "hello\n" {
		t.Errorf("content = %q", got)
	}
}

// TestExtractHonoursCap: a stream past the cap writes nothing — neither the
// target nor a temporary file survives.
func TestExtractHonoursCap(t *testing.T) {
	src := writeGz(t, "bomb.gz", "", bytes.Repeat([]byte{0}, 4096))
	dir := filepath.Dir(src)
	dest := filepath.Join(dir, "bomb")
	if _, err := Extract(src, dest, 1024); !errors.Is(err, ErrExtractTooLarge) {
		t.Fatalf("err = %v, want ErrExtractTooLarge", err)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("a capped extraction must not leave the target behind")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".ike-gunzip-*")); len(left) > 0 {
		t.Errorf("temporary files left: %v", left)
	}
}
