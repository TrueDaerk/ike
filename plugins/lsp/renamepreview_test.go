package lsp

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// renamepreview_test.go covers the multi-file rename confirmation (#2149): a
// rename whose WorkspaceEdit reaches beyond one file is previewed instead of
// applied, the preview describes every affected file, confirming applies
// exactly those edits, and dropping the message leaves the project untouched.

// previewConnector dials an in-memory server that answers textDocument/rename
// with the synthetic WorkspaceEdit that edit() builds.
func previewConnector(edit func() protocol.WorkspaceEdit) manager.Connector {
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
						RenameProvider:   json.RawMessage(`true`),
					}}, nil)
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

// wholeWordEdit rewrites the [col, col+len) span of one line.
func wholeWordEdit(line, col, length int, text string) protocol.TextEdit {
	return protocol.TextEdit{
		Range: protocol.Range{
			Start: protocol.Position{Line: line, Character: col},
			End:   protocol.Position{Line: line, Character: col + length},
		},
		NewText: text,
	}
}

// previewBridge wires a bridge over two files: an open buffer (a.go, synced
// into the manager) and a closed file on disk (b.go). edit supplies the
// server's rename answer, keyed by the temp dir the test gets back.
func previewBridge(t *testing.T, edit func(dir string) protocol.WorkspaceEdit) (*bridge, string, string, chan tea.Msg) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	openPath := filepath.Join(dir, "a.go")
	openText := "func Greet() {}\n"
	if err := os.WriteFile(openPath, []byte(openText), 0o644); err != nil {
		t.Fatal(err)
	}
	closedPath := filepath.Join(dir, "b.go")
	if err := os.WriteFile(closedPath, []byte("call(Greet)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := ilsp.ServerSpec{Language: "go", Command: "fake", RootMarkers: []string{".git"}}
	resolve := func(lang string) (ilsp.ServerSpec, bool) {
		if lang == spec.Language {
			return spec, true
		}
		return ilsp.ServerSpec{}, false
	}
	msgs := make(chan tea.Msg, 32)
	h := host.New(nil)
	h.SetSender(func(m tea.Msg) { msgs <- m })
	b := &bridge{h: h, mgr: manager.New(resolve, previewConnector(func() protocol.WorkspaceEdit {
		return edit(dir)
	}), manager.Callbacks{})}
	if err := b.mgr.Open(openPath, "go", openText); err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(b.mgr.Shutdown)
	return b, openPath, closedPath, msgs
}

// twoFileEdit renames Greet to Greeting in both files.
func twoFileEdit(dir string) protocol.WorkspaceEdit {
	return protocol.WorkspaceEdit{Changes: map[string][]protocol.TextEdit{
		protocol.PathToURI(filepath.Join(dir, "a.go")): {wholeWordEdit(0, 5, 5, "Greeting")},
		protocol.PathToURI(filepath.Join(dir, "b.go")): {wholeWordEdit(0, 5, 5, "Greeting")},
	}}
}

// TestRenameMultiFilePreviewsBeforeApplying is the core contract: a rename
// touching two files asks first, describing both files and the text they would
// end up with — and nothing is written while the answer is pending.
func TestRenameMultiFilePreviewsBeforeApplying(t *testing.T) {
	b, openPath, closedPath, msgs := previewBridge(t, twoFileEdit)

	b.applyRename(b.h, openPath, buffer.Position{Line: 0, Col: 5}, "Greet", "Greeting")
	msg, ok := recvMsg(t, msgs, "rename preview").(ilsp.RenamePreviewMsg)
	if !ok {
		t.Fatalf("a multi-file rename must ask first, got %#v", msg)
	}
	if msg.OldName != "Greet" || msg.NewName != "Greeting" {
		t.Fatalf("preview names = %q → %q", msg.OldName, msg.NewName)
	}
	if len(msg.Files) != 2 {
		t.Fatalf("preview must list both files, got %#v", msg.Files)
	}
	byPath := map[string]ilsp.RenamePreviewFile{}
	for _, f := range msg.Files {
		byPath[f.Path] = f
	}
	openFile, closedFile := byPath[openPath], byPath[closedPath]
	if !openFile.Open || openFile.Edits != 1 || openFile.After != "func Greeting() {}\n" {
		t.Fatalf("open buffer preview = %#v", openFile)
	}
	if closedFile.Open || closedFile.Edits != 1 || closedFile.After != "call(Greeting)\n" {
		t.Fatalf("closed file preview = %#v", closedFile)
	}
	if openFile.Before != "func Greet() {}\n" {
		t.Fatalf("preview must diff against the current text, got %q", openFile.Before)
	}

	// Nothing applied yet: the file on disk still holds the old name and no
	// buffer edits went out.
	data, _ := os.ReadFile(closedPath)
	if string(data) != "call(Greet)\n" {
		t.Fatalf("a pending preview must not write anything, disk = %q", data)
	}
	select {
	case extra := <-msgs:
		t.Fatalf("a pending preview must not dispatch edits, got %#v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestRenamePreviewConfirmAppliesEverything checks the confirm path applies
// exactly what the preview showed: the open buffer through FormatEditsMsg, the
// closed file on disk, and one summary toast.
func TestRenamePreviewConfirmAppliesEverything(t *testing.T) {
	b, openPath, closedPath, msgs := previewBridge(t, twoFileEdit)

	b.applyRename(b.h, openPath, buffer.Position{Line: 0, Col: 5}, "Greet", "Greeting")
	msg := recvMsg(t, msgs, "rename preview").(ilsp.RenamePreviewMsg)
	msg.Apply()

	fm, ok := recvMsg(t, msgs, "buffer edits").(ilsp.FormatEditsMsg)
	if !ok || fm.Path != openPath {
		t.Fatalf("confirm must edit the open buffer, got %#v", fm)
	}
	status, ok := recvMsg(t, msgs, "summary toast").(ilsp.ServerStatusMsg)
	if !ok || status.Text != "renamed in 2 files" {
		t.Fatalf("summary = %#v", status)
	}
	data, _ := os.ReadFile(closedPath)
	if string(data) != "call(Greeting)\n" {
		t.Fatalf("closed file = %q, want the renamed text", data)
	}
}

// TestRenamePreviewCancelLeavesEverythingUntouched is the cancel half: the
// dialog is simply dropped — the disk file keeps its content and no buffer
// ever sees an edit.
func TestRenamePreviewCancelLeavesEverythingUntouched(t *testing.T) {
	b, openPath, closedPath, msgs := previewBridge(t, twoFileEdit)

	b.applyRename(b.h, openPath, buffer.Position{Line: 0, Col: 5}, "Greet", "Greeting")
	_ = recvMsg(t, msgs, "rename preview").(ilsp.RenamePreviewMsg) // dropped: cancelled

	select {
	case extra := <-msgs:
		t.Fatalf("cancel must dispatch nothing, got %#v", extra)
	case <-time.After(100 * time.Millisecond):
	}
	data, _ := os.ReadFile(closedPath)
	if string(data) != "call(Greet)\n" {
		t.Fatalf("cancelled rename must not touch disk, got %q", data)
	}
	if lines, _ := b.mgr.DocLines(openPath); len(lines) == 0 || lines[0] != "func Greet() {}" {
		t.Fatalf("cancelled rename must not touch the buffer, got %#v", lines)
	}
}

// TestRenameSingleFileAppliesInstantly guards the unchanged half: a rename
// confined to one file never opens the dialog.
func TestRenameSingleFileAppliesInstantly(t *testing.T) {
	b, openPath, _, msgs := previewBridge(t, func(dir string) protocol.WorkspaceEdit {
		return protocol.WorkspaceEdit{Changes: map[string][]protocol.TextEdit{
			protocol.PathToURI(filepath.Join(dir, "a.go")): {wholeWordEdit(0, 5, 5, "Greeting")},
		}}
	})

	b.applyRename(b.h, openPath, buffer.Position{Line: 0, Col: 5}, "Greet", "Greeting")
	fm, ok := recvMsg(t, msgs, "buffer edits").(ilsp.FormatEditsMsg)
	if !ok || fm.Path != openPath {
		t.Fatalf("a single-file rename must apply straight away, got %#v", fm)
	}
	status, ok := recvMsg(t, msgs, "summary toast").(ilsp.ServerStatusMsg)
	if !ok || status.Text != "renamed in 1 file" {
		t.Fatalf("summary = %#v", status)
	}
}

// TestRenamePreviewFilesSkipsEmptyAndUnreadable checks the payload builder
// directly: files without edits and files whose content is unreachable are not
// offered as previewable changes.
func TestRenamePreviewFilesSkipsEmptyAndUnreadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real.go")
	if err := os.WriteFile(path, []byte("aaa\nbbb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := previewFiles(nil, []manager.FileEdits{
		{Path: path, Edits: []ilsp.FormatEdit{{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, Text: "AAA"}}},
		{Path: filepath.Join(dir, "gone.go"), Edits: []ilsp.FormatEdit{{EndCol: 1, Text: "X"}}},
		{Path: path}, // no edits
	})
	if len(got) != 1 || got[0].Path != path {
		t.Fatalf("preview = %#v", got)
	}
	if got[0].Before != "aaa\nbbb\n" || got[0].After != "AAA\nbbb\n" {
		t.Fatalf("before/after = %q / %q", got[0].Before, got[0].After)
	}
}
