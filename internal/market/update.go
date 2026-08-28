package market

// update.go is the marketplace's update-detection half (#2257): diff the
// installed sidecars against the catalog, and remember when the last check
// ran so the automatic check on IDE start stays rate-limited to at most once
// per interval. The package stays bubbletea-free — the app adapts these calls
// into tea.Cmds and the marketplace page renders the result.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultCheckInterval is how often the automatic update check may run. The
// catalog is a static document that changes rarely; once a day is plenty and
// keeps a restart loop from hammering the host.
const DefaultCheckInterval = 24 * time.Hour

// Update is one installed plugin whose catalog entry is newer.
type Update struct {
	// Entry is the catalog entry to install; From is the installed version.
	Entry Entry
	From  Version
	// AddedCapabilities are capabilities the new version requests that the
	// installed manifest does not pin. A non-empty list is the trust-model
	// gate: the update is never applied without explicit per-plugin
	// confirmation, and never by update-all.
	AddedCapabilities []string
}

// Name is the plugin identity the update applies to.
func (u Update) Name() string { return u.Entry.Name }

// To is the catalog version the update installs.
func (u Update) To() Version { return u.Entry.ParsedVersion() }

// NeedsConfirm reports whether the update grows the capability list, which
// makes it a re-review rather than a routine version bump.
func (u Update) NeedsConfirm() bool { return len(u.AddedCapabilities) > 0 }

// FindUpdates returns one Update per installed plugin whose catalog entry is
// newer, sorted by name. Plugins that are not installed, or installed without
// a parsable manifest version, are skipped (see UpdateAvailable).
func FindUpdates(idx Index, installed map[string]Installed) []Update {
	var out []Update
	for _, e := range idx.Plugins {
		inst, ok := installed[e.Name]
		if !ok || !UpdateAvailable(e, inst) {
			continue
		}
		out = append(out, Update{
			Entry:             e,
			From:              inst.Version,
			AddedCapabilities: AddedCapabilities(e, inst),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// AddedCapabilities lists, in catalog order, the capabilities entry requests
// beyond the ones the installed manifest already pins. Capabilities that were
// dropped do not matter — a shorter list can only reduce what the runtime
// allows, so it needs no review.
func AddedCapabilities(entry Entry, inst Installed) []string {
	have := map[string]bool{}
	for _, c := range inst.Capabilities {
		have[c] = true
	}
	var added []string
	for _, c := range entry.Capabilities {
		if !have[c] {
			added = append(added, c)
		}
	}
	return added
}

// CheckState is the persisted rate-limit state of the automatic check.
type CheckState struct {
	// LastCheck is when the last catalog fetch for update detection finished.
	// A failed fetch does not update it — a network outage must not silence
	// the check until tomorrow.
	LastCheck time.Time `json:"last_check"`
}

// StatePath is where the check state lives: next to the plugins directory in
// the user's IKE state directory (IKE_CONFIG_DIR redirects it, as everywhere).
// An empty string means "no home directory" — the caller then skips
// persistence and simply checks once per session.
func StatePath() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "marketplace-check.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", "marketplace-check.json")
}

// LoadCheckState reads the state file. Anything unreadable or malformed
// yields the zero state, which is simply "never checked" — losing this file
// costs one extra catalog fetch, so it is never an error worth reporting.
func LoadCheckState(path string) CheckState {
	if path == "" {
		return CheckState{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CheckState{}
	}
	var s CheckState
	if err := json.Unmarshal(data, &s); err != nil {
		return CheckState{}
	}
	return s
}

// SaveCheckState writes the state file atomically.
func SaveCheckState(path string, s CheckState) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("marketplace state: %w", err)
	}
	if err := atomicWrite(path, data); err != nil {
		return fmt.Errorf("marketplace state: %w", err)
	}
	return nil
}

// Due reports whether an automatic check may run at now. A state written in
// the future (clock moved backwards) is treated as due rather than blocking
// the check until the clock catches up.
func (s CheckState) Due(now time.Time, interval time.Duration) bool {
	if s.LastCheck.IsZero() || now.Before(s.LastCheck) {
		return true
	}
	return now.Sub(s.LastCheck) >= interval
}
