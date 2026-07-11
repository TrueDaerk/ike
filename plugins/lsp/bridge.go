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
	// diags caches the latest published protocol diagnostics per path, so a
	// code-action request can pass the overlapping ones as context.
	diags map[string][]protocol.Diagnostic
	// sigActive marks a showing signature popup for a path: while set, every
	// change retriggers the request so the active parameter tracks the
	// cursor; the server answering null clears it (and the popup).
	sigActive map[string]bool
	// semInFlight coalesces semantic-token requests per path: while one is
	// running, further changes are absorbed and a fresh request fires right
	// after it lands (semPending), so the overlay converges without queueing.
	semInFlight map[string]bool
	semPending  map[string]bool
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
		ApplyEdit:   b.onApplyEdit,
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
			b.maybeSignatureHelp(ev)
			b.requestSemanticTokens(ev.Path)
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
			return
		}
		// Initial semantic overlay for the fresh document (#9).
		b.requestSemanticTokens(path)
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

// requestFailed surfaces a failed server request as an error toast and reports
// whether it fired (#372). A swallowed error is indistinguishable from a
// command that never ran, so every user-initiated request routes its error
// here; op is the user-facing action name ("find usages").
func requestFailed(h host.API, op string, err error) bool {
	if err == nil {
		return false
	}
	h.Send(ilsp.ServerStatusMsg{Text: op + " failed: " + err.Error(), Kind: ilsp.ServerEventError})
	return true
}

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
		if requestFailed(h, "hover", err) || hv == nil {
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
		if requestFailed(h, "go to definition", err) || len(locs) == 0 {
			return
		}
		if len(locs) > 1 {
			// Several definition sites (interface implementations, build-tag
			// variants): pick, don't guess (#279).
			h.Send(ilsp.DefinitionCandidatesMsg{Refs: locationsToRefs(mgr, path, locs)})
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
		if requestFailed(h, "find usages", err) {
			return
		}
		h.Send(ilsp.ReferencesMsg{Refs: locationsToRefs(mgr, path, locs)})
	}()
	return nil
}

// goToSymbol opens the workspace-symbol prompt (project.goToClass, 0250,
// #294): the app collects the query, then Apply runs the actual request.
func (b *bridge) goToSymbol(h host.API) tea.Cmd {
	b.ensure(h)
	return func() tea.Msg {
		return ilsp.SymbolPromptMsg{
			Apply: func(query string) tea.Cmd { return b.workspaceSymbols(h, query) },
		}
	}
}

// workspaceSymbols fans the query out through the manager and delivers the
// hits (converted like references) as a SymbolResultsMsg.
func (b *bridge) workspaceSymbols(h host.API, query string) tea.Cmd {
	mgr := b.manager()
	if mgr == nil {
		return nil
	}
	go func() {
		syms, ok := mgr.WorkspaceSymbols(context.Background(), query)
		if !ok {
			h.Send(ilsp.SymbolResultsMsg{Query: query, NoProvider: true})
			return
		}
		locs := make([]protocol.Location, len(syms))
		for i, sym := range syms {
			locs[i] = sym.Location
		}
		path := ""
		if len(syms) > 0 {
			path = protocol.URIToPath(syms[0].Location.URI)
		}
		refs := locationsToRefs(mgr, path, locs)
		hits := make([]ilsp.SymbolHit, len(refs))
		for i, ref := range refs {
			hits[i] = ilsp.SymbolHit{Name: syms[i].Name, Ref: ref}
		}
		h.Send(ilsp.SymbolResultsMsg{Query: query, Hits: hits})
	}()
	return nil
}

