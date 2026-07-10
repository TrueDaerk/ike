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
	"sort"
	"strings"
	"sync"
	"time"

	"ike/internal/editor/buffer"
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
// function. The default production connector spawns a process; tests inject an
// in-memory pipe. handler must be wired into the connection at creation so
// notifications are not missed during initialize.
type Connector func(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (cl *client.Client, stop func(), err error)

// Callbacks deliver async server output and status to the host (the bridge wires
// these to host.Send). All run on manager goroutines and must not block.
type Callbacks struct {
	// Diagnostics delivers a publishDiagnostics for path, with the document's
	// current lines and the server's position encoding so the receiver can map to
	// editor coordinates.
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
}

// server is one running language server instance.
type server struct {
	lang    string
	root    string
	cl      *client.Client
	stop    func()
	spec    lsp.ServerSpec
	closing bool // set before a deliberate stop so watchExit does not restart
}

// document tracks an open buffer's identity and latest text, so the manager can
// convert positions and resend full text on change (full-sync MVP).
type document struct {
	path    string
	lang    string
	root    string
	version int
	lines   []string
	srvKey  string
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
	}
}

// processConnector is the default connector: spawn the server binary over stdio.
func processConnector(spec lsp.ServerSpec, root string, handler jsonrpc.Handler) (*client.Client, func(), error) {
	proc, err := transport.Start(transport.Spec{
		Command: spec.Command,
		Args:    spec.Args,
		Env:     spec.Env,
		Dir:     root,
	})
	if err != nil {
		return nil, nil, err
	}
	conn := jsonrpc.NewConn(proc.Conn(), handler)
	cl := client.New(conn)
	stop := func() {
		_ = conn.Close()
		_ = proc.Stop()
	}
	return cl, stop, nil
}

func key(lang, root string) string { return lang + "\x00" + root }

// Open registers a document and ensures its server is running, sending didOpen.
// It blocks on the initialize handshake the first time a (lang, root) server is
// needed — safe because the bridge calls this from a tea.Cmd goroutine.
func (m *Manager) Open(path, lang, text string) error {
	spec, ok := m.resolve(lang)
	if !ok {
		return nil // no server configured for this language: silent no-op
	}
	root := detectRoot(path, spec.RootMarkers)
	srv, err := m.ensureServer(lang, root, spec)
	if err != nil {
		text, kind := statusForErr(spec.Command, err)
		m.status(lang, text, kind)
		return err
	}

	lines := splitLines(text)
	m.mu.Lock()
	doc := &document{path: path, lang: lang, root: root, version: 1, lines: lines, srvKey: srv.key()}
	m.docs[path] = doc
	m.mu.Unlock()

	return srv.cl.DidOpen(protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        protocol.PathToURI(path),
			LanguageID: lang,
			Version:    doc.version,
			Text:       text,
		},
	})
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
	doc.version++
	version := doc.version
	doc.lines = splitLines(text)
	srv := m.servers[doc.srvKey]
	m.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.cl.DidChange(protocol.DidChangeTextDocumentParams{
		TextDocument:   protocol.VersionedTextDocumentIdentifier{URI: protocol.PathToURI(path), Version: version},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: text}},
	})
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

// Close sends didClose and forgets the document.
func (m *Manager) Close(path string) error {
	srv, _, ok := m.docServer(path)
	m.mu.Lock()
	delete(m.docs, path)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return srv.cl.DidClose(protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
	})
}

// Completion requests completion at an editor position, gated on capability.
func (m *Manager) Completion(ctx context.Context, path string, pos buffer.Position) ([]protocol.CompletionItem, error) {
	srv, doc, ok := m.docServer(path)
	if !ok || !srv.cl.Caps().Completion {
		return nil, nil
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	return srv.cl.Completion(cctx, protocol.CompletionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.PathToURI(path)},
		Position:     protocol.ToLSPPosition(doc.lines, pos, srv.cl.Encoding()),
		Context:      &protocol.CompletionContext{TriggerKind: protocol.CompletionTriggerInvoked},
	})
}

// Hover requests hover at an editor position, gated on capability.
func (m *Manager) Hover(ctx context.Context, path string, pos buffer.Position) (*protocol.Hover, error) {
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

// Definition requests definition locations at an editor position.
func (m *Manager) Definition(ctx context.Context, path string, pos buffer.Position) ([]protocol.Location, error) {
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

// References requests every reference to the symbol at an editor position.
// IncludeDeclaration mirrors the LSP request option (JetBrains' find-usages
// includes the declaration, so callers default to true).
func (m *Manager) References(ctx context.Context, path string, pos buffer.Position, includeDecl bool) ([]protocol.Location, error) {
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

// PrepareRename validates a rename at an editor position. ok reports whether
// the position is renameable; placeholder is the symbol text the prompt should
// prefill (empty when the server offers no range — defaultBehavior — or no
// prepareRename support at all, which skips validation entirely).
func (m *Manager) PrepareRename(ctx context.Context, path string, pos buffer.Position) (placeholder string, ok bool, err error) {
	srv, doc, found := m.docServer(path)
	if !found || !srv.cl.Caps().Rename {
		return "", false, nil
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
	for path, doc := range m.docs {
		if doc.lang == lang {
			delete(m.docs, path)
		}
	}
	m.mu.Unlock()
	for _, srv := range stopped {
		if srv.stop != nil {
			srv.stop()
		}
	}
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

// Shutdown stops every server. Best-effort; used on app exit.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	servers := m.servers
	m.servers = make(map[string]*server)
	m.docs = make(map[string]*document)
	for _, srv := range servers {
		srv.closing = true // suppress restart on the resulting Done
	}
	m.mu.Unlock()
	for _, srv := range servers {
		if srv.stop != nil {
			srv.stop()
		}
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
	cl, stop, err := m.connect(spec, root, handler)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout*2)
	defer cancel()
	if _, err := cl.Initialize(ctx, client.InitParams{RootURI: protocol.PathToURI(root), ProcessID: os.Getpid(), InitializationOptions: spec.SettingsJSON()}); err != nil {
		if stop != nil {
			stop()
		}
		return nil, err
	}

	srv := &server{lang: lang, root: root, cl: cl, stop: stop, spec: spec}
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

	m.status(lang, lang+" language server ready", lsp.ServerState)
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
	m.status(srv.lang, srv.lang+" language server crashed", lsp.ServerEventWarn)
	go m.restart(srv, docs)
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
	path := protocol.URIToPath(p.URI)
	m.mu.Lock()
	doc := m.docs[path]
	var lines []string
	var enc string
	if doc != nil {
		lines = doc.lines
		if srv := m.servers[doc.srvKey]; srv != nil {
			enc = srv.cl.Encoding()
		}
	}
	m.mu.Unlock()
	if m.cb.Diagnostics != nil {
		m.cb.Diagnostics(path, p, lines, enc)
	}
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
			return nil
		}
		cur = mm[part]
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

// statusForErr renders a launch failure as a user-facing status string plus its
// classification: a missing binary is persistent state (LSP stays off for the
// language), any other launch failure is a transient error event.
func statusForErr(command string, err error) (string, lsp.ServerStatusKind) {
	if isNotFound(err) {
		return command + " not found (LSP disabled for this language)", lsp.ServerState
	}
	return command + ": " + err.Error(), lsp.ServerEventError
}

func splitLines(text string) []string { return strings.Split(text, "\n") }

// isNotFound reports whether err is a missing-binary failure.
func isNotFound(err error) bool { return errors.Is(err, transport.ErrNotFound) }
