package lsp

// traitrename_test.go covers the PHP trait-scope rename complement (#2672)
// at the bridge level, against a fake Intelephense over the #2671 fixture
// (trait A calling $this->abc() and $this->fromC(), class B declaring abc
// and using A and C, trait C declaring fromC and calling $this->abc()):
//
//   - a server rename of abc inside B only is extended with the calls in A
//     and C, announced in the prompt, previewed, applied once per
//     occurrence, and reported to telemetry;
//   - a server refusing prepareRename inside A lets the index rename fromC
//     itself — the declaration in C and the call in A — after the preview
//     was confirmed, and applies nothing when the preview is dropped;
//   - an ambiguous target (two unrelated consumers declaring abc
//     differently) is refused, naming both;
//   - the prompt validator rejects non-identifiers;
//   - php.trait_index = false keeps today's "cannot rename here".

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
	"ike/internal/phpindex"
)

// renameB2PHP is a second, unrelated consumer of A declaring abc with a
// different signature — the ambiguous target.
const renameB2PHP = `<?php

namespace App\Models;

use App\Traits\A;

class B2
{
    use A;

    public function abc(): void
    {
    }
}
`

// phpRenameConnector dials an in-memory server advertising rename and
// prepareRename, answering prepareRename with prepare() (nil refuses) and
// rename with edit().
func phpRenameConnector(prepare func() *protocol.Range, edit func() protocol.WorkspaceEdit) manager.Connector {
	return func(spec ilsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		cr, sw := io.Pipe()
		sr, cw := io.Pipe()
		connCh := make(chan *jsonrpc.Conn, 1)
		var srv *jsonrpc.Conn
		srvConn := jsonrpc.NewConn(pipeRWC{Reader: sr, Writer: sw}, jsonrpc.Handler{
			Request: func(id jsonrpc.ID, method string, params json.RawMessage) {
				if srv == nil {
					srv = <-connCh
				}
				switch method {
				case "initialize":
					_ = srv.Respond(id, protocol.InitializeResult{Capabilities: protocol.ServerCapabilities{
						TextDocumentSync: json.RawMessage(`1`),
						RenameProvider:   json.RawMessage(`{"prepareProvider":true}`),
					}}, nil)
				case "textDocument/prepareRename":
					if r := prepare(); r != nil {
						_ = srv.Respond(id, r, nil)
					} else {
						_ = srv.Respond(id, nil, nil)
					}
				case "textDocument/rename":
					_ = srv.Respond(id, edit(), nil)
				default:
					_ = srv.Respond(id, nil, nil)
				}
			},
		})
		connCh <- srvConn
		conn := jsonrpc.NewConn(pipeRWC{Reader: cr, Writer: cw}, handler)
		return client.New(conn), func() { conn.Close(); srvConn.Close() }, nil, nil
	}
}

// renameTelemetry records what the index seam reported as applied.
type renameTelemetry struct {
	side  host.TraitRenameSide
	edits int
	calls int
}

