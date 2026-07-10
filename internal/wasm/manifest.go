package wasm

// manifest.go implements the per-plugin manifest (Roadmap 9900, #27): a JSON
// sidecar "<plugin>.manifest.json" next to the .wasm declaring name, version,
// and the capabilities the plugin requests. A present manifest is validated
// strictly — a malformed one rejects the module — and capability gating is
// enforced against it at registration time (bridge) and per host call (abi
// gate). A module without a manifest keeps full capabilities: the manifest
// narrows the sandbox, the sandbox itself (no FS/net, memory cap, call
// timeouts) applies regardless.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Capability names a manifest may request. Registration kinds gate what the
// bridge accepts from register(); host-call kinds gate the "ike" imports.
const (
	CapCommands  = "commands"
	CapKeymaps   = "keymaps"
	CapHooks     = "hooks"
	CapOpenFile  = "open_file"
	CapDispatch  = "dispatch"
	CapNotify    = "notify"
	CapSetStatus = "set_status"
	CapConfigGet = "config_get"
)

// knownCapabilities is the closed set a manifest may request.
var knownCapabilities = map[string]bool{
	CapCommands:  true,
	CapKeymaps:   true,
	CapHooks:     true,
	CapOpenFile:  true,
	CapDispatch:  true,
	CapNotify:    true,
	CapSetStatus: true,
	CapConfigGet: true,
}

// KnownCapabilities lists every capability name a manifest may request,
// sorted, for diagnostics and documentation.
func KnownCapabilities() []string {
	out := make([]string, 0, len(knownCapabilities))
	for c := range knownCapabilities {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Manifest is a plugin's parsed sidecar manifest.
type Manifest struct {
	// Name must equal the module name (the .wasm file base name) — the
	// manifest cannot claim a different identity than the file it sits next to.
	Name    string `json:"name"`
	Version string `json:"version"`
	// Capabilities lists what the plugin requests; anything not listed is
	// denied (registration kinds are dropped, host calls are no-ops).
	Capabilities []string `json:"capabilities"`
}

// ParseManifest decodes and validates manifest JSON. Validation is strict:
// unknown top-level fields are tolerated (forward compatibility) but name,
// version, and every capability entry must be well-formed.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	if strings.TrimSpace(m.Name) == "" {
		return m, fmt.Errorf("manifest: missing name")
	}
	if strings.TrimSpace(m.Version) == "" {
		return m, fmt.Errorf("manifest: missing version")
	}
	seen := map[string]bool{}
	for _, c := range m.Capabilities {
		if !knownCapabilities[c] {
			return m, fmt.Errorf("manifest: unknown capability %q (known: %s)",
				c, strings.Join(KnownCapabilities(), ", "))
		}
		if seen[c] {
			return m, fmt.Errorf("manifest: duplicate capability %q", c)
		}
		seen[c] = true
	}
	return m, nil
}

// Allows reports whether the manifest grants a capability. A nil manifest
// (module shipped without one) grants everything.
func (m *Manifest) Allows(capability string) bool {
	if m == nil {
		return true
	}
	for _, c := range m.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// manifestPathFor returns the sidecar path for a module path:
// "<dir>/<base>.manifest.json" next to "<dir>/<base>.wasm".
func manifestPathFor(wasmPath string) string {
	return strings.TrimSuffix(wasmPath, ".wasm") + ".manifest.json"
}

// loadManifest reads and validates the sidecar for wasmPath. A missing
// sidecar returns (nil, nil); any other failure — unreadable file, malformed
// JSON, failed validation, name not matching the module — is an error that
// rejects the module.
func loadManifest(wasmPath, moduleName string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPathFor(wasmPath))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	m, err := ParseManifest(data)
	if err != nil {
		return nil, err
	}
	if m.Name != moduleName {
		return nil, fmt.Errorf("manifest: name %q does not match module %q", m.Name, moduleName)
	}
	return &m, nil
}
