package lsp

import (
	"context"
	"os"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
)

// bridge is the long-lived glue between the editor and the LSP manager. It is a
// process-wide singleton because the plugin value itself is stateless registry
// data; the manager and captured host must persist across command/hook calls.
type bridge struct {
	mu      sync.Mutex
	h       host.API
	mgr     *manager.Manager
	curPath string
	curLine int
	curCol  int
}

var (
	sharedOnce sync.Once
	sharedInst *bridge
)

// shared returns the process-wide bridge, created on first use.
func shared() *bridge {
	sharedOnce.Do(func() { sharedInst = &bridge{} })
	return sharedInst
}

// ensure captures the host and lazily builds the manager + registers this bridge
// as the host's editor emitter, so edits start flowing. Idempotent.
func (b *bridge) ensure(h host.API) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.h == nil {
		b.h = h
	}
	if b.mgr != nil {
		return
	}
	b.mgr = manager.New(resolveSpec, nil, manager.Callbacks{
		Diagnostics: b.onDiagnostics,
		Status:      b.onStatus,
	})
	h.SetEditorEmitter(b)
}

// Emit implements host.EditorEmitter: it routes editor lifecycle events to the
// manager. It runs on the main goroutine and must not block — change sync is a
// quick notification write; completion is dispatched to a goroutine.
func (b *bridge) Emit(ev host.EditorEvent) {
	switch ev.Kind {
	case host.EditorChange:
		b.setCur(ev.Path, ev.Line, ev.Col)
		if l, ok := lang.ByPath(ev.Path); ok && l.Server != nil && b.manager() != nil {
			_ = b.manager().Change(ev.Path, ev.Text)
		}
	case host.EditorCursorMove:
		b.setCur(ev.Path, ev.Line, ev.Col)
	case host.EditorCompletionTrigger:
		b.setCur(ev.Path, ev.Line, ev.Col)
		b.requestCompletion(ev.Path, ev.Line, ev.Col)
	}
}

// --- hooks ---

// fileOpened activates the subsystem and opens the document (didOpen). The
// just-loaded buffer equals the file on disk, so the disk content is the initial
// text. Open blocks on the initialize handshake, so it runs on a goroutine.
func (b *bridge) fileOpened(h host.API, path string) {
	b.ensure(h)
	l, ok := lang.ByPath(path)
	if !ok || l.Server == nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	mgr := b.manager()
	go func() { _ = mgr.Open(path, l.ID, string(data)) }()
}

func (b *bridge) fileSaved(h host.API, path string) {
	b.ensure(h)
	if mgr := b.manager(); mgr != nil {
		go func() { _ = mgr.Save(path) }()
	}
}

func (b *bridge) fileClosed(path string) {
	if mgr := b.manager(); mgr != nil {
		go func() { _ = mgr.Close(path) }()
	}
}

// --- commands ---

// hover requests hover at the current cursor and sends a HoverMsg.
func (b *bridge) hover(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	go func() {
		hv, err := mgr.Hover(context.Background(), path, buffer.Position{Line: line, Col: col})
		if err != nil || hv == nil {
			return
		}
		if text := ilsp.HoverText(hv); text != "" {
			h.Send(ilsp.HoverMsg{Path: path, Contents: text})
		}
	}()
	return nil
}

// definition requests the definition target, converts it to editor coordinates
// (reading the target file), and sends a DefinitionMsg for the app to navigate.
func (b *bridge) definition(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	go func() {
		locs, err := mgr.Definition(context.Background(), path, buffer.Position{Line: line, Col: col})
		if err != nil || len(locs) == 0 {
			return
		}
		loc := locs[0]
		target := protocol.URIToPath(loc.URI)
		tline, tcol := loc.Range.Start.Line, 0
		if data, rerr := os.ReadFile(target); rerr == nil {
			p := protocol.FromLSPPosition(strings.Split(string(data), "\n"), loc.Range.Start, mgr.Encoding(path))
			tline, tcol = p.Line, p.Col
		}
		h.Send(ilsp.DefinitionMsg{Path: target, Line: tline, Col: tcol})
	}()
	return nil
}

// restart stops every server; they respawn lazily on the next file open/edit.
// The work happens inside the returned tea.Cmd: a command's Run resolves on
// the Update goroutine, where a blocking Shutdown would stall the UI and a
// host.Send would deadlock outright (bubbletea's Send writes to an unbuffered
// channel only the — then busy — event loop drains). The status message is
// therefore returned, never Sent.
func (b *bridge) restart(h host.API) tea.Cmd {
	b.ensure(h)
	mgr := b.manager()
	return func() tea.Msg {
		if mgr != nil {
			mgr.Shutdown()
		}
		return ilsp.ServerStatusMsg{Text: "LSP servers restarted", Kind: ilsp.ServerEventInfo}
	}
}

// restartLang stops one language's servers; they respawn lazily like the
// global restart. Work happens inside the returned tea.Cmd (see restart) —
// and the bridge may not be activated yet (no file opened), in which case
// there is nothing to stop and the status message still reports the action.
func (b *bridge) restartLang(langID string) tea.Cmd {
	return func() tea.Msg {
		if mgr := b.manager(); mgr != nil {
			mgr.StopLang(langID)
		}
		return ilsp.ServerStatusMsg{Lang: langID, Text: langID + " language server restarted", Kind: ilsp.ServerEventInfo}
	}
}

// restartAll mirrors the lsp.restart command for the settings page: it works
// without a captured host (the page can restart before any file was opened).
func (b *bridge) restartAll() tea.Cmd {
	return func() tea.Msg {
		if mgr := b.manager(); mgr != nil {
			mgr.Shutdown()
		}
		return ilsp.ServerStatusMsg{Text: "LSP servers restarted", Kind: ilsp.ServerEventInfo}
	}
}

// runningLangs lists the languages with a live server, for the settings page.
func (b *bridge) runningLangs() []string {
	if mgr := b.manager(); mgr != nil {
		return mgr.RunningLangs()
	}
	return nil
}

// requestCompletion fires a completion request on a goroutine and sends the
// result as a CompletionMsg anchored at the trigger position.
func (b *bridge) requestCompletion(path string, line, col int) {
	mgr := b.manager()
	if mgr == nil || b.h == nil {
		return
	}
	h := b.h
	go func() {
		items, err := mgr.Completion(context.Background(), path, buffer.Position{Line: line, Col: col})
		if err != nil || len(items) == 0 {
			return
		}
		h.Send(ilsp.CompletionMsg{Path: path, Line: line, Col: col, Items: ilsp.ConvertCompletion(items)})
	}()
}

// --- manager callbacks ---

func (b *bridge) onDiagnostics(path string, p protocol.PublishDiagnosticsParams, lines []string, enc string) {
	if b.h == nil {
		return
	}
	b.h.Send(ilsp.DiagnosticsMsg{Path: path, Diagnostics: ilsp.ConvertDiagnostics(p, lines, enc)})
}

func (b *bridge) onStatus(lang, text string, kind ilsp.ServerStatusKind) {
	if b.h != nil {
		b.h.Send(ilsp.ServerStatusMsg{Lang: lang, Text: text, Kind: kind})
	}
}

// --- helpers ---

func (b *bridge) manager() *manager.Manager {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mgr
}

func (b *bridge) setCur(path string, line, col int) {
	b.mu.Lock()
	b.curPath, b.curLine, b.curCol = path, line, col
	b.mu.Unlock()
}

func (b *bridge) cur() (string, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.curPath, b.curLine, b.curCol
}
