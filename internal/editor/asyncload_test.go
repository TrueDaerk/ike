package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"ike/internal/largefile"
)

// bigFixture writes a file just above asyncLoadBytes: ndjson lines of 500
// bytes, enough of them to cross the threshold.
func bigFixture(t *testing.T) string {
	t.Helper()
	n := asyncLoadBytes/500 + 100
	return ndjsonFixture(t, t.TempDir(), n, 500)
}

// runLoad drains the parked load command and returns its message.
func runLoad(t *testing.T, m *Model) tea.Msg {
	t.Helper()
	cmd := m.TakeLoadCmd()
	if cmd == nil {
		t.Fatal("Load above the threshold must park a command")
	}
	return cmd()
}

// TestLoadAboveThresholdParksCommandWithoutReading guards the core of #2996:
// a big file's Load returns at once with an empty read-only placeholder and
// a parked command; the file is not read on the caller. The fixture is made
// unreadable after the stat, so a synchronous read would have failed Load.
func TestLoadAboveThresholdParksCommandWithoutReading(t *testing.T) {
	path := bigFixture(t)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	m := New()
	m.SetSize(80, 20)
	if err := m.Load(path); err != nil {
		t.Fatalf("Load must not read the file synchronously: %v", err)
	}
	if !m.Loading() || !m.HasFile() || m.Path() != path {
		t.Fatalf("the placeholder must be installed under the path: loading=%v path=%q", m.Loading(), m.Path())
	}
	if m.buf.LineCount() != 1 || m.buf.Line(0) != "" {
		t.Fatal("the placeholder must be empty")
	}
	if !m.ReadOnly() {
		t.Fatal("the placeholder must be read-only until the text lands")
	}
	if !m.LargeFile() {
		t.Fatal("the stat size already crosses the default cliff: the flag must be provisional-on")
	}
	msg := runLoad(t, &m)
	lm, ok := msg.(LoadMsg)
	if !ok || lm.Err == nil {
		t.Fatalf("the read of an unreadable file must land as a LoadMsg error, got %#v", msg)
	}
	m, _ = m.Update(lm)
	if m.Loading() || m.ReadOnly() {
		t.Fatal("a failed load must clear the pending state and the read-only lock")
	}
	if !strings.HasPrefix(m.cmdMsg, "E: ") {
		t.Fatalf("the error must reach the ex line, got %q", m.cmdMsg)
	}
}

// TestAsyncLoadLandsLikeSyncLoad: after the landing the document is
// indistinguishable from a synchronous load — text, line endings, encoding,
// size, large-file flag, undo hash, cursor and mode.
func TestAsyncLoadLandsLikeSyncLoad(t *testing.T) {
	path := bigFixture(t)
	async := New()
	async.SetSize(80, 20)
	if err := async.Load(path); err != nil {
		t.Fatal(err)
	}
	msg := runLoad(t, &async)
	lm, ok := msg.(LoadMsg)
	if !ok || lm.Err != nil {
		t.Fatalf("expected a successful LoadMsg, got %#v", msg)
	}
	var cmd tea.Cmd
	async, cmd = async.Update(lm)
	if cmd == nil {
		t.Fatal("the landing must schedule the parse")
	}
	sync := New()
	sync.SetSize(80, 20)
	if err := sync.LoadSync(path); err != nil {
		t.Fatal(err)
	}
	if async.Loading() || async.ReadOnly() {
		t.Fatal("the landed document must be writable and no longer loading")
	}
	if a, s := async.buf.Lines(), sync.buf.Lines(); len(a) != len(s) || a[0] != s[0] || a[len(a)-1] != s[len(s)-1] {
		t.Fatalf("buffers differ: %d vs %d lines", len(a), len(s))
	}
	if async.eol != sync.eol || async.enc != sync.enc || async.mixedEOL != sync.mixedEOL {
		t.Fatal("line-ending / encoding detection must match the synchronous load")
	}
	if async.largeFile != sync.largeFile || async.docBytes != sync.docBytes || async.diskHash != sync.diskHash {
		t.Fatalf("size state differs: large %v/%v bytes %d/%d hash %q/%q",
			async.largeFile, sync.largeFile, async.docBytes, sync.docBytes, async.diskHash, sync.diskHash)
	}
	if async.cursor != sync.cursor || async.mode != sync.mode || async.dirty {
		t.Fatal("cursor, mode and dirty state must match the synchronous load")
	}
}

