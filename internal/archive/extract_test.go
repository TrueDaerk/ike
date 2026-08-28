package archive

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// extractTo plans and runs an extraction into a fresh directory, failing the
// test on an unexpected error.
func extractTo(t *testing.T, archivePath, dest string, members []string, opts Options) Result {
	t.Helper()
	pl, err := PlanExtract(archivePath, dest, members, opts.MaxBytes)
	if err != nil {
		t.Fatalf("PlanExtract: %v", err)
	}
	res, err := Extract(pl, opts)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return res
}

// readFile is the extracted-content assertion.
func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// TestExtractWholeArchive covers the three supported formats: every member
// lands under the target directory with its content and its tree.
func TestExtractWholeArchive(t *testing.T) {
	tarData := writeTar(t, sampleMembers())
	cases := []struct {
		name string
		file string
		data []byte
	}{
		{"tar", "src.tar", tarData},
		{"tar.gz", "src.tgz", gzipped(t, tarData)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := write(t, c.file, c.data)
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
			if res.Bytes == 0 {
				t.Fatal("the result must count the written bytes")
			}
		})
	}
	// The bzip2 fixture is precomputed (no standard-library writer), so it
	// carries a single member.
	t.Run("tar.bz2", func(t *testing.T) {
		p := writeBzip2Tar(t, "one.tar.bz2")
		dest := filepath.Join(t.TempDir(), "out")
		res := extractTo(t, p, dest, nil, Options{MaxBytes: DefaultExtractLimit})
		if res.Files != 1 {
			t.Fatalf("result = %+v, want one file", res)
		}
		if got := readFile(t, filepath.Join(dest, "hello.txt")); got != "hello\n" {
			t.Fatalf("hello.txt = %q", got)
		}
	})
}

