package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/largefile"
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
	// hintInFlight/hintPending coalesce inlay-hint requests the same way (#171).
	hintInFlight map[string]bool
	hintPending  map[string]bool
	// hlTimer debounces the occurrence-highlight request (#172): each cursor
	// move re-arms it, so only the last position of a motion burst reaches
	// the server.
	hlTimer *time.Timer
	// pendingChange/changeTimer coalesce didChange per path (#595): each edit
	// stores its latest event and (re)arms a short debounce; the flush runs the
	// O(document) diff + notification off the Update goroutine (on the timer
	// goroutine), or synchronously just before any request that needs the server
	// to hold the latest text. Typing bursts collapse to far fewer syncs.
	pendingChange map[string]host.EditorEvent
	changeTimer   map[string]*time.Timer
	// pendingDiags/diagTimer coalesce diagnostics publishes (#597): a
	// workspace-diagnostic server reporting hundreds of library files would
	// otherwise push one tea.Msg — one Update pass + re-render — per file,
	// starving keystrokes. Publishes accumulate here (latest per path) and flush
	// as one DiagnosticsBatchMsg.
	pendingDiags map[string]ilsp.DiagnosticsMsg
	diagTimer    *time.Timer
}

// diagCoalesce is how long diagnostics publishes accumulate before one batched
// message is sent. Short enough that squiggles feel live, long enough to fold a
// workspace publish storm into a single re-render.
const diagCoalesce = 50 * time.Millisecond

// changeDebounce is how long a didChange is held to coalesce a typing burst.
// Short enough to feel instant, long enough to collapse fast keystrokes; any
// request (completion, hover, …) flushes the pending change first, so a stale
// sync never reaches the server ahead of a request.
const changeDebounce = 40 * time.Millisecond

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
	// Embedded fragments (0300): tree-sitter injections feed the manager's
	// virtual documents; a no-cgo build detects nothing and this stays inert.
	b.mgr.SetFragmentDetector(highlight.Fragments)
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
		if ev.Large {
			// Large-file mode (#149): the event carries no text on purpose.
			// The document is usually not open server-side (the didOpen gate),
			// but a reload can grow an open document past the threshold —
			// close it instead of syncing emptiness; unopened paths no-op.
			if mgr := b.manager(); mgr != nil {
				path := ev.Path
				go func() { _ = mgr.Close(path) }()
			}
			return
		}
		if l, ok := lang.ByPath(ev.Path); ok && l.Server != nil && b.manager() != nil {
			// Coalesce the sync off the Update goroutine (#595): the O(document)
			// diff + notification and the follow-up requests run from the flush,
			// not on every keystroke. A request flushes first, so nothing reads
			// stale server text.
			b.scheduleChange(ev)
		}
	case host.EditorCursorMove:
		b.setCur(ev.Path, ev.Line, ev.Col)
		b.setSel(ev)
		if l, ok := lang.ByPath(ev.Path); ok && l.Server != nil {
			b.scheduleDocumentHighlight(ev.Path)
			// A showing signature popup follows the cursor (#523): the server
			// re-picks the active parameter, or answers null to dismiss.
			b.mu.Lock()
			active := b.sigActive[ev.Path]
			b.mu.Unlock()
			if active {
				b.requestSignature(ev.Path, ev.Line, ev.Col, false)
			}
		}
	case host.EditorCompletionTrigger:
		b.setCur(ev.Path, ev.Line, ev.Col)
		if b.shouldComplete(ev) {
			b.requestCompletion(ev.Path, ev.Line, ev.Col)
		}
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
	if largeFileGated(h.Config(), path, data) {
		// Large-file mode (#149): servers choke on huge documents too, so the
		// didOpen never happens — diagnostics and completion are silently
		// absent. Change events for the unopened document no-op in the
		// manager. editor.forceCodeInsight re-fires this hook with the
		// override set.
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
		// Initial semantic overlay (#9) and inlay hints (#171) for the fresh
		// document.
		b.requestSemanticTokens(path)
		b.requestInlayHints(path)
	}()
}

