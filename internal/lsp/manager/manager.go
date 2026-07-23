// Package manager owns every running language server: it maps a (language,
// workspace-root) pair to one client.Client, spawns servers lazily on the first
// relevant didOpen, routes editor operations to the right server, and forwards
// server notifications (diagnostics) back out through callbacks (Roadmap 0100).
// It is the single place that touches transport + client; features never spawn a
// process or hold a raw connection.
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"ike/internal/editor/buffer"
	"ike/internal/highlight"
	"ike/internal/highlight/semantic"
	langreg "ike/internal/lang"
	"ike/internal/lsp"
	"ike/internal/lsp/client"
	"ike/internal/lsp/jsonrpc"
	"ike/internal/lsp/protocol"
	"ike/internal/lsp/transport"
)

// requestTimeout bounds a single server request so a hung server cannot wedge a
// feature forever.
const requestTimeout = 5 * time.Second

// Connector dials a server: it creates a connection (with the manager's handler
// installed for notifications/requests) and returns the client plus a stop
// function. stderr, when non-nil, returns the server's captured stderr tail —
// the crash path extracts the decisive error line from it (#990). The default
// production connector spawns a process; tests inject an in-memory pipe.
// handler must be wired into the connection at creation so notifications are
// not missed during initialize.
type Connector func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (cl *client.Client, stop func(), stderr func() string, err error)

// Callbacks deliver async server output and status to the host (the bridge wires
// these to host.Send). All run on manager goroutines and must not block.
type Callbacks struct {
	// Diagnostics delivers a publishDiagnostics for path, with the document's
	// current lines and the server's position encoding so the receiver can map to
	// editor coordinates. For open documents the params carry the merged view:
	// the host server's diagnostics plus any embedded-fragment diagnostics
	// already mapped to host coordinates (#415).
	Diagnostics func(path string, params protocol.PublishDiagnosticsParams, lines []string, enc string)
	// Status reports a human-readable server state change (started, crashed, …).
	// kind classifies it: persistent state stays on the status line, transient
	// events become toast notifications (Roadmap 0130).
	Status func(lang, text string, kind lsp.ServerStatusKind)
	// ApplyEdit receives a server-initiated workspace/applyEdit (e.g. the
	// effect of an executed code-action command), already converted to
	// per-file editor coordinates. The manager answers applied=true whenever
	// the callback is installed.
	ApplyEdit func(files []FileEdits)
}

// Manager coordinates servers and open documents.
type Manager struct {
	resolve func(lang string) (lsp.ServerSpec, bool)
	connect Connector
	cb      Callbacks

	mu       sync.Mutex
	servers  map[string]*server // key: lang + "\x00" + root
	docs     map[string]*document
	restarts map[string]int // crash-restart attempts per server key
	// companionsHinted marks languages whose optional companion tools were
	// already probed (#1067) — the missing-tool hint fires once per language
	// per manager lifetime, not per file or per root.
	companionsHinted map[string]bool

	// Embedded-fragment state (0300, #413): detector, fragment documents per
	// host path (keyed by detector slot), and a per-host generation counter so
	// only the newest async sync run commits. fragMu serializes reconciliation.
	detect  FragmentDetector
	frags   map[string]map[int]*fragmentDoc
	fragGen map[string]int
	fragMu  sync.Mutex

	// Diagnostics state (#415): the host server's last publish per host path
	// and each fragment server's last publish per (host, slot), merged into
	// one host-path publish whenever either side changes.
	hostDiags map[string][]protocol.Diagnostic
	fragDiags map[string]map[int][]fragDiagnostic
}

// server is one running language server instance.
type server struct {
	lang    string
	root    string
	cl      *client.Client
	stop    func()
	stderr  func() string // captured stderr tail; nil for in-memory connectors
	spec    lsp.ServerSpec
	closing bool // set before a deliberate stop so watchExit does not restart
}

// document tracks an open buffer's identity and latest text, so the manager can
// convert positions and resend full text on change (full-sync MVP).
type document struct {
	path    string
	lang    string // server language: keys the server instance and spec
	langID  string // wire languageId sent in didOpen (differs for delegating languages, #1063)
	root    string
	version int
	lines   []string
	srvKey  string
	// Semantic-token state (#9): the last full data array and its result id,
	// so a delta-capable server only sends edits.
	semData     []uint32
	semResultID string
}

// New builds a manager. resolve maps a language to its ServerSpec; connect dials
// servers (pass nil for the default process connector); cb receives async output.
func New(resolve func(lang string) (lsp.ServerSpec, bool), connect Connector, cb Callbacks) *Manager {
	if connect == nil {
		connect = processConnector
	}
	return &Manager{
		resolve:  resolve,
		connect:  connect,
		cb:       cb,
		servers:  make(map[string]*server),
		docs:     make(map[string]*document),
		restarts: make(map[string]int),

		companionsHinted: make(map[string]bool),
		frags:    make(map[string]map[int]*fragmentDoc),
		fragGen:  make(map[string]int),

		hostDiags: make(map[string][]protocol.Diagnostic),
		fragDiags: make(map[string]map[int][]fragDiagnostic),
	}
}

// processConnector is the default connector: spawn the server binary over stdio.
func processConnector(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), func() string, error) {
	proc, err := transport.Start(transport.Spec{
		Command: spec.Command,
		Args:    spec.Args,
		Env:     spec.Env,
		Dir:     root,
		LogPath: LogPath(spec.Language),
	})
	if err != nil {
		return nil, nil, nil, err
	}
	conn := jsonrpc.NewConn(proc.Conn(), handler)
	cl := client.New(conn)
	stop := func() {
		_ = conn.Close()
		_ = proc.Stop()
	}
	return cl, stop, proc.Stderr, nil
}