// phpRenameBridge wires a bridge over the references fixture (plus extra
// files): the fake server, a host carrying the real declaration index with
// the master switch as given, and the named document open. The returned
// recorder sees every applied rename the index took part in.
func phpRenameBridge(t *testing.T, root, rel string, enabled bool, prepare func() *protocol.Range, edit func() protocol.WorkspaceEdit) (*bridge, host.API, chan tea.Msg, string, string, *renameTelemetry) {
	t.Helper()
	idx := phpindex.New(root, phpindex.Options{Enabled: enabled, ParentDepth: 3, MaxFiles: 1000})
	if !idx.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	if enabled {
		for start := time.Now(); !idx.ScanDone(); {
			if time.Since(start) > 10*time.Second {
				t.Fatal("the PHP index scan did not finish")
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	view := phpindex.NewHostView(idx)
	rec := &renameTelemetry{}
	view.SetRenameTelemetry(func(side host.TraitRenameSide, edits int) {
		rec.side, rec.edits, rec.calls = side, edits, rec.calls+1
	})

	msgs := make(chan tea.Msg, 16)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	h.SetTraitIndex(&countingIndex{inner: view})

	spec := ilsp.ServerSpec{Language: "php", Command: "fake", RootMarkers: []string{"composer.json"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) { return spec, lang == spec.Language }
	b := &bridge{h: h, mgr: manager.New(resolve, phpRenameConnector(prepare, edit), manager.Callbacks{})}
	t.Cleanup(b.mgr.Shutdown)

	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.mgr.Open(path, "php", string(data)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return b, h, msgs, path, string(data), rec
}

// rangeAt is a one-line LSP range of length n at (line, col).
func rangeAt(line, col, n int) *protocol.Range {
	return &protocol.Range{
		Start: protocol.Position{Line: line, Character: col},
		End:   protocol.Position{Line: line, Character: col + n},
	}
}

// bOnlyEdit is the server's rename of abc → xyz confined to class B: the
// declaration and the two calls in twice().
func bOnlyEdit(bPath string) protocol.WorkspaceEdit {
	declLine, declCol := posInText(refsClassBPHP, "public function abc", 16)
	callLine, callCol := posInText(refsClassBPHP, "$this->abc()", 7)
	edits := []protocol.TextEdit{
		wholeWordEdit(declLine, declCol, 3, "xyz"),
		wholeWordEdit(callLine, callCol, 3, "xyz"),
		wholeWordEdit(callLine, callCol+15, 3, "xyz"),
	}
	return protocol.WorkspaceEdit{Changes: map[string][]protocol.TextEdit{protocol.PathToURI(bPath): edits}}
}

// posInText is posIn without the test handle, for fixture constants.
func posInText(content, needle string, within int) (int, int) {
	for i, line := range strings.Split(content, "\n") {
		if c := strings.Index(line, needle); c >= 0 {
			return i, len([]rune(line[:c])) + within
		}
	}
	return -1, -1
}

func previewByBase(files []ilsp.RenamePreviewFile) map[string]ilsp.RenamePreviewFile {
	out := map[string]ilsp.RenamePreviewFile{}
	for _, f := range files {
		out[filepath.Base(f.Path)] = f
	}
	return out
}

// drain asserts no further message arrives for a moment.
func drain(t *testing.T, msgs chan tea.Msg, what string) {
	t.Helper()
	select {
	case extra := <-msgs:
		t.Fatalf("%s: unexpected message %#v", what, extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestTraitRenameExtendsServerRename: the server renames abc inside B
// only; the prompt announces the two occurrences in A and C, the preview
// lists all three files, confirming rewrites A and C on disk (once each)
// and B through the buffer, and telemetry sees the extended path.
func TestTraitRenameExtendsServerRename(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	bPath := filepath.Join(root, "src", "Models", "B.php")
	declLine, declCol := posInText(refsClassBPHP, "public function abc", 16)
	b, h, msgs, path, _, rec := phpRenameBridge(t, root, "src/Models/B.php", true,
		func() *protocol.Range { return rangeAt(declLine, declCol, 3) },
		func() protocol.WorkspaceEdit { return bOnlyEdit(bPath) })
	b.setCur(path, declLine, declCol+1)
	b.rename(h)

	prompt, ok := recvMsg(t, msgs, "rename prompt").(ilsp.RenamePromptMsg)
	if !ok {
		t.Fatalf("expected a RenamePromptMsg")
	}
	if prompt.Placeholder != "abc" {
		t.Fatalf("placeholder = %q", prompt.Placeholder)
	}
	if prompt.Note != "+ 2 occurrences in traits A, C" {
		t.Fatalf("note = %q, want the trait occurrences announced", prompt.Note)
	}
	if prompt.Validate == nil || prompt.Validate("1abc") == "" || prompt.Validate("a-b") == "" || prompt.Validate("xyz") != "" {
		t.Fatalf("the prompt must validate PHP identifiers")
	}

	prompt.Apply("xyz")
	preview, ok := recvMsg(t, msgs, "rename preview").(ilsp.RenamePreviewMsg)
	if !ok {
		t.Fatalf("an extended rename must preview first")
	}
	if preview.OldName != "abc" || preview.NewName != "xyz" || len(preview.Files) != 3 {
		t.Fatalf("preview = %q → %q over %d files", preview.OldName, preview.NewName, len(preview.Files))
	}
	files := previewByBase(preview.Files)
	if f := files["A.php"]; f.Open || f.Edits != 1 || !strings.Contains(f.After, "$this->xyz();") || strings.Contains(f.After, "abc") {
		t.Fatalf("A preview = %+v", f)
	}
	if f := files["C.php"]; f.Open || f.Edits != 1 || !strings.Contains(f.After, "$this->xyz();") {
		t.Fatalf("C preview = %+v", f)
	}
	if f := files["B.php"]; !f.Open || f.Edits != 3 || strings.Count(f.After, "xyz") != 3 || strings.Contains(f.After, "abc(") {
		t.Fatalf("B preview = %+v", f)
	}
	// Nothing written while the preview is pending.
	aPath := filepath.Join(root, "src", "Traits", "A.php")
	if data, _ := os.ReadFile(aPath); !strings.Contains(string(data), "$this->abc();") {
		t.Fatal("a pending preview must not write")
	}

	preview.Apply()
	fm, ok := recvMsg(t, msgs, "buffer edits").(ilsp.FormatEditsMsg)
	if !ok || fm.Path != bPath || len(fm.Edits) != 3 {
		t.Fatalf("the open buffer must get its edits as one message, got %#v", fm)
	}
	status, ok := recvMsg(t, msgs, "summary").(ilsp.ServerStatusMsg)
	if !ok || status.Text != "renamed in 3 files" {
		t.Fatalf("summary = %#v", status)
	}
	for _, rel := range []string{"src/Traits/A.php", "src/Traits/C.php"} {
		data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if strings.Count(string(data), "$this->xyz();") != 1 || strings.Contains(string(data), "abc") {
			t.Fatalf("%s after rename:\n%s", rel, data)
		}
	}
	for start := time.Now(); rec.calls == 0 && time.Since(start) < 2*time.Second; {
		time.Sleep(5 * time.Millisecond)
	}
	if rec.calls != 1 || rec.side != host.TraitRenameExtend || rec.edits != 2 {
		t.Fatalf("telemetry = %+v, want one extended rename with 2 edits", *rec)
	}
}

// TestTraitRenameDeduplicatesServerEdits: a server that already edits the
// call inside A gets no second edit there — the index's row is skipped.
func TestTraitRenameDeduplicatesServerEdits(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	bPath := filepath.Join(root, "src", "Models", "B.php")
	aPath := filepath.Join(root, "src", "Traits", "A.php")
	declLine, declCol := posInText(refsClassBPHP, "public function abc", 16)
	aLine, aCol := posInText(traitAPHP, "$this->abc()", 7)
	b, h, msgs, path, _, _ := phpRenameBridge(t, root, "src/Models/B.php", true,
		func() *protocol.Range { return rangeAt(declLine, declCol, 3) },
		func() protocol.WorkspaceEdit {
			we := bOnlyEdit(bPath)
			we.Changes[protocol.PathToURI(aPath)] = []protocol.TextEdit{wholeWordEdit(aLine, aCol, 3, "xyz")}
			return we
		})
	b.setCur(path, declLine, declCol+1)
	b.rename(h)
	prompt := recvMsg(t, msgs, "rename prompt").(ilsp.RenamePromptMsg)
	prompt.Apply("xyz")
	preview := recvMsg(t, msgs, "rename preview").(ilsp.RenamePreviewMsg)
	files := previewByBase(preview.Files)
	if f := files["A.php"]; f.Edits != 1 || strings.Count(f.After, "xyz") != 1 {
		t.Fatalf("A must be edited once, preview = %+v", f)
	}
	if f := files["C.php"]; f.Edits != 1 {
		t.Fatalf("C must still get the index's edit, preview = %+v", f)
	}
}

// TestTraitRenameIndexDriven: the server refuses prepareRename inside A;
// the index renames fromC — the declaration in C and the call in A — after
// the preview is confirmed: A through the buffer, C on disk.
func TestTraitRenameIndexDriven(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, rec := phpRenameBridge(t, root, "src/Traits/A.php", true,
		func() *protocol.Range { return nil },
		func() protocol.WorkspaceEdit { return protocol.WorkspaceEdit{} })
	line, col := posIn(t, content, "$this->fromC()", 8)
	b.setCur(path, line, col)
	b.rename(h)

	prompt, ok := recvMsg(t, msgs, "rename prompt").(ilsp.RenamePromptMsg)
	if !ok {
		t.Fatalf("a refused position inside a trait body must prompt through the index")
	}
	if prompt.Placeholder != "fromC" || !strings.HasPrefix(prompt.Note, "via PHP trait index: 2 occurrences in 2 files") || prompt.Validate == nil {
		t.Fatalf("prompt = %+v", prompt)
	}
	prompt.Apply("fromD")
	preview, ok := recvMsg(t, msgs, "rename preview").(ilsp.RenamePreviewMsg)
	if !ok {
		t.Fatalf("an index-driven rename must preview first")
	}
	if preview.OldName != "fromC" || preview.NewName != "fromD" || len(preview.Files) != 2 {
		t.Fatalf("preview = %+v", preview)
	}
	files := previewByBase(preview.Files)
	if f := files["A.php"]; !f.Open || f.Edits != 1 || !strings.Contains(f.After, "$this->fromD();") {
		t.Fatalf("A preview = %+v", f)
	}
	if f := files["C.php"]; f.Open || f.Edits != 1 || !strings.Contains(f.After, "public function fromD(): ?int") {
		t.Fatalf("C preview = %+v", f)
	}

	preview.Apply()
	fm, ok := recvMsg(t, msgs, "buffer edits").(ilsp.FormatEditsMsg)
	if !ok || fm.Path != path || len(fm.Edits) != 1 || fm.Edits[0].Text != "fromD" {
		t.Fatalf("the open buffer must get its edit through the buffer, got %#v", fm)
	}
	status, ok := recvMsg(t, msgs, "summary").(ilsp.ServerStatusMsg)
	if !ok || status.Text != "renamed in 2 files" {
		t.Fatalf("summary = %#v", status)
	}
	data, _ := os.ReadFile(filepath.Join(root, "src", "Traits", "C.php"))
	if !strings.Contains(string(data), "public function fromD(): ?int") || strings.Contains(string(data), "fromC") {
		t.Fatalf("C after rename:\n%s", data)
	}
	for start := time.Now(); rec.calls == 0 && time.Since(start) < 2*time.Second; {
		time.Sleep(5 * time.Millisecond)
	}
	if rec.calls != 1 || rec.side != host.TraitRenameIndex || rec.edits != 2 {
		t.Fatalf("telemetry = %+v, want one index rename with 2 edits", *rec)
	}
}

// TestTraitRenameIndexDrivenCancel: dropping the preview (esc) applies
// nothing — no buffer edit goes out and the disk keeps the old name.
func TestTraitRenameIndexDrivenCancel(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, rec := phpRenameBridge(t, root, "src/Traits/A.php", true,
		func() *protocol.Range { return nil },
		func() protocol.WorkspaceEdit { return protocol.WorkspaceEdit{} })
	line, col := posIn(t, content, "$this->fromC()", 8)
	b.setCur(path, line, col)
	b.rename(h)
	prompt := recvMsg(t, msgs, "rename prompt").(ilsp.RenamePromptMsg)
	prompt.Apply("fromD")
	_ = recvMsg(t, msgs, "rename preview").(ilsp.RenamePreviewMsg) // dropped: cancelled

	drain(t, msgs, "cancel")
	data, _ := os.ReadFile(filepath.Join(root, "src", "Traits", "C.php"))
	if !strings.Contains(string(data), "public function fromC(): ?int") {
		t.Fatalf("cancel must leave the disk untouched:\n%s", data)
	}
	if rec.calls != 0 {
		t.Fatal("a cancelled rename must record nothing")
	}
}

// TestTraitRenameAmbiguousRefused: with B and B2 both consuming A and
// declaring abc differently, the index-driven rename is refused with a
// message naming both declarations, and nothing else happens.
func TestTraitRenameAmbiguousRefused(t *testing.T) {
	files := refsProjectFiles()
	files["src/Models/B2.php"] = renameB2PHP
	root := phpProject(t, files)
	b, h, msgs, path, content, _ := phpRenameBridge(t, root, "src/Traits/A.php", true,
		func() *protocol.Range { return nil },
		func() protocol.WorkspaceEdit { return protocol.WorkspaceEdit{} })
	line, col := posIn(t, content, "$this->abc()", 8)
	b.setCur(path, line, col)
	b.rename(h)
	status, ok := recvMsg(t, msgs, "refusal").(ilsp.ServerStatusMsg)
	if !ok || status.Kind != ilsp.ServerEventWarn {
		t.Fatalf("expected a warn toast, got %#v", status)
	}
	if !strings.HasPrefix(status.Text, "cannot rename abc: declared differently in ") ||
		!strings.Contains(status.Text, "class B (B.php:") || !strings.Contains(status.Text, "class B2 (B2.php:") {
		t.Fatalf("refusal = %q", status.Text)
	}
	drain(t, msgs, "refusal")
}

// TestTraitRenameDisabledKeepsServerVerdict: with php.trait_index = false
// a refused position stays "cannot rename here", and a server rename gets
// no note, no validator and no extra edits.
func TestTraitRenameDisabledKeepsServerVerdict(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	b, h, msgs, path, content, _ := phpRenameBridge(t, root, "src/Traits/A.php", false,
		func() *protocol.Range { return nil },
		func() protocol.WorkspaceEdit { return protocol.WorkspaceEdit{} })
	line, col := posIn(t, content, "$this->fromC()", 8)
	b.setCur(path, line, col)
	b.rename(h)
	status, ok := recvMsg(t, msgs, "verdict").(ilsp.ServerStatusMsg)
	if !ok || status.Text != "cannot rename here" {
		t.Fatalf("expected today's verdict, got %#v", status)
	}

	bPath := filepath.Join(root, "src", "Models", "B.php")
	declLine, declCol := posInText(refsClassBPHP, "public function abc", 16)
	b2, h2, msgs2, path2, _, _ := phpRenameBridge(t, root, "src/Models/B.php", false,
		func() *protocol.Range { return rangeAt(declLine, declCol, 3) },
		func() protocol.WorkspaceEdit { return bOnlyEdit(bPath) })
	b2.setCur(path2, declLine, declCol+1)
	b2.rename(h2)
	prompt := recvMsg(t, msgs2, "rename prompt").(ilsp.RenamePromptMsg)
	if prompt.Note != "" || prompt.Validate != nil {
		t.Fatalf("a disabled index must leave the prompt plain, got %+v", prompt)
	}
	prompt.Apply("xyz")
	// A single-file server rename applies instantly, as it always has.
	fm, ok := recvMsg(t, msgs2, "buffer edits").(ilsp.FormatEditsMsg)
	if !ok || fm.Path != bPath || len(fm.Edits) != 3 {
		t.Fatalf("disabled index must apply the server's edits alone, got %#v", fm)
	}
	if st := recvMsg(t, msgs2, "summary").(ilsp.ServerStatusMsg); st.Text != "renamed in 1 file" {
		t.Fatalf("summary = %q", st.Text)
	}
	if data, _ := os.ReadFile(filepath.Join(root, "src", "Traits", "A.php")); !strings.Contains(string(data), "$this->abc();") {
		t.Fatal("a disabled index must not touch the traits")
	}
}

// TestTraitRenameUnsupportedServerFallsBack: a server without rename at all
// (intelephense without a licence) still lets the index rename a
// trait-scope member; outside a trait body the unsupported toast stays.
func TestTraitRenameUnsupportedServerFallsBack(t *testing.T) {
	root := phpProject(t, refsProjectFiles())
	noRename := func(spec ilsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
		return phpConnector(nil, "")(spec, root, handler)
	}
	idx := phpindex.New(root, phpindex.Options{Enabled: true, ParentDepth: 3, MaxFiles: 1000})
	if !idx.Available() {
		t.Skip("no PHP grammar in this build (cgo off)")
	}
	for start := time.Now(); !idx.ScanDone(); {
		if time.Since(start) > 10*time.Second {
			t.Fatal("the PHP index scan did not finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
	msgs := make(chan tea.Msg, 16)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	h.SetTraitIndex(phpindex.NewHostView(idx))
	spec := ilsp.ServerSpec{Language: "php", Command: "fake", RootMarkers: []string{"composer.json"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) { return spec, lang == spec.Language }
	b := &bridge{h: h, mgr: manager.New(resolve, noRename, manager.Callbacks{})}
	t.Cleanup(b.mgr.Shutdown)
	aPath := filepath.Join(root, "src", "Traits", "A.php")
	if err := b.mgr.Open(aPath, "php", traitAPHP); err != nil {
		t.Fatal(err)
	}
	line, col := posIn(t, traitAPHP, "$this->fromC()", 8)
	b.setCur(aPath, line, col)
	b.rename(h)
	if prompt, ok := recvMsg(t, msgs, "rename prompt").(ilsp.RenamePromptMsg); !ok || prompt.Placeholder != "fromC" {
		t.Fatalf("expected the index prompt, got %#v", prompt)
	}

	bPath := filepath.Join(root, "src", "Models", "B.php")
	if err := b.mgr.Open(bPath, "php", refsClassBPHP); err != nil {
		t.Fatal(err)
	}
	declLine, declCol := posInText(refsClassBPHP, "public function abc", 16)
	b.setCur(bPath, declLine, declCol+1)
	b.rename(h)
	if st, ok := recvMsg(t, msgs, "verdict").(ilsp.ServerStatusMsg); !ok || st.Text != "language server does not support rename" {
		t.Fatalf("expected the unsupported toast outside a trait body, got %#v", st)
	}
}

// TestTraitRenameFilesPreservesDollarAndSkipsCovered is the pure
// conversion: a property declaration keeps its `$`, a `$`-prefixed new name
// is normalised, and an identifier a server edit overlaps is skipped.
func TestTraitRenameFilesPreservesDollarAndSkipsCovered(t *testing.T) {
	plan := host.TraitRenamePlan{OldName: "x", Edits: []host.TraitRenameEdit{
		{Path: "/p/C.php", Line: 6, Col: 14, EndCol: 16, Text: "$x", Decl: true},
		{Path: "/p/A.php", Line: 9, Col: 15, EndCol: 16, Text: "x"},
		{Path: "/p/A.php", Line: 9, Col: 25, EndCol: 27, Text: "$x"},
	}}
	server := []manager.FileEdits{{Path: "/p/A.php", Open: true, Edits: []ilsp.FormatEdit{
		{StartLine: 9, StartCol: 15, EndLine: 9, EndCol: 16, Text: "y"},
	}}}
	files, n := traitRenameFiles(nil, plan, "$y", server)
	if n != 2 || len(files) != 2 {
		t.Fatalf("files = %+v n=%d, want 2 index edits over 2 files", files, n)
	}
	if files[0].Path != "/p/A.php" || len(files[0].Edits) != 2 || files[0].Edits[1].Text != "$y" || !files[0].Open {
		t.Fatalf("A = %+v", files[0])
	}
	if files[1].Path != "/p/C.php" || len(files[1].Edits) != 1 || files[1].Edits[0].Text != "$y" || files[1].Open {
		t.Fatalf("C = %+v", files[1])
	}
	for _, in := range []string{"abc", "_a1", "$abc", "äbc"} {
		if msg := validatePHPName(in); msg != "" {
			t.Errorf("%q rejected: %s", in, msg)
		}
	}
	for _, in := range []string{"1abc", "a-b", "", "a b", "$", "a.b"} {
		if validatePHPName(in) == "" {
			t.Errorf("%q accepted", in)
		}
	}
	if traitRenameNote(host.TraitRenamePlan{}) != "" {
		t.Fatal("an empty plan has no note")
	}
	if got := traitRenameNote(host.TraitRenamePlan{Edits: []host.TraitRenameEdit{{DeclName: "A"}}}); got != "+ 1 occurrence in trait A" {
		t.Fatalf("note = %q", got)
	}
}
