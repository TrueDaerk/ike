package lsp

// silent.go is the known-silent server list (#3080): a language server that
// never published a single diagnostic during a project switch's warm-up — the
// two-minute quiet fallback closed the wait — is remembered per project and
// per server id, so the next switch into that project does not arm the
// fallback timer (and the silent-server notice) for it again. The marker
// clears as soon as that server does publish, or via lsp.forgetSilent and the
// server's row in Settings › Language Servers.
//
// Like the complexity pins it is personal state in the state store: under
// IKE_CONFIG_DIR when it is set, else <root>/.ike/. Entries are keyed by the
// cleaned project root as well, so a state directory shared by several
// projects still keeps them apart.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SilentFileName is the file's name inside the state directory.
const SilentFileName = "lsp-silent.json"

// SilentVersion is the format version the file carries.
const SilentVersion = 1

// SilentPath is the known-silent file of the project at root.
func SilentPath(root string) string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, SilentFileName)
	}
	return filepath.Join(root, ".ike", SilentFileName)
}

// silentKey normalises a project root: absolute, cleaned and with symlinks
// resolved where possible, so the app's workspace root and the settings
// page's working directory name the same project.
func silentKey(root string) string {
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	return filepath.Clean(root)
}

// SilentStore is the file's content: project root → known-silent server ids.
type SilentStore struct {
	Version  int                 `json:"version"`
	Projects map[string][]string `json:"projects"`
}

// LoadSilent reads path. A missing file is an empty store; a malformed one is
// an empty store plus the error (the caller decides; the app treats it as
// empty — the list is a convenience, and the next write replaces it).
func LoadSilent(path string) (*SilentStore, error) {
	s := &SilentStore{Version: SilentVersion, Projects: map[string][]string{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var in SilentStore
	if err := json.Unmarshal(data, &in); err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	if in.Version != SilentVersion {
		return s, fmt.Errorf("%s: unsupported version %d (this build writes %d)", path, in.Version, SilentVersion)
	}
	for root, ids := range in.Projects {
		for _, id := range ids {
			if id != "" {
				s.Mark(root, id)
			}
		}
	}
	return s, nil
}

// Has reports whether server id is known-silent in the project at root.
func (s *SilentStore) Has(root, id string) bool {
	for _, x := range s.Projects[silentKey(root)] {
		if x == id {
			return true
		}
	}
	return false
}

// IDs lists the project's known-silent server ids, sorted.
func (s *SilentStore) IDs(root string) []string {
	return append([]string(nil), s.Projects[silentKey(root)]...)
}

// Mark records server id as silent in the project; false when it already was.
func (s *SilentStore) Mark(root, id string) bool {
	if s.Has(root, id) {
		return false
	}
	if s.Projects == nil {
		s.Projects = map[string][]string{}
	}
	k := silentKey(root)
	ids := append(s.Projects[k], id)
	sort.Strings(ids)
	s.Projects[k] = ids
	return true
}

// Clear forgets server id for the project — every id when id is "" — and
// reports whether anything was removed.
func (s *SilentStore) Clear(root, id string) bool {
	k := silentKey(root)
	ids := s.Projects[k]
	var keep []string
	for _, x := range ids {
		if id != "" && x != id {
			keep = append(keep, x)
		}
	}
	if len(keep) == len(ids) {
		return false
	}
	if len(keep) == 0 {
		delete(s.Projects, k)
	} else {
		s.Projects[k] = keep
	}
	return true
}

// Save writes the store atomically (temporary file renamed over it); an empty
// store removes the file.
func (s *SilentStore) Save(path string) error {
	if len(s.Projects) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	s.Version = SilentVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+SilentFileName+".*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
	}
	return werr
}

// UpdateSilent loads path, applies fn and saves when fn reports a change —
// a read-modify-write, so two writers (the app and the settings page) never
// resurrect each other's removals from a stale copy. It returns the store as
// written.
func UpdateSilent(path string, fn func(*SilentStore) bool) (*SilentStore, error) {
	s, _ := LoadSilent(path)
	if !fn(s) {
		return s, nil
	}
	return s, s.Save(path)
}
