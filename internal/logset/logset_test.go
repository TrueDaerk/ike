package logset

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/largefile"
	"ike/internal/logline"
)

// logset_test.go covers rotation-set detection (numeric, dated and gz
// suffixes), the oldest-first ordering, and the merge: separator lines, gz
// members read decompressed, the large-file budgets cutting the oldest end,
// and the follow anchor on the newest member (#1996).

// write puts body into dir/name and stamps its mtime at the given age in
// minutes, so ordering tests do not depend on the filesystem's clock.
func write(t *testing.T, dir, name, body string, ageMin int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-time.Duration(ageMin) * time.Minute)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
	return p
}

// writeGz puts body gzipped into dir/name.
func writeGz(t *testing.T, dir, name, body string, ageMin int) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return write(t, dir, name, buf.String(), ageMin)
}

func TestStem(t *testing.T) {
	cases := []struct {
		path string
		want string
		ok   bool
	}{
		{"/var/log/app.log", "/var/log/app.log", true},
		{"/var/log/app.log.1", "/var/log/app.log", true},
		{"/var/log/app.log.42", "/var/log/app.log", true},
		{"/var/log/app.log.2026-08-01", "/var/log/app.log", true},
		{"/var/log/app.log.20260801", "/var/log/app.log", true},
		{"/var/log/app.log.gz", "/var/log/app.log", true},
		{"/var/log/app.log.2.gz", "/var/log/app.log", true},
		{"/var/log/app.log.2.GZ", "/var/log/app.log", true},
		{"app.log.1", "app.log", true},
		// No inner extension left: not a log set, the #1745 rule.
		{"/tmp/backup.1", "", false},
		{"/tmp/notes.gz", "", false},
		{"/tmp/README", "", false},
		// A suffix that is neither a number nor a date stays part of the name.
		{"/var/log/app.log.bak", "/var/log/app.log.bak", true},
	}
	for _, c := range cases {
		got, ok := Stem(c.path)
		if ok != c.ok || got != c.want {
			t.Errorf("Stem(%q) = %q/%v, want %q/%v", c.path, got, ok, c.want, c.ok)
		}
	}
}

func TestDetectNumericSuffixesOldestFirst(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", "live\n", 0)
	write(t, dir, "app.log.1", "one\n", 10)
	write(t, dir, "app.log.2.gz", "two\n", 20)
	write(t, dir, "app.log.10", "ten\n", 30)
	// Not members: another log, a suffix that is no rotation suffix, a dir.
	write(t, dir, "other.log", "x\n", 0)
	write(t, dir, "app.log.bak", "x\n", 0)
	if err := os.Mkdir(filepath.Join(dir, "app.log.9"), 0o755); err != nil {
		t.Fatal(err)
	}

	set, ok := Detect(filepath.Join(dir, "app.log.1"))
	if !ok {
		t.Fatal("a rotated member must resolve its set")
	}
	if !set.Rotated() {
		t.Fatal("four members are a rotated set")
	}
	want := []string{"app.log.10", "app.log.2.gz", "app.log.1", "app.log"}
	if got := set.Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v (oldest first)", got, want)
	}
	newest, _ := set.Newest()
	if newest.Name != "app.log" || newest.Kind != Live || newest.Gz {
		t.Fatalf("newest = %+v, want the live log", newest)
	}
	if set.Stem != filepath.Join(dir, "app.log") {
		t.Fatalf("stem = %q", set.Stem)
	}
}

