// snapshot.go keeps the latest completed scan result globally readable
// (#2419). Hover and intention providers are synchronous seams that fire
// on every caret move, so they read this precomputed snapshot instead of
// touching the toolchain.
package deps

import "sync"

var (
	snapMu sync.RWMutex
	snap   Result
)

// SetSnapshot publishes a completed scan for the synchronous consumers.
func SetSnapshot(r Result) {
	snapMu.Lock()
	snap = r
	snapMu.Unlock()
}

// Snapshot returns the last published scan (zero Result before any scan).
func Snapshot() Result {
	snapMu.RLock()
	defer snapMu.RUnlock()
	return snap
}

// DepAt finds the dependency declared on the given 1-based line of a
// scanned manifest, along with its provider ID.
func DepAt(path string, line int) (Dep, string, bool) {
	snapMu.RLock()
	defer snapMu.RUnlock()
	for _, m := range snap.Manifests {
		if m.Path != path {
			continue
		}
		for _, d := range m.Deps {
			if d.Line == line {
				return d, m.Provider, true
			}
		}
	}
	return Dep{}, "", false
}

// ProviderByID returns the built-in provider with the given ID.
func ProviderByID(id string) Provider {
	for _, p := range Providers() {
		if p.ID() == id {
			return p
		}
	}
	return nil
}

// IsManifestName reports whether the base name is a known dependency
// manifest of any provider.
func IsManifestName(base string) bool { return providerFor(base) != nil }
