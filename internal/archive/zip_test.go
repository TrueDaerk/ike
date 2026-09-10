package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// zipTime is the fixed mtime every zip fixture carries, so a mtime assertion
// is about the header round-trip and not about the clock.
var zipTime = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// writeZip builds a zip in memory from members, mirroring writeTar: a member
// with dir set becomes a "name/" directory record.
func writeZip(t *testing.T, members []member) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range members {
		h := &zip.FileHeader{Name: m.name, Method: zip.Deflate, Modified: zipTime}
		mode := fs.FileMode(m.mode)
		if mode == 0 {
			mode = 0o644
		}
		if m.dir {
			h.Name = strings.TrimSuffix(m.name, "/") + "/"
			h.Method, mode = zip.Store, fs.ModeDir|0o755
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if !m.dir {
			if _, err := w.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestDetectZip: the magic classifies a zip, the format names itself for the
// pane header, and the file handler's Match claims it.
func TestDetectZip(t *testing.T) {
	data := writeZip(t, sampleMembers())
	p := write(t, "src.zip", data)
	head := data
	if len(head) > blockSize {
		head = head[:blockSize]
	}
	if f := Detect(p, head); f != FormatZip {
		t.Fatalf("Detect = %v, want FormatZip", f)
	}
	if !IsArchive(p, head) {
		t.Fatal("a zip must be an archive")
	}
	if s := FormatZip.String(); s != "zip" {
		t.Fatalf("FormatZip.String() = %q, want zip", s)
	}
	// Content, not extension: a zip container under any name is an archive,
	// and a text file called .zip is not one.
	jar := write(t, "lib.jar", data)
	if f := Detect(jar, head); f != FormatZip {
		t.Fatalf("Detect(lib.jar) = %v, want FormatZip", f)
	}
	plain := []byte("not a zip at all\n")
	if IsArchive(write(t, "fake.zip", plain), plain) {
		t.Fatal("a text file must not be claimed by its name")
	}
}

// TestListZipEntries checks the listed metadata: names normalized,
// directories flagged and zero-sized, sizes, modes and mtimes carried over.
func TestListZipEntries(t *testing.T) {
	p := write(t, "src.zip", writeZip(t, sampleMembers()))
	l, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if l.Format != FormatZip {
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
		if !got.ModTime.Equal(zipTime) {
			t.Errorf("entry %d mtime = %v, want %v", i, got.ModTime, zipTime)
		}
	}
	if m := l.Entries[1].Mode.Perm(); m != 0o644 {
		t.Errorf("mode = %v", m)
	}
	if !l.Entries[0].Mode.IsDir() {
		t.Errorf("the directory member lost its mode: %v", l.Entries[0].Mode)
	}
}

// TestReadZipEntry previews one member and refuses the ones it must, exactly
// as ReadEntry does for a tar.
func TestReadZipEntry(t *testing.T) {
	p := write(t, "src.zip", writeZip(t, sampleMembers()))
	data, err := ReadEntry(p, "src/main.go", 0)
	if err != nil {
		t.Fatalf("ReadEntry: %v", err)
	}
	if string(data) != "package main\n" {
		t.Fatalf("content = %q", data)
	}
	if _, err := ReadEntry(p, "nope.txt", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing entry error = %v, want ErrNotFound", err)
	}
	if _, err := ReadEntry(p, "src", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("directory error = %v, want ErrNotFound", err)
	}
	if _, err := ReadEntry(p, "src/main.go", 3); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limited read error = %v, want ErrTooLarge", err)
	}
}

// TestListZipDegenerate: an archive with nothing in it and one holding only
// directory records both list without an error.
func TestListZipDegenerate(t *testing.T) {
	empty := write(t, "empty.zip", writeZip(t, nil))
	l, err := List(empty)
	if err != nil {
		t.Fatalf("List(empty): %v", err)
	}
	if l.Format != FormatZip || len(l.Entries) != 0 {
		t.Fatalf("empty listing = %v / %+v", l.Format, l.Entries)
	}
	// An empty zip is nothing but an end-of-central-directory record, so the
	// sniff has to recognise that too or the file opens as a binary blob.
	head, err := os.ReadFile(empty)
	if err != nil {
		t.Fatal(err)
	}
	if f := Detect(empty, head); f != FormatZip {
		t.Fatalf("Detect(empty.zip) = %v, want FormatZip", f)
	}

	dirs := write(t, "dirs.zip", writeZip(t, []member{{name: "a/", dir: true}, {name: "a/b/", dir: true}}))
	l, err = List(dirs)
	if err != nil {
		t.Fatalf("List(dirs): %v", err)
	}
	if len(l.Entries) != 2 || !l.Entries[0].IsDir || !l.Entries[1].IsDir {
		t.Fatalf("directory-only listing = %+v", l.Entries)
	}
}

// nopCloser makes a plain writer usable as a zip compressor.
type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

// writeExoticZip builds a zip whose second member uses compression method 99,
// which the standard library cannot decompress. The writer stores the bytes
// verbatim under that method number, which is exactly what a zip packed by a
// tool with its own codec looks like from Go's side.
func writeExoticZip(t *testing.T) []byte {
	t.Helper()
	const exotic = 99
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(exotic, func(w io.Writer) (io.WriteCloser, error) {
		return nopCloser{w}, nil
	})
	for _, m := range []struct {
		name   string
		body   string
		method uint16
	}{
		{"plain.txt", "readable\n", zip.Deflate},
		{"exotic.bin", "opaque\n", exotic},
	} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: m.name, Method: m.method, Modified: zipTime})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestZipUnsupportedMethod: a member Go cannot decompress is still listed,
// reports a readable error on preview, and is one skip on extraction — the
// rest of the archive comes out intact.
func TestZipUnsupportedMethod(t *testing.T) {
	p := write(t, "exotic.zip", writeExoticZip(t))
	l, err := List(p)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(l.Entries) != 2 {
		t.Fatalf("entries = %+v, want both members listed", l.Entries)
	}

	_, err = ReadEntry(p, "exotic.bin", 0)
	if !errors.Is(err, ErrUnsupportedMethod) {
		t.Fatalf("ReadEntry error = %v, want ErrUnsupportedMethod", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "exotic.bin") || !strings.Contains(msg, "99") {
		t.Fatalf("error %q must name the member and the method", msg)
	}
	if data, err := ReadEntry(p, "plain.txt", 0); err != nil || string(data) != "readable\n" {
		t.Fatalf("readable member = %q / %v", data, err)
	}

	dest := filepath.Join(t.TempDir(), "out")
	res := extractTo(t, p, dest, nil, Options{MaxBytes: DefaultExtractLimit})
	if res.Files != 1 {
		t.Fatalf("result = %+v, want the readable member only", res)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Name != "exotic.bin" {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
	if !strings.Contains(res.Skipped[0].Reason, "unsupported") {
		t.Fatalf("skip reason = %q", res.Skipped[0].Reason)
	}
	if got := readFile(t, filepath.Join(dest, "plain.txt")); got != "readable\n" {
		t.Fatalf("plain.txt = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "exotic.bin")); err == nil {
		t.Fatal("the undecompressable member must not be written")
	}
}

// TestExtractZipArchive: the whole archive lands on disk with its tree, the
// same as for a tar.
func TestExtractZipArchive(t *testing.T) {
	p := write(t, "src.zip", writeZip(t, sampleMembers()))
	dest := filepath.Join(t.TempDir(), "out")
	res := extractTo(t, p, dest, nil, Options{MaxBytes: DefaultExtractLimit})
	if res.Files != 3 || res.Dirs != 1 {
		t.Fatalf("result = %+v, want 3 files and 1 dir", res)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("skipped = %+v", res.Skipped)
	}
	if got := readFile(t, filepath.Join(dest, "src", "main.go")); got != "package main\n" {
		t.Fatalf("src/main.go = %q", got)
	}
	if got := readFile(t, filepath.Join(dest, "README.md")); got != "# hi\n" {
		t.Fatalf("README.md = %q", got)
	}

	// A named directory extracts its subtree, the pane's "e" on a folder row.
	sub := filepath.Join(t.TempDir(), "sub")
	res = extractTo(t, p, sub, []string{"src"}, Options{MaxBytes: DefaultExtractLimit})
	if res.Files != 2 {
		t.Fatalf("subtree result = %+v, want the two src members", res)
	}
	if _, err := os.Stat(filepath.Join(sub, "README.md")); err == nil {
		t.Fatal("the subtree extraction must not reach outside src/")
	}
}

// TestExtractZipRefusesTraversal is the zip twin of the tar traversal test:
// escaping names and symlinks are refused with reasons, nothing is written
// outside the destination.
func TestExtractZipRefusesTraversal(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, mode fs.FileMode, body string) {
		t.Helper()
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipTime}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("../escape.txt", 0o644, "evil")
	add("/etc/ike-escape", 0o644, "evil")
	add("deep/../../escape2.txt", 0o644, "evil")
	add("link", fs.ModeSymlink|0o777, "../../etc/passwd")
	add("ok.txt", 0o644, "fine")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	p := write(t, "evil.zip", buf.Bytes())
	root := t.TempDir()
	dest := filepath.Join(root, "out")
	res := extractTo(t, p, dest, nil, Options{MaxBytes: DefaultExtractLimit})

	if res.Files != 1 {
		t.Fatalf("result = %+v, want only the safe member", res)
	}
	reasons := map[string]int{}
	for _, s := range res.Skipped {
		reasons[s.Reason]++
	}
	if reasons[SkipUnsafePath] != 3 || reasons[SkipLink] != 1 {
		t.Fatalf("skip reasons = %v, want three unsafe paths and one link", reasons)
	}
	for _, escaped := range []string{
		filepath.Join(root, "escape.txt"),
		filepath.Join(root, "escape2.txt"),
		filepath.Join(string(filepath.Separator), "etc", "ike-escape"),
		filepath.Join(dest, "link"),
	} {
		if _, err := os.Lstat(escaped); err == nil {
			t.Fatalf("%s was created outside the target directory", escaped)
		}
	}
	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "ok.txt" {
		t.Fatalf("target dir holds %v, want ok.txt only", entries)
	}
}

// TestListZipSymlink: a zip that carries a symlink lists it with its target,
// the way the pane shows a tar link instead of a size.
func TestListZipSymlink(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "link", Method: zip.Deflate, Modified: zipTime}
	h.SetMode(fs.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("README.md")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	l, err := List(write(t, "link.zip", buf.Bytes()))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(l.Entries) != 1 || l.Entries[0].Link != "README.md" {
		t.Fatalf("entries = %+v, want the link target", l.Entries)
	}
}
