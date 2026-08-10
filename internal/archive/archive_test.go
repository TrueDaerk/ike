package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// member is one entry written into a test archive.
type member struct {
	name string
	body string
	dir  bool
	mode int64
}

// writeTar builds a tar in memory from members.
func writeTar(t *testing.T, members []member) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, m := range members {
		h := &tar.Header{
			Name:     m.name,
			Mode:     m.mode,
			Size:     int64(len(m.body)),
			ModTime:  time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
			Typeflag: tar.TypeReg,
		}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if m.dir {
			h.Typeflag, h.Size, h.Mode = tar.TypeDir, 0, 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !m.dir {
			if _, err := tw.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gzipped wraps data in a gzip stream.
func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// write drops data at name inside a fresh temp dir and returns the path.
func write(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// sampleMembers is the shared fixture: a directory entry, two files under it
// and one at the root.
func sampleMembers() []member {
	return []member{
		{name: "src/", dir: true},
		{name: "src/main.go", body: "package main\n"},
		{name: "src/util.go", body: "package main // util\n"},
		{name: "README.md", body: "# hi\n"},
	}
}

// bzip2TarB64 is a bzip2-compressed tar holding a single "hello.txt" entry
// with the body "hello\n". The standard library has a bzip2 *reader* only, so
// the fixture is precomputed (produced with `tar cjf`) rather than generated.
const bzip2TarB64 = `QlpoOTFBWSZTWWWLRcQAAJB/mPVQAIBAAf/iOuZ5sH/n31CEAgIAAAgwAPmZhKkTyRo0A9Q0BoAA
NpA0yMgJVPRATIyNNAANA0A0AAASpE8p6ajNTQ0BkAAAANAGmwlDy2aAfHUCwEFaJQHcWKQPuIC3
AkRqlI7Cw+MIxwgBLBrklx1KZSE5jTdjTEwBpDgnAD4XhCbaoZBB0qCxiSVT6NFCOzJfcHz5FA8p
ICvKtZCMheQgHGFxELWJ59BodHeUKkGRUyNtwNrtsR59Jwnni5o3RMYQU8nFglPZVH82PWoSUbNB
QK533uChNyUjhaX6JETmqMdH9ZyknXMXUWUeSaBI8HxxCy+Oih8aKg9vUdhpGRn0JGFAol76RFVS
i0sgeR+mdQj+LuSKcKEgyxaLiA==`

// writeBzip2Tar materializes the precomputed bzip2 tar fixture.
func writeBzip2Tar(t *testing.T, name string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(bzip2TarB64)
	if err != nil {
		t.Fatal(err)
	}
	return write(t, name, data)
}

// TestDetectFormats covers the sniff: plain, gzip- and bzip2-wrapped tars are
// claimed by content, and a gzip stream that is *not* a tar is not (#1762) —
// the coordination point with the gz viewer, which owns app.log.gz.
func TestDetectFormats(t *testing.T) {
	tarData := writeTar(t, sampleMembers())
	cases := []struct {
		name string
		file string
		data []byte
		want Format
	}{
		{"plain tar", "src.tar", tarData, FormatTar},
		{"gzip tar", "src.tar.gz", gzipped(t, tarData), FormatTarGz},
		{"gzip non-tar", "app.log.gz", gzipped(t, []byte("just a log line\n")), FormatNone},
		{"plain text", "notes.txt", []byte("nothing archival here\n"), FormatNone},
		{"short file", "tiny.bin", []byte{1, 2, 3}, FormatNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := write(t, c.file, c.data)
			head := c.data
			if len(head) > 512 {
				head = head[:512]
			}
			if got := Detect(p, head); got != c.want {
				t.Fatalf("Detect = %v, want %v", got, c.want)
			}
			if got := IsArchive(p, head); got != (c.want != FormatNone) {
				t.Fatalf("IsArchive = %v", got)
			}
		})
	}
}

// TestDetectBzip2Tar sniffs inside a bzip2 stream, the same rule as gzip.
func TestDetectBzip2Tar(t *testing.T) {
	p := writeBzip2Tar(t, "one.tar.bz2")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := Detect(p, data[:min(len(data), 512)]); got != FormatTarBz2 {
		t.Fatalf("Detect = %v, want FormatTarBz2", got)
	}
	l, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(l.Entries) != 1 || l.Entries[0].Name != "hello.txt" {
		t.Fatalf("entries = %+v", l.Entries)
	}
	body, err := ReadEntry(p, "hello.txt", 0)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if string(body) != "hello\n" {
		t.Fatalf("body = %q", body)
	}
}

// TestDetectV7Tar recognises a magic-less v7 header through the checksum.
func TestDetectV7Tar(t *testing.T) {
	block := make([]byte, 512)
	copy(block, "old.txt")
	copy(block[100:], "000644 \x00")  // mode
	copy(block[108:], "000000 \x00")  // uid
	copy(block[116:], "000000 \x00")  // gid
	copy(block[124:], "00000000000 ") // size
	copy(block[136:], "00000000000 ") // mtime
	copy(block[148:], "        ")     // checksum field blank while summing
	block[156] = '0'                  // regular file
	sum := 0
	for _, b := range block {
		sum += int(b)
	}
	copy(block[148:], []byte(octal6(sum)+"\x00 "))
	if !looksLikeTar(block) {
		t.Fatal("a checksum-valid v7 header must sniff as tar")
	}
	block[3] ^= 0xff // corrupt the name: the checksum no longer matches
	if looksLikeTar(block) {
		t.Fatal("a broken checksum must not sniff as tar")
	}
}

// octal6 renders n as six octal digits, the tar checksum field's width.
func octal6(n int) string {
	out := []byte("000000")
	for i := 5; i >= 0 && n > 0; i-- {
		out[i] = byte('0' + n%8)
		n /= 8
	}
	return string(out)
}

// TestListEntries checks the listed metadata: names normalized, directories
// flagged and zero-sized, sizes and mtimes carried through.
func TestListEntries(t *testing.T) {
	p := write(t, "src.tar", writeTar(t, sampleMembers()))
	l, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if l.Format != FormatTar {
		t.Fatalf("format = %v", l.Format)
	}
	if l.Truncated {
		t.Fatal("a four-entry archive is not truncated")
	}
	want := []struct {
		name  string
		isDir bool
		size  int64
	}{
		{"src", true, 0},
		{"src/main.go", false, int64(len("package main\n"))},
		{"src/util.go", false, int64(len("package main // util\n"))},
		{"README.md", false, int64(len("# hi\n"))},
	}
	if len(l.Entries) != len(want) {
		t.Fatalf("entries = %+v", l.Entries)
	}
	for i, w := range want {
		got := l.Entries[i]
		if got.Name != w.name || got.IsDir != w.isDir || got.Size != w.size {
			t.Errorf("entry %d = %+v, want %v/%v/%d", i, got, w.name, w.isDir, w.size)
		}
		if got.ModTime.IsZero() {
			t.Errorf("entry %d lost its mtime", i)
		}
	}
	if m := l.Entries[1].Mode.Perm(); m != 0o644 {
		t.Errorf("mode = %v", m)
	}
}

// TestListGzipTar reads through the gzip layer transparently.
func TestListGzipTar(t *testing.T) {
	p := write(t, "src.tgz", gzipped(t, writeTar(t, sampleMembers())))
	l, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if l.Format != FormatTarGz || len(l.Entries) != 4 {
		t.Fatalf("listing = %v / %d entries", l.Format, len(l.Entries))
	}
}

// TestReadEntry extracts one member and refuses the ones it must.
func TestReadEntry(t *testing.T) {
	p := write(t, "src.tar", writeTar(t, sampleMembers()))
	body, err := ReadEntry(p, "src/main.go", 0)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if string(body) != "package main\n" {
		t.Fatalf("body = %q", body)
	}
	if _, err := ReadEntry(p, "src/missing.go", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing entry err = %v", err)
	}
	if _, err := ReadEntry(p, "src", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("directory entry err = %v", err)
	}
	// The large-file limit applies before the bytes are buffered (#149).
	if _, err := ReadEntry(p, "src/main.go", 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over-limit err = %v", err)
	}
	if _, err := ReadEntry(p, "src/main.go", int64(len("package main\n"))); err != nil {
		t.Fatalf("exactly-at-limit must pass: %v", err)
	}
}

// TestTruncatedArchive degrades to a partial listing plus an error rather
// than losing everything or panicking.
func TestTruncatedArchive(t *testing.T) {
	full := writeTar(t, sampleMembers())
	// Cut mid-header (not on a 512-byte block boundary), the shape a partial
	// download or a killed `tar c` leaves behind.
	p := write(t, "cut.tar", full[:600])
	l, err := List(p)
	if err == nil {
		t.Fatal("a truncated archive must report an error")
	}
	if len(l.Entries) == 0 {
		t.Fatal("the entries read before the cut must survive")
	}
}

// TestGarbageArchive fails cleanly on a file that only looks like a tar.
func TestGarbageArchive(t *testing.T) {
	junk := bytes.Repeat([]byte{0x7f}, 2048)
	copy(junk[257:], "ustar")
	p := write(t, "fake.tar", junk)
	if _, err := List(p); err == nil {
		t.Fatal("garbage claiming the ustar magic must error, not parse")
	}
}
