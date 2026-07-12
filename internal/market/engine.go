package market

// engine.go is the marketplace install engine (Roadmap 0310, #445): download
// a catalog entry's .wasm over HTTPS, verify its SHA-256 against the catalog
// digest, and atomically place "<name>.wasm" plus its manifest sidecar in the
// plugins directory (wasm.DefaultDir). The manifest written here pins the
// capability list the user reviewed in the catalog, so the runtime's gate
// enforces exactly that. Installed state is read back from the sidecars —
// there is no separate state file. The package stays bubbletea-free; the
// marketplace page adapts it into tea.Cmds.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ike/internal/wasm"
)

// maxArtifactBytes caps one plugin download.
const maxArtifactBytes = 64 << 20 // 64 MiB

// Engine installs, updates and removes plugins in a plugins directory.
type Engine struct {
	client *Client
	dir    string
}

// NewEngine returns an Engine writing into dir (typically wasm.DefaultDir())
// and downloading through client.
func NewEngine(client *Client, dir string) *Engine {
	return &Engine{client: client, dir: dir}
}

// Installed is one plugin found in the plugins directory.
type Installed struct {
	Name string
	// Version is the manifest version; VersionOK is false when the plugin has
	// no sidecar or its version does not parse (hand-installed plugins) — the
	// marketplace then never claims an update is available for it.
	Version   Version
	VersionOK bool
}

// Installed scans the plugins directory and reports every plugin present
// (any .wasm file, with version detail when its sidecar parses). A missing
// directory is normal and yields an empty map.
func (e *Engine) Installed() (map[string]Installed, error) {
	entries, err := os.ReadDir(e.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Installed{}, nil
		}
		return nil, fmt.Errorf("plugins dir %s: %w", e.dir, err)
	}
	out := map[string]Installed{}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".wasm") {
			continue
		}
		name := strings.TrimSuffix(ent.Name(), ".wasm")
		inst := Installed{Name: name}
		if data, err := os.ReadFile(e.manifestPath(name)); err == nil {
			if m, err := wasm.ParseManifest(data); err == nil {
				if v, err := ParseVersion(m.Version); err == nil {
					inst.Version, inst.VersionOK = v, true
				}
			}
		}
		out[name] = inst
	}
	return out, nil
}

// UpdateAvailable reports whether entry is newer than inst. An installed
// plugin without a parsable version is never offered an update — overwriting
// a hand-installed plugin of unknown provenance is not the marketplace's call.
func UpdateAvailable(entry Entry, inst Installed) bool {
	return inst.VersionOK && entry.ParsedVersion().Compare(inst.Version) > 0
}

// Install downloads entry's artifact, verifies its SHA-256 and writes
// "<name>.wasm" plus the manifest sidecar pinning the catalog capability
// list. On any failure nothing in the plugins directory changes. Updating is
// the same operation over an existing install.
func (e *Engine) Install(ctx context.Context, entry Entry) error {
	if _, err := validateEntry(entry); err != nil {
		return fmt.Errorf("install %s: %w", entry.Name, err)
	}
	data, err := e.client.get(ctx, entry.Artifact.URL, maxArtifactBytes)
	if err != nil {
		return fmt.Errorf("install %s: %w", entry.Name, err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, entry.Artifact.SHA256) {
		return fmt.Errorf("install %s: checksum mismatch (catalog %s, artifact %s)",
			entry.Name, strings.ToLower(entry.Artifact.SHA256), got)
	}
	manifest, err := json.MarshalIndent(wasm.Manifest{
		Name:         entry.Name,
		Version:      entry.ParsedVersion().String(),
		Capabilities: append([]string{}, entry.Capabilities...),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("install %s: %w", entry.Name, err)
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return fmt.Errorf("install %s: %w", entry.Name, err)
	}
	// Manifest lands before the module: a module without a sidecar runs
	// unrestricted, so the capability pin must never trail the .wasm.
	if err := atomicWrite(e.manifestPath(entry.Name), manifest); err != nil {
		return fmt.Errorf("install %s: %w", entry.Name, err)
	}
	if err := atomicWrite(e.wasmPath(entry.Name), data); err != nil {
		return fmt.Errorf("install %s: %w", entry.Name, err)
	}
	return nil
}

// Remove deletes a plugin's .wasm and manifest sidecar. The module goes
// first: a leftover sidecar is inert, a leftover module would run
// unrestricted.
func (e *Engine) Remove(name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("remove %q: invalid name", name)
	}
	if err := os.Remove(e.wasmPath(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	if err := os.Remove(e.manifestPath(name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", name, err)
	}
	return nil
}

func (e *Engine) wasmPath(name string) string {
	return filepath.Join(e.dir, name+".wasm")
}

func (e *Engine) manifestPath(name string) string {
	return filepath.Join(e.dir, name+".manifest.json")
}

// atomicWrite writes data to a temp file in the target directory and renames
// it into place, so a crash never leaves a half-written file at path.
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
