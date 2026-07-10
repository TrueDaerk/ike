// Package bridge adapts loaded WASM modules into plugin.Plugin capabilities
// (Roadmap 9900, #25): it calls each module's exported register(), translates
// the declared descriptors into the same plugin.Command / plugin.Keymap /
// plugin.Hook shapes compile-in plugins produce, and registers the result —
// from then on a WASM module is indistinguishable from a compile-in plugin
// to the rest of IKE. The extension-point contract (Roadmap 0020) is
// consumed unchanged.
package bridge

import (
	"context"
	"encoding/json"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"ike/internal/host"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/wasm"
	"ike/internal/wasm/abi"
)

// Plugin is the plugin.Plugin face of one registered WASM module.
type Plugin struct {
	id   string
	caps plugin.Capabilities
}

// ID implements plugin.Plugin.
func (p *Plugin) ID() string { return p.id }

// Capabilities implements plugin.Plugin.
func (p *Plugin) Capabilities() plugin.Capabilities { return p.caps }

// hookEvents maps the ABI's event names onto plugin.Event.
var hookEvents = map[string]plugin.Event{
	"file_opened":   plugin.EventFileOpened,
	"buffer_saved":  plugin.EventBufferSaved,
	"buffer_closed": plugin.EventBufferClosed,
}

// adapt builds the plugin face for a module from its declared capabilities.
// Every callback runs the guest inside a tea.Cmd goroutine — a slow or
// faulting guest never stalls the Update loop; faults surface as warnings.
func adapt(ctx context.Context, mod *wasm.Module, caps *abi.Capabilities) *Plugin {
	id := caps.Name
	if id == "" {
		id = mod.Name
	}
	p := &Plugin{id: id}

	guestCall := func(kind, target string, call func(context.Context) error) func(h host.API) tea.Cmd {
		return func(h host.API) tea.Cmd {
			return func() tea.Msg {
				if err := call(ctx); err != nil {
					h.Notify(host.Warn, fmt.Sprintf("plugin %s: %s %s: %v", id, kind, target, err))
				}
				return nil
			}
		}
	}

	for _, c := range caps.Commands {
		if c.ID == "" {
			continue
		}
		scope := plugin.GlobalScope()
		if c.Context != "" {
			scope = plugin.PaneScope(c.Context)
		}
		cmdID := c.ID
		p.caps.Commands = append(p.caps.Commands, plugin.Command{
			ID:    cmdID,
			Title: c.Title,
			Scope: scope,
			Run:   guestCall("command", cmdID, func(cctx context.Context) error { return abi.CallCommand(cctx, mod.API(), cmdID) }),
		})
	}
	for _, k := range caps.Keymaps {
		if k.Keys == "" {
			continue
		}
		scope := plugin.GlobalScope()
		if k.Context != "" {
			scope = plugin.PaneScope(k.Context)
		}
		bindID := k.CommandID
		if bindID == "" {
			bindID = id + "." + k.Keys
		}
		p.caps.Keymaps = append(p.caps.Keymaps, plugin.Keymap{
			Keys:      k.Keys,
			Scope:     scope,
			CommandID: k.CommandID,
			Priority:  plugin.CorePriority,
			Action:    guestCall("key", bindID, func(cctx context.Context) error { return abi.CallKey(cctx, mod.API(), bindID) }),
		})
	}
	for _, hk := range caps.Hooks {
		event, ok := hookEvents[hk.Event]
		if !ok || hk.ID == "" {
			continue
		}
		hookID := hk.ID
		p.caps.Hooks = append(p.caps.Hooks, plugin.Hook{
			ID:    hookID,
			Event: event,
			Notify: func(h host.API, payload any) tea.Cmd {
				return func() tea.Msg {
					data, err := json.Marshal(payload)
					if err != nil {
						data = nil
					}
					if err := abi.CallHook(ctx, mod.API(), hookID, data); err != nil {
						h.Notify(host.Warn, fmt.Sprintf("plugin %s: hook %s: %v", p.id, hookID, err))
					}
					return nil
				}
			},
		})
	}
	return p
}

// RegisterModules asks every loaded module for its capabilities and adds the
// resulting plugins to reg. Modules without a register() export contribute
// nothing; a faulting registration is unloaded and reported, never fatal.
func RegisterModules(ctx context.Context, rt *wasm.Runtime, reg *registry.Registry) []string {
	var diags []string
	for _, mod := range rt.Modules() {
		caps, err := abi.Register(ctx, mod.API())
		if err != nil {
			rt.Unload(mod.Name)
			diags = append(diags, fmt.Sprintf("plugin %s: register: %v", mod.Name, err))
			continue
		}
		if caps == nil {
			continue // no register export: a bare module, nothing to add
		}
		reg.Add(adapt(ctx, mod, caps))
	}
	return diags
}
