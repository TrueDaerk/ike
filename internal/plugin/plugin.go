// Package plugin defines the extension contract for IKE: the Plugin interface
// and the capability types a plugin may contribute (Command, Keymap, Pane,
// FileHandler, Hook). Capabilities are data plus a callback — never inheritance
// — and each carries an owner id for diagnostics and ordering. Plugins depend
// only on this package and internal/host, which keeps the contract narrow and
// portable to the later Wasm runtime (Roadmap 9900).
package plugin

import (
	tea "github.com/charmbracelet/bubbletea"

	"ike/internal/host"
)

// Plugin is the unit of extension. Implementations are typically compiled in
// and self-register from init(); a later Wasm layer adds new producers of the
// same Capabilities without changing this interface.
type Plugin interface {
	// ID is the stable, unique plugin identifier (e.g. "example"). It namespaces
	// the plugin's capability ids and labels conflict diagnostics.
	ID() string
	// Capabilities reports everything the plugin contributes.
	Capabilities() Capabilities
}

// Capabilities groups the extension points a plugin contributes.
type Capabilities struct {
	Commands     []Command
	Keymaps      []Keymap
	Panes        []Pane
	FileHandlers []FileHandler
	Hooks        []Hook
}

// Scope constrains where a Command or Keymap applies. A global capability is
// always available; a context-scoped one is offered only when the focused pane
// advertises a matching ContextID (see ContextProvider).
type Scope struct {
	Global    bool
	ContextID string
}

// GlobalScope returns a Scope that always applies.
func GlobalScope() Scope { return Scope{Global: true} }

// PaneScope returns a Scope active only when a pane advertises ctxID.
func PaneScope(ctxID string) Scope { return Scope{ContextID: ctxID} }

// Matches reports whether the scope applies for the given focused pane context.
// Global scopes always match; context scopes match when ctxID is equal.
func (s Scope) Matches(ctxID string) bool {
	if s.Global {
		return true
	}
	return s.ContextID != "" && s.ContextID == ctxID
}

// Command is a named action invokable from the command palette or ":" line.
type Command struct {
	ID    string // unique across plugins, e.g. "example.hello"
	Title string // human-facing label
	Scope Scope
	// Shortcut is an optional documentation-only hint shown in the help sheet
	// when the command has no registry Keymap to resolve (e.g. vim ex-commands
	// like ":w" or modal keys handled outside the keymap layer). A real Keymap
	// binding, when present, takes precedence over this hint.
	Shortcut string
	// Run produces the tea.Cmd to execute when the command is invoked.
	Run func(h host.API) tea.Cmd
}

// Keymap binds a key sequence to an action. Bindings are layered: a plugin
// binding never shadows a core binding unless its Priority is strictly higher.
type Keymap struct {
	Keys  string // bubbletea key string, e.g. "ctrl+p"
	Scope Scope
	// CommandID optionally links this binding to the Command it triggers, so the
	// help sheet can show a command's shortcut. Empty when the binding is not a
	// command alias.
	CommandID string
	Priority  int // higher wins; core bindings sit at CorePriority
	Action    func(h host.API) tea.Cmd
}

// CorePriority is the priority assigned to core (non-plugin) key bindings.
// Plugin Keymaps must exceed it to override a core binding.
const CorePriority = 100

// Pane is a tea.Model-shaped component the window manager can host.
type Pane struct {
	ID        string
	Title     string
	ContextID string // advertised when focused, for context-scoped resolution
	// New builds a fresh pane model wired to the host.
	New func(h host.API) tea.Model
}

// FileHandler opens files it claims, keyed by extension or a content sniff.
type FileHandler struct {
	ID         string
	Extensions []string // e.g. {".md"}; matched case-insensitively
	// Match optionally claims a file by sniffing its leading bytes. It is only
	// consulted when Extensions do not match. head may be empty.
	Match func(path string, head []byte) bool
	// Open is invoked when the handler claims a file.
	Open func(h host.API, path string) tea.Cmd
}

// Event enumerates the lifecycle messages a Hook can subscribe to.
type Event int

const (
	EventFileOpened Event = iota
	EventBufferSaved
	EventBufferClosed
)

// Hook subscribes to a lifecycle Event.
type Hook struct {
	ID    string
	Event Event
	// Notify runs when Event fires; payload is event-specific (e.g. a file path).
	Notify func(h host.API, payload any) tea.Cmd
}

// ContextProvider is the optional interface a focused pane implements to
// advertise its context id, enabling context-scoped command/keymap resolution.
type ContextProvider interface {
	ContextID() string
}
