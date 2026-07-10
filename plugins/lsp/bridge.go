package lsp

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/lsp/manager"
	"ike/internal/lsp/protocol"
	"ike/internal/lsp/transport"
)

// bridge is the long-lived glue between the editor and the LSP manager. It is a
// process-wide singleton because the plugin value itself is stateless registry
// data; the manager and captured host must persist across command/hook calls.
type bridge struct {
	mu      sync.Mutex
	h       host.API
	mgr     *manager.Manager
	inst    *installer
	curPath string
	curLine int
	curCol  int
	// Latest visual selection off the event stream (host.Sel* kinds); the
	// anchor is one end, the cursor the other. selKind is host.SelNone when
	// no selection is active.
	selKind    int
	anchorLine int
	anchorCol  int
}

var (
	sharedOnce sync.Once
	sharedInst *bridge
)

// shared returns the process-wide bridge, created on first use.
func shared() *bridge {
	sharedOnce.Do(func() { sharedInst = &bridge{inst: newInstaller()} })
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
		b.setSel(ev)
		if l, ok := lang.ByPath(ev.Path); ok && l.Server != nil && b.manager() != nil {
			_ = b.manager().Change(ev.Path, ev.Text)
		}
	case host.EditorCursorMove:
		b.setCur(ev.Path, ev.Line, ev.Col)
		b.setSel(ev)
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
	// The just-opened file is the current one even before the first cursor
	// event, so position-less actions (lsp.format) work immediately.
	b.setCur(path, 0, 0)
	l, ok := lang.ByPath(path)
	if !ok || l.Server == nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	mgr := b.manager()
	go func() {
		err := mgr.Open(path, l.ID, string(data))
		if err != nil && errors.Is(err, transport.ErrNotFound) {
			// Missing binary on first use: activation implies installation
			// (#131). We are already off the Update loop here.
			b.autoInstall(l.ID, path)
		}
	}()
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

// references requests every usage of the symbol under the cursor, converts
// the locations to editor coordinates (reading each distinct target file once,
// which also supplies the preview lines) and sends a ReferencesMsg the app
// renders as a navigable list.
func (b *bridge) references(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	go func() {
		locs, err := mgr.References(context.Background(), path, buffer.Position{Line: line, Col: col}, true)
		if err != nil {
			return
		}
		files := map[string][]string{}
		refs := make([]ilsp.Reference, 0, len(locs))
		for _, loc := range locs {
			target := protocol.URIToPath(loc.URI)
			lines, ok := files[target]
			if !ok {
				if data, rerr := os.ReadFile(target); rerr == nil {
					lines = strings.Split(string(data), "\n")
				}
				files[target] = lines
			}
			ref := ilsp.Reference{Path: target, Line: loc.Range.Start.Line}
			if lines != nil {
				p := protocol.FromLSPPosition(lines, loc.Range.Start, mgr.Encoding(path))
				ref.Line, ref.Col = p.Line, p.Col
				if ref.Line >= 0 && ref.Line < len(lines) {
					ref.Preview = strings.TrimSpace(lines[ref.Line])
				}
			}
			refs = append(refs, ref)
		}
		h.Send(ilsp.ReferencesMsg{Refs: refs})
	}()
	return nil
}

// format requests whole-document formatting with the editor's indent settings
// and delivers the edits as a FormatEditsMsg the owning editor applies.
func (b *bridge) format(h host.API) tea.Cmd {
	b.ensure(h)
	path, _, _ := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	opts := formattingOptions(h)
	go func() {
		edits, err := mgr.Format(context.Background(), path, opts)
		if err != nil || len(edits) == 0 {
			return
		}
		h.Send(ilsp.FormatEditsMsg{Path: path, Edits: edits})
	}()
	return nil
}

// formatRange formats the active visual selection; without one it reports
// what to do instead of silently doing nothing.
func (b *bridge) formatRange(h host.API) tea.Cmd {
	b.ensure(h)
	path, _, _ := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	start, end, ok := b.sel()
	if !ok {
		return func() tea.Msg {
			return ilsp.ServerStatusMsg{Text: "select a range first (visual mode), or use LSP: Reformat File", Kind: ilsp.ServerEventInfo}
		}
	}
	opts := formattingOptions(h)
	go func() {
		edits, err := mgr.FormatRange(context.Background(), path, start, end, opts)
		if err != nil || len(edits) == 0 {
			return
		}
		h.Send(ilsp.FormatEditsMsg{Path: path, Edits: edits})
	}()
	return nil
}

// formattingOptions reads the editor indent settings from config; the defaults
// mirror internal/config's editor defaults.
func formattingOptions(h host.API) protocol.FormattingOptions {
	opts := protocol.FormattingOptions{TabSize: 4, InsertSpaces: true}
	if h == nil {
		return opts
	}
	cfg := h.Config()
	if cfg == nil {
		return opts
	}
	if v, ok := cfg.Get("editor.tab_width"); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			opts.TabSize = n
		}
	}
	if v, ok := cfg.Get("editor.use_spaces"); ok {
		opts.InsertSpaces = v == "true"
	}
	return opts
}