// TestExtractSingleMember: naming one member extracts exactly it, parent
// directories included.
func TestExtractSingleMember(t *testing.T) {
	p := write(t, "src.tar", writeTar(t, sampleMembers()))
	dest := filepath.Join(t.TempDir(), "out")
	res := extractTo(t, p, dest, []string{"src/main.go"}, Options{MaxBytes: DefaultExtractLimit})
	if res.Files != 1 {
		t.Fatalf("result = %+v, want one file", res)
	}
	if got := readFile(t, filepath.Join(dest, "src", "main.go")); got != "package main\n" {
		t.Fatalf("main.go = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err == nil {
		t.Fatal("only the named member may be extracted")
	}
}

// TestExtractDirectorySubtree: naming a directory extracts everything below
// it, which is what the pane's "e" on a folder row means.
func TestExtractDirectorySubtree(t *testing.T) {
	p := write(t, "src.tar", writeTar(t, sampleMembers()))
	dest := filepath.Join(t.TempDir(), "out")
	res := extractTo(t, p, dest, []string{"src"}, Options{MaxBytes: DefaultExtractLimit})
	if res.Files != 2 || res.Dirs != 1 {
		t.Fatalf("result = %+v, want 2 files and 1 dir", res)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err == nil {
		t.Fatal("a subtree extraction must not reach the archive root")
	}
}

// TestSafeTarget is the sanitizer's unit test: everything that could escape
// the destination is refused, ordinary names resolve inside it.
func TestSafeTarget(t *testing.T) {
	dest := filepath.Join(string(filepath.Separator), "tmp", "out")
	bad := []string{"../escape.txt", "../../etc/passwd", "/etc/passwd", "..", ".", "", "a/../../b"}
	for _, name := range bad {
		if got, ok := SafeTarget(dest, name); ok {
			t.Errorf("SafeTarget(%q) = %q, want refused", name, got)
		}
	}
	good := map[string]string{
		"a.txt":       filepath.Join(dest, "a.txt"),
		"src/main.go": filepath.Join(dest, "src", "main.go"),
		"./b.txt":     filepath.Join(dest, "b.txt"),
		"a/../c.txt":  filepath.Join(dest, "c.txt"),
	}
	for name, want := range good {
		got, ok := SafeTarget(dest, name)
		if !ok || got != want {
			t.Errorf("SafeTarget(%q) = %q/%v, want %q", name, got, ok, want)
		}
	}
}

// TestExtractRefusesTraversal crafts the hostile archive: members named
// "../escape.txt" and "/etc/ike-escape" plus a symlink. Nothing lands outside
// the destination, and every refusal is reported.
func TestExtractRefusesTraversal(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	writeHeader := func(h *tar.Header, body string) {
		t.Helper()
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	writeHeader(&tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 4, ModTime: now, Typeflag: tar.TypeReg}, "evil")
	writeHeader(&tar.Header{Name: "/etc/ike-escape", Mode: 0o644, Size: 4, ModTime: now, Typeflag: tar.TypeReg}, "evil")
	writeHeader(&tar.Header{Name: "deep/../../escape2.txt", Mode: 0o644, Size: 4, ModTime: now, Typeflag: tar.TypeReg}, "evil")
	writeHeader(&tar.Header{Name: "link", Mode: 0o777, ModTime: now, Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}, "")
	writeHeader(&tar.Header{Name: "ok.txt", Mode: 0o644, Size: 4, ModTime: now, Typeflag: tar.TypeReg}, "fine")
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	p := write(t, "evil.tar", buf.Bytes())
	root := t.TempDir()
	dest := filepath.Join(root, "out")
	res := extractTo(t, p, dest, nil, Options{MaxBytes: DefaultExtractLimit})

	if res.Files != 1 {
		t.Fatalf("result = %+v, want only the safe member", res)
	}
	if len(res.Skipped) != 4 {
		t.Fatalf("skipped = %+v, want the three unsafe paths and the link", res.Skipped)
	}
	reasons := map[string]int{}
	for _, s := range res.Skipped {
		reasons[s.Reason]++
	}
	if reasons[SkipUnsafePath] != 3 || reasons[SkipLink] != 1 {
		t.Fatalf("skip reasons = %v", reasons)
	}
	// Nothing outside the destination was created.
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

// TestPlanReportsConflictsAndOverwrite: existing files are reported before
// anything is written, skipped without Overwrite and replaced with it.
func TestPlanReportsConflictsAndOverwrite(t *testing.T) {
	p := write(t, "src.tar", writeTar(t, sampleMembers()))
	dest := t.TempDir()
	existing := filepath.Join(dest, "README.md")
	if err := os.WriteFile(existing, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pl, err := PlanExtract(p, dest, nil, DefaultExtractLimit)
	if err != nil {
		t.Fatalf("PlanExtract: %v", err)
	}
	if len(pl.Conflicts) != 1 || pl.Conflicts[0] != "README.md" {
		t.Fatalf("conflicts = %v, want README.md", pl.Conflicts)
	}
	// Without Overwrite the existing file survives and the skip is reported.
	res, err := Extract(pl, Options{MaxBytes: DefaultExtractLimit})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got := readFile(t, existing); got != "mine\n" {
		t.Fatalf("README.md = %q, want the untouched file", got)
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Reason != SkipExists {
		t.Fatalf("skipped = %+v, want one SkipExists", res.Skipped)
	}
	if res.Files != 2 {
		t.Fatalf("files = %d, want the other two members", res.Files)
	}
	// With Overwrite the member replaces it.
	if _, err := Extract(pl, Options{Overwrite: true, MaxBytes: DefaultExtractLimit}); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got := readFile(t, existing); got != "# hi\n" {
		t.Fatalf("README.md = %q, want the archive's version", got)
	}
}

// TestExtractCapRefusesBeforeWriting: the plan's declared total over the cap
// is refused outright, and nothing is written.
func TestExtractCapRefusesBeforeWriting(t *testing.T) {
	p := write(t, "src.tar", writeTar(t, sampleMembers()))
	dest := filepath.Join(t.TempDir(), "out")
	pl, err := PlanExtract(p, dest, nil, 5)
	if !errors.Is(err, ErrExtractTooLarge) {
		t.Fatalf("PlanExtract error = %v, want ErrExtractTooLarge", err)
	}
	if pl.Bytes <= 5 {
		t.Fatalf("the plan must report the offending size, got %d", pl.Bytes)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("a refused extraction must not create the target directory")
	}
}

// TestExtractCapEnforcedOnWrittenBytes: a header that lies about its size
// cannot slip past the cap — the write stops at the ceiling, the partial file
// is removed, and the error names the cap.
func TestExtractCapEnforcedOnWrittenBytes(t *testing.T) {
	body := strings.Repeat("x", 4096)
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	h := &tar.Header{
		Name: "big.txt", Mode: 0o644, Size: int64(len(body)),
		ModTime: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(h); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	p := write(t, "big.tar", buf.Bytes())
	dest := filepath.Join(t.TempDir(), "out")
	// The plan is built without a cap — the header size is the untrusted
	// number — and the write is capped at 100 bytes.
	pl, err := PlanExtract(p, dest, nil, 0)
	if err != nil {
		t.Fatalf("PlanExtract: %v", err)
	}
	if _, err := Extract(pl, Options{MaxBytes: 100}); !errors.Is(err, ErrExtractTooLarge) {
		t.Fatalf("Extract error = %v, want ErrExtractTooLarge", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "big.txt")); err == nil {
		t.Fatal("the partial file must be removed when the cap is hit")
	}
}

// TestExtractCreatesDestination: a target directory that does not exist yet is
// created, which is what the prompt's default proposal relies on.
func TestExtractCreatesDestination(t *testing.T) {
	p := write(t, "src.tar", writeTar(t, sampleMembers()))
	dest := filepath.Join(t.TempDir(), "new", "nested")
	extractTo(t, p, dest, []string{"README.md"}, Options{MaxBytes: DefaultExtractLimit})
	if got := readFile(t, filepath.Join(dest, "README.md")); got != "# hi\n" {
		t.Fatalf("README.md = %q", got)
	}
}
