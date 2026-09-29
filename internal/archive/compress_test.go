package archive

import (
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestWriteGzipRoundTrip: the .gz decompresses to the original, which is kept,
// and the header names the original file (#2805).
func TestWriteGzipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "app.log")
	if err := os.WriteFile(src, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := src + ".gz"
	res, err := WriteGzip(src, dest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || res.Bytes != 18 {
		t.Errorf("result = %+v, want 1 file, 18 bytes", res)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the original must be kept: %v", err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != "line one\nline two\n" || zr.Name != "app.log" {
		t.Errorf("got %q (name %q)", got, zr.Name)
	}
	assertNoTemp(t, dir)
}

// TestWriteGzipRefusesDirectory: only regular files are gzipped.
func TestWriteGzipRefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteGzip(dir, filepath.Join(t.TempDir(), "x.gz")); err == nil {
		t.Fatal("gzipping a directory must fail")
	}
}

// TestWriteZipRelativeMembers: a directory keeps its own name as the top-level
// folder, members are relative and slash-separated, symlinks are skipped, and
// the archive extracts back through the existing pipeline.
func TestWriteZipRelativeMembers(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "proj")
	mustWrite(t, filepath.Join(src, "main.go"), "package main\n")
	mustWrite(t, filepath.Join(src, "sub", "a.txt"), "a\n")
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "proj.zip")
	res, err := WriteZip([]string{src}, dest)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || res.Skipped != 1 {
		t.Errorf("result = %+v, want 2 files and 1 skipped link", res)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	zr.Close()
	sort.Strings(names)
	want := []string{"proj/", "proj/main.go", "proj/sub/", "proj/sub/a.txt"}
	if len(names) != len(want) {
		t.Fatalf("members = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("members = %v, want %v", names, want)
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	pl, err := PlanExtract(dest, out, nil, DefaultExtractLimit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Extract(pl, Options{MaxBytes: DefaultExtractLimit}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "proj", "sub", "a.txt")); string(got) != "a\n" {
		t.Errorf("round trip = %q", got)
	}
	assertNoTemp(t, dir)
}

// TestWriteZipMultiSelection: several sources pack side by side, an existing
// target is replaced, and a target inside a walked tree never packs itself.
func TestWriteZipMultiSelection(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	mustWrite(t, a, "a")
	mustWrite(t, b, "b")
	dest := filepath.Join(dir, "archive.zip")
	mustWrite(t, dest, "old")
	res, err := WriteZip([]string{a, b, dir}, dest)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("the existing target must be replaced by a valid zip: %v", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) == "archive.zip" {
			t.Fatalf("the zip packed itself: %s", f.Name)
		}
	}
	if res.Files != 4 { // a, b, and the directory's own a, b
		t.Errorf("files = %d, want 4", res.Files)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	left, _ := filepath.Glob(filepath.Join(dir, tempPattern))
	if len(left) > 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}