// rename starts the rename flow: prepareRename validates the cursor position
// off the Update loop, then the app is asked to prompt for the new name; the
// prompt's answer runs the Apply continuation, which requests the rename and
// applies the WorkspaceEdit (workspace_edit.go). A rejected position surfaces
// as a warn toast; the current project is untouched until edits arrive.
func (b *bridge) rename(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	pos := buffer.Position{Line: line, Col: col}
	go func() {
		placeholder, ok, err := mgr.PrepareRename(context.Background(), path, pos)
		if err != nil || !ok {
			h.Send(ilsp.ServerStatusMsg{Text: "cannot rename here", Kind: ilsp.ServerEventWarn})
			return
		}
		h.Send(ilsp.RenamePromptMsg{
			Path:        path,
			Placeholder: placeholder,
			Apply:       func(newName string) tea.Cmd { return b.applyRename(h, path, pos, newName) },
		})
	}()
	return nil
}

// applyRename requests the workspace edit for newName and applies it: open
// buffers in-editor, closed files on disk, followed by a summary toast.
func (b *bridge) applyRename(h host.API, path string, pos buffer.Position, newName string) tea.Cmd {
	mgr := b.manager()
	if mgr == nil || strings.TrimSpace(newName) == "" {
		return nil
	}
	go func() {
		files, err := mgr.Rename(context.Background(), path, pos, newName)
		if err != nil {
			h.Send(ilsp.ServerStatusMsg{Text: "rename failed: " + err.Error(), Kind: ilsp.ServerEventError})
			return
		}
		n, derr := dispatchWorkspaceEdits(h, files)
		switch {
		case derr != nil:
			h.Send(ilsp.ServerStatusMsg{Text: "rename applied partially: " + derr.Error(), Kind: ilsp.ServerEventWarn})
		case n == 0:
			h.Send(ilsp.ServerStatusMsg{Text: "nothing to rename", Kind: ilsp.ServerEventInfo})
		default:
			h.Send(ilsp.ServerStatusMsg{Text: editSummary(n), Kind: ilsp.ServerEventInfo})
		}
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

func (b *bridge) setSel(ev host.EditorEvent) {
	b.mu.Lock()
	b.selKind, b.anchorLine, b.anchorCol = ev.Sel, ev.AnchorLine, ev.AnchorCol
	b.mu.Unlock()
}

// sel returns the active selection as a normalised [start, end) editor range,
// or ok=false when none is active. A line-wise selection expands to whole
// lines (end exclusive at the start of the following line).
func (b *bridge) sel() (start, end buffer.Position, ok bool) {
	b.mu.Lock()
	kind, aLine, aCol := b.selKind, b.anchorLine, b.anchorCol
	cLine, cCol := b.curLine, b.curCol
	b.mu.Unlock()
	if kind == host.SelNone {
		return start, end, false
	}
	start = buffer.Position{Line: aLine, Col: aCol}
	end = buffer.Position{Line: cLine, Col: cCol}
	if end.Before(start) {
		start, end = end, start
	}
	if kind == host.SelLine {
		start = buffer.Position{Line: start.Line, Col: 0}
		end = buffer.Position{Line: end.Line + 1, Col: 0}
	} else {
		// Visual selections are inclusive of the cursor cell; LSP ranges are
		// end-exclusive.
		end.Col++
	}
	return start, end, true
}

func (b *bridge) cur() (string, int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.curPath, b.curLine, b.curCol
}
