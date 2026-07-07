// Package registry is the global home for compiled-in plugins. Plugins call
// Register from init(); the root model queries the resulting Registry at startup
// to build the command palette, merge keymaps, and route file opens through
// handlers.
//
// Registration is deterministic and conflict-aware: duplicate command ids, pane
// ids, file-handler claims, and unprioritised key clashes are detected and
// surfaced via Conflicts rather than silently overwritten. Disabled plugins are
// excluded from every lookup.
package registry

import (
	"path/filepath"
	"sort"
	"strings"

	"ike/internal/plugin"
	"ike/internal/theme"
)

// Registry holds registered plugins and resolves their capabilities.
type Registry struct {
	plugins []plugin.Plugin
	enabled map[string]bool // plugin id -> enabled; absent means enabled
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{enabled: map[string]bool{}}
}

// global is the process-wide registry populated by init() side effects.
var global = New()

// Global returns the process-wide registry.
func Global() *Registry { return global }

// Register adds p to the global registry. Intended for plugin init() functions.
func Register(p plugin.Plugin) { global.Add(p) }

// Add registers p with this registry. Registration order is preserved for
// stability; lookups impose their own deterministic ordering on top.
func (r *Registry) Add(p plugin.Plugin) {
	r.plugins = append(r.plugins, p)
}

// PluginIDs returns every registered plugin id (enabled or not), in
// registration order.
func (r *Registry) PluginIDs() []string {
	out := make([]string, len(r.plugins))
	for i, p := range r.plugins {
		out[i] = p.ID()
	}
	return out
}

// SetEnabled toggles a plugin by id. Unknown ids are remembered so a plugin
// registered later still honours the flag.
func (r *Registry) SetEnabled(id string, on bool) { r.enabled[id] = on }

// IsEnabled reports whether the plugin id is enabled (default true).
func (r *Registry) IsEnabled(id string) bool {
	on, ok := r.enabled[id]
	return !ok || on
}

