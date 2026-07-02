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

// applyDefaults seeds the [lsp] server table for Go, PHP and Python. A user can
// override any field (or disable a server) via their settings.toml; these are the
// lowest-precedence baseline. Servers are external binaries the user installs.
func applyDefaults(c *config.Config) {
	c.LSP.Enabled = true
	if c.LSP.Servers == nil {
		c.LSP.Servers = map[string]map[string]any{}
	}
	defaults := map[string]map[string]any{
		"go": {
			"command":      "gopls",
			"root_markers": []any{"go.mod", "go.work", ".git"},
		},
		"php": {
			"command":      "intelephense",
			"args":         []any{"--stdio"},
			"root_markers": []any{"composer.json", ".git"},
		},
		"python": {
			"command":      "pyright-langserver",
			"args":         []any{"--stdio"},
			"root_markers": []any{"pyproject.toml", "setup.py", "setup.cfg", ".git"},
		},
	}
	for lang, spec := range defaults {
		if _, exists := c.LSP.Servers[lang]; !exists {
			c.LSP.Servers[lang] = spec
		}
	}
}

// resolveSpec maps a language to its configured ServerSpec, honouring the global
// enable flag. It reads the live merged config so user overrides apply.
func resolveSpec(lang string) (ilsp.ServerSpec, bool) {
	c := config.Get()
	if c == nil || !c.LSP.Enabled {
		return ilsp.ServerSpec{}, false
	}
	return ilsp.SpecFor(c.LSP.Servers, lang)
}