func key(lang, root string) string { return lang + "\x00" + root }

// serverLangFor maps a document language to the language whose server handles
// it (#1063): a registered language with ServerLanguage set delegates (e.g.
// "go.mod" → "go"); everything else — including ids unknown to the registry —
// maps to itself.
func serverLangFor(langID string) string {
	if l, ok := langreg.ByID(langID); ok {
		return l.ServerLang()
	}
	return langID
}

// Open registers a document and ensures its server is running, sending didOpen.
// It blocks on the initialize handshake the first time a (lang, root) server is
// needed — safe because the bridge calls this from a tea.Cmd goroutine.
func (m *Manager) Open(path, lang, text string) error {
	// A delegating language (#1063, e.g. "go.mod") runs on its delegate's
	// server: spec resolution, the server key and every status message use
	// the server language, while the didOpen languageId keeps the document
	// language's own id — exactly what gopls expects for go.mod/go.work.
	srvLang := serverLangFor(lang)
	spec, ok := m.resolve(srvLang)
	if !ok {
		return nil // no server configured for this language: silent no-op
	}
	root := detectRoot(path, spec.RootMarkers)
	srv, err := m.ensureServer(srvLang, root, spec)
	if err != nil {
		text, kind := statusForErr(spec.Command, err)
		m.status(srvLang, text, kind)
		return err
	}

	lines := splitLines(text)
	m.mu.Lock()
	doc := &document{path: path, lang: srvLang, langID: lang, root: root, version: 1, lines: lines, srvKey: srv.key()}
	m.docs[path] = doc
	m.mu.Unlock()

	err = srv.cl.DidOpen(protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        protocol.PathToURI(path),
			LanguageID: doc.langID,
			Version:    doc.version,
			Text:       text,
		},
	})
	m.scheduleFragmentSync(path)
	return err
}

// Change resends the full document text (full-sync MVP) under a monotonically
// increasing version the manager owns. Unknown documents (not yet opened) are a
// no-op.
func (m *Manager) Change(path, text string) error {
	m.mu.Lock()
	doc, ok := m.docs[path]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	oldLines := doc.lines
	srv := m.servers[doc.srvKey]
	m.mu.Unlock()
	if srv == nil {
		return nil
	}

	// Respect the negotiated sync kind (#13): incremental servers get the
	// minimal change region diffed against the previously synced lines;
	// full-sync servers get the whole document; SyncNone servers get nothing.
	// The version stays monotonic per document and only advances when a
	// notification is actually sent.
	var changes []protocol.TextDocumentContentChangeEvent
	switch srv.cl.Caps().SyncKind {
	case protocol.SyncNone:
		m.setDocLines(path, text)
		m.scheduleFragmentSync(path)
		return nil
	case protocol.SyncIncremental:
		ev, changed := incrementalEvent(oldLines, text, srv.cl.Encoding())
		if !changed {
			return nil
		}
		changes = []protocol.TextDocumentContentChangeEvent{ev}
	default:
		changes = []protocol.TextDocumentContentChangeEvent{{Text: text}}
	}

	version := m.setDocLines(path, text)
	err := srv.cl.DidChange(protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{URI: protocol.PathToURI(path), Version: version},
		ContentChanges: changes,
	})
	m.scheduleFragmentSync(path)
	return err
}

// setDocLines commits the new document text and bumps its version.
func (m *Manager) setDocLines(path, text string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[path]
	if !ok {
		return 0
	}
	doc.version++
	doc.lines = splitLines(text)
	return doc.version
}

// Save sends didSave for path.
func (m *Manager) Save(path string) error {
	srv, _, ok := m.docServer(path)
	if !ok {
		return nil
	}
	return srv.cl.DidSave(protocol.DidSaveTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
	})
}

// Close sends didClose and forgets the document, including its fragments.
func (m *Manager) Close(path string) error {
	m.closeFragmentsFor(path)
	srv, _, ok := m.docServer(path)
	m.mu.Lock()
	delete(m.docs, path)
	delete(m.hostDiags, path)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return srv.cl.DidClose(protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
	})
}

// ConvertCompletionItems maps protocol completion items to editor items and
// converts each item's additionalTextEdits (auto-import, #848) to editor
// coordinates against the synced document. An unknown document (or a
// fragment-routed result whose edits would target the virtual doc) keeps the
// items but drops the additional edits.
func (m *Manager) ConvertCompletionItems(path string, items []protocol.CompletionItem) []lsp.CompletionItem {
	out := lsp.ConvertCompletion(items)
	srv, doc, ok := m.docServer(path)
	if !ok {
		return out
	}
	enc := srv.cl.Encoding()
	for i := range items {
		if len(items[i].AdditionalTextEdits) == 0 {
			continue
		}
		out[i].AdditionalEdits = convertEdits(doc.lines, items[i].AdditionalTextEdits, enc)
	}
	return out
}

