package gzfile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGz gzips body into a temp file called name and returns its path. When
// header is non-empty it is stamped into the gzip original-name field.
func writeGz(t *testing.T, name, header string, body []byte) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Name = header
	if _, err := zw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeTarGz gzips a one-entry tar into a temp file called name.
func writeTarGz(t *testing.T, name string) string {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	body := "package main\n"
	if err := tw.WriteHeader(&tar.Header{Name: "main.go", Mode: 0o644, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return writeGz(t, name, "", tarBuf.Bytes())
}

// head reads the sniff window the file handler passes to Match.
func head(t *testing.T, p string) []byte {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return buf[:n]
}

// TestIsPlainClaimsPlainGzip: the ordinary case — a compressed log.
func TestIsPlainClaimsPlainGzip(t *testing.T) {
	for _, name := range []string{"app.log.gz", "data.json.gz", "dump"} {
		p := writeGz(t, name, "", []byte("2026-08-10 boot ok\n"))
		if !IsPlain(p, head(t, p)) {
			t.Fatalf("%s must be claimed as a plain gzip", name)
		}
	}
}

// TestIsPlainDeclinesTarballs is the routing contract with the archive viewer
// (#1762): a gzipped tar is listed as an archive, never decompressed into a
// buffer — by content *and* by name.
func TestIsPlainDeclinesTarballs(t *testing.T) {
	for _, name := range []string{"backup.tar.gz", "backup.tgz"} {
		p := writeTarGz(t, name)
		if IsPlain(p, head(t, p)) {
			t.Fatalf("%s holds a tar and must go to the archive viewer", name)
		}
	}
	// A file merely *named* like a tarball is the archive viewer's problem
	// too: the name is what the user asked for.
	p := writeGz(t, "odd.tar.gz", "", []byte("not a tar at all\n"))
	if IsPlain(p, head(t, p)) {
		t.Fatal(".tar.gz must never be claimed by the gz viewer")
	}
}

// TestIsPlainDeclinesNonGzip: no magic, no claim.
func TestIsPlainDeclinesNonGzip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plain.log")
	if err := os.WriteFile(p, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsPlain(p, head(t, p)) {
		t.Fatal("an uncompressed file must not be claimed")
	}
	if IsGzip(nil) || IsGzip([]byte{0x1f}) {
		t.Fatal("a short or empty head is not gzip")
	}
}

// TestInnerNamePrefersTheStrippedExtension: the language of app.log.gz comes
// from "app.log", whatever the gzip header claims — the header field is
// optional and routinely wrong.
func TestInnerNamePrefersTheStrippedExtension(t *testing.T) {
	cases := []struct{ path, header, want string }{
		{"app.log.gz", "", "app.log"},
		{"data.json.gz", "somewhere-else.txt", "data.json"},
		{"APP.LOG.GZ", "", "APP.LOG"},
		{"archive.gzip", "", "archive"},
		// No .gz suffix to strip: the header field is the fallback, reduced
		// to its base name so a recorded absolute path cannot escape.
		{"dump", "original.json", "original.json"},
		{"dump", "/var/log/original.log", "original.log"},
		{"dump", "", "dump"},
		{"dump", "   ", "dump"},
		{".gz", "", ".gz"},
	}
	for _, c := range cases {
		if got := InnerName(c.path, c.header); got != c.want {
			t.Errorf("InnerName(%q, %q) = %q, want %q", c.path, c.header, got, c.want)
		}
	}
}

// TestInnerNameFallsBackToTheHeaderWithoutAStrippedExtension (#1853): stripping
// dump.gz leaves "dump", which names no language at all — so a header that
// does carry an extension is the better answer. A header without one is not:
// it would trade one anonymous name for another.
func TestInnerNameFallsBackToTheHeaderWithoutAStrippedExtension(t *testing.T) {
	cases := []struct{ path, header, want string }{
		{"dump.gz", "payload.sql", "payload.sql"},
		{"dump.gz", "/var/backups/payload.sql", "payload.sql"},
		{"backup.gzip", "schema.json", "schema.json"},
		{"dump.gz", "payload", "dump"},      // the header names no language either
		{"dump.gz", "", "dump"},             // no header at all
		{"dump.gz", "  ", "dump"},           // blank header
		{"app.log.gz", "x.json", "app.log"}, // the file name still wins when it says something
	}
	for _, c := range cases {
		if got := InnerName(c.path, c.header); got != c.want {
			t.Errorf("InnerName(%q, %q) = %q, want %q", c.path, c.header, got, c.want)
		}
	}
}

// TestReadDecompresses covers the happy path plus the metadata the notice
// needs.
func TestReadDecompresses(t *testing.T) {
	body := "line one\nline two\n"
	p := writeGz(t, "app.log.gz", "", []byte(body))
	c, err := Read(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if string(c.Data) != body {
		t.Fatalf("Data = %q, want %q", c.Data, body)
	}
	if c.Truncated {
		t.Fatal("an uncapped read of a small file is not truncated")
	}
	if c.Name != "app.log" {
		t.Fatalf("Name = %q", c.Name)
	}
	if !c.OriginalOK || c.Original != int64(len(body)) {
		t.Fatalf("footer size = %d/%v, want %d", c.Original, c.OriginalOK, len(body))
	}
	if c.Compressed <= 0 {
		t.Fatal("the compressed size must come from the file on disk")
	}
	if c.Ratio() <= 0 {
		t.Fatalf("Ratio = %v", c.Ratio())
	}
}

// TestReadUsesTheHeaderNameWhenTheFileNameGivesNothing.
func TestReadUsesTheHeaderNameWhenTheFileNameGivesNothing(t *testing.T) {
	p := writeGz(t, "dump", "payload.json", []byte("{}\n"))
	c, err := Read(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "payload.json" {
		t.Fatalf("Name = %q, want payload.json", c.Name)
	}
}

// TestReadCapsDecompressedBytes is the bomb guard: the cap counts *output*,
// so a few compressed kilobytes cannot expand without bound.
func TestReadCapsDecompressedBytes(t *testing.T) {
	const limit = 4096
	// 4 MB of zeros compresses to a few kilobytes — the classic shape.
	p := writeGz(t, "bomb.bin.gz", "", bytes.Repeat([]byte{'a'}, 4<<20))
	c, err := Read(p, limit)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Truncated {
		t.Fatal("a stream past the cap must report truncation")
	}
	if int64(len(c.Data)) != limit {
		t.Fatalf("read %d bytes, want exactly the cap %d", len(c.Data), limit)
	}
	if c.Compressed >= limit {
		t.Skip("fixture did not compress below the cap; the guard is still the output size")
	}
}

// TestReadExactlyAtTheCapIsNotTruncated: the limit is inclusive, so a file
// landing exactly on it shows in full.
func TestReadExactlyAtTheCapIsNotTruncated(t *testing.T) {
	body := bytes.Repeat([]byte{'x'}, 100)
	p := writeGz(t, "exact.txt.gz", "", body)
	c, err := Read(p, 100)
	if err != nil {
		t.Fatal(err)
	}
	if c.Truncated || len(c.Data) != 100 {
		t.Fatalf("truncated=%v len=%d, want a full 100-byte read", c.Truncated, len(c.Data))
	}
}

// TestReadCorruptTailKeepsWhatItGot: a partially written log is worth
// reading; only a stream yielding nothing at all is an error.
func TestReadCorruptTailKeepsWhatItGot(t *testing.T) {
	p := writeGz(t, "cut.log.gz", "", []byte(strings.Repeat("entry\n", 500)))
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw[:len(raw)-12], 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Read(p, 0)
	if err != nil {
		t.Fatalf("a truncated tail must not fail the read: %v", err)
	}
	if len(c.Data) == 0 || !c.Truncated {
		t.Fatalf("len=%d truncated=%v, want partial content flagged truncated", len(c.Data), c.Truncated)
	}
}

// TestReadRejectsNonGzip: the caller gets an error, not an empty buffer.
func TestReadRejectsNonGzip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plain.log")
	if err := os.WriteFile(p, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p, 0); err == nil {
		t.Fatal("a non-gzip file must fail the read")
	}
	if _, err := Read(filepath.Join(t.TempDir(), "missing.gz"), 0); err == nil {
		t.Fatal("a missing file must fail the read")
	}
}

// TestRatioWithoutFooter: a truncated read has no trustworthy size, so the
// notice shows no ratio rather than a wrong one.
func TestRatioWithoutFooter(t *testing.T) {
	c := Content{Data: []byte("abc"), Compressed: 10, Truncated: true}
	if got := c.Ratio(); got != 0 {
		t.Fatalf("Ratio = %v, want 0 without a trustworthy size", got)
	}
	c = Content{Data: bytes.Repeat([]byte("a"), 100), Compressed: 0}
	if got := c.Ratio(); got != 0 {
		t.Fatalf("Ratio = %v, want 0 for a zero-byte archive", got)
	}
}