func TestDetectDateSuffixesOldestFirst(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", "live\n", 0)
	write(t, dir, "app.log.2026-08-01", "aug 1\n", 0)
	write(t, dir, "app.log.20260730.gz", "jul 30\n", 0)
	write(t, dir, "app.log.2026-07-31", "jul 31\n", 0)

	set, ok := Detect(filepath.Join(dir, "app.log"))
	if !ok {
		t.Fatal("Detect must find the dated set")
	}
	want := []string{"app.log.20260730.gz", "app.log.2026-07-31", "app.log.2026-08-01", "app.log"}
	if got := set.Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestDetectMixedSuffixesOrderByModTime: a directory mixing the two rotation
// spellings has no suffix order to read, so the modification times decide —
// with the live log still last.
func TestDetectMixedSuffixesOrderByModTime(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", "live\n", 0)
	write(t, dir, "app.log.1", "newer\n", 5)
	write(t, dir, "app.log.2026-07-31", "older\n", 60)

	set, ok := Detect(filepath.Join(dir, "app.log"))
	if !ok {
		t.Fatal("Detect must find the mixed set")
	}
	want := []string{"app.log.2026-07-31", "app.log.1", "app.log"}
	if got := set.Names(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// TestDetectSingleFileIsNoRotatedSet: an unrotated log resolves to a set of
// one, which callers refuse to merge.
func TestDetectSingleFileIsNoRotatedSet(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "app.log", "live\n", 0)
	set, ok := Detect(p)
	if !ok || len(set.Members) != 1 {
		t.Fatalf("Detect = %+v/%v, want the single member", set.Names(), ok)
	}
	if set.Rotated() {
		t.Fatal("one member is not a rotated set")
	}
	if _, ok := Detect(filepath.Join(dir, "backup.1")); ok {
		t.Fatal("a name with no inner extension is no set member")
	}
}

func TestMergeOrdersMembersWithOriginSeparators(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", "live one\nlive two\n", 0)
	write(t, dir, "app.log.1", "yesterday\n", 10)
	writeGz(t, dir, "app.log.2.gz", "day before\n", 20)

	set, ok := Detect(filepath.Join(dir, "app.log"))
	if !ok {
		t.Fatal("setup: Detect")
	}
	merged, err := Merge(set, largefile.Limits{MaxBytes: 1 << 20, MaxLines: 1000})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		logline.OriginLine("app.log.2.gz"),
		"day before",
		logline.OriginLine("app.log.1"),
		"yesterday",
		logline.OriginLine("app.log"),
		"live one",
		"live two",
		"",
	}, "\n")
	if merged.Text != want {
		t.Fatalf("merged text =\n%q\nwant\n%q", merged.Text, want)
	}
	if merged.Truncated || merged.Omitted != 0 || len(merged.Failed) != 0 {
		t.Fatalf("a set well under the caps must merge whole: %+v", merged)
	}
	// Every region is discoverable: its separator line and its own length.
	wantRegions := []Region{
		{Name: "app.log.2.gz", Line: 0, Lines: 1},
		{Name: "app.log.1", Line: 2, Lines: 1},
		{Name: "app.log", Line: 4, Lines: 2},
	}
	if len(merged.Regions) != len(wantRegions) {
		t.Fatalf("regions = %+v", merged.Regions)
	}
	for i, want := range wantRegions {
		if merged.Regions[i] != want {
			t.Fatalf("region %d = %+v, want %+v", i, merged.Regions[i], want)
		}
	}
	// The follow anchor points at the newest member's end.
	if merged.Tail != int64(len("live one\nlive two\n")) || !merged.TailTerm {
		t.Fatalf("tail anchor = %d/%v", merged.Tail, merged.TailTerm)
	}
	// The separators are recognizable again — that is what styles them.
	for _, line := range strings.Split(merged.Text, "\n") {
		if name, ok := logline.OriginName(line); ok && !strings.HasPrefix(name, "app.log") {
			t.Fatalf("unexpected origin line %q", line)
		}
	}
}

