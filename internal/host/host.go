// Package host defines the narrow API surface plugins call to affect the
// running editor: open a file, dispatch a message, set the status line, and
// read configuration. The contract is an interface (API) so Roadmap 9900 can
// swap the in-process implementation for a Wasm-bridged one without touching
// plugin code.
package host

import (
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
	// SetEditorEmitter registers the sink for editor lifecycle events (the LSP
	// bridge, Roadmap 0100). The host forwards every editor change/cursor/
	// completion signal to it; passing nil disables forwarding.
	SetEditorEmitter(e EditorEmitter)
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
}

// EditorEvent selection kinds (mirrors editor.SelKind).
const (
	SelNone = iota
	SelChar
	SelLine
)

// EditorEvent kinds.
const (
	EditorChange = iota
	EditorCursorMove
	EditorCompletionTrigger
	EditorSave
)

// EditorEmitter receives editor lifecycle events. Implementations must not block.
type EditorEmitter interface{ Emit(EditorEvent) }

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
	cfg       Config
	status    string
	send      func(tea.Msg)
	edEmitter EditorEmitter

	// Queued notifications awaiting the root model's drain. Guarded by mu:
	// background workers (LSP goroutines) may Notify while Update drains.
	mu            sync.Mutex
	notifications []Notification
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

// Send implements API.
func (h *Host) Send(msg tea.Msg) {
	if h.send != nil {
		h.send(msg)
	}
}

// SetEditorEmitter implements API.
func (h *Host) SetEditorEmitter(e EditorEmitter) { h.edEmitter = e }

// EmitEditor forwards an editor event to the registered emitter (the app's
// editor-emitter adapter calls this; the host fans it out to the LSP bridge).
func (h *Host) EmitEditor(ev EditorEvent) {
	if h.edEmitter != nil {
		h.edEmitter.Emit(ev)
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
