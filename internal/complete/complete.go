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
	// Key identifies the requesting buffer where Path cannot (#2048): a
	// buffer with no file has no path, so sources keying per-buffer text by
	// Path alone would fold every file-less buffer into one entry. Empty
	// means "same as Path"; read it through BufKey.
	Key string
	// LangPath is the name language lookups resolve for this buffer (#2048):
	// the file path, or the synthetic name of a language chosen with "Treat
	// Buffer as …" (#2033), so a file-less Go buffer gets the sources a .go
	// file gets. Empty means "same as Path"; read it through LangName.
	LangPath string
}

// BufKey is the request's buffer identity: Key when set, else Path. Sources
// keying observed buffer text use it, so a file-less buffer's own text is
// found instead of whatever else answered to the empty path (#2048).
func (r Request) BufKey() string {
	if r.Key != "" {
		return r.Key
	}
	return r.Path
}

// LangName is the name a source resolves the buffer's language by: LangPath
// when set, else Path. It classifies only — never open it (#2048).
func (r Request) LangName() string {
	if r.LangPath != "" {
		return r.LangPath
	}
	return r.Path
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

// ExclusiveSource is an optional Source extension (#1302): a language source
// that fully owns completion for its own files declares it here. The engine
// hands it the request's LangName, so a file-less buffer switched to the
// source's language is claimed like a real file of it (#2048). While a source
// claims a path, the engine dispatches only claiming sources for it — a `.http`
// header line offers header names, never the buffer's random identifiers, and a
// request body offers nothing rather than every word in the file. The LSP bridge
// is not a Source and is unaffected; no source claims anything by default, so
// every other language keeps the full merged popup.
type ExclusiveSource interface {
	Exclusive(path string) bool
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

// TriggerSource is an optional Source extension (#1913): a source that has
// something position-specific to say after a punctuation character the engine
// otherwise reserves for the LSP bridge declares it here. Postfix completion is
// the case — `err.` must offer its `if`/`nil` transformations right after the
// dot — and only the declaring sources are dispatched for such a character, so
// a "." keeps not producing a word-index echo.
type TriggerSource interface {
	TriggerChar(ch string) bool
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
// sources, completion triggers additionally dispatch the sources. Identifier-ish
// characters and manual requests dispatch everything — server trigger characters
// ("." "->" "$") are the LSP bridge's business; a local index has nothing
// position-specific to say after a "." — and a punctuation character reaches
// only the sources claiming it through TriggerSource (#1913). Emit must not
// block (dispatch spawns goroutines).
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
		// A punctuation trigger reaches only the sources claiming it (#1913),
		// and never past an exclusive claim on the path (#1302) — a source
		// owning the buffer keeps owning it after a ".".
		sources = charTriggered(ev.Char, exclusiveFor(ev.LangName(), sources))
		if len(sources) == 0 {
			return
		}
	}
	e.dispatch(Request{
		Path:     ev.Path,
		Key:      ev.BufKey(),
		LangPath: ev.LangName(),
		Line:     ev.Line,
		Col:      ev.Col,
		Char:     ev.Char,
	}, sources)
}

// charTriggered narrows sources to the ones claiming ch as their own trigger
// character (#1913).
func charTriggered(ch string, sources []Source) []Source {
	var claimed []Source
	for _, s := range sources {
		if t, ok := s.(TriggerSource); ok && t.TriggerChar(ch) {
			claimed = append(claimed, s)
		}
	}
	return claimed
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

// exclusiveFor narrows sources to the ones claiming name — a request's
// LangName — exclusively (#1302).
// With no claim the full set runs, which is the normal case. Applying it twice
// is a no-op, so Emit may pre-filter before the trigger-character narrowing.
func exclusiveFor(name string, sources []Source) []Source {
	var claimed []Source
	for _, s := range sources {
		if x, ok := s.(ExclusiveSource); ok && x.Exclusive(name) {
			claimed = append(claimed, s)
		}
	}
	if len(claimed) == 0 {
		return sources
	}
	return claimed
}

// dispatch cancels the previous dispatch and runs the given sources
// concurrently, sending each result as a tagged batch (an empty batch clears
// the source's contribution from a merged popup). Results landing after the
// context died (timeout or a newer trigger) are dropped.
func (e *Engine) dispatch(req Request, sources []Source) {
	e.mu.Lock()
	if e.cancel != nil {
		e.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), e.Timeout)
	e.cancel = cancel
	e.mu.Unlock()
	sources = exclusiveFor(req.LangName(), sources)
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
				Key:            req.BufKey(),
				Line:           req.Line,
				Col:            req.Col,
				Items:          items,
				Source:         s.Name(),
				SourcePriority: s.Priority(),
			})
		}(s)
	}
}
