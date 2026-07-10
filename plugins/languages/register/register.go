// Package register is the one-call registration seam for language packages
// (Roadmap 0180, #133): it enters the language into the lang registry (as
// before) and additionally registers a `lang.<id>` plugin shim in the plugin
// registry, so the plugin manager page can list and toggle languages like any
// other plugin — LSP activation follows the plugin, no second mechanism.
package register

import (
	"ike/internal/lang"
	"ike/internal/plugin"
	"ike/internal/registry"
)

// Language registers l in both registries. Call it from a language package's
// init() instead of lang.Register. The shim id is "lang-<id>" — dash, not
// dot: plugin toggles persist as the dotted key plugins.<id>.enabled, so a
// dot inside the id would splinter it into nested tables.
func Language(l lang.Language) {
	lang.Register(l)
	registry.Register(shim{id: "lang-" + l.ID})
}

// shim is the plugin-registry face of a language package. Its capabilities
// live in the lang registry (grammar, server, toolchain), so the plugin shape
// is empty — the id and the enabled toggle are what the manager page needs.
type shim struct{ id string }

func (s shim) ID() string                      { return s.id }
func (shim) Capabilities() plugin.Capabilities { return plugin.Capabilities{} }
