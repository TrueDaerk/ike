// Package host defines the narrow API surface plugins call to affect the
// running editor: open a file, dispatch a message, set the status line, and
// read configuration. The contract is an interface (API) so Roadmap 9900 can
// swap the in-process implementation for a Wasm-bridged one without touching
// plugin code.
package host

import (
	"fmt"
	"sort"
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

// Severity classifies a notification (Roadmap 0130). Info and Warn toasts
// expire on their own; Error toasts persist until dismissed.
type Severity int

const (
	Info Severity = iota
	Warn
	Error
)

// Notification is one Notify payload, drained and rendered by the root model.
type Notification struct {
	Severity Severity
	Text     string
}

// API is everything a plugin may ask of the host. It intentionally stays small
// and message-oriented so it ports to an out-of-process/Wasm bridge later.
type API interface {
	// OpenFile asks the host to open path in the editor. It returns a tea.Cmd
	// the caller hands back to bubbletea; the host routes the resulting request
	// through its registered file handlers. It defaults to replacing the active
	// editor's buffer (today's behaviour).
	OpenFile(path string) tea.Cmd
	// OpenFileIn is OpenFile with an explicit open-target: newPane=true splits off
	// a fresh editor and loads path there instead of replacing the active buffer.
	// OpenFile is exactly OpenFileIn(path, false), kept so existing plugins stay
	// source-compatible.
	OpenFileIn(path string, newPane bool) tea.Cmd
	// Dispatch turns an arbitrary message into a tea.Cmd that re-injects it into
	// the program's Update loop.
	Dispatch(msg tea.Msg) tea.Cmd
	// Send injects a message into the Update loop from any goroutine. Unlike
	// Dispatch (which returns a tea.Cmd for the caller to hand back from Update),
	// Send is for background workers — async LSP results, server notifications —
	// that have no Cmd to return. It is a no-op until the program is running.
	// It never blocks the caller: delivery is queued (#2027), so a seam that
	// answers straight from Update — a command's Run, an EditorEmitter.Emit —
	// may Send without deadlocking the program against itself. Queued messages
	// keep their Send order. The queue is bounded and coalesces Coalescable
	// snapshots (#2169), so a producer outrunning the Update loop cannot grow
	// it without limit.
	Send(msg tea.Msg)
	// SetStatus replaces the persistent status-line segment (e.g. LSP server
	// state). It is rendered until overwritten; event-like messages belong in
	// Notify instead.
	SetStatus(text string)
	// Notify raises a toast notification (Roadmap 0130): a short, event-like
	// message with a severity. Info/Warn toasts expire on their own; Error
	// toasts persist until the user dismisses them. Use Notify for events
	// ("saved 3 files", "server crashed") and SetStatus only for persistent
	// state segments.
	Notify(sev Severity, text string)
	// SetEditorEmitter registers (or replaces) a named sink for editor
	// lifecycle events (the LSP bridge "lsp", the completion engine
	// "complete", #851). The host fans every editor change/cursor/completion
	// signal out to all sinks; a nil emitter removes the name.
	SetEditorEmitter(name string, e EditorEmitter)
	// Config exposes read-only configuration access.
	Config() Config
}

// EditorEvent is a lifecycle signal from the editor the LSP bridge consumes. It
// mirrors editor.Event but lives in host so the host (and the plugin contract)
// carries no internal/editor import. Kind is one of the Editor* constants below;
// Text holds the full buffer content and is populated only on EditorChange so
// cursor moves stay cheap.
type EditorEvent struct {
	Kind int
	Path string
	Line int
	Col  int
	Text string
	// Sel carries an active visual selection (SelNone when there is none):
	// the anchor is one end, the cursor (Line/Col) the other. SelLine means a
	// line-wise selection spanning whole lines. Range-scoped LSP features
	// (range formatting) read it off the latest event.
	Sel        int
	AnchorLine int
	AnchorCol  int
	// Large marks a change on a document in large-file mode (#149): Text is
	// intentionally absent, so the LSP bridge stops syncing the document
	// instead of treating the event as "the file is now empty".
	Large bool
	// Char carries the just-typed character on EditorCompletionTrigger
	// (#527); empty means a manual request the bridge honours unconditionally.
	Char string
	// CompletionID carries the selected item's reply index on
	// EditorCompletionSelect (#847), for completionItem/resolve.
	CompletionID int
	// Key identifies the emitting buffer where Path cannot (#2048): a buffer
	// with no file has an empty Path, so per-buffer state keyed by it — the
	// word index's text store, the completion route back to the view — would
	// collapse every file-less buffer into one entry. It mirrors
	// editor.ParseKey: the file path when there is one, else the view's own
	// tag. Empty means "same as Path"; read it through BufKey.
	Key string
	// LangPath is the path language resolution keys on (#2048). It differs
	// from Path only for a file-less buffer given a language through "Treat
	// Buffer as …" (#2033), where it is that language's synthetic name
	// ("buffer.go") — never a path to read or write. Empty means "same as
	// Path"; read it through LangName.
	LangPath string
}

// BufKey is the event's buffer identity: Key when the emitter set one, else
// Path. Sources keying per-buffer state use it instead of Path so a buffer
// with no file gets its own entry rather than sharing the empty one (#2048).
func (e EditorEvent) BufKey() string {
	if e.Key != "" {
		return e.Key
	}
	return e.Path
}

// LangName is the name language lookups resolve for this event: LangPath when
// the emitter set one, else Path. It is a name for classification only, never
// a path to open (#2048).
func (e EditorEvent) LangName() string {
	if e.LangPath != "" {
		return e.LangPath
	}
	return e.Path
}

// EditorEvent selection kinds (mirrors editor.SelKind).
const (
	SelNone = iota
	SelChar
	SelLine
)

// EditorEvent kinds. The order mirrors editor.EventKind — Kind is cast
// straight across in the app's emitter adapter.
const (
	EditorChange = iota
	EditorCursorMove
	EditorCompletionTrigger
	EditorSave
	EditorJump
	EditorCompletionSelect
	// EditorHoverRequest is app-originated (mouse-idle hover, #1129), not a
	// cast editor.EventKind: it asks the LSP bridge for hover content at the
	// event's Line/Col (the hovered cell, not the cursor). Keep it after every
	// mirrored kind so the straight cast in the emitter adapter stays valid.
	EditorHoverRequest
)

// EditorEmitter receives editor lifecycle events. Implementations must not block.
type EditorEmitter interface{ Emit(EditorEvent) }

// Coalescable marks a Send message as an idempotent snapshot (#2169, the
// jsonrpc NotifyCoalesced pattern from #1542): while an earlier message with
// the same non-empty key still waits in the Send outbox, a newer one replaces
// it in place — same queue position, latest payload — instead of growing the
// queue. Implement it only where the newest payload fully subsumes the older
// one (a whole diagnostics set, a terminal output snapshot), and key by the
// bounded resource the snapshot describes (a document URI, a session id) so
// distinct keys stay few. An empty key never coalesces.
type Coalescable interface{ CoalesceKey() string }

// maxOutbox bounds the Send outbox (#2169). A firehose producer that outruns
// the Update loop (a busy terminal session, an LSP diagnostics storm,
// per-keystroke editor events) otherwise grows the queue monotonically: the
// pump delivers back-to-back forever while RSS climbs with the retained
// payloads, defeating every subsystem's own backpressure at the last hop. At
// the cap the incoming message is dropped — newest loses, since keeping the
// already-queued messages preserves Send order — counted, and reported
// through the diag logger. Snapshot classes should implement Coalescable
// instead: a keyed replace never grows the queue, so it never drops.
const maxOutbox = 1024

// sendEntry is one queued Send message with its coalescing key ("" for
// none), captured at enqueue so the outbox scan never re-asserts the
// interface.
type sendEntry struct {
	key string
	msg tea.Msg
}

// Config is read-only key/value configuration access for plugins.
type Config interface {
	Get(key string) (value string, ok bool)
	// Keys lists every configuration key, so a consumer can enumerate a dynamic
	// section (e.g. all "explorer.colors.*" entries) it cannot name in advance.
	Keys() []string
}

// OpenFileRequest is emitted by API.OpenFile / OpenFileIn. The root model handles
// it by resolving a file handler and opening the file, keeping plugins decoupled
// from the concrete explorer/editor message types. NewPane carries the additive
// open-target intent: false (the zero value) replaces the active editor, true
// splits off a fresh editor. It is a primitive flag rather than a pane.OpenTarget
// so host stays free of an import cycle with internal/pane.
type OpenFileRequest struct {
	Path    string
	NewPane bool
}

// OpenModalRequest asks the root model to present arbitrary content in the
// floating shell (Roadmap 0035). A plugin dispatches it (h.Dispatch) to show its
// pane as a modal popup: View renders the body, Title is the heading. It is an
// additive in-process seam — it adds no plugin contract field and needs no new
// API method; the host hosts the existing tea.Model/Pane shape via ui.Floating.
type OpenModalRequest struct {
	Title string
	View  func() string
}

// MapConfig is a trivial in-memory Config, handy for tests and for plugins that
// need a few literal keys without the full typed schema.
type MapConfig map[string]string

// Get implements Config.
func (c MapConfig) Get(key string) (string, bool) {
	v, ok := c[key]
	return v, ok
}

// Keys implements Config.
func (c MapConfig) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// FromConfig adapts a typed *config.Config (Roadmap 0040) to the read-only
// key/value Config plugins see. It flattens the schema once via Config.Flat, so
// the typed structs stay the single source of truth and host never re-derives
// key names. A nil c yields an empty configuration.
func FromConfig(c *config.Config) Config {
	if c == nil {
		return MapConfig{}
	}
	return MapConfig(c.Flat())
}

// Host is the in-process implementation of API.
type Host struct {
	cfg        Config
	status     string
	send       func(tea.Msg)
	edEmitters map[string]EditorEmitter

	// Queued notifications awaiting the root model's drain. Guarded by mu:
	// background workers (LSP goroutines) may Notify while Update drains.
	mu            sync.Mutex
	notifications []Notification

	// Outbox for Send (#2027), guarded by sendMu: messages awaiting the
	// dispatcher goroutine that hands them to the program in Send order.
	// pumping says that dispatcher is alive, so at most one runs at a time.
	// The outbox is bounded at maxOutbox and coalesces keyed snapshots
	// (#2169); dropped counts messages the bound rejected, diagLog is the
	// best-effort drop reporter (the app's debug.log).
	sendMu  sync.Mutex
	outbox  []sendEntry
	pumping bool
	dropped uint64
	diagLog func(string)
}

// New returns a Host backed by cfg. A nil cfg yields an empty configuration.
func New(cfg Config) *Host {
	if cfg == nil {
		cfg = MapConfig{}
	}
	return &Host{cfg: cfg}
}

// SetSender wires the program's Send so background workers can inject messages.
// main.go calls this once after tea.NewProgram, before Run.
func (h *Host) SetSender(send func(tea.Msg)) { h.send = send }

// SetConfig replaces the configuration the host exposes; the root model calls
// it on a live config reload so plugins (and the host's own consumers) read
// fresh values. A nil cfg is ignored.
func (h *Host) SetConfig(cfg Config) {
	if cfg != nil {
		h.cfg = cfg
	}
}

// Send implements API. The message is queued for a dispatcher goroutine
// instead of being handed to the program on the caller's own goroutine
// (#2027): bubbletea's Send blocks until the event loop receives the message,
// so a Send from inside Update — a command's Run answering without a server, a
// local hover/definition provider claiming an EditorEmitter.Emit — froze the
// whole IDE against itself. Queueing keeps Send order (one dispatcher, FIFO
// outbox) while making it non-blocking for every caller.
//
// The outbox is bounded and coalescing (#2169): a Coalescable message with a
// non-empty key replaces a queued message with the same key in place, and any
// other message arriving while the outbox holds maxOutbox entries is dropped,
// counted (SendDrops) and reported through the diag logger.
func (h *Host) Send(msg tea.Msg) {
	if h.send == nil {
		return // no program yet: nothing to deliver to
	}
	var key string
	if c, ok := msg.(Coalescable); ok {
		key = c.CoalesceKey()
	}
	h.sendMu.Lock()
	if key != "" {
		for i := range h.outbox {
			if h.outbox[i].key == key {
				// Superseded snapshot: latest payload, same queue slot.
				h.outbox[i].msg = msg
				h.sendMu.Unlock()
				return
			}
		}
	}
	if len(h.outbox) >= maxOutbox {
		h.dropped++
		dropped, logf := h.dropped, h.diagLog
		h.sendMu.Unlock()
		// Report the first drop of a flood and every 500th after, so the
		// bound tripping is visible without the log becoming the flood.
		if logf != nil && (dropped == 1 || dropped%500 == 0) {
			logf(fmt.Sprintf("host: send outbox full (%d), dropping %T (%d dropped so far)", maxOutbox, msg, dropped))
		}
		return
	}
	h.outbox = append(h.outbox, sendEntry{key: key, msg: msg})
	if h.pumping {
		h.sendMu.Unlock()
		return // the live dispatcher picks it up
	}
	h.pumping = true
	h.sendMu.Unlock()
	go h.pump()
}

// SetDiagLog wires a best-effort diagnostic logger (the app's debug.log) that
// reports Send outbox drops (#2169). Re-wiring on a project switch is safe
// while background workers Send.
func (h *Host) SetDiagLog(logf func(string)) {
	h.sendMu.Lock()
	h.diagLog = logf
	h.sendMu.Unlock()
}

// SendDrops reports how many messages the bounded Send outbox has rejected
// since start (#2169) — the counter behind the diag log line, exposed for
// tests and diagnostics. Coalesced replacements are not drops.
func (h *Host) SendDrops() uint64 {
	h.sendMu.Lock()
	defer h.sendMu.Unlock()
	return h.dropped
}

// pump drains the outbox into the program, in Send order, until it runs dry.
// It is the only goroutine allowed to block on the program's Send.
func (h *Host) pump() {
	for {
		h.sendMu.Lock()
		if len(h.outbox) == 0 {
			h.pumping = false
			h.sendMu.Unlock()
			return
		}
		msg := h.outbox[0].msg
		// Drop the slot's reference before advancing: a delivered message
		// (a whole document's spans, a diagnostics batch) must not stay
		// reachable through the backing array.
		h.outbox[0] = sendEntry{}
		h.outbox = h.outbox[1:]
		h.sendMu.Unlock()
		h.send(msg)
	}
}

// SetEditorEmitter implements API: it registers (or replaces) the named sink.
// Re-registering under the same name is idempotent, so a project switch that
// re-wires seams never duplicates a sink.
func (h *Host) SetEditorEmitter(name string, e EditorEmitter) {
	if h.edEmitters == nil {
		h.edEmitters = map[string]EditorEmitter{}
	}
	if e == nil {
		delete(h.edEmitters, name)
		return
	}
	h.edEmitters[name] = e
}

// EmitEditor fans an editor event out to every registered sink (the LSP
// bridge, the local completion engine, …) in deterministic name order (#851).
func (h *Host) EmitEditor(ev EditorEvent) {
	names := make([]string, 0, len(h.edEmitters))
	for n := range h.edEmitters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h.edEmitters[n].Emit(ev)
	}
}

// OpenFile implements API.
func (h *Host) OpenFile(path string) tea.Cmd {
	return h.OpenFileIn(path, false)
}

// OpenFileIn implements API.
func (h *Host) OpenFileIn(path string, newPane bool) tea.Cmd {
	return func() tea.Msg { return OpenFileRequest{Path: path, NewPane: newPane} }
}

// Dispatch implements API.
func (h *Host) Dispatch(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// SetStatus implements API.
func (h *Host) SetStatus(text string) { h.status = text }

// Notify implements API: it queues the notification for the root model, which
// drains the queue after every Update pass (rendering and expiry live there).
func (h *Host) Notify(sev Severity, text string) {
	h.mu.Lock()
	h.notifications = append(h.notifications, Notification{Severity: sev, Text: text})
	h.mu.Unlock()
}

// DrainNotifications returns and clears the queued notifications, for the root
// model. Safe to call from the Update loop while background workers Notify.
func (h *Host) DrainNotifications() []Notification {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.notifications) == 0 {
		return nil
	}
	out := h.notifications
	h.notifications = nil
	return out
}

// Config implements API.
func (h *Host) Config() Config { return h.cfg }

// Status returns the last text set via SetStatus, for the root model to render.
func (h *Host) Status() string { return h.status }
