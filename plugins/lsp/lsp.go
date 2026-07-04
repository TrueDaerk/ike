// Package lsp is the compile-in plugin that activates language intelligence: it
// registers the [lsp] config defaults, owns the manager.Manager, installs the
// editor-event bridge, and exposes LSP actions (hover, definition, restart) as
// registry commands (Roadmap 0100). It self-registers via init() and is wired
// into the build by a blank import in cmd/ike/main.go.
package lsp

import (
	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
	"ike/internal/plugin"
	"ike/internal/registry"
)

func init() {
	registry.Register(Plugin{})
	config.Register(config.Extension{
		Name:     "lsp",
		Defaults: applyDefaults,
	})
}

// Plugin is the LSP capability provider.
type Plugin struct{}

// ID implements plugin.Plugin.
func (Plugin) ID() string { return "lsp" }

// Capabilities registers the LSP commands plus a file-opened hook that activates
// the subsystem (captures the host, wires the editor emitter, and opens the
// document) on the first file open.
func (Plugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{
			{
				ID:    "lsp.hover",
				Title: "LSP: Hover",
				Scope: plugin.PaneScope("editor"),
				Run:   func(h host.API) tea.Cmd { return shared().hover(h) },
			},
			{
				ID:    "lsp.definition",
				Title: "LSP: Go to Definition",
				Scope: plugin.PaneScope("editor"),
				Run:   func(h host.API) tea.Cmd { return shared().definition(h) },
			},
			{
				ID:    "lsp.restart",
				Title: "LSP: Restart Servers",
				Scope: plugin.GlobalScope(),
				Run:   func(h host.API) tea.Cmd { return shared().restart(h) },
			},
		},
		Hooks: []plugin.Hook{
			{
				ID:    "lsp.didopen",
				Event: plugin.EventFileOpened,
				Notify: func(h host.API, payload any) tea.Cmd {
					path, _ := payload.(string)
					shared().fileOpened(h, path)
					return nil
				},
			},
			{
				ID:    "lsp.didsave",
				Event: plugin.EventBufferSaved,
				Notify: func(h host.API, payload any) tea.Cmd {
					path, _ := payload.(string)
					shared().fileSaved(h, path)
					return nil
				},
			},
			{
				ID:    "lsp.didclose",
				Event: plugin.EventBufferClosed,
				Notify: func(h host.API, payload any) tea.Cmd {
					path, _ := payload.(string)
					shared().fileClosed(path)
					return nil
				},
			},
		},
	}
}

// applyDefaults enables the LSP subsystem. Server baselines (command, args, root
// markers) now come from each language plugin's lang.Language.Server, not from a
// hardcoded table here; the [lsp.servers.<id>] config section only carries user
// overrides. Servers are external binaries the user installs.
func applyDefaults(c *config.Config) {
	c.LSP.Enabled = true
	if c.LSP.Servers == nil {
		c.LSP.Servers = map[string]map[string]any{}
	}
}

// resolveSpec resolves a language's effective ServerSpec: the language plugin's
// baseline (lang.ByID) overlaid with the user's [lsp.servers.<id>] config
// (per-field override). ok=false when LSP is disabled or no command is resolved
// (e.g. a language with a grammar but no server).
func resolveSpec(langID string) (ilsp.ServerSpec, bool) {
	c := config.Get()
	if c == nil || !c.LSP.Enabled {
		return ilsp.ServerSpec{}, false
	}
	var spec ilsp.ServerSpec
	if l, ok := lang.ByID(langID); ok && l.Server != nil {
		spec = *l.Server
	}
	if ov, ok := ilsp.Overlay(c.LSP.Servers, langID); ok {
		spec = mergeSpec(spec, ov)
	}
	spec.Language = langID
	if spec.Command == "" {
		return ilsp.ServerSpec{}, false
	}
	return spec, true
}

// mergeSpec overlays a config spec onto a baseline: each field the config sets
// wins; unset fields keep the baseline. Settings deep-merge (user keys win).
func mergeSpec(base, ov ilsp.ServerSpec) ilsp.ServerSpec {
	if ov.Command != "" {
		base.Command = ov.Command
	}
	if ov.Args != nil {
		base.Args = ov.Args
	}
	if ov.Env != nil {
		base.Env = ov.Env
	}
	if ov.RootMarkers != nil {
		base.RootMarkers = ov.RootMarkers
	}
	if ov.Settings != nil {
		base.Settings = lang.MergeSettings(base.Settings, ov.Settings)
	}
	return base
}
