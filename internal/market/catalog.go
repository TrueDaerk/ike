package market

// catalog.go defines the marketplace index format and its strict validation
// (Roadmap 0310, #444). The catalog is a static JSON document ("index.json")
// served over HTTPS; ParseIndex rejects a document whose top-level version is
// unsupported, and validates every plugin entry individually — a bad entry is
// skipped with a diagnostic while the rest of the index still loads, mirroring
// how wasm.ScanDir treats a faulting module.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"ike/internal/wasm"
)

// IndexVersion is the catalog format version this build understands.
const IndexVersion = 1

// DefaultCatalogURL is the built-in catalog location. It is empty until an
// official catalog exists; the marketplace page shows "no catalog configured"
// when both this and marketplace.catalog_url are empty.
const DefaultCatalogURL = ""

// Artifact locates a plugin's .wasm bundle and pins its content.
type Artifact struct {
	// URL is the HTTPS download location of the .wasm file.
	URL string `json:"url"`
	// SHA256 is the lowercase hex digest of the artifact; the install engine
	// refuses to write anything whose download does not match it.
	SHA256 string `json:"sha256"`
}

// Entry is one plugin in the catalog.
type Entry struct {
	// Name is the plugin identity: it becomes the .wasm base name in the
	// plugins directory and must match the manifest name the runtime checks.
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Homepage    string `json:"homepage"`
	// Capabilities is the closed capability list the plugin requests; it is
	// written verbatim into the installed manifest, so the runtime enforces
	// exactly what the user reviewed in the marketplace.
	Capabilities []string `json:"capabilities"`
	Artifact     Artifact `json:"artifact"`

	// parsed is the validated Version; set by validateEntry.
	parsed Version
}

// ParsedVersion returns the entry's version parsed at validation time.
func (e Entry) ParsedVersion() Version { return e.parsed }

// Index is the parsed, validated catalog.
type Index struct {
	Version int     `json:"version"`
	Plugins []Entry `json:"plugins"`
}

// nameRE bounds entry names to safe file-base characters: the name becomes
// "<name>.wasm" on disk, so path separators and dot-prefixes are out.
var nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// sha256RE matches a lowercase-or-uppercase 64-digit hex digest.
var sha256RE = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ParseIndex decodes and validates catalog JSON. It returns the index with
// only the valid entries, one diagnostic string per rejected entry, and an
// error only when the document as a whole is unusable (bad JSON, unsupported
// top-level version).
func ParseIndex(data []byte) (Index, []string, error) {
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, nil, fmt.Errorf("catalog: %w", err)
	}
	if idx.Version != IndexVersion {
		return Index{}, nil, fmt.Errorf("catalog: unsupported index version %d (want %d)", idx.Version, IndexVersion)
	}
	var diags []string
	seen := map[string]bool{}
	valid := idx.Plugins[:0]
	for _, e := range idx.Plugins {
		v, err := validateEntry(e)
		if err != nil {
			diags = append(diags, fmt.Sprintf("catalog: entry %q skipped: %v", e.Name, err))
			continue
		}
		if seen[e.Name] {
			diags = append(diags, fmt.Sprintf("catalog: entry %q skipped: duplicate name", e.Name))
			continue
		}
		seen[e.Name] = true
		e.parsed = v
		valid = append(valid, e)
	}
	idx.Plugins = valid
	return idx, diags, nil
}

// validateEntry checks one entry against the format rules and returns its
// parsed version.
func validateEntry(e Entry) (Version, error) {
	if strings.TrimSpace(e.Name) == "" {
		return Version{}, fmt.Errorf("missing name")
	}
	if !nameRE.MatchString(e.Name) {
		return Version{}, fmt.Errorf("invalid name")
	}
	v, err := ParseVersion(e.Version)
	if err != nil {
		return Version{}, err
	}
	known := map[string]bool{}
	for _, c := range wasm.KnownCapabilities() {
		known[c] = true
	}
	seen := map[string]bool{}
	for _, c := range e.Capabilities {
		if !known[c] {
			return Version{}, fmt.Errorf("unknown capability %q", c)
		}
		if seen[c] {
			return Version{}, fmt.Errorf("duplicate capability %q", c)
		}
		seen[c] = true
	}
	if err := checkHTTPS(e.Artifact.URL); err != nil {
		return Version{}, fmt.Errorf("artifact url: %w", err)
	}
	if !sha256RE.MatchString(e.Artifact.SHA256) {
		return Version{}, fmt.Errorf("artifact sha256: want 64 hex digits")
	}
	return v, nil
}

// checkHTTPS accepts only well-formed https:// URLs.
func checkHTTPS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%q is not an https URL", raw)
	}
	return nil
}