// activePlugins returns enabled plugins sorted by id for deterministic output.
func (r *Registry) activePlugins() []plugin.Plugin {
	out := make([]plugin.Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		if r.IsEnabled(p.ID()) {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// OwnedCommand pairs a Command with its owning plugin id.
type OwnedCommand struct {
	Owner string
	plugin.Command
}

// OwnedKeymap pairs a Keymap with its owning plugin id.
type OwnedKeymap struct {
	Owner string
	plugin.Keymap
}

// OwnedPane pairs a Pane with its owning plugin id.
type OwnedPane struct {
	Owner string
	plugin.Pane
}

// OwnedHandler pairs a FileHandler with its owning plugin id.
type OwnedHandler struct {
	Owner string
	plugin.FileHandler
}

// OwnedHook pairs a Hook with its owning plugin id.
type OwnedHook struct {
	Owner string
	plugin.Hook
}

// Commands returns enabled commands, ordered by id. Conflicting duplicates are
// dropped (first owner by sorted plugin order wins); see Conflicts.
func (r *Registry) Commands() []OwnedCommand {
	seen := map[string]bool{}
	var out []OwnedCommand
	for _, p := range r.activePlugins() {
		for _, c := range p.Capabilities().Commands {
			if seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			out = append(out, OwnedCommand{Owner: p.ID(), Command: c})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// CommandsForContext returns commands whose scope applies for the focused pane
// context ctxID (empty ctxID yields only global commands).
func (r *Registry) CommandsForContext(ctxID string) []OwnedCommand {
	var out []OwnedCommand
	for _, c := range r.Commands() {
		if c.Scope.Matches(ctxID) {
			out = append(out, c)
		}
	}
	return out
}

// Command looks up a single enabled command by id.
func (r *Registry) Command(id string) (OwnedCommand, bool) {
	for _, c := range r.Commands() {
		if c.ID == id {
			return c, true
		}
	}
	return OwnedCommand{}, false
}

// Keymaps returns enabled key bindings, layered by Priority (highest first) and
// then by owner id for determinism.
func (r *Registry) Keymaps() []OwnedKeymap {
	var out []OwnedKeymap
	for _, p := range r.activePlugins() {
		for _, k := range p.Capabilities().Keymaps {
			out = append(out, OwnedKeymap{Owner: p.ID(), Keymap: k})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority > out[j].Priority
		}
		return out[i].Owner < out[j].Owner
	})
	return out
}

// Binding returns the shortcut bound to command cmdID, if any. Keymaps are
// priority-sorted, so the first match is the winning binding. It satisfies the
// help package's BindingResolver, letting the cheat sheet show command keys.
func (r *Registry) Binding(cmdID string) (string, bool) {
	if cmdID == "" {
		return "", false
	}
	for _, k := range r.Keymaps() {
		if k.CommandID == cmdID {
			return k.Keys, true
		}
	}
	return "", false
}

// ResolveKey returns the winning binding for keys in the given pane context, if
// any. The highest-priority scope-matching binding wins.
func (r *Registry) ResolveKey(keys, ctxID string) (OwnedKeymap, bool) {
	for _, k := range r.Keymaps() {
		if k.Keys == keys && k.Scope.Matches(ctxID) {
			return k, true
		}
	}
	return OwnedKeymap{}, false
}

// Panes returns enabled panes, ordered by id. Duplicate ids are dropped.
func (r *Registry) Panes() []OwnedPane {
	seen := map[string]bool{}
	var out []OwnedPane
	for _, p := range r.activePlugins() {
		for _, pane := range p.Capabilities().Panes {
			if seen[pane.ID] {
				continue
			}
			seen[pane.ID] = true
			out = append(out, OwnedPane{Owner: p.ID(), Pane: pane})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Themes returns the themes contributed by enabled plugins, in plugin order
// (Roadmap 0110). Duplicate names are dropped, first owner by sorted plugin
// order wins — mirroring every other capability. Built-ins are not included
// here; theme.Select layers these over theme.Builtins.
func (r *Registry) Themes() []theme.Theme {
	seen := map[string]bool{}
	var out []theme.Theme
	for _, p := range r.activePlugins() {
		for _, t := range p.Capabilities().Themes {
			if t.Name == "" || seen[t.Name] {
				continue
			}
			seen[t.Name] = true
			out = append(out, t)
		}
	}
	return out
}

// Pane looks up a single enabled pane by id.
func (r *Registry) Pane(id string) (OwnedPane, bool) {
	for _, p := range r.Panes() {
		if p.ID == id {
			return p, true
		}
	}
	return OwnedPane{}, false
}

// ResolveHandler returns the file handler that claims path. Extension matches
// are tried first (in deterministic owner/id order); if none match, content
// Match sniffs are consulted with head.
func (r *Registry) ResolveHandler(path string, head []byte) (OwnedHandler, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	handlers := r.handlers()
	for _, h := range handlers {
		for _, e := range h.Extensions {
			if strings.ToLower(e) == ext && ext != "" {
				return h, true
			}
		}
	}
	for _, h := range handlers {
		if h.Match != nil && h.Match(path, head) {
			return h, true
		}
	}
	return OwnedHandler{}, false
}

// handlers returns enabled file handlers, ordered by id.
func (r *Registry) handlers() []OwnedHandler {
	var out []OwnedHandler
	for _, p := range r.activePlugins() {
		for _, h := range p.Capabilities().FileHandlers {
			out = append(out, OwnedHandler{Owner: p.ID(), FileHandler: h})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Hooks returns enabled hooks subscribed to event, ordered by id.
func (r *Registry) Hooks(event plugin.Event) []OwnedHook {
	var out []OwnedHook
	for _, p := range r.activePlugins() {
		for _, h := range p.Capabilities().Hooks {
			if h.Event == event {
				out = append(out, OwnedHook{Owner: p.ID(), Hook: h})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Conflict describes a registration clash between two enabled capabilities.
type Conflict struct {
	Kind   string // "command", "keymap", "pane", "handler"
	Key    string // the clashing id / keys / extension
	Owners []string
	Detail string
}

// Conflicts scans enabled plugins for duplicate command ids, pane ids,
// overlapping file-extension claims, and same-key bindings that share a
// priority (an ambiguous clash). The result is deterministic.
func (r *Registry) Conflicts() []Conflict {
	var out []Conflict
	out = append(out, r.idConflicts("command", func(c plugin.Capabilities) []string {
		ids := make([]string, len(c.Commands))
		for i, x := range c.Commands {
			ids[i] = x.ID
		}
		return ids
	})...)
	out = append(out, r.idConflicts("pane", func(c plugin.Capabilities) []string {
		ids := make([]string, len(c.Panes))
		for i, x := range c.Panes {
			ids[i] = x.ID
		}
		return ids
	})...)
	out = append(out, r.handlerConflicts()...)
	out = append(out, r.keymapConflicts()...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// idConflicts finds ids contributed by more than one enabled plugin.
func (r *Registry) idConflicts(kind string, ids func(plugin.Capabilities) []string) []Conflict {
	owners := map[string][]string{}
	for _, p := range r.activePlugins() {
		for _, id := range ids(p.Capabilities()) {
			owners[id] = append(owners[id], p.ID())
		}
	}
	return collectClashes(kind, owners, "duplicate id")
}

// handlerConflicts finds file extensions claimed by more than one plugin.
func (r *Registry) handlerConflicts() []Conflict {
	owners := map[string][]string{}
	for _, p := range r.activePlugins() {
		for _, h := range p.Capabilities().FileHandlers {
			for _, e := range h.Extensions {
				key := strings.ToLower(e)
				owners[key] = append(owners[key], p.ID())
			}
		}
	}
	return collectClashes("handler", owners, "extension claimed by multiple handlers")
}

// keymapConflicts finds same-key bindings that share a priority and scope, which
// makes their resolution order ambiguous.
func (r *Registry) keymapConflicts() []Conflict {
	type key struct {
		keys     string
		priority int
		scope    plugin.Scope
	}
	owners := map[key][]string{}
	for _, p := range r.activePlugins() {
		for _, k := range p.Capabilities().Keymaps {
			kk := key{k.Keys, k.Priority, k.Scope}
			owners[kk] = append(owners[kk], p.ID())
		}
	}
	var out []Conflict
	for kk, os := range owners {
		if len(os) < 2 {
			continue
		}
		sort.Strings(os)
		out = append(out, Conflict{
			Kind:   "keymap",
			Key:    kk.keys,
			Owners: os,
			Detail: "same key, equal priority — ambiguous binding",
		})
	}
	return out
}

// collectClashes turns an id->owners map into Conflicts for ids with >1 owner.
func collectClashes(kind string, owners map[string][]string, detail string) []Conflict {
	var out []Conflict
	for k, os := range owners {
		if len(os) < 2 {
			continue
		}
		sort.Strings(os)
		out = append(out, Conflict{Kind: kind, Key: k, Owners: os, Detail: detail})
	}
	return out
}