// TestAsyncLoadVanishedFile: a file deleted between the stat and the read
// lands as an error, leaving the empty placeholder with the message.
func TestAsyncLoadVanishedFile(t *testing.T) {
	path := bigFixture(t)
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	cmd := m.TakeLoadCmd()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	lm, ok := cmd().(LoadMsg)
	if !ok || !os.IsNotExist(lm.Err) {
		t.Fatalf("expected a not-exist LoadMsg, got %#v", lm)
	}
	m, _ = m.Update(lm)
	if m.Loading() || m.buf.LineCount() != 1 || !strings.Contains(m.cmdMsg, "E: ") {
		t.Fatalf("the placeholder must stay with the error on the ex line: loading=%v msg=%q", m.Loading(), m.cmdMsg)
	}
}

// TestAsyncLoadDecodeError: bytes that fail the decoder land as an error,
// exactly as LoadSync would have returned it.
func TestAsyncLoadDecodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.txt")
	data := make([]byte, asyncLoadBytes+16)
	for i := range data {
		data[i] = 0xff // never valid UTF-8
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	lm, ok := runLoad(t, &m).(LoadMsg)
	if !ok || lm.Err == nil {
		t.Fatalf("expected a decode error, got %#v", lm)
	}
	sync := New()
	if err := sync.LoadSync(path); err == nil || err.Error() != lm.Err.Error() {
		t.Fatalf("the async error must be the synchronous one: %v vs %v", lm.Err, err)
	}
}

// TestAsyncLoadCancelledOnClose: closing the tab stops the read — the
// command yields nil — and a result for an older generation is dropped.
func TestAsyncLoadCancelledOnClose(t *testing.T) {
	path := bigFixture(t)
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	cmd := m.TakeLoadCmd()
	m.Close()
	if m.Loading() {
		t.Fatal("Close must drop the pending load")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("a cancelled read must yield nil, got %#v", msg)
	}
	// A result that raced the cancellation is dropped by the generation.
	m2 := New()
	if err := m2.Load(path); err != nil {
		t.Fatal(err)
	}
	stale := runLoad(t, &m2).(LoadMsg)
	if err := m2.Load(path); err != nil { // a newer load supersedes
		t.Fatal(err)
	}
	m2, _ = m2.Update(stale)
	if !m2.Loading() || m2.buf.LineCount() != 1 {
		t.Fatal("a stale landing must not install its document")
	}
}

// TestAsyncLoadKeysWhileLoading: the placeholder takes keys — a tab switch
// is the app's — but refuses edits and writes, which would otherwise land in
// the empty text or truncate the file.
func TestAsyncLoadKeysWhileLoading(t *testing.T) {
	path := bigFixture(t)
	m := New()
	m.SetSize(80, 20)
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if m.mode != Normal {
		t.Fatal("insert mode must be refused while the text is on its way")
	}
	if err := m.saveAs(path); err == nil {
		t.Fatal("a write while loading must be refused")
	}
	if st, _ := os.Stat(path); st.Size() < asyncLoadBytes {
		t.Fatal("the file must not have been truncated")
	}
}

// TestAsyncLoadParksCaretRestore: a caret / framing restore requested while
// the text is on its way (a deferred tab's saved position) applies once it
// lands instead of clamping against the placeholder.
func TestAsyncLoadParksCaretRestore(t *testing.T) {
	path := bigFixture(t)
	m := New()
	m.SetSize(80, 20)
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	m.SetCursor(120, 7)
	m.SetScroll(100, 0)
	lm := runLoad(t, &m).(LoadMsg)
	m, _ = m.Update(lm)
	if m.cursor.Line != 120 || m.cursor.Col != 7 {
		t.Fatalf("the parked caret must apply after the landing, got %+v", m.cursor)
	}
	if m.ScrollTop() != 100 {
		t.Fatalf("the parked framing must apply after the landing, got top %d", m.ScrollTop())
	}
}

// TestSmallFileLoadsSynchronously: below the threshold Load keeps reading in
// place — no command, text present at once.
func TestSmallFileLoadsSynchronously(t *testing.T) {
	path := ndjsonFixture(t, t.TempDir(), 20, 100)
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if m.Loading() || m.TakeLoadCmd() != nil || m.buf.LineCount() != 20 || m.ReadOnly() {
		t.Fatal("a small file must load synchronously")
	}
}

// TestAsyncLoadDegradedFeaturesWhileLoading: the provisional large-file
// flag keeps every gated service off from the first frame, so nothing ships
// the placeholder to an LSP or diffs it against HEAD.
func TestAsyncLoadDegradedFeaturesWhileLoading(t *testing.T) {
	path := bigFixture(t)
	m := New()
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	if !m.FeatureOff(largefile.FeatureLSP) || !m.FeatureOff(largefile.FeatureVCS) {
		t.Fatal("the placeholder of a file past the cliff must be degraded from the start")
	}
}
