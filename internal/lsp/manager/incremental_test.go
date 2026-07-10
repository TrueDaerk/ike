package manager

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"ike/internal/lsp"
	"ike/internal/lsp/protocol"
)

// ev is a helper running the diff and failing on "no change".
func ev(t *testing.T, old []string, new, enc string) protocol.TextDocumentContentChangeEvent {
	t.Helper()
	e, changed := incrementalEvent(old, new, enc)
	if !changed {
		t.Fatalf("expected a change for %q -> %q", old, new)
	}
	if e.Range == nil {
		t.Fatalf("incremental event must carry a range")
	}
	return e
}

func TestIncrementalSingleCharInsert(t *testing.T) {
	e := ev(t, []string{"hello world"}, "helloX world", protocol.EncodingUTF16)
	if e.Text != "X" {
		t.Fatalf("text = %q", e.Text)
	}
	if e.Range.Start != (protocol.Position{Line: 0, Character: 5}) || e.Range.End != e.Range.Start {
		t.Fatalf("range = %+v", e.Range)
	}
}

func TestIncrementalSingleCharDelete(t *testing.T) {
	e := ev(t, []string{"abc"}, "ac", protocol.EncodingUTF16)
	if e.Text != "" {
		t.Fatalf("text = %q", e.Text)
	}
	if e.Range.Start.Character != 1 || e.Range.End.Character != 2 {
		t.Fatalf("range = %+v", e.Range)
	}
}

func TestIncrementalMultiLineReplace(t *testing.T) {
	old := []string{"one", "two", "three"}
	e := ev(t, old, "one\nTWO-X\nthree", protocol.EncodingUTF16)
	if e.Range.Start.Line != 1 || e.Range.End.Line != 1 {
		t.Fatalf("range = %+v", e.Range)
	}
	if e.Text != "TWO-X" {
		t.Fatalf("text = %q", e.Text)
	}
}

func TestIncrementalNewlineInsertAndJoin(t *testing.T) {
	// Split a line.
	e := ev(t, []string{"ab"}, "a\nb", protocol.EncodingUTF16)
	if e.Text != "\n" || e.Range.Start.Character != 1 || e.Range.End.Character != 1 {
		t.Fatalf("split: text=%q range=%+v", e.Text, e.Range)
	}
	// Join two lines.
	e = ev(t, []string{"a", "b"}, "ab", protocol.EncodingUTF16)
	if e.Text != "" || e.Range.Start != (protocol.Position{Line: 0, Character: 1}) || e.Range.End != (protocol.Position{Line: 1, Character: 0}) {
		t.Fatalf("join: text=%q range=%+v", e.Text, e.Range)
	}
}

func TestIncrementalUTF16Offsets(t *testing.T) {
	// The emoji is one rune but two UTF-16 units; an insert after it must
	// report character 3 under UTF-16 and 2 under UTF-32.
	e := ev(t, []string{"a🙂b"}, "a🙂Xb", protocol.EncodingUTF16)
	if e.Range.Start.Character != 3 {
		t.Fatalf("utf-16 start = %+v", e.Range)
	}
	e = ev(t, []string{"a🙂b"}, "a🙂Xb", protocol.EncodingUTF32)
	if e.Range.Start.Character != 2 {
		t.Fatalf("utf-32 start = %+v", e.Range)
	}
}

func TestIncrementalRepeatedRunsAndCRLF(t *testing.T) {
	// Repeated characters: prefix wins, suffix must not overlap.
	e := ev(t, []string{"aaa"}, "aaaa", protocol.EncodingUTF16)
	if e.Text != "a" || e.Range.Start.Character != 3 || e.Range.End.Character != 3 {
		t.Fatalf("repeat: text=%q range=%+v", e.Text, e.Range)
	}
	// Embedded carriage returns are ordinary runes and survive the diff.
	e = ev(t, []string{"a\r", "b"}, "a\r\nXb", protocol.EncodingUTF16)
	if e.Text != "X" || e.Range.Start.Line != 1 {
		t.Fatalf("crlf: text=%q range=%+v", e.Text, e.Range)
	}
}

func TestIncrementalNoChange(t *testing.T) {
	if _, changed := incrementalEvent([]string{"same"}, "same", protocol.EncodingUTF16); changed {
		t.Fatal("identical text must not produce an event")
	}
}

// TestManagerIncrementalSync drives Change against a server negotiating
// incremental sync: events carry ranges, versions stay monotonic, and an
// unchanged text sends nothing.
func TestManagerIncrementalSync(t *testing.T) {
	didChanges := make(chan protocol.DidChangeTextDocumentParams, 8)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncIncremental, didChanges: didChanges}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	_ = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644)
	if err := m.Open(path, "go", "package main\n"); err != nil {
		t.Fatal(err)
	}

	recv := func() protocol.DidChangeTextDocumentParams {
		t.Helper()
		select {
		case p := <-didChanges:
			return p
		case <-time.After(3 * time.Second):
			t.Fatal("no didChange received")
			return protocol.DidChangeTextDocumentParams{}
		}
	}

	if err := m.Change(path, "package main\nX"); err != nil {
		t.Fatal(err)
	}
	p1 := recv()
	if len(p1.ContentChanges) != 1 || p1.ContentChanges[0].Range == nil || p1.ContentChanges[0].Text != "X" {
		t.Fatalf("first change = %+v", p1.ContentChanges)
	}

	// Unchanged text: nothing goes out, the version does not burn.
	if err := m.Change(path, "package main\nX"); err != nil {
		t.Fatal(err)
	}
	if err := m.Change(path, "package main\nXY"); err != nil {
		t.Fatal(err)
	}
	p2 := recv()
	if p2.TextDocument.Version != p1.TextDocument.Version+1 {
		t.Fatalf("versions must stay monotonic without gaps for silent no-ops: %d -> %d", p1.TextDocument.Version, p2.TextDocument.Version)
	}
	if p2.ContentChanges[0].Text != "Y" {
		t.Fatalf("second change = %+v", p2.ContentChanges)
	}
	select {
	case extra := <-didChanges:
		t.Fatalf("no-op change must not notify, got %+v", extra)
	default:
	}
}

// TestManagerFullSyncFallback: a full-sync server keeps receiving the whole
// document (rangeless events).
func TestManagerFullSyncFallback(t *testing.T) {
	didChanges := make(chan protocol.DidChangeTextDocumentParams, 2)
	spec := lsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{"go.mod"}}
	m := New(resolver(spec), fakeConnectorOpts(fakeOpts{syncKind: protocol.SyncFull, didChanges: didChanges}), Callbacks{})
	defer m.Shutdown()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := m.Open(path, "go", "abc"); err != nil {
		t.Fatal(err)
	}
	if err := m.Change(path, "abcd"); err != nil {
		t.Fatal(err)
	}
	select {
	case p := <-didChanges:
		if len(p.ContentChanges) != 1 || p.ContentChanges[0].Range != nil || p.ContentChanges[0].Text != "abcd" {
			t.Fatalf("full sync should send the whole document, got %+v", p.ContentChanges)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no didChange received")
	}
}
