package agenttrace

import (
	"os"
	"path/filepath"
	"testing"
)

const (
	promptLine = `{"type":"user","uuid":"u1","sessionId":"s1","cwd":"/w","timestamp":"2026-09-30T14:00:01.000Z","message":{"role":"user","content":"first"}}` + "\n"
	replyLine  = `{"type":"assistant","uuid":"a1","sessionId":"s1","cwd":"/w","timestamp":"2026-09-30T14:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"reply"}]}}` + "\n"
)

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// TestReaderConsumesOnlyAppendedLines: the reader keeps its byte offset,
// returns only what was added, and holds back a line the harness has not
// finished writing.
func TestReaderConsumesOnlyAppendedLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	r := NewReader(path)

	// Missing file: nothing yet, no error.
	if n, err := r.Update(); n != 0 || err != nil {
		t.Fatalf("missing file: n=%d err=%v", n, err)
	}

	half := replyLine[:40]
	appendFile(t, path, promptLine+half)
	n, err := r.Update()
	if err != nil || n != 1 {
		t.Fatalf("first update: n=%d err=%v", n, err)
	}
	if got := r.Session().Events; len(got) != 1 || got[0].Text != "first" {
		t.Fatalf("events after half line = %+v", got)
	}
	if r.Offset() != int64(len(promptLine)) {
		t.Errorf("offset = %d, want %d (half line held back)", r.Offset(), len(promptLine))
	}

	// Nothing appended: no change.
	if n, err := r.Update(); n != 0 || err != nil {
		t.Fatalf("idle update: n=%d err=%v", n, err)
	}

	appendFile(t, path, replyLine[40:])
	if n, err := r.Update(); n != 1 || err != nil {
		t.Fatalf("second update: n=%d err=%v", n, err)
	}
	got := r.Session().Events
	if len(got) != 2 || got[1].Text != "reply" || got[1].Turn != 1 {
		t.Fatalf("events after completion = %+v", got)
	}
	if r.Offset() != int64(len(promptLine)+len(replyLine)) {
		t.Errorf("offset = %d", r.Offset())
	}
}

// TestReaderRestartsOnTruncation: a file that shrank is a different
// transcript; parse it from the top with a fresh session.
func TestReaderRestartsOnTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	r := NewReader(path)
	appendFile(t, path, promptLine+replyLine)
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(promptLine), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Update(); err != nil || n != 1 {
		t.Fatalf("after truncation: n=%d err=%v", n, err)
	}
	if got := r.Session().Events; len(got) != 1 || r.Offset() != int64(len(promptLine)) {
		t.Errorf("events = %+v offset = %d", got, r.Offset())
	}
}

// TestReaderResolvesEditsAgainstCurrentFile: the file cache is per Update,
// so a later batch sees the file as it is then.
func TestReaderResolvesEditsAgainstCurrentFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "x.go")
	path := filepath.Join(dir, "s1.jsonl")
	if err := os.WriteFile(target, []byte("a\nb\nNEW\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	edit := func(id, needle string) string {
		return `{"type":"assistant","uuid":"` + id + `","sessionId":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id + `","name":"Edit","input":{"file_path":"` + target + `","old_string":"OLD","new_string":"` + needle + `"}}]}}` + "\n"
	}
	r := NewReader(path)
	appendFile(t, path, edit("e1", "NEW"))
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("NEW\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appendFile(t, path, edit("e2", "NEW"))
	if _, err := r.Update(); err != nil {
		t.Fatal(err)
	}
	files := r.Session().Files()
	if len(files) != 2 || files[0].Line != 3 || files[1].Line != 1 {
		t.Errorf("files = %+v", files)
	}
}

// TestReaderPicksUpReplacedTranscript (#2857): a transcript renamed over
// the tailed path — even one of the very same size — and one truncated and
// rewritten past the old size are both parsed from the start again.
func TestReaderPicksUpReplacedTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.jsonl")
	appendFile(t, path, promptLine)
	r := NewReader(path)
	if n, _ := r.Update(); n != 1 {
		t.Fatalf("first read = %d", n)
	}
	rev := r.Revision()

	// Same size, other content, renamed over the path.
	other := `{"type":"user","uuid":"u9","sessionId":"s1","cwd":"/w","timestamp":"2026-09-30T14:00:01.000Z","message":{"role":"user","content":"other"}}` + "\n"
	if len(other) != len(promptLine) {
		t.Fatalf("fixture sizes differ: %d vs %d", len(other), len(promptLine))
	}
	tmp := filepath.Join(dir, "s1.jsonl.tmp")
	appendFile(t, tmp, other)
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Update(); n != 1 || err != nil {
		t.Fatalf("replaced read = %d, %v", n, err)
	}
	if s := r.Session(); len(s.Events) != 1 || s.Events[0].Text != "other" {
		t.Fatalf("events after replace = %+v", s.Events)
	}
	if r.Revision() == rev {
		t.Fatal("a replaced transcript must change the revision")
	}

	// Truncated in place and rewritten longer: the byte before the offset is
	// no longer the line end the reader stopped behind.
	long := `{"type":"user","uuid":"u7","sessionId":"s1","cwd":"/w","timestamp":"2026-09-30T14:00:01.000Z","message":{"role":"user","content":"rewritten prompt"}}` + "\n"
	if err := os.WriteFile(path, []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := r.Update(); n != 1 || err != nil {
		t.Fatalf("rewritten read = %d, %v", n, err)
	}
	if s := r.Session(); len(s.Events) != 1 || s.Events[0].Text != "rewritten prompt" {
		t.Fatalf("events after rewrite = %+v", s.Events)
	}
}

// TestReaderCompletesLineWrittenInPieces (#2857): a large line arriving in
// several writes is held back until its newline lands, then read whole; the
// revision moves only with a read that changed the session.
func TestReaderCompletesLineWrittenInPieces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	appendFile(t, path, promptLine)
	r := NewReader(path)
	if n, rev, err := r.Read(nil); n != 1 || err != nil || rev != r.Revision() {
		t.Fatalf("first read = %d, %d, %v", n, rev, err)
	}
	rev := r.Revision()
	const piece = 16
	body := replyLine[:len(replyLine)-1] // everything but the newline
	for i := 0; i < len(body); i += piece {
		appendFile(t, path, body[i:min(i+piece, len(body))])
		if n, _ := r.Update(); n != 0 {
			t.Fatalf("partial line counted after %d bytes", i+piece)
		}
		if r.Revision() != rev {
			t.Fatal("a read without changes moved the revision")
		}
	}
	appendFile(t, path, "\n")
	var events int
	n, newRev, err := r.Read(func(s *Session) { events = len(s.Events) })
	if n != 1 || err != nil || events != 2 || newRev == rev {
		t.Fatalf("completed line: n=%d events=%d rev %d→%d err=%v", n, events, rev, newRev, err)
	}
}
