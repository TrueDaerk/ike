//go:build !ike_complexity_only

package register

import (
	"ike/internal/plugin"
	"ike/internal/registry"
)

// registerShim enters the language's plugin shim into the plugin registry.
func registerShim(id string) { registry.Register(shim{id: id}) }

// shim is the plugin-registry face of a language package. Its capabilities
// live in the lang registry (grammar, server, toolchain), so the plugin shape
// is empty — the id and the enabled toggle are what the manager page needs.
type shim struct{ id string }

func (s shim) ID() string                      { return s.id }
func (shim) Capabilities() plugin.Capabilities { return plugin.Capabilities{} }