// ResolveCompletion runs completionItem/resolve for one raw item (#847),
// gated on the server's resolveProvider; ok is false when the document has no
// server or the server does not resolve.
func (m *Manager) ResolveCompletion(ctx context.Context, path string, item protocol.CompletionItem) (protocol.CompletionItem, bool, error) {
	srv, _, okDoc := m.docServer(path)
	if !okDoc || !srv.cl.Caps().CompletionResolve {
		return item, false, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	out, err := srv.cl.Resolve(cctx, item)
	return out, err == nil, err
}

// Completion requests completion at an editor position, gated on capability.
// A position inside an embedded fragment routes to the fragment's server
// (0300, #414) with results mapped back to host coordinates.
func (m *Manager) Completion(ctx context.Context, path string, pos buffer.Position, triggerChar string) ([]protocol.CompletionItem, bool, error) {
	if items, incomplete, handled, err := m.fragmentCompletion(ctx, path, pos, triggerChar); handled {
		// Fragment edits target the virtual document; the host-coordinate
		// conversion in ConvertCompletionItems would misplace them (#848).
		for i := range items {
			items[i].AdditionalTextEdits = nil
		}
		return items, incomplete, err
	}
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().Completion {
		return nil, false, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.Completion(cctx, protocol.CompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
		Context:      completionContext(triggerChar, srv.cl.Caps().CompletionTriggers),
	})
}

// completionContext builds the request context (#850): a typed character that
// is one of the server's declared trigger characters reports TriggerCharacter
// with the character; anything else (identifier runes, manual ctrl+space)
// reports Invoked.
func completionContext(ch string, triggers []string) *protocol.CompletionContext {
	if ch != "" {
		for _, t := range triggers {
			if t == ch {
				return &protocol.CompletionContext{TriggerKind: protocol.CompletionTriggerCharacter, TriggerCharacter: ch}
			}
		}
	}
	return &protocol.CompletionContext{TriggerKind: protocol.CompletionTriggerInvoked}
}

// Hover requests hover at an editor position, gated on capability. Fragment
// positions route to the fragment's server like Completion.
func (m *Manager) Hover(ctx context.Context, path string, pos buffer.Position) (*protocol.Hover, error) {
	if h, handled, err := m.fragmentHover(ctx, path, pos); handled {
		return h, err
	}
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().Hover {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.Hover(cctx, protocol.HoverParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
	})
}

// Definition requests definition locations at an editor position. Fragment
// positions route to the fragment's server (0300, #416); fragment-document
// locations in the result are rewritten to host-file locations.
func (m *Manager) Definition(ctx context.Context, path string, pos buffer.Position) ([]protocol.Location, error) {
	if locs, handled, err := m.fragmentDefinition(ctx, path, pos); handled {
		return locs, err
	}
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().Definition {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.Definition(cctx, protocol.DefinitionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
	})
}

// DefinitionSupported reports whether path currently has a ready server
// advertising the definition capability (#858) — the difference between "the
// server found nothing" and "nobody was asked", so a silent F4 can say which.
func (m *Manager) DefinitionSupported(path string) bool {
	srv, _, ok := m.docServer(path)
	return ok && srv.cl.Caps().Definition
}

// maxWorkspaceSymbols caps the merged workspace/symbol result so one broad
// query never floods the palette.
const maxWorkspaceSymbols = 200

// WorkspaceSymbols fans the query out to every running server advertising the
// workspaceSymbolProvider capability and merges the results, capped (0250,
// #294). ok=false when no running server supports the request at all, so the
// caller can distinguish "no provider" from "no hits".
func (m *Manager) WorkspaceSymbols(ctx context.Context, query string) ([]protocol.SymbolInformation, bool) {
	m.mu.Lock()
	var clients []*server
	for _, srv := range m.servers {
		if srv.cl != nil && srv.cl.Caps().WorkspaceSymbol {
			clients = append(clients, srv)
		}
	}
	m.mu.Unlock()
	if len(clients) == 0 {
		return nil, false
	}
	var out []protocol.SymbolInformation
	for _, srv := range clients {
		cctx, cancel := context.WithTimeout(ctx, requestTimeout)
		syms, err := srv.cl.WorkspaceSymbols(cctx, protocol.WorkspaceSymbolParams{Query: query})
		cancel()
		if err != nil {
			continue // one slow/broken server must not sink the others
		}
		out = append(out, syms...)
		if len(out) >= maxWorkspaceSymbols {
			out = out[:maxWorkspaceSymbols]
			break
		}
	}
	return out, true
}

// References requests every reference to the symbol at an editor position.
// IncludeDeclaration mirrors the LSP request option (JetBrains' find-usages
// includes the declaration, so callers default to true). Fragment positions
// route to the fragment's server (0300, #416) like Definition.
func (m *Manager) References(ctx context.Context, path string, pos buffer.Position, includeDecl bool) ([]protocol.Location, error) {
	if locs, handled, err := m.fragmentReferences(ctx, path, pos, includeDecl); handled {
		return locs, err
	}
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().References {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.References(cctx, protocol.ReferenceParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
		Context:      protocol.ReferenceContext{IncludeDeclaration: includeDecl},
	})
}

// DocumentHighlight requests the occurrences of the symbol at an editor
// position (#172), returned in editor coordinates (the manager owns the
// synced document lines, so the conversion happens here, like Format).
// Fragment positions route to the fragment's server like Hover.
func (m *Manager) DocumentHighlight(ctx context.Context, path string, pos buffer.Position) ([]lsp.DocumentHighlight, error) {
	if hs, handled, err := m.fragmentDocumentHighlight(ctx, path, pos); handled {
		return hs, err
	}
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().DocumentHighlight {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	hs, err := srv.cl.DocumentHighlight(cctx, protocol.DocumentHighlightParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
	})
	if err != nil {
		return nil, err
	}
	return convertHighlights(doc.lines, hs, srv.cl.Encoding()), nil
}

// DocumentSymbols requests the symbol tree of one document (#1025), converted
// to editor coordinates — the Structure tool pane's data source. ok=false when
// no ready server tracks the document or the server lacks the
// documentSymbolProvider capability, so the caller can distinguish "no
// provider" from "no symbols".
func (m *Manager) DocumentSymbols(ctx context.Context, path string) ([]lsp.SymbolNode, bool, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().DocumentSymbol {
		return nil, false, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	syms, err := srv.cl.DocumentSymbols(cctx, protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
	})
	if err != nil {
		return nil, true, err
	}
	return lsp.ConvertDocumentSymbols(syms, doc.lines, srv.cl.Encoding()), true, nil
}

// convertHighlights maps protocol document highlights to editor coordinates.
func convertHighlights(lines []string, hs []protocol.DocumentHighlight, enc string) []lsp.DocumentHighlight {
	out := make([]lsp.DocumentHighlight, len(hs))
	for i, h := range hs {
		out[i] = lsp.DocumentHighlight{Range: protocol.FromLSPRange(lines, h.Range, enc), Kind: h.Kind}
	}
	return out
}

// InlayHints requests the inline parameter/type hints for a whole document
// (#171), returned in editor coordinates sorted by position. Hints inside
// embedded fragments come from each fragment's server, mapped onto the host
// buffer; a fragment failure only drops that fragment's hints — the decoration
// is passive, so partial results beat none.
func (m *Manager) InlayHints(ctx context.Context, path string) ([]lsp.InlayHint, error) {
	var out []lsp.InlayHint
	srv, doc, ok := m.docServer(path)
	if ok && srv.cl.Caps().InlayHint {
		enc := srv.cl.Encoding()
		cctx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		hints, err := srv.cl.InlayHints(cctx, protocol.InlayHintParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
			Range:        wholeDocRange(doc.lines, enc),
		})
		if err != nil {
			return nil, err
		}
		for _, h := range hints {
			out = append(out, convertInlayHint(doc.lines, h, enc, nil))
		}
	}
	out = append(out, m.fragmentInlayHints(ctx, path)...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Col < out[j].Col
	})
	return out, nil
}

