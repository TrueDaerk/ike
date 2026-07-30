// Package format is the compile-in plugin owning the reformat commands
// (Roadmap 0470, #1401). The command ids stay `lsp.format` / `lsp.formatRange`
// so existing keymaps (`cmd+alt+l`, user bindings, JetBrains imports) keep
// working, but the titles are language-neutral and the implementation is the
// formatter registry (internal/format): config override → external command →
// LSP → built-in, resolved per buffer by the app's handler. It also wires the
// config side of the external-command provider (#1402): the
// `[format.<languageID>]` override table, the enabled hook and the Formatters
// settings page. It self-registers via init() and is wired into the build by
// a blank import in cmd/ike/main.go.
package format

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	iformat "ike/internal/format"
	"ike/internal/host"
	"ike/internal/lang"
	"ike/internal/plugin"
	"ike/internal/registry"
	"ike/internal/settings"
)

func init() {
	registry.Register(Plugin{})
	config.Register(config.Extension{
		Name:     "format",
		Defaults: applyDefaults,
	})
	// The config-override provider (#1402): an explicit [format.<lang>]
	// command is the strongest chain entry, beating plugin defaults and LSP.
	iformat.Register(overrideProvider())
	// [format.<lang>] enabled = false switches a language's external
	// formatting off — both the override and the plugin default.
	iformat.SetExternalEnabled(func(langID string) bool { return formatFlag(langID, "enabled") })
	// [format.<lang>] builtin = false disables a language's built-in
	// formatter (#1403): for SQL that re-exposes sqls' LSP formatting.
	iformat.SetBuiltinEnabled(func(langID string) bool { return formatFlag(langID, "builtin") })
}

// formatFlag reads a [format.<lang>] boolean; absent or non-bool means true.
func formatFlag(langID, key string) bool {
	c := config.Get()
	if c == nil {
		return true
	}
	if raw, ok := c.Format[langID]; ok {
		if b, isBool := raw[key].(bool); isBool {
			return b
		}
	}
	return true
}

// applyDefaults seeds the free-form override slot.
func applyDefaults(c *config.Config) {
	if c.Format == nil {
		c.Format = map[string]map[string]any{}
	}
}

// Plugin provides the reformat commands.
type Plugin struct{}

// ID implements plugin.Plugin.
func (Plugin) ID() string { return "format" }

// Capabilities registers the reformat commands. They dispatch request
// messages the app answers — only the app holds the active buffer, its
// effective per-buffer options and the view to apply edits to.
func (Plugin) Capabilities() plugin.Capabilities {
	return plugin.Capabilities{
		Commands: []plugin.Command{
			{
				ID:    "lsp.format",
				Title: "Reformat File",
				Scope: plugin.PaneScope("editor"),
				Run:   func(h host.API) tea.Cmd { return h.Dispatch(iformat.FileRequestMsg{}) },
			},
			{
				ID:    "lsp.formatRange",
				Title: "Reformat Selection",
				Scope: plugin.PaneScope("editor"),
				Run:   func(h host.API) tea.Cmd { return h.Dispatch(iformat.RangeRequestMsg{}) },
			},
		},
		SettingsPages: []settings.Page{
			{
				Title:       "Formatters",
				Description: "External reformat commands per language: the effective command line, the config layer supplying it and whether the binary is installed.",
				Custom:      settings.NewFormatPage(config.Discover(".")),
			},
		},
	}
}

// overrideSpec reads the effective [format.<lang>] override for a language;
// ok is false without a configured command.
func overrideSpec(langID string) (iformat.External, bool) {
	c := config.Get()
	if c == nil || langID == "" {
		return iformat.External{}, false
	}
	raw, ok := c.Format[langID]
	if !ok {
		return iformat.External{}, false
	}
	if b, isBool := raw["enabled"].(bool); isBool && !b {
		return iformat.External{}, false
	}
	spec := iformat.ExternalFromConfig(raw)
	if spec.Command == "" {
		return iformat.External{}, false
	}
	return spec, true
}

// langIDFor resolves path's registered language id ("" when unknown).
func langIDFor(path string) string {
	if l, ok := lang.ByPath(path); ok {
		return l.ID
	}
	return ""
}

// overrideProvider is the TierOverride registry entry: it serves every
// language and resolves per path against the live config, so a config edit
// applies without restarts (like the [lsp.servers.*] overlay).
func overrideProvider() iformat.Provider {
	return iformat.Provider{
		Name: "config",
		NameFor: func(path string) string {
			if spec, ok := overrideSpec(langIDFor(path)); ok {
				return spec.Command
			}
			return ""
		},
		Tier: iformat.TierOverride,
		Available: func(path string) bool {
			langID := langIDFor(path)
			spec, ok := overrideSpec(langID)
			if !ok {
				return false
			}
			if !spec.Ok() {
				// The user configured this tool explicitly — a missing binary
				// deserves the one-time install hint (#1067 pattern).
				iformat.HintMissing(langID, spec)
				return false
			}
			return true
		},
		RangeAvailable: func(path string) bool {
			spec, ok := overrideSpec(langIDFor(path))
			return ok && len(spec.RangeArgs) > 0 && spec.Ok()
		},
		Format: func(ctx context.Context, req iformat.Request) (iformat.Result, error) {
			spec, ok := overrideSpec(req.Language)
			if !ok {
				return iformat.Result{}, errors.New("no [format." + req.Language + "] override configured")
			}
			return spec.Run(ctx, req)
		},
		FormatRange: func(ctx context.Context, req iformat.Request, start, end iformat.Pos) (iformat.Result, error) {
			spec, ok := overrideSpec(req.Language)
			if !ok {
				return iformat.Result{}, errors.New("no [format." + req.Language + "] override configured")
			}
			return spec.RunRange(ctx, req, start, end)
		},
	}
}
