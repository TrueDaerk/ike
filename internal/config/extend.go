package config

import "sync"

// extend.go is the registration hook other roadmaps use to grow configuration
// without editing the core structs. The baseline structs define the *slots*
// (Explorer.Colors, Keymap.Bindings, LSP.Servers, …); an Extension supplies the
// *entries* and optional validation. This keeps cross-roadmap coupling additive:
// Roadmap 0050 fills explorer colors, 0080 the keymap bindings, 0100 the LSP
// servers — each by registering, never by patching this package.
//
// Contract:
//   - Defaults(c) mutates the default-layer Config before user/project merge, so
//     its entries are the lowest precedence and a user can still override them.
//   - Validate(c) runs after the merge on the final Config and returns
//     clamp-and-warn diagnostics for that section.
//   - Register is idempotent by Name: re-registering replaces the prior entry,
//     which keeps tests and hot-reload from accumulating duplicates.

// Extension adds a named configuration contribution.
type Extension struct {
	Name     string
	Defaults func(*Config)
	Validate func(*Config) []Diagnostic
}

var (
	extMu   sync.Mutex
	extList []Extension
)

// Register installs (or replaces, by Name) an Extension. It is safe to call from
// package init functions in downstream roadmaps.
func Register(e Extension) {
	extMu.Lock()
	defer extMu.Unlock()
	for i := range extList {
		if extList[i].Name == e.Name {
			extList[i] = e
			return
		}
	}
	extList = append(extList, e)
}

// registered returns a snapshot of the current extensions.
func registered() []Extension {
	extMu.Lock()
	defer extMu.Unlock()
	out := make([]Extension, len(extList))
	copy(out, extList)
	return out
}

// applyExtensionDefaults runs every extension's Defaults over c (the base
// defaults) before any file layer is merged.
func applyExtensionDefaults(c *Config) {
	for _, e := range registered() {
		if e.Defaults != nil {
			e.Defaults(c)
		}
	}
}

// resetExtensions clears the registry; intended for tests only.
func resetExtensions() {
	extMu.Lock()
	defer extMu.Unlock()
	extList = nil
}