// wholeDocRange is the full-document LSP range, for range-scoped requests that
// IKE issues document-wide.
func wholeDocRange(lines []string, enc string) protocol.Range {
	last := len(lines) - 1
	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.ToLSPPosition(lines, buffer.Position{Line: last, Col: len([]rune(lines[last]))}, enc),
	}
}

// convertInlayHint maps one protocol hint to editor coordinates; toHost (nil
// for host-document hints) additionally maps a fragment position onto the
// host buffer.
func convertInlayHint(lines []string, h protocol.InlayHint, enc string, toHost func(buffer.Position) buffer.Position) lsp.InlayHint {
	p := protocol.FromLSPPosition(lines, h.Position, enc)
	if toHost != nil {
		p = toHost(p)
	}
	return lsp.InlayHint{Line: p.Line, Col: p.Col, Label: string(h.Label), Kind: h.Kind, PadLeft: h.PaddingLeft, PadRight: h.PaddingRight}
}

// PrepareCallHierarchy resolves the symbol at an editor position into
// call-hierarchy items (#173), gated on the server capability.
func (m *Manager) PrepareCallHierarchy(ctx context.Context, path string, pos buffer.Position) ([]protocol.CallHierarchyItem, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().CallHierarchy {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.PrepareCallHierarchy(cctx, protocol.CallHierarchyPrepareParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
	})
}

