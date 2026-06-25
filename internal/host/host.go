// Package host defines the narrow API surface plugins call to affect the
// running editor: open a file, dispatch a message, set the status line, and
// read configuration. The contract is an interface (API) so Roadmap 9900 can
// swap the in-process implementation for a Wasm-bridged one without touching
// plugin code.
package host

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
)

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
	// SetStatus replaces the transient status-line text shown to the user.
	SetStatus(text string)
	// Config exposes read-only configuration access.
	Config() Config
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
	cfg    Config
	status string
}

// New returns a Host backed by cfg. A nil cfg yields an empty configuration.
func New(cfg Config) *Host {
	if cfg == nil {
		cfg = MapConfig{}
	}
	return &Host{cfg: cfg}
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

// Config implements API.
func (h *Host) Config() Config { return h.cfg }

// Status returns the last text set via SetStatus, for the root model to render.
func (h *Host) Status() string { return h.status }
