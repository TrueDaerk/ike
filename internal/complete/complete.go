// Package complete is the local completion engine (Roadmap 0410, #851): a
// registry of CompletionSources — the word index, the symbol index, later
// Emmet — dispatched asynchronously per completion trigger. Each source's
// result is sent as its own tagged lsp.CompletionMsg batch; the editor merges
// batches for the same request position, so instant local answers open the
// popup and slower ones (the LSP server, which is its own event sink, not a
// Source here) merge in on arrival. A slow source is bounded by the engine
// timeout; a new trigger cancels the previous dispatch.
package complete

import (
	"context"
	"sync"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// Request is one completion query: the file, the 0-based editor position, and
// the just-typed character ("" for a manual ctrl+space request).
type Request struct {
	Path string
	Line int
	Col  int
	Char string
}

// Source is one asynchronous completion provider. Complete runs off the UI
// goroutine under the engine's context — it must respect cancellation and
// return editor-ready items (the engine stamps Source on them).
type Source interface {
	// Name tags the source's batches; one popup shows one batch per name.
	Name() string
	// Priority orders sources in the merged popup and decides de-dup winners
	// (higher wins); see the lsp.Priority* constants.
	Priority() int
	Complete(ctx context.Context, req Request) ([]ilsp.CompletionItem, error)
}

// Engine dispatches registered sources per completion trigger. It implements
// host.EditorEmitter and is registered with the host next to the LSP bridge.
type Engine struct {
	mu      sync.Mutex
	sources []Source
	cancel  context.CancelFunc
	send    func(tea.Msg)
	// Timeout bounds one dispatch; a source still running when it expires is
	// cancelled and its result dropped.
	Timeout time.Duration
}

// pluginSources are sources registered from plugin init()s (#922) — before
// any engine exists — picked up by every NewEngine.
var (
	pluginMu      sync.Mutex
	pluginSources []Source
)

// RegisterSource adds a source to every engine created afterwards. The plugin
// seam (#922): internal sources are registered on the engine instance by the
// app; a plugin's init() has no engine yet, so it registers here.
func RegisterSource(s Source) {
	pluginMu.Lock()
	defer pluginMu.Unlock()
	pluginSources = append(pluginSources, s)
}

// NewEngine returns an engine sending result batches through send (host.Send —
// safe to call from goroutines). Plugin-registered sources are included.
func NewEngine(send func(tea.Msg)) *Engine {
	e := &Engine{send: send, Timeout: 2 * time.Second}
	pluginMu.Lock()
	e.sources = append(e.sources, pluginSources...)
	pluginMu.Unlock()
	return e
}

// Register adds a source. Safe to call any time; the next dispatch sees it.
func (e *Engine) Register(s Source) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sources = append(e.sources, s)
}

// EventObserver is an optional Source extension (#852): a source that also
// wants the editor lifecycle events (buffer changes for an index, saves, …)
// implements it and the engine forwards every event. Observe runs on the UI
// goroutine and must not block — stash and mark dirty, extract lazily in
// Complete.
type EventObserver interface {
	Observe(ev host.EditorEvent)
}

// FileObserver is an optional Source extension (#853): a source indexing
// on-disk files implements it and the app forwards watcher file-change
// events through NotifyFileChanged.
type FileObserver interface {
	InvalidateFile(path string)
}

// NotifyFileChanged tells file-observing sources that path changed on disk.
// Must not block; observers do their re-extraction off this goroutine.
func (e *Engine) NotifyFileChanged(path string) {
	e.mu.Lock()
	sources := make([]Source, len(e.sources))
	copy(sources, e.sources)
	e.mu.Unlock()
	for _, s := range sources {
		if o, ok := s.(FileObserver); ok {
			o.InvalidateFile(path)
		}
	}
}

// Emit implements host.EditorEmitter: every event forwards to observing
// sources, completion triggers additionally dispatch the sources. Only
// identifier-ish characters and manual requests fire — server trigger
// characters ("." "->" "$") are the LSP bridge's business; a local index has
// nothing position-specific to say after a "." — and it must not block
// (dispatch spawns goroutines).
func (e *Engine) Emit(ev host.EditorEvent) {
	e.mu.Lock()
	sources := make([]Source, len(e.sources))
	copy(sources, e.sources)
	e.mu.Unlock()
	for _, s := range sources {
		if o, ok := s.(EventObserver); ok {
			o.Observe(ev)
		}
	}
	if ev.Kind != host.EditorCompletionTrigger {
		return
	}
	if !localTrigger(ev.Char) {
		return
	}
	e.dispatch(Request{Path: ev.Path, Line: ev.Line, Col: ev.Col, Char: ev.Char})
}

// localTrigger reports whether a typed character warrants querying the local
// sources: manual requests ("") and identifier runes do; punctuation not.
func localTrigger(ch string) bool {
	if ch == "" {
		return true
	}
	r := []rune(ch)
	return len(r) == 1 && (r[0] == '_' || unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]))
}

// dispatch cancels the previous dispatch and runs every source concurrently,
// sending each result as a tagged batch (an empty batch clears the source's
// contribution from a merged popup). Results landing after the context died
// (timeout or a newer trigger) are dropped.
func (e *Engine) dispatch(req Request) {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	e.cancel = cancel
	sources := make([]Source, len(e.sources))
	copy(sources, e.sources)
	e.mu.Unlock()
	for _, s := range sources {
		go func(s Source) {
			items, err := s.Complete(ctx, req)
			if err != nil || ctx.Err() != nil {
				return
			}
			for i := range items {
				items[i].Source = s.Name()
			}
			e.send(ilsp.CompletionMsg{
				Path:           req.Path,
				Line:           req.Line,
				Col:            req.Col,
				Items:          items,
				Source:         s.Name(),
				SourcePriority: s.Priority(),
			})
		}(s)
	}
}
