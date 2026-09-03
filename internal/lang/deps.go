// deps.go is the dependency-manifest declaration seam (#2419): language
// plugins name the manifest files they own via Language.DepManifests, and
// the Dependencies tool window (internal/deps) intersects its providers'
// manifests with the declared set — an ecosystem is only scanned while a
// registered language claims its manifest.
package lang

// DepManifests returns the union of every registered language's declared
// dependency manifest base names, deduplicated, in no particular order.
func DepManifests() []string {
	mu.RLock()
	defer mu.RUnlock()
	seen := map[string]bool{}
	var out []string
	for _, l := range byID {
		for _, name := range l.DepManifests {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}