// largeFileGated reports whether path's document crosses the large-file
// thresholds (#149) without an active per-path override — the didOpen gate.
// It decides from the just-read bytes, mirroring the editor's Load-time flag.
func largeFileGated(cfg host.Config, path string, data []byte) bool {
	if largefile.Forced(path) {
		return false
	}
	var get largefile.Getter
	if cfg != nil {
		get = cfg.Get
	}
	lines := strings.Count(string(data), "\n") + 1
	return largefile.LimitsFrom(get).Exceeded(int64(len(data)), lines)
}

func (b *bridge) fileSaved(h host.API, path string) {
	b.ensure(h)
	// Sync the latest edits before didSave so the server's document matches the
	// bytes now on disk (#595).
	b.flushChange(path)
	if mgr := b.manager(); mgr != nil {
		go func() { _ = mgr.Save(path) }()
	}
}

func (b *bridge) fileClosed(path string) {
	// Drop any queued change so a debounced sync never lands after didClose (#595).
	b.cancelChange(path)
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

// parameterInfo requests signature help at the current cursor on demand
// (#523) and opens the cursor-anchored popup, regardless of mode or the
// lsp.signature_auto toggle. No server or no signatureHelp capability yields
// an empty answer, which no-ops as a dismissal.
func (b *bridge) parameterInfo(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	if path == "" || b.manager() == nil {
		return nil
	}
	b.requestSignature(path, line, col, true)
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

// callHierarchy prepares the call hierarchy for the symbol under the cursor
// (#173) and sends a CallHierarchyMsg carrying the root items plus the Fetch
// continuation the overlay expands nodes with. Nothing prepared (position not
// on a callable, or the server lacks the capability) surfaces as a toast.
func (b *bridge) callHierarchy(h host.API) tea.Cmd {
	b.ensure(h)
	path, line, col := b.cur()
	mgr := b.manager()
	if path == "" || mgr == nil {
		return nil
	}
	go func() {
		items, err := mgr.PrepareCallHierarchy(context.Background(), path, buffer.Position{Line: line, Col: col})
		if requestFailed(h, "call hierarchy", err) {
			return
		}
		if len(items) == 0 {
			h.Send(ilsp.ServerStatusMsg{Text: "no call hierarchy here", Kind: ilsp.ServerEventInfo})
			return
		}
		roots := make([]ilsp.CallHierarchyEntry, len(items))
		for i, it := range items {
			roots[i] = hierEntry(mgr, path, it, it.URI, it.SelectionRange.Start)
		}
		h.Send(ilsp.CallHierarchyMsg{
			Path:  path,
			Roots: roots,
			Fetch: func(reqID int, item protocol.CallHierarchyItem, incoming bool) tea.Cmd {
				return b.fetchCalls(h, path, reqID, item, incoming)
			},
		})
	}()
	return nil
}

// fetchCalls expands one hierarchy node: callers (incoming) navigate to the
// call site inside the caller, callees to the callee's declaration.
func (b *bridge) fetchCalls(h host.API, path string, reqID int, item protocol.CallHierarchyItem, incoming bool) tea.Cmd {
	mgr := b.manager()
	if mgr == nil {
		return nil
	}
	go func() {
		var entries []ilsp.CallHierarchyEntry
		if incoming {
			calls, err := mgr.IncomingCalls(context.Background(), path, item)
			if requestFailed(h, "call hierarchy", err) {
				return
			}
			entries = make([]ilsp.CallHierarchyEntry, len(calls))
			for i, c := range calls {
				nav := c.From.SelectionRange.Start
				if len(c.FromRanges) > 0 {
					nav = c.FromRanges[0].Start
				}
				entries[i] = hierEntry(mgr, path, c.From, c.From.URI, nav)
			}
		} else {
			calls, err := mgr.OutgoingCalls(context.Background(), path, item)
			if requestFailed(h, "call hierarchy", err) {
				return
			}
			entries = make([]ilsp.CallHierarchyEntry, len(calls))
			for i, c := range calls {
				entries[i] = hierEntry(mgr, path, c.To, c.To.URI, c.To.SelectionRange.Start)
			}
		}
		h.Send(ilsp.CallHierarchyCallsMsg{ReqID: reqID, Incoming: incoming, Calls: entries})
	}()
	return nil
}

// hierEntry converts one CallHierarchyItem to its editor-coordinate entry,
// with the navigation target at navPos inside navURI (like locationsToRefs,
// the target file read supplies the position conversion base).
func hierEntry(mgr *manager.Manager, path string, item protocol.CallHierarchyItem, navURI string, navPos protocol.Position) ilsp.CallHierarchyEntry {
	target := protocol.URIToPath(navURI)
	e := ilsp.CallHierarchyEntry{
		Item:   item,
		Name:   item.Name,
		Detail: item.Detail,
		Path:   target,
		Line:   navPos.Line,
	}
	if data, err := os.ReadFile(target); err == nil {
		p := protocol.FromLSPPosition(strings.Split(string(data), "\n"), navPos, mgr.Encoding(path))
		e.Line, e.Col = p.Line, p.Col
	}
	return e
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
		if errors.Is(err, manager.ErrRenameUnsupported) {
			h.Send(ilsp.ServerStatusMsg{Text: "language server does not support rename", Kind: ilsp.ServerEventWarn})
			return
		}
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
	// The auto toggle (#523) only gates the initial open; a showing popup —
	// however it was opened — keeps following the cursor.
	if !active && (!b.signatureAutoEnabled() ||
		!isTriggerChar(typedChar(ev), mgr.SignatureTriggers(ev.Path))) {
		return
	}
	b.requestSignature(ev.Path, ev.Line, ev.Col, false)
}

// requestSignature asks the server for signature help at the given position
// and delivers the popup message. Manual replies (lsp.parameterInfo) may open
// the popup outside insert mode; an empty answer dismisses it. Some servers
// (gopls) answer null when the position sits inside a string literal — the
// most common place to ask "which argument is this?" — so an empty answer
// retries once at the literal's opening delimiter, which is still inside the
// argument and yields the correct active parameter (#525).
func (b *bridge) requestSignature(path string, line, col int, manual bool) {
	// Flush a pending change so signature help reflects the latest text (#595);
	// a no-op when called from flushChange, which has already drained it.
	b.flushChange(path)
	mgr := b.manager()
	if mgr == nil {
		return
	}
	go func() {
		sh, err := mgr.SignatureHelp(context.Background(), path, buffer.Position{Line: line, Col: col})
		if err != nil {
			return
		}
		msg := ilsp.SignatureContent(sh)
		if msg.Label == "" {
			if text, ok := mgr.Line(path, line); ok {
				// gopls treats the whole literal, delimiters included, as
				// answer-free, so retry just before the opening quote (the
				// space/comma/paren still belongs to the argument) and, when
				// that is also empty, just past the closing quote.
				for _, c := range stringRetryCols(text, col) {
					sh, err = mgr.SignatureHelp(context.Background(), path, buffer.Position{Line: line, Col: c})
					if err != nil {
						return
					}
					if msg = ilsp.SignatureContent(sh); msg.Label != "" {
						break
					}
				}
			}
		}
		msg.Path, msg.Manual = path, manual
		b.mu.Lock()
		if b.sigActive == nil {
			b.sigActive = map[string]bool{}
		}
		b.sigActive[path] = msg.Label != ""
		b.mu.Unlock()
		if b.h != nil {
			b.h.Send(msg)
		}
	}()
}

// stringRetryCols scans line (a single line of source) and, when rune column
// col sits inside a string literal delimited by ", ' or a backtick (honoring
// backslash escapes inside "/' literals), returns the retry candidates: the
// column before the opening delimiter, then the column after the closing one
// — positions still inside the surrounding argument list but outside the
// literal, which servers like gopls do answer (#525). Outside a literal (or
// in a multi-line literal opened on an earlier line) it returns nil and the
// fallback does not fire.
func stringRetryCols(line string, col int) []int {
	runes := []rune(line)
	open := -1
	var delim rune
	escaped := false
	for i, r := range runes {
		if i >= col {
			break
		}
		switch {
		case open >= 0 && escaped:
			escaped = false
		case open >= 0 && r == '\\' && delim != '`':
			escaped = true
		case open >= 0 && r == delim:
			open = -1
		case open < 0 && (r == '"' || r == '\'' || r == '`'):
			open, delim = i, r
		}
	}
	if open < 0 {
		return nil
	}
	var cols []int
	if open > 0 {
		cols = append(cols, open-1)
	}
	escaped = false
	for i := col; i < len(runes); i++ {
		r := runes[i]
		switch {
		case escaped:
			escaped = false
		case r == '\\' && delim != '`':
			escaped = true
		case r == delim:
			return append(cols, i+1)
		}
	}
	return cols
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

// shouldComplete decides whether a completion-trigger event warrants a server
// request (#527). A manual request (no character) always does. A typed
// character does when it is one of the server's completion trigger characters
// — falling back to "." while no server capabilities are known, preserving
// the old hard-coded behavior — or when it starts an identifier and the
// lsp.completion_auto toggle allows the as-you-type popup.
func (b *bridge) shouldComplete(ev host.EditorEvent) bool {
	if ev.Char == "" {
		return true
	}
	mgr := b.manager()
	if mgr == nil {
		return false
	}
	return completionWarranted(ev.Char, mgr.CompletionTriggers(ev.Path), b.completionAutoEnabled())
}

// completionWarranted is the trigger decision for a typed character: a server
// trigger character (defaulting to "." while none are known) always fires; an
// identifier-starting rune fires when the as-you-type popup is enabled.
func completionWarranted(ch string, triggers []string, autoIdent bool) bool {
	if len(triggers) == 0 {
		triggers = []string{"."}
	}
	if isTriggerChar(ch, triggers) {
		return true
	}
	r := []rune(ch)
	return autoIdent && len(r) == 1 && (r[0] == '_' || unicode.IsLetter(r[0]))
}

// completionAutoEnabled reads the lsp.completion_auto config toggle (#527);
// unset means enabled, matching the config default. It only gates the
// identifier-rune auto-trigger — server trigger characters and the manual
// ctrl+space request work regardless.
func (b *bridge) completionAutoEnabled() bool {
	v, ok := b.configGet("lsp.completion_auto")
	return !ok || v != "false"
}

// isTriggerChar reports whether ch is one of the server's trigger (or
// retrigger) characters.
func isTriggerChar(ch string, triggers []string) bool {
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

// inlayHintsEnabled reads the lsp.inlay_hints config toggle (#171); unset
// means disabled, matching the config default (#523).
func (b *bridge) inlayHintsEnabled() bool {
	v, ok := b.configGet("lsp.inlay_hints")
	return ok && v == "true"
}

// signatureAutoEnabled reads the lsp.signature_auto config toggle (#523);
// unset means enabled, matching the config default. It only gates the
// automatic trigger-character open — an already-showing popup keeps
// retriggering, and lsp.parameterInfo works regardless.
func (b *bridge) signatureAutoEnabled() bool {
	v, ok := b.configGet("lsp.signature_auto")
	return !ok || v != "false"
}

// configGet reads a flattened config key via the host, if one is attached.
func (b *bridge) configGet(key string) (string, bool) {
	b.mu.Lock()
	h := b.h
	b.mu.Unlock()
	if h == nil {
		return "", false
	}
	cfg := h.Config()
	if cfg == nil {
		return "", false
	}
	return cfg.Get(key)
}

// requestInlayHints refreshes the inlay hints for path, coalesced per path
// like requestSemanticTokens: at most one request runs; changes during a run
// mark a pending re-request that fires when it lands. The config toggle off
// skips the traffic entirely (the editor also stops rendering cached hints).
// Errors stay silent — a passive decoration, not a user action.
func (b *bridge) requestInlayHints(path string) {
	mgr := b.manager()
	if mgr == nil || !b.inlayHintsEnabled() {
		return
	}
	b.mu.Lock()
	if b.hintInFlight == nil {
		b.hintInFlight = map[string]bool{}
		b.hintPending = map[string]bool{}
	}
	if b.hintInFlight[path] {
		b.hintPending[path] = true
		b.mu.Unlock()
		return
	}
	b.hintInFlight[path] = true
	b.mu.Unlock()

	go func() {
		for {
			hints, err := mgr.InlayHints(context.Background(), path)
			if err == nil && b.h != nil {
				b.h.Send(ilsp.InlayHintsMsg{Path: path, Hints: hints})
			}
			b.mu.Lock()
			if b.hintPending[path] {
				b.hintPending[path] = false
				b.mu.Unlock()
				continue
			}
			b.hintInFlight[path] = false
			b.mu.Unlock()
			return
		}
	}()
}

// scheduleChange stores the latest change for a path and (re)arms the coalescing
// debounce (#595). It only appends to state and arms a timer, so it is cheap on
// the Update goroutine — the O(document) diff and the notification happen in the
// flush.
func (b *bridge) scheduleChange(ev host.EditorEvent) {
	b.mu.Lock()
	if b.pendingChange == nil {
		b.pendingChange = map[string]host.EditorEvent{}
		b.changeTimer = map[string]*time.Timer{}
	}
	b.pendingChange[ev.Path] = ev
	if t := b.changeTimer[ev.Path]; t != nil {
		t.Reset(changeDebounce)
	} else {
		path := ev.Path
		b.changeTimer[path] = time.AfterFunc(changeDebounce, func() { b.flushChange(path) })
	}
	b.mu.Unlock()
}

// flushChange syncs any pending change for path to the server now and fires the
// follow-up requests (signature/semantic/inlay/highlight). It is called from the
// debounce timer (off the Update goroutine) and synchronously from cur()/the
// completion+signature paths, so a request never sees stale server text. With no
// pending change it is a cheap no-op. Popping under the lock makes it safe for
// the timer and a request to race — only one drains the change.
func (b *bridge) flushChange(path string) {
	b.mu.Lock()
	ev, ok := b.pendingChange[path]
	if ok {
		delete(b.pendingChange, path)
	}
	if t := b.changeTimer[path]; t != nil {
		t.Stop()
		delete(b.changeTimer, path)
	}
	b.mu.Unlock()
	if !ok {
		return
	}
	if mgr := b.manager(); mgr != nil {
		_ = mgr.Change(ev.Path, ev.Text)
	}
	b.maybeSignatureHelp(ev)
	b.requestSemanticTokens(ev.Path)
	b.requestInlayHints(ev.Path)
	b.scheduleDocumentHighlight(ev.Path)
}

// cancelChange drops any pending change for path without syncing it — used when
// the document is closed so a queued sync never lands after didClose.
func (b *bridge) cancelChange(path string) {
	b.mu.Lock()
	delete(b.pendingChange, path)
	if t := b.changeTimer[path]; t != nil {
		t.Stop()
		delete(b.changeTimer, path)
	}
	b.mu.Unlock()
}

// highlightDebounce delays the occurrence-highlight request after a cursor
// move so a hjkl motion sequence fires one request, not one per step (#172).
const highlightDebounce = 150 * time.Millisecond

// scheduleDocumentHighlight (re)arms the debounced occurrence-highlight
// request (#172). The fired request reads the then-current cursor, so a
// re-arm during the delay simply moves the target.
func (b *bridge) scheduleDocumentHighlight(path string) {
	if b.manager() == nil {
		return
	}
	b.mu.Lock()
	if b.hlTimer != nil {
		b.hlTimer.Stop()
	}
	b.hlTimer = time.AfterFunc(highlightDebounce, func() { b.requestDocumentHighlight(path) })
	b.mu.Unlock()
}

// requestDocumentHighlight asks for the occurrences of the symbol under the
// cursor and delivers them; an empty set clears the editor's marks. Errors
// stay silent — this is a passive decoration, not a user-initiated action.
func (b *bridge) requestDocumentHighlight(path string) {
	mgr := b.manager()
	curPath, line, col := b.cur()
	if mgr == nil || b.h == nil || curPath != path {
		return
	}
	hs, err := mgr.DocumentHighlight(context.Background(), path, buffer.Position{Line: line, Col: col})
	if err != nil {
		return
	}
	b.h.Send(ilsp.DocumentHighlightsMsg{Path: path, Line: line, Col: col, Highlights: hs})
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
	// The server must hold the just-typed text before completing at this
	// position (#595); the completion trigger arrives via ev.Path, bypassing
	// cur(), so flush explicitly here.
	b.flushChange(path)
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
	// Convert here (on the server read-loop goroutine, off the UI thread), then
	// coalesce the delivery so a publish storm folds into one batched message.
	msg := ilsp.DiagnosticsMsg{Path: path, Diagnostics: ilsp.ConvertDiagnostics(p, lines, enc)}
	b.mu.Lock()
	if b.pendingDiags == nil {
		b.pendingDiags = map[string]ilsp.DiagnosticsMsg{}
	}
	b.pendingDiags[path] = msg
	if b.diagTimer == nil {
		b.diagTimer = time.AfterFunc(diagCoalesce, b.flushDiagnostics)
	}
	b.mu.Unlock()
}

// flushDiagnostics sends every accumulated publish as one DiagnosticsBatchMsg, so
// a workspace publish storm costs a single Update pass + re-render (#597).
func (b *bridge) flushDiagnostics() {
	b.mu.Lock()
	batch := make([]ilsp.DiagnosticsMsg, 0, len(b.pendingDiags))
	for _, m := range b.pendingDiags {
		batch = append(batch, m)
	}
	b.pendingDiags = nil
	b.diagTimer = nil
	h := b.h
	b.mu.Unlock()
	if h == nil || len(batch) == 0 {
		return
	}
	h.Send(ilsp.DiagnosticsBatchMsg{Items: batch})
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

// workspaceClosed releases the LSP state of a torn-down workspace (#825): the
// bridge's per-path caches under root drop on the spot, and the manager closes
// its documents and stops its servers under root inside the returned cmd —
// blocking server work stays off the Update goroutine (see restart).
func (b *bridge) workspaceClosed(root string) tea.Cmd {
	b.mu.Lock()
	for path, t := range b.changeTimer {
		if pathUnder(path, root) {
			t.Stop()
			delete(b.changeTimer, path)
		}
	}
	prunePaths(b.pendingChange, root)
	prunePaths(b.diags, root)
	prunePaths(b.sigActive, root)
	prunePaths(b.semInFlight, root)
	prunePaths(b.semPending, root)
	prunePaths(b.hintInFlight, root)
	prunePaths(b.hintPending, root)
	prunePaths(b.pendingDiags, root)
	mgr := b.mgr
	b.mu.Unlock()
	if mgr == nil {
		return nil
	}
	return func() tea.Msg {
		mgr.CloseRoot(root)
		return nil
	}
}

// prunePaths drops every entry whose path lies under root.
func prunePaths[V any](m map[string]V, root string) {
	for p := range m {
		if pathUnder(p, root) {
			delete(m, p)
		}
	}
}

// pathUnder reports whether path lies inside (or equals) root.
func pathUnder(path, root string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// cur returns the tracked cursor position. Every request path funnels through
// here, so it first flushes any pending debounced change (#595) — the server
// therefore always holds the latest text before a completion/hover/definition/
// … request that reads this position acts on it.
func (b *bridge) cur() (string, int, int) {
	b.mu.Lock()
	path := b.curPath
	b.mu.Unlock()
	if path != "" {
		b.flushChange(path)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.curPath, b.curLine, b.curCol
}