// IncomingCalls requests the callers of a prepared item (#173). Path names the
// document the hierarchy was prepared from and selects the server.
func (m *Manager) IncomingCalls(ctx context.Context, path string, item protocol.CallHierarchyItem) ([]protocol.CallHierarchyIncomingCall, error) {
	srv, _, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().CallHierarchy {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.IncomingCalls(cctx, protocol.CallHierarchyCallsParams{Item: item})
}

// OutgoingCalls requests the callees of a prepared item (#173).
func (m *Manager) OutgoingCalls(ctx context.Context, path string, item protocol.CallHierarchyItem) ([]protocol.CallHierarchyOutgoingCall, error) {
	srv, _, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().CallHierarchy {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.OutgoingCalls(cctx, protocol.CallHierarchyCallsParams{Item: item})
}

// Format requests whole-document formatting and returns the edits already
// converted to editor rune coordinates (the manager owns the synced document
// lines, so the UTF-16 mapping happens here and nowhere else).
func (m *Manager) Format(ctx context.Context, path string, opts protocol.FormattingOptions) ([]lsp.FormatEdit, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().Formatting {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	edits, err := srv.cl.Formatting(cctx, protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Options:      opts,
	})
	if err != nil {
		return nil, err
	}
	return convertEdits(doc.lines, edits, srv.cl.Encoding()), nil
}

// FormatRange requests formatting for the [start, end) editor range, converted
// through the negotiated encoding both ways.
func (m *Manager) FormatRange(ctx context.Context, path string, start, end buffer.Position, opts protocol.FormattingOptions) ([]lsp.FormatEdit, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().RangeFormatting {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	edits, err := srv.cl.RangeFormatting(cctx, protocol.DocumentRangeFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Range:        protocol.ToLSPRange(doc.lines, buffer.Range{Start: start, End: end}, srv.cl.Encoding()),
		Options:      opts,
	})
	if err != nil {
		return nil, err
	}
	return convertEdits(doc.lines, edits, srv.cl.Encoding()), nil
}

// ErrRenameUnsupported reports that the document's server does not offer
// rename at all (e.g. intelephense without a licence), as opposed to a
// position the server rejected — the two deserve different user feedback.
var ErrRenameUnsupported = errors.New("server does not support rename")

// PrepareRename validates a rename at an editor position. ok reports whether
// the position is renameable; placeholder is the symbol text the prompt should
// prefill (empty when the server offers no range — defaultBehavior — or no
// prepareRename support at all, which skips validation entirely). A server
// without the rename capability returns ErrRenameUnsupported.
func (m *Manager) PrepareRename(ctx context.Context, path string, pos buffer.Position) (placeholder string, ok bool, err error) {
	srv, doc, found := m.docServer(path)
	if !found {
		return "", false, nil
	}
	if !srv.cl.Caps().Rename {
		return "", false, ErrRenameUnsupported
	}
	if !srv.cl.Caps().PrepareRename {
		// No server-side validation offered; let the rename attempt decide.
		return "", true, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	r, valid, err := srv.cl.PrepareRename(cctx, protocol.PrepareRenameParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
	})
	if err != nil || !valid {
		return "", false, err
	}
	er := protocol.FromLSPRange(doc.lines, r, srv.cl.Encoding())
	if er.Start.Line == er.End.Line && er.Start.Line >= 0 && er.Start.Line < len(doc.lines) && er.End.Col > er.Start.Col {
		runes := []rune(doc.lines[er.Start.Line])
		if er.End.Col <= len(runes) {
			placeholder = string(runes[er.Start.Col:er.End.Col])
		}
	}
	return placeholder, true, nil
}

// FileEdits is one file's slice of a WorkspaceEdit, converted to editor rune
// coordinates. Open reports whether the manager tracks the document (an open
// editor buffer): open files are edited in-buffer, the rest on disk.
type FileEdits struct {
	Path  string
	Open  bool
	Edits []lsp.FormatEdit
}

// Rename requests the workspace-wide rename and returns the edits per file,
// deterministically ordered by path. Files the manager does not track are
// read from disk for the position conversion.
func (m *Manager) Rename(ctx context.Context, path string, pos buffer.Position, newName string) ([]FileEdits, error) {
	srv, doc, found := m.docServer(path)
	if !found || !srv.cl.Caps().Rename {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	we, err := srv.cl.Rename(cctx, protocol.RenameParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
		NewName:      newName,
	})
	if err != nil {
		return nil, err
	}
	return m.convertWorkspaceEdit(we, srv.cl.Encoding()), nil
}

// convertWorkspaceEdit maps a WorkspaceEdit into per-file editor-coordinate
// edits, deterministically ordered by path: open documents convert from their
// synced lines, closed files are read from disk (a vanished target is skipped
// rather than corrupted).
func (m *Manager) convertWorkspaceEdit(we protocol.WorkspaceEdit, enc string) []FileEdits {
	var out []FileEdits
	for uri, edits := range we.AllChanges() {
		target := protocol.URIToPath(uri)
		lines, open := m.DocLines(target)
		if !open {
			data, rerr := os.ReadFile(target)
			if rerr != nil {
				continue
			}
			lines = splitLines(string(data))
		}
		out = append(out, FileEdits{Path: target, Open: open, Edits: convertEdits(lines, edits, enc)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ConvertWorkspaceEdit converts we under the encoding of the server handling
// path — the seam a code action's inline Edit applies through.
func (m *Manager) ConvertWorkspaceEdit(path string, we protocol.WorkspaceEdit) []FileEdits {
	srv, _, ok := m.docServer(path)
	if !ok {
		return nil
	}
	return m.convertWorkspaceEdit(we, srv.cl.Encoding())
}

// CodeActions requests the actions available for the [start, end] editor
// range, passing the client-known diagnostics so servers offer quick-fixes.
func (m *Manager) CodeActions(ctx context.Context, path string, start, end buffer.Position, diags []protocol.Diagnostic) ([]protocol.CodeAction, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().CodeAction {
		return nil, nil
	}
	if diags == nil {
		diags = []protocol.Diagnostic{}
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.CodeActions(cctx, protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Range:        protocol.ToLSPRange(doc.lines, buffer.Range{Start: start, End: end}, srv.cl.Encoding()),
		Context:      protocol.CodeActionContext{Diagnostics: diags},
	})
}

// ExecuteCommand runs a server-defined command for the server handling path;
// its effects arrive as workspace/applyEdit requests (Callbacks.ApplyEdit).
func (m *Manager) ExecuteCommand(ctx context.Context, path string, cmd protocol.Command) error {
	srv, _, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().ExecuteCommand {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.ExecuteCommand(cctx, protocol.ExecuteCommandParams{Command: cmd.Command, Arguments: cmd.Arguments})
}

// DocLines returns the tracked document lines for path, when open.
func (m *Manager) DocLines(path string) ([]string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if doc, ok := m.docs[path]; ok {
		return doc.lines, true
	}
	return nil, false
}

// convertEdits maps server TextEdits into editor-coordinate FormatEdits.
func convertEdits(lines []string, edits []protocol.TextEdit, enc string) []lsp.FormatEdit {
	out := make([]lsp.FormatEdit, len(edits))
	for i, e := range edits {
		r := protocol.FromLSPRange(lines, e.Range, enc)
		out[i] = lsp.FormatEdit{
			StartLine: r.Start.Line, StartCol: r.Start.Col,
			EndLine: r.End.Line, EndCol: r.End.Col,
			Text: e.NewText,
		}
	}
	return out
}

// SignatureHelp requests call-signature info at an editor position.
func (m *Manager) SignatureHelp(ctx context.Context, path string, pos buffer.Position) (*protocol.SignatureHelp, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().SignatureHelp {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.SignatureHelp(cctx, protocol.SignatureHelpParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
	})
}

// Line returns the synced text of one line of an open document — the exact
// text the server has seen, unlike a disk read that misses unsaved edits.
func (m *Manager) Line(path string, line int) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[path]
	if !ok || line < 0 || line >= len(doc.lines) {
		return "", false
	}
	return doc.lines[line], true
}

// CompletionTriggers returns the trigger characters the server handling path
// advertises for completion.
func (m *Manager) CompletionTriggers(path string) []string {
	if srv, _, ok := m.docServer(path); ok {
		return srv.cl.Caps().CompletionTriggers
	}
	return nil
}

// SignatureTriggers returns the trigger (and retrigger) characters the server
// handling path advertises for signature help.
func (m *Manager) SignatureTriggers(path string) []string {
	if srv, _, ok := m.docServer(path); ok {
		return srv.cl.Caps().SignatureTriggers
	}
	return nil
}

// SemanticTokens requests (or delta-updates) the document's semantic tokens
// and returns them decoded into highlight spans against the server's legend.
// Gated on the capability: no support returns nil spans, no error.
func (m *Manager) SemanticTokens(ctx context.Context, path string) ([]highlight.Span, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().SemanticTokens {
		return nil, nil
	}
	caps := srv.cl.Caps()
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	m.mu.Lock()
	prevID, prevData := doc.semResultID, doc.semData
	m.mu.Unlock()

	var data []uint32
	var resultID string
	uri := protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)}
	if caps.SemanticDelta && prevID != "" {
		delta, full, err := srv.cl.SemanticTokensDelta(cctx, protocol.SemanticTokensDeltaParams{TextDocument: uri, PreviousResultID: prevID})
		if err != nil {
			return nil, err
		}
		switch {
		case delta != nil:
			data, resultID = semantic.ApplyDelta(prevData, delta.Edits), delta.ResultID
		case full != nil:
			data, resultID = full.Data, full.ResultID
		default:
			return nil, nil
		}
	} else {
		full, err := srv.cl.SemanticTokensFull(cctx, protocol.SemanticTokensParams{TextDocument: uri})
		if err != nil || full == nil {
			return nil, err
		}
		data, resultID = full.Data, full.ResultID
	}

	m.mu.Lock()
	lines := doc.lines
	doc.semData, doc.semResultID = data, resultID
	m.mu.Unlock()
	legend := semantic.Legend{TokenTypes: caps.SemanticTypes, TokenModifiers: caps.SemanticModifiers}
	return semantic.Decode(data, legend, lines, srv.cl.Encoding()), nil
}

// Encoding returns the negotiated position encoding for the server handling path,
// defaulting to UTF-16 when unknown. Used to convert results (e.g. definition
// targets) back to editor coordinates.
func (m *Manager) Encoding(path string) string {
	if srv, _, ok := m.docServer(path); ok {
		return srv.cl.Encoding()
	}
	return protocol.EncodingUTF16
}

// StopLang stops every running server for one language (all roots), dropping
// its open documents; the next document event respawns it lazily — the
// per-server restart of the settings page (#130). Best-effort, like Shutdown.
func (m *Manager) StopLang(lang string) {
	m.mu.Lock()
	var stopped []*server
	for k, srv := range m.servers {
		if srv.lang != lang {
			continue
		}
		srv.closing = true // suppress restart on the resulting Done
		stopped = append(stopped, srv)
		delete(m.servers, k)
		delete(m.restarts, k)
	}
	type clearedDoc struct {
		path    string
		lines   []string
		version int
	}
	var cleared []clearedDoc
	for path, doc := range m.docs {
		if doc.lang == lang {
			cleared = append(cleared, clearedDoc{path, doc.lines, doc.version})
			delete(m.docs, path)
			delete(m.frags, path)
			delete(m.fragGen, path)
			delete(m.hostDiags, path)
			delete(m.fragDiags, path)
		}
	}
	// Fragment documents in the stopped language lose their server; drop them
	// (and their published diagnostics) so the next host change reopens cleanly.
	republish := map[string]bool{}
	for host, fds := range m.frags {
		for slot, fd := range fds {
			if fd.lang == lang {
				delete(fds, slot)
				if _, ok := m.fragDiags[host][slot]; ok {
					delete(m.fragDiags[host], slot)
					republish[host] = true
				}
			}
		}
		if len(fds) == 0 {
			delete(m.frags, host)
		}
		if len(m.fragDiags[host]) == 0 {
			delete(m.fragDiags, host)
		}
	}
	m.mu.Unlock()
	for _, srv := range stopped {
		if srv.stop != nil {
			srv.stop()
		}
	}
	// The stopped language's documents left the manager, but their editors
	// stay open — tell them explicitly that no diagnostics remain (#994).
	for _, d := range cleared {
		m.publishEmpty(d.path, d.lines, d.version)
	}
	for host := range republish {
		m.publishHostDiagnostics(host)
	}
}

// CloseRoot releases everything the manager holds under one project root
// (#825): every open document whose path lies inside root closes (didClose to
// servers that keep running), and every server rooted inside root stops
// outright. Called when a background workspace is torn down; the next
// document event respawns lazily. Best-effort, like Shutdown.
func (m *Manager) CloseRoot(root string) {
	m.mu.Lock()
	var paths []string
	for path := range m.docs {
		if underRoot(path, root) {
			paths = append(paths, path)
		}
	}
	m.mu.Unlock()
	// Close releases the doc, its fragments and its diagnostics, and notifies
	// the server — wasted on servers stopped below, but harmless.
	for _, p := range paths {
		_ = m.Close(p)
	}
	m.mu.Lock()
	var stopped []*server
	for k, srv := range m.servers {
		if !underRoot(srv.root, root) {
			continue
		}
		srv.closing = true // suppress restart on the resulting Done
		stopped = append(stopped, srv)
		delete(m.servers, k)
		delete(m.restarts, k)
	}
	m.mu.Unlock()
	for _, srv := range stopped {
		if srv.stop != nil {
			srv.stop()
		}
	}
}

// underRoot reports whether path lies inside (or equals) root.
func underRoot(path, root string) bool {
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// RunningLangs returns the languages with at least one live server, sorted.
func (m *Manager) RunningLangs() []string {
	m.mu.Lock()
	seen := map[string]bool{}
	for _, srv := range m.servers {
		seen[srv.lang] = true
	}
	m.mu.Unlock()
	out := make([]string, 0, len(seen))
	for l := range seen {
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// Shutdown stops every server. Best-effort; used on app exit and by the
// restart commands, so open editors get an explicit empty publish for every
// tracked document (#994) — a respawned server republishes on reopen.
func (m *Manager) Shutdown() {
	type clearedDoc struct {
		path    string
		lines   []string
		version int
	}
	m.mu.Lock()
	servers := m.servers
	var cleared []clearedDoc
	for path, doc := range m.docs {
		cleared = append(cleared, clearedDoc{path, doc.lines, doc.version})
	}
	m.servers = make(map[string]*server)
	m.docs = make(map[string]*document)
	m.frags = make(map[string]map[int]*fragmentDoc)
	m.fragGen = make(map[string]int)
	m.hostDiags = make(map[string][]protocol.Diagnostic)
	m.fragDiags = make(map[string]map[int][]fragDiagnostic)
	for _, srv := range servers {
		srv.closing = true // suppress restart on the resulting Done
	}
	m.mu.Unlock()
	for _, srv := range servers {
		if srv.stop != nil {
			srv.stop()
		}
	}
	for _, d := range cleared {
		m.publishEmpty(d.path, d.lines, d.version)
	}
}

// ensureServer returns the running server for (lang, root), spawning + initialising
// it on first use.
func (m *Manager) ensureServer(lang, root string, spec lsp.ServerSpec) (*server, error) {
	spec = withToolchain(lang, root, spec)
	k := key(lang, root)
	m.mu.Lock()
	if srv, ok := m.servers[k]; ok {
		m.mu.Unlock()
		return srv, nil
	}
	m.mu.Unlock()

	handler := jsonrpc.Handler{
		Notify:  func(method string, params json.RawMessage) { m.onNotify(lang, method, params) },
		Request: func(id jsonrpc.ID, method string, params json.RawMessage) { m.onRequest(k, id, method, params) },
	}
	cl, stop, stderr, err := m.connect(spec, root, handler)
	if err != nil {
		return nil, err
	}
	srv := &server{lang: lang, root: root, cl: cl, stop: stop, stderr: stderr, spec: spec}
	// Register before Initialize: a server may issue workspace/configuration as
	// soon as it receives our "initialized" notification (sent inside
	// Initialize), and onRequest must find the server to answer it with the
	// toolchain-detected settings — otherwise the request is dropped and e.g.
	// pyright never learns the Python interpreter path (#563).
	m.mu.Lock()
	// Another goroutine may have raced us; keep the first winner.
	if existing, ok := m.servers[k]; ok {
		m.mu.Unlock()
		if stop != nil {
			stop()
		}
		return existing, nil
	}
	m.servers[k] = srv
	m.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout*2)
	defer cancel()
	if _, err := cl.Initialize(ctx, client.InitParams{RootURI: protocol.PathToURI(root), ProcessID: os.Getpid(), InitializationOptions: spec.SettingsJSON()}); err != nil {
		m.mu.Lock()
		if m.servers[k] == srv {
			delete(m.servers, k)
		}
		m.mu.Unlock()
		if stop != nil {
			stop()
		}
		// A broken binary dies before answering initialize; the transport
		// error alone ("jsonrpc: connection closed") tells the user nothing.
		// Fold the decisive stderr line in (#1062), like the crash path does.
		return nil, startupError(err, srv.stderr)
	}

	m.status(lang, lang+" language server ready", lsp.ServerState)
	// Ready is the moment a missing optional companion becomes relevant: the
	// server works, but a delegated capability is silently off (#1067).
	m.hintCompanions(lang, spec)
	go m.watchExit(srv)
	return srv, nil
}

// watchExit reacts to a server connection ending: a deliberate stop is silent; an
// unexpected exit (crash) triggers restart.go's recovery.
func (m *Manager) watchExit(srv *server) {
	<-srv.cl.Done()
	m.mu.Lock()
	deliberate := srv.closing
	if m.servers[srv.key()] == srv {
		delete(m.servers, srv.key())
	}
	docs := m.docsForServerLocked(srv.key())
	m.mu.Unlock()
	if deliberate {
		return
	}
	// Name the concrete error when the stderr tail yields one (#990) — both
	// in the toast and as a log marker, so neither reader has to fish the
	// message out of a raw dump.
	tail := ""
	if srv.stderr != nil {
		tail = transport.ErrorLine(srv.stderr())
	}
	text, marker := srv.lang+" language server crashed", "server crashed"
	if tail != "" {
		text += ": " + tail
		marker += ": " + tail
	}
	m.status(srv.lang, text, lsp.ServerEventWarn)
	appendLog(srv.lang, marker)
	go m.restart(srv, docs, tail)
}

// onNotify routes a server notification. Only publishDiagnostics is consumed in
// the MVP; the rest are ignored.
func (m *Manager) onNotify(lang, method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p protocol.PublishDiagnosticsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return
	}
	if isFragmentURI(string(p.URI)) {
		m.onFragmentDiagnostics(p) // mapped back onto the host buffer (#415)
		return
	}
	path := protocol.URIToPath(p.URI)
	m.mu.Lock()
	doc := m.docs[path]
	if doc != nil {
		m.hostDiags[path] = p.Diagnostics
	}
	m.mu.Unlock()
	if doc == nil {
		// Not an open document (a dependency the server also checks): pass
		// through untouched — there is nothing to merge with.
		if m.cb.Diagnostics != nil {
			m.cb.Diagnostics(path, p, nil, "")
		}
		return
	}
	m.publishHostDiagnostics(path)
}

// onRequest answers server→client requests minimally so a server does not stall:
// configuration gets a matching array of nulls; everything else gets null.
func (m *Manager) onRequest(srvKey string, id jsonrpc.ID, method string, params json.RawMessage) {
	m.mu.Lock()
	srv := m.servers[srvKey]
	m.mu.Unlock()
	if srv == nil {
		return
	}
	switch method {
	case "workspace/configuration":
		// Answer each requested section from the server's merged settings (which
		// include toolchain-detected values like the Python interpreter path); an
		// absent section returns null, as before.
		var p struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		out := make([]any, len(p.Items))
		for i, it := range p.Items {
			out[i] = settingsSection(srv.spec.Settings, it.Section)
		}
		_ = srv.cl.Respond(id, out, nil)
	case "workspace/applyEdit":
		// The effect of an executed command (code actions, #8): convert and
		// hand to the ApplyEdit callback. Off the read-loop goroutine — the
		// conversion may read files from disk, and responding inline can
		// deadlock against a server still flushing its own write.
		var p protocol.ApplyWorkspaceEditParams
		_ = json.Unmarshal(params, &p)
		go func() {
			applied := false
			if m.cb.ApplyEdit != nil {
				m.cb.ApplyEdit(m.convertWorkspaceEdit(p.Edit, srv.cl.Encoding()))
				applied = true
			}
			_ = srv.cl.Respond(id, protocol.ApplyWorkspaceEditResult{Applied: applied}, nil)
		}()
	default:
		_ = srv.cl.Respond(id, nil, nil)
	}
}

func (s *server) key() string { return key(s.lang, s.root) }

// withToolchain merges any toolchain-detected settings (e.g. the resolved Python
// interpreter path) into the spec before initialize. The language's detector runs
// against the workspace root; an explicit user setting in the same key wins over a
// detected one. Languages without a Toolchain pass through unchanged.
func withToolchain(langID, root string, spec lsp.ServerSpec) lsp.ServerSpec {
	l, ok := langreg.ByID(langID)
	if !ok || l.Toolchain == nil {
		return spec
	}
	extra, ok := l.Toolchain.Detect(root)
	if !ok {
		return spec
	}
	spec.Settings = langreg.MergeSettings(extra, spec.Settings)
	return spec
}

// settingsSection resolves a (possibly dotted) configuration section from a
// settings map, e.g. "python" or "python.analysis". It returns the whole map for
// an empty section and nil when a path segment is missing.
func settingsSection(s map[string]any, section string) any {
	if section == "" {
		return s
	}
	var cur any = s
	for _, part := range strings.Split(section, ".") {
		mm, ok := cur.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		cur = mm[part]
	}
	if cur == nil {
		// An absent section answers an empty object, never null (#1061):
		// vscode-css-language-server reads options off the answer and
		// silently stops validating when it gets null — VS Code effectively
		// provides a defaults object, so {} is what servers are built for.
		return map[string]any{}
	}
	return cur
}

// status forwards a status update when a callback is set.
func (m *Manager) status(lang, text string, kind lsp.ServerStatusKind) {
	if m.cb.Status != nil {
		m.cb.Status(lang, text, kind)
	}
}

// docsForServerLocked returns the documents bound to a server key. Caller holds m.mu.
func (m *Manager) docsForServerLocked(srvKey string) []*document {
	var out []*document
	for _, d := range m.docs {
		if d.srvKey == srvKey {
			out = append(out, d)
		}
	}
	return out
}

// docServer returns the server and document for path.
func (m *Manager) docServer(path string) (*server, *document, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, ok := m.docs[path]
	if !ok {
		return nil, nil, false
	}
	srv := m.servers[doc.srvKey]
	if srv == nil {
		return nil, nil, false
	}
	return srv, doc, true
}

// startupError folds the decisive stderr line into a bare transport error
// (#1062): a server binary that dies during the handshake surfaces its actual
// complaint (e.g. taplo's "the LSP is not part of this build") instead of
// "jsonrpc: connection closed". Without a recognizable stderr line the
// original error stands.
func startupError(err error, stderr func() string) error {
	if stderr == nil {
		return err
	}
	if tail := transport.ErrorLine(stderr()); tail != "" {
		return errors.New(tail)
	}
	return err
}

// knownLaunchFailures maps recognizable startup complaints to actionable
// advice (#1065): when the extracted stderr line matches, the notification
// tells the user how to get a working binary instead of only what broke.
var knownLaunchFailures = []struct{ command, needle, advice string }{
	// Homebrew builds taplo without the lsp feature; the npm/cargo builds
	// carry it.
	{"taplo", "not part of this build", "this taplo was built without the LSP — install an LSP-capable build: npm install -g @taplo/cli (or cargo install taplo-cli --features lsp)"},
}

// launchAdvice returns the actionable hint for a recognized launch failure,
// or "" when none matches.
func launchAdvice(command, errText string) string {
	for _, k := range knownLaunchFailures {
		if k.command == command && strings.Contains(errText, k.needle) {
			return k.advice
		}
	}
	return ""
}

// statusForErr renders a launch failure as a user-facing status string plus its
// classification: a missing binary is persistent state (LSP stays off for the
// language), any other launch failure is a transient error event pointing at
// the server log (#1062, matching the repeated-crash disable message #715).
// Recognized failures append concrete install advice (#1065).
func statusForErr(command string, err error) (string, lsp.ServerStatusKind) {
	if isNotFound(err) {
		return command + " not found (LSP disabled for this language)", lsp.ServerState
	}
	text := command + ": " + err.Error()
	if advice := launchAdvice(command, err.Error()); advice != "" {
		text += " — " + advice
	}
	return text + " — details: \"LSP: Show Server Log\"", lsp.ServerEventError
}

func splitLines(text string) []string { return strings.Split(text, "\n") }

// isNotFound reports whether err is a missing-binary failure.
func isNotFound(err error) bool { return errors.Is(err, transport.ErrNotFound) }
