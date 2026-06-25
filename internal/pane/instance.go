package pane

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/editor"
	"ike/internal/explorer"
	"ike/internal/host"
)

// Kind is the type of component an Instance wraps. The explorer is a singleton;
// editors are many.
type Kind int

const (
	// KindExplorer is the file-tree pane. Exactly one exists, under key "explorer".
	KindExplorer Kind = iota
	// KindEditor is a text editor pane. Any number may exist, tiled side by side.
	KindEditor
)

// Context ids an Instance advertises for context-scoped command/keymap
// resolution, matching the constants internal/app historically owned.
const (
	ctxExplorer = "explorer"
	ctxEditor   = "editor"
)

// Instance is one live pane: a stable key plus the component it drives. Only one
// of exp/ed is meaningful, selected by kind. Component models are value types
// with pointer-receiver methods, so the Instance is held behind a pointer in the
// registry and its accessors hand out pointers into the embedded value.
type Instance struct {
	key  string
	kind Kind
	exp  explorer.Model
	ed   editor.Model
}

// Key returns the instance's stable identity, the same string used as the
// layout leaf id and the persistence key.
func (i *Instance) Key() string { return i.key }

// Kind reports whether the instance is an explorer or an editor.
func (i *Instance) Kind() Kind { return i.kind }

// ContextID is the context id the instance advertises for command/keymap
// resolution: explorer panes resolve under "explorer", editors under "editor".
func (i *Instance) ContextID() string {
	if i.kind == KindExplorer {
		return ctxExplorer
	}
	return ctxEditor
}

// Explorer returns the underlying explorer model. It is only valid for an
// explorer instance; callers gate on Kind first.
func (i *Instance) Explorer() *explorer.Model { return &i.exp }

// Editor returns the underlying editor model. It is only valid for an editor
// instance; callers gate on Kind first.
func (i *Instance) Editor() *editor.Model { return &i.ed }

// SetSize pushes an interior content size into the wrapped component.
func (i *Instance) SetSize(w, h int) {
	switch i.kind {
	case KindExplorer:
		i.exp.SetSize(w, h)
	case KindEditor:
		i.ed.SetSize(w, h)
	}
}

// SetFocused marks the wrapped component focused or blurred.
func (i *Instance) SetFocused(f bool) {
	switch i.kind {
	case KindExplorer:
		i.exp.SetFocused(f)
	case KindEditor:
		i.ed.SetFocused(f)
	}
}

// View renders the wrapped component's content (without pane chrome).
func (i *Instance) View() string {
	switch i.kind {
	case KindExplorer:
		return i.exp.View()
	case KindEditor:
		return i.ed.View()
	}
	return ""
}

// Update dispatches a message to the wrapped component, mutating it in place and
// returning any resulting command.
func (i *Instance) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch i.kind {
	case KindExplorer:
		i.exp, cmd = i.exp.Update(msg)
	case KindEditor:
		i.ed, cmd = i.ed.Update(msg)
	}
	return cmd
}

// Init returns the wrapped component's initialisation command.
func (i *Instance) Init() tea.Cmd {
	switch i.kind {
	case KindExplorer:
		return i.exp.Init()
	case KindEditor:
		return i.ed.Init()
	}
	return nil
}

// newInstance builds an instance of the given kind, configuring it against cfg.
// The explorer is rooted at the working directory; editors start with an empty
// scratch buffer.
func newInstance(key string, kind Kind, cfg host.Config) *Instance {
	i := &Instance{key: key, kind: kind}
	switch kind {
	case KindExplorer:
		i.exp = explorer.New(".")
		i.exp.Configure(cfg)
	case KindEditor:
		i.ed = editor.New()
		i.ed.Configure(cfg)
	}
	return i
}
