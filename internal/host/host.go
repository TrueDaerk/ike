// Package host defines the narrow API surface plugins call to affect the
// running editor: open a file, dispatch a message, set the status line, and
// read configuration. The contract is an interface (API) so Roadmap 9900 can
// swap the in-process implementation for a Wasm-bridged one without touching
// plugin code.
package host

import tea "github.com/charmbracelet/bubbletea"

// API is everything a plugin may ask of the host. It intentionally stays small
// and message-oriented so it ports to an out-of-process/Wasm bridge later.
type API interface {
	// OpenFile asks the host to open path in the editor. It returns a tea.Cmd
	// the caller hands back to bubbletea; the host routes the resulting request
	// through its registered file handlers.
	OpenFile(path string) tea.Cmd
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
}

// OpenFileRequest is emitted by API.OpenFile. The root model handles it by
// resolving a file handler and opening the file, keeping plugins decoupled from
// the concrete explorer/editor message types.
type OpenFileRequest struct{ Path string }

// MapConfig is a trivial in-memory Config used until Roadmap 0040 lands real
// configuration loading.
type MapConfig map[string]string

// Get implements Config.
func (c MapConfig) Get(key string) (string, bool) {
	v, ok := c[key]
	return v, ok
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
	return func() tea.Msg { return OpenFileRequest{Path: path} }
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
