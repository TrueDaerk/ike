// Package register is the one-call registration seam for language packages
// (Roadmap 0180, #133): it enters the language into the lang registry (as
// before) and additionally registers a `lang.<id>` plugin shim in the plugin
// registry, so the plugin manager page can list and toggle languages like any
// other plugin — LSP activation follows the plugin, no second mechanism.
//
// The complexity-only build (#2939, build tag ike_complexity_only) has no
// plugin manager: there the shim half compiles to a no-op (shim_none.go), so
// importing a language package does not drag internal/plugin — and with it
// the host, the settings UI and the WASM runtime — into that binary.
package register

import "ike/internal/lang"

// Language registers l in both registries. Call it from a language package's
// init() instead of lang.Register. The shim id is "lang-<id>" — dash, not
// dot: plugin toggles persist as the dotted key plugins.<id>.enabled, so a
// dot inside the id would splinter it into nested tables.
func Language(l lang.Language) {
	lang.Register(l)
	registerShim("lang-" + l.ID)
}