// TestMergeUnterminatedNewestMember: only the newest member may end mid-line —
// that is the tail a follow append continues in place. An older one gets its
// terminator so the next separator starts on its own line.
func TestMergeUnterminatedNewestMember(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", "partial", 0)
	write(t, dir, "app.log.1", "old no newline", 10)

	set, _ := Detect(filepath.Join(dir, "app.log"))
	merged, err := Merge(set, largefile.Limits{MaxBytes: 1 << 20, MaxLines: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(merged.Text, "partial") {
		t.Fatalf("the newest member's tail must stay unterminated: %q", merged.Text)
	}
	if !strings.Contains(merged.Text, "old no newline\n"+logline.OriginLine("app.log")) {
		t.Fatalf("an older member must be terminated before the next separator: %q", merged.Text)
	}
	if merged.TailTerm {
		t.Fatal("an unterminated newest member must not report a terminated anchor")
	}
	if merged.Tail != int64(len("partial")) {
		t.Fatalf("tail = %d", merged.Tail)
	}
}

// TestMergeGzMemberDecompressed: a compressed member contributes its text, not
// its bytes — and a corrupt one is reported instead of failing the merge.
func TestMergeGzMemberDecompressed(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", "live\n", 0)
	writeGz(t, dir, "app.log.1.gz", "compressed line\n", 10)
	write(t, dir, "app.log.2.gz", "not gzip at all\n", 20) // corrupt member

	set, _ := Detect(filepath.Join(dir, "app.log"))
	merged, err := Merge(set, largefile.Limits{MaxBytes: 1 << 20, MaxLines: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(merged.Text, "compressed line") {
		t.Fatalf("a gz member must be included decompressed: %q", merged.Text)
	}
	if strings.Contains(merged.Text, "not gzip") {
		t.Fatal("a corrupt gz member must not leak its bytes into the buffer")
	}
	if len(merged.Failed) != 1 || merged.Failed[0] != "app.log.2.gz" {
		t.Fatalf("failed = %v, want the corrupt member", merged.Failed)
	}
	if len(merged.Regions) != 2 {
		t.Fatalf("regions = %+v, want the two readable members", merged.Regions)
	}
}

// TestMergeGzNewestMemberHasNoFollowAnchor: a compressed newest member has no
// byte offset an append could resume from, so the merge offers none.
func TestMergeGzNewestMemberHasNoFollowAnchor(t *testing.T) {
	dir := t.TempDir()
	writeGz(t, dir, "app.log.gz", "archived\n", 0)
	writeGz(t, dir, "app.log.1.gz", "older\n", 10)

	set, ok := Detect(filepath.Join(dir, "app.log.gz"))
	if !ok || len(set.Members) != 2 {
		t.Fatalf("setup: %v/%v", set.Names(), ok)
	}
	merged, err := Merge(set, largefile.Limits{MaxBytes: 1 << 20, MaxLines: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Tail != 0 || merged.TailTerm {
		t.Fatalf("anchor = %d/%v, want none", merged.Tail, merged.TailTerm)
	}
}

// TestMergeByteCapDropsOldestMembers: the budget is filled from the newest
// member backwards, so what a cap costs is the oldest end of the timeline.
func TestMergeByteCapDropsOldestMembers(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", strings.Repeat("live\n", 20), 0)
	write(t, dir, "app.log.1", strings.Repeat("one\n", 20), 10)
	write(t, dir, "app.log.2", strings.Repeat("two\n", 20), 20)

	set, _ := Detect(filepath.Join(dir, "app.log"))
	merged, err := Merge(set, largefile.Limits{MaxBytes: 150, MaxLines: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(merged.Text)) > 150 {
		t.Fatalf("merged text is %d bytes, want at most the 150-byte cap", len(merged.Text))
	}
	if !strings.Contains(merged.Text, "live") {
		t.Fatalf("the newest member must survive the cap: %q", merged.Text)
	}
	if strings.Contains(merged.Text, "two") {
		t.Fatalf("the oldest member must be dropped first: %q", merged.Text)
	}
	if !merged.Truncated || merged.Omitted == 0 {
		t.Fatalf("a cut timeline must say so: %+v", merged)
	}
	// The newest member still ends at the file's last byte, so follow works.
	if merged.Tail != int64(len(strings.Repeat("live\n", 20))) {
		t.Fatalf("tail = %d", merged.Tail)
	}
}

// TestMergeLineCapCutsFromTheFront: the other large-file threshold, applied
// the same way — a region keeps its end, the part next to the newer file.
func TestMergeLineCapCutsFromTheFront(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log", "a\nb\nc\nd\ne\n", 0)
	write(t, dir, "app.log.1", "old\n", 10)

	set, _ := Detect(filepath.Join(dir, "app.log"))
	merged, err := Merge(set, largefile.Limits{MaxBytes: 1 << 20, MaxLines: 3})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(merged.Text, "\n"); n > 3 {
		t.Fatalf("merged holds %d lines, want at most 3: %q", n, merged.Text)
	}
	if !strings.Contains(merged.Text, "e\n") || strings.Contains(merged.Text, "a\n") {
		t.Fatalf("the newest lines must survive: %q", merged.Text)
	}
	if !merged.Truncated {
		t.Fatal("a cut timeline must say so")
	}
	if merged.Tail != int64(len("a\nb\nc\nd\ne\n")) {
		t.Fatalf("tail = %d, a front cut must not move the anchor", merged.Tail)
	}
}

// TestMergeNoReadableMemberErrors: only a set yielding nothing at all fails.
func TestMergeNoReadableMemberErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.log.gz", "not gzip\n", 0)
	write(t, dir, "app.log.1.gz", "not gzip either\n", 10)

	set, _ := Detect(filepath.Join(dir, "app.log.gz"))
	if _, err := Merge(set, largefile.Limits{MaxBytes: 1 << 20, MaxLines: 100}); err == nil {
		t.Fatal("a set with no readable member must error")
	}
}

func TestTailLines(t *testing.T) {
	cases := []struct {
		text string
		max  int
		want string
		cut  bool
	}{
		{"a\nb\nc\n", 2, "b\nc\n", true},
		{"a\nb\nc\n", 3, "a\nb\nc\n", false},
		{"a\nb\nc\n", 1, "c\n", true},
		{"a\nb", 1, "b", true},
		{"", 5, "", false},
		{"a\n", 0, "", true},
	}
	for _, c := range cases {
		got, cut := tailLines(c.text, c.max)
		if got != c.want || cut != c.cut {
			t.Errorf("tailLines(%q, %d) = %q/%v, want %q/%v", c.text, c.max, got, cut, c.want, c.cut)
		}
	}
}