// locationsToRefs converts LSP locations to editor-coordinate references,
// reading each distinct target file once — the read supplies both the
// position conversion base and the preview line. Shared by find-references
// and the multi-target definition picker (#279).
func locationsToRefs(mgr *manager.Manager, path string, locs []protocol.Location) []ilsp.Reference {
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
	return refs
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
		if requestFailed(h, "reformat", err) || len(edits) == 0 {
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
		if requestFailed(h, "reformat selection", err) || len(edits) == 0 {
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

// codeAction lists the actions available at the cursor (or the active visual
// selection) and asks the app to show the picker; the chosen action applies
// its WorkspaceEdit (workspace_edit.go) and/or executes its command, whose
// effects come back as workspace/applyEdit.
func (b *bridge) codeAction(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	start := buffer.Position{Line: line, Col: col}
	end := start
	if s, e, ok := b.sel(); ok {
		start, end = s, e
	}
	diags := b.diagsOverlapping(path, start.Line, end.Line)
	go func() {
		actions, err := mgr.CodeActions(context.Background(), path, start, end, diags)
		if requestFailed(h, "code actions", err) {
			return
		}
		if len(actions) == 0 {
			h.Send(ilsp.ServerStatusMsg{Text: "no code actions here", Kind: ilsp.ServerEventInfo})
			return
		}
		choices := make([]ilsp.CodeActionChoice, len(actions))
		for i, a := range actions {
			choices[i] = ilsp.CodeActionChoice{Title: a.Title, Kind: a.Kind, Preferred: a.IsPreferred}
		}
		h.Send(ilsp.CodeActionsMsg{
			Path:    path,
			Actions: choices,
			Apply: func(i int) tea.Cmd {
				if i < 0 || i >= len(actions) {
					return nil
				}
				return b.applyAction(h, path, actions[i])
			},
		})
	}()
	return nil
}

// applyAction performs one chosen action: the inline Edit first (per spec),
// then the command, whose edits arrive via workspace/applyEdit.
func (b *bridge) applyAction(h host.API, path string, action protocol.CodeAction) tea.Cmd {
	mgr := b.manager()
	if mgr == nil {
		return nil
	}
	go func() {
		// Every outcome reports (#309): a silent action is indistinguishable
		// from a broken one.
		switch {
		case action.Edit == nil && action.Command == nil:
			// Lazy actions need codeAction/resolve, which is not implemented
			// yet — say so instead of doing nothing.
			h.Send(ilsp.ServerStatusMsg{Text: "'" + action.Title + "' returned no edit (codeAction/resolve not supported yet)", Kind: ilsp.ServerEventWarn})
			return
		case action.Edit != nil:
			files := mgr.ConvertWorkspaceEdit(path, *action.Edit)
			if n, err := dispatchWorkspaceEdits(h, files); err != nil {
				h.Send(ilsp.ServerStatusMsg{Text: "edit applied partially: " + err.Error(), Kind: ilsp.ServerEventWarn})
			} else if n > 0 {
				h.Send(ilsp.ServerStatusMsg{Text: "'" + action.Title + "': " + applySummary(n), Kind: ilsp.ServerEventInfo})
			} else if action.Command == nil {
				h.Send(ilsp.ServerStatusMsg{Text: "'" + action.Title + "' changed nothing", Kind: ilsp.ServerEventInfo})
			}
		}
		if action.Command != nil {
			if err := mgr.ExecuteCommand(context.Background(), path, *action.Command); err != nil {
				h.Send(ilsp.ServerStatusMsg{Text: "code action failed: " + err.Error(), Kind: ilsp.ServerEventError})
			}
		}
	}()
	return nil
}

// maybeSignatureHelp fires a signature request after a change when the typed
// character is one of the server's triggers, or whenever the popup is already
// showing (so the active parameter follows the cursor). The server answering
// null dismisses the popup.
func (b *bridge) maybeSignatureHelp(ev host.EditorEvent) {
	mgr := b.manager()
	if mgr == nil {
		return
	}
	b.mu.Lock()
	active := b.sigActive[ev.Path]
	b.mu.Unlock()
	if !active && !isSignatureTrigger(typedChar(ev), mgr.SignatureTriggers(ev.Path)) {
		return
	}
	path, line, col := ev.Path, ev.Line, ev.Col
	go func() {
		sh, err := mgr.SignatureHelp(context.Background(), path, buffer.Position{Line: line, Col: col})
		if err != nil {
			return
		}
		label, start, end, doc, more := ilsp.SignatureContent(sh)
		b.mu.Lock()
		if b.sigActive == nil {
			b.sigActive = map[string]bool{}
		}
		b.sigActive[path] = label != ""
		b.mu.Unlock()
		if b.h != nil {
			b.h.Send(ilsp.SignatureHelpMsg{Path: path, Label: label, ParamStart: start, ParamEnd: end, Doc: doc, More: more})
		}
	}()
}

// typedChar extracts the character the change just inserted: the one left of
// the cursor. Deletions and multi-line pastes yield "".
func typedChar(ev host.EditorEvent) string {
	if ev.Col <= 0 {
		return ""
	}
	lines := strings.Split(ev.Text, "\n")
	if ev.Line < 0 || ev.Line >= len(lines) {
		return ""
	}
	runes := []rune(lines[ev.Line])
	if ev.Col > len(runes) {
		return ""
	}
	return string(runes[ev.Col-1])
}

// isSignatureTrigger reports whether ch is one of the server's trigger (or
// retrigger) characters.
func isSignatureTrigger(ch string, triggers []string) bool {
	if ch == "" {
		return false
	}
	for _, t := range triggers {
		if t == ch {
			return true
		}
	}
	return false
}

// requestSemanticTokens refreshes the semantic overlay for path, coalescing
// concurrent requests: at most one runs; changes during a run mark a pending
// re-request that fires when it lands. Missing capability yields no spans and
// no traffic beyond the gate check.
func (b *bridge) requestSemanticTokens(path string) {
	mgr := b.manager()
	if mgr == nil {
		return
	}
	b.mu.Lock()
	if b.semInFlight == nil {
		b.semInFlight = map[string]bool{}
		b.semPending = map[string]bool{}
	}
	if b.semInFlight[path] {
		b.semPending[path] = true
		b.mu.Unlock()
		return
	}
	b.semInFlight[path] = true
	b.mu.Unlock()

	go func() {
		for {
			spans, err := mgr.SemanticTokens(context.Background(), path)
			if err == nil && spans != nil && b.h != nil {
				b.h.Send(ilsp.SemanticSpansMsg{Path: path, Spans: spans})
			}
			b.mu.Lock()
			if b.semPending[path] {
				b.semPending[path] = false
				b.mu.Unlock()
				continue
			}
			b.semInFlight[path] = false
			b.mu.Unlock()
			return
		}
	}()
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
	b.mu.Lock()
	if b.diags == nil {
		b.diags = map[string][]protocol.Diagnostic{}
	}
	b.diags[path] = p.Diagnostics
	b.mu.Unlock()
	if b.h == nil {
		return
	}
	b.h.Send(ilsp.DiagnosticsMsg{Path: path, Diagnostics: ilsp.ConvertDiagnostics(p, lines, enc)})
}

// onApplyEdit lands a server-initiated workspace/applyEdit (the effect of an
// executed code-action command): open buffers in-editor, the rest on disk.
func (b *bridge) onApplyEdit(files []manager.FileEdits) {
	if b.h == nil {
		return
	}
	if n, err := dispatchWorkspaceEdits(b.h, files); err != nil {
		b.h.Send(ilsp.ServerStatusMsg{Text: "edit applied partially: " + err.Error(), Kind: ilsp.ServerEventWarn})
	} else if n > 0 {
		b.h.Send(ilsp.ServerStatusMsg{Text: applySummary(n), Kind: ilsp.ServerEventInfo})
	}
}

// diagsOverlapping returns the cached diagnostics for path whose range
// overlaps the [startLine, endLine] span (line granularity is enough context
// for servers to match their own diagnostics).
func (b *bridge) diagsOverlapping(path string, startLine, endLine int) []protocol.Diagnostic {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []protocol.Diagnostic
	for _, d := range b.diags[path] {
		if d.Range.End.Line >= startLine && d.Range.Start.Line <= endLine {
			out = append(out, d)
		}
	}
	return out
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
