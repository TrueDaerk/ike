package httpclient

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBodySinkKeepsSmallBodiesInMemory: below the threshold nothing touches
// the disk — the ordinary API answer must not pay for the large-body case.
func TestBodySinkKeepsSmallBodiesInMemory(t *testing.T) {
	s := newBodySink(64, 1024)
	if _, err := s.Write([]byte("hello world")); err != nil {
		t.Fatal(err)
	}
	head, path, total := s.close()
	if string(head) != "hello world" || path != "" || total != 11 {
		t.Errorf("head=%q path=%q total=%d", head, path, total)
	}
	if len(s.warnings()) != 0 {
		t.Errorf("unexpected warnings: %v", s.warnings())
	}
}

// TestBodySinkSpillsPastThreshold: the head stays in memory, the spool file
// holds the *whole* body — head included, so it is a complete artifact.
func TestBodySinkSpillsPastThreshold(t *testing.T) {
	t.Cleanup(CleanupSpool)
	s := newBodySink(10, 1<<20)
	// Three writes, the middle one straddling the threshold.
	for _, chunk := range []string{"0123456", "789abcdef", "ghij"} {
		if _, err := s.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	head, path, total := s.close()
	if string(head) != "0123456789" {
		t.Errorf("head = %q, want the first 10 bytes", head)
	}
	if total != 20 {
		t.Errorf("total = %d, want 20", total)
	}
	if path == "" {
		t.Fatal("want a spool file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0123456789abcdefghij" {
		t.Errorf("spool contents = %q", data)
	}
}

// TestBodySinkTruncatesAtMax: the total cap still bounds a spooled body, and
// the warning still names it.
func TestBodySinkTruncatesAtMax(t *testing.T) {
	t.Cleanup(CleanupSpool)
	s := newBodySink(4, 12)
	if kept := s.add([]byte(strings.Repeat("x", 20))); kept != 12 {
		t.Errorf("kept = %d, want 12", kept)
	}
	if kept := s.add([]byte("more")); kept != 0 {
		t.Errorf("kept after the cap = %d, want 0", kept)
	}
	_, path, total := s.close()
	if total != 12 || !s.truncated {
		t.Errorf("total=%d truncated=%v", total, s.truncated)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 12 {
		t.Errorf("spool holds %d bytes, want 12", len(data))
	}
	if w := s.warnings(); len(w) != 1 || !strings.Contains(w[0], "truncated") {
		t.Errorf("warnings = %v", w)
	}
}

// TestResponseFullBodyReadsTheSpool: FullBody is the seam every consumer that
// needs all the bytes goes through.
func TestResponseFullBodyReadsTheSpool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.bin")
	if err := os.WriteFile(path, []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := &Response{Body: []byte("01234"), SpoolPath: path, BodySize: 16}

	if !resp.Spooled() || resp.BodyBytes() != 16 {
		t.Fatalf("spooled=%v size=%d", resp.Spooled(), resp.BodyBytes())
	}
	full, err := resp.FullBody()
	if err != nil {
		t.Fatal(err)
	}
	if string(full) != "0123456789abcdef" {
		t.Errorf("FullBody = %q", full)
	}
	rc, err := resp.BodyReader()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	streamed, err := io.ReadAll(rc)
	if err != nil || string(streamed) != "0123456789abcdef" {
		t.Errorf("BodyReader = %q, %v", streamed, err)
	}
}

// TestResponseBodyRangeWindows: "load more" reads exactly the window it asks
// for, and stops at the end of the body instead of erroring.
func TestResponseBodyRangeWindows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.bin")
	if err := os.WriteFile(path, []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := &Response{Body: []byte("01234"), SpoolPath: path, BodySize: 16}

	got, err := resp.BodyRange(5, 6)
	if err != nil || string(got) != "56789a" {
		t.Errorf("BodyRange(5,6) = %q, %v", got, err)
	}
	got, err = resp.BodyRange(12, 100)
	if err != nil || string(got) != "cdef" {
		t.Errorf("BodyRange past the end = %q, %v", got, err)
	}
	if got, err := resp.BodyRange(16, 4); err != nil || got != nil {
		t.Errorf("BodyRange at the end = %q, %v", got, err)
	}
}

// TestResponseBodyRangeInMemory: an unspooled response answers the same
// questions without touching the filesystem.
func TestResponseBodyRangeInMemory(t *testing.T) {
	resp := &Response{Body: []byte("abcdef")}
	if resp.Spooled() || resp.BodyBytes() != 6 {
		t.Fatalf("spooled=%v size=%d", resp.Spooled(), resp.BodyBytes())
	}
	got, err := resp.BodyRange(2, 10)
	if err != nil || string(got) != "cdef" {
		t.Errorf("BodyRange = %q, %v", got, err)
	}
	full, err := resp.FullBody()
	if err != nil || string(full) != "abcdef" {
		t.Errorf("FullBody = %q, %v", full, err)
	}
}

// TestSpoolGoneDegradesToTheHead: a spool file removed under us (another
// process cleaned the temp dir) reports ErrSpoolGone and still hands back the
// head, so the viewer shows something rather than nothing.
func TestSpoolGoneDegradesToTheHead(t *testing.T) {
	resp := &Response{Body: []byte("head"), SpoolPath: filepath.Join(t.TempDir(), "missing.bin"), BodySize: 999}
	body, err := resp.FullBody()
	if !errors.Is(err, ErrSpoolGone) {
		t.Errorf("err = %v, want ErrSpoolGone", err)
	}
	if string(body) != "head" {
		t.Errorf("body = %q, want the head", body)
	}
	if _, err := resp.BodyRange(0, 4); !errors.Is(err, ErrSpoolGone) {
		t.Errorf("BodyRange err = %v, want ErrSpoolGone", err)
	}
}

// TestSweepStaleSpools removes what a crashed process left behind and keeps
// both fresh spool directories and unrelated temp entries.
func TestSweepStaleSpools(t *testing.T) {
	tmp := t.TempDir()
	stale := filepath.Join(tmp, spoolPrefix+"old")
	fresh := filepath.Join(tmp, spoolPrefix+"new")
	other := filepath.Join(tmp, "somebody-elses-dir")
	for _, d := range []string{stale, fresh, other} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	sweepStaleSpools(tmp, time.Now())

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the stale spool directory survived the sweep")
	}
	for _, d := range []string{fresh, other} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(d), err)
		}
	}
}
