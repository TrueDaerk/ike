// scan.go orchestrates a project dependency scan (#2419): detect the
// root manifests, parse them, and enrich each dependency with the latest
// version and vulnerabilities from the toolchain. Results are cached per
// manifest mtime so re-scans without manifest edits skip the (slow,
// network-bound) toolchain calls; deps.refresh forces past the cache.
package deps

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ManifestDeps is one manifest file with its enriched dependencies.
type ManifestDeps struct {
	// Path is the absolute manifest path.
	Path string
	// Provider is the owning provider's ID.
	Provider string
	// Deps are the declared dependencies, in manifest order.
	Deps []Dep
}

// Result is one completed project scan.
type Result struct {
	// Root is the scanned project root.
	Root string
	// Manifests lists the detected manifests with their dependencies,
	// ordered by path.
	Manifests []ManifestDeps
	// Missing lists absent toolchain binaries per provider, each with an
	// install hint. A missing required tool skips that provider's
	// enrichment but still lists the parsed manifest.
	Missing []MissingTool
	// Errors collects non-fatal per-provider failures (tool exit or
	// parse errors), for the status line / log.
	Errors []string
}

// VulnCount sums the vulnerabilities across all manifests.
func (r Result) VulnCount() int {
	n := 0
	for _, m := range r.Manifests {
		for _, d := range m.Deps {
			n += len(d.Vulns)
		}
	}
	return n
}

// OutdatedCount counts dependencies with a newer known version.
func (r Result) OutdatedCount() int {
	n := 0
	for _, m := range r.Manifests {
		for _, d := range m.Deps {
			if d.Outdated() {
				n++
			}
		}
	}
	return n
}

type cacheEntry struct {
	mtime time.Time
	deps  ManifestDeps
}

// Scanner runs project scans with a per-manifest mtime cache. The zero
// value is not usable — construct with NewScanner.
type Scanner struct {
	mu    sync.Mutex
	cache map[string]cacheEntry
}

// NewScanner returns an empty scanner.
func NewScanner() *Scanner {
	return &Scanner{cache: map[string]cacheEntry{}}
}

// Scan detects and enriches the root's manifests. With force, the mtime
// cache is bypassed. Safe for concurrent use; intended to run inside a
// tea.Cmd goroutine.
func (s *Scanner) Scan(ctx context.Context, root string, force bool) Result {
	res := Result{Root: root}
	seenProvider := map[string]bool{}
	for _, path := range DetectManifests(root) {
		p := providerFor(filepath.Base(path))
		if p == nil {
			continue
		}
		if !seenProvider[p.ID()] {
			seenProvider[p.ID()] = true
			res.Missing = append(res.Missing, missingTools(p)...)
		}
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !force {
			s.mu.Lock()
			e, ok := s.cache[path]
			s.mu.Unlock()
			if ok && e.mtime.Equal(fi.ModTime()) {
				res.Manifests = append(res.Manifests, e.deps)
				continue
			}
		}
		md := s.scanManifest(ctx, p, path, &res)
		res.Manifests = append(res.Manifests, md)
		s.mu.Lock()
		s.cache[path] = cacheEntry{mtime: fi.ModTime(), deps: md}
		s.mu.Unlock()
	}
	return res
}

// Invalidate drops one manifest's cache entry (e.g. after a bump).
func (s *Scanner) Invalidate(path string) {
	s.mu.Lock()
	delete(s.cache, path)
	s.mu.Unlock()
}

// scanManifest parses one manifest and enriches it via the provider's
// toolchain when the required tools are present.
func (s *Scanner) scanManifest(ctx context.Context, p Provider, path string, res *Result) ManifestDeps {
	md := ManifestDeps{Path: path, Provider: p.ID()}
	deps, err := p.Manifest(path)
	if err != nil {
		res.Errors = append(res.Errors, p.ID()+": "+err.Error())
		return md
	}
	md.Deps = deps
	for _, t := range p.Tools() {
		if !t.Optional && !hasTool(t.Name) {
			// The required toolchain is absent: keep the parsed listing,
			// skip enrichment. The missing tool is already reported.
			return md
		}
	}
	latest, err := p.Outdated(ctx, filepath.Dir(path))
	if err != nil {
		res.Errors = append(res.Errors, p.ID()+" outdated: "+err.Error())
	}
	vulns, err := p.Audit(ctx, filepath.Dir(path))
	if err != nil {
		res.Errors = append(res.Errors, p.ID()+" audit: "+err.Error())
	}
	for i := range md.Deps {
		d := &md.Deps[i]
		if v, ok := lookupDep(latest, d.Name); ok {
			d.Latest = v
		}
		if v, ok := lookupDep(vulns, d.Name); ok {
			d.Vulns = v
		}
	}
	return md
}

// lookupDep resolves a dependency name in a toolchain-keyed map, falling
// back to PEP 503 normalization so Python name spellings still match.
func lookupDep[V any](m map[string]V, name string) (V, bool) {
	if v, ok := m[name]; ok {
		return v, true
	}
	v, ok := m[normalizePyPkg(name)]
	return v, ok
}
