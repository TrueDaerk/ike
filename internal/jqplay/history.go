package jqplay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// HistoryLimit caps the program history.
const HistoryLimit = 50

// historyVersion stamps the on-disk envelope; a file of another version reads
// as empty rather than being guessed at.
const historyVersion = 1

// historyFileName is the per-user history file under the IKE config
// directory ($IKE_CONFIG_DIR, else ~/.ike).
const historyFileName = "playground-history.json"

// HistoryFile resolves the per-user history file: $IKE_CONFIG_DIR/
// playground-history.json when the override is set (tests, sandboxed runs),
// else ~/.ike/playground-history.json. An undiscoverable home yields "", which
// keeps the history in memory only — the same degradation the scratch
// directory and the filter library choose over scattering files into a
// relative path.
//
// The file is *user* state, not project state, on purpose (#2536): the
// history answers "what did I run recently, anywhere", and the argument
// against persisting it — every half-typed experiment landing in the
// project's `.ike` — was about the project. A per-user list shared across
// projects is where "anywhere" already pointed.
func HistoryFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, historyFileName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", historyFileName)
}

// History is the one list of programs the playground evaluated, newest
// first. Its lifetime is the *user*, not the session, the buffer, the
// dialect or the source snapshot: every open playground writes into the
// root model's single instance (#1977), a yq or xmq program is a jq program
// here (#2039), an HTTP response is a source like any file, and an external
// overwrite of the source file renews the input without touching the list
// (#2356). With a file attached (NewHistory / HistoryFile) every Add is
// persisted, so the list also survives a restart and is shared by every IKE
// process of the same user (#2536). The zero value is a memory-only list —
// tests and hand-assembled models never write a file by accident.
//
// Persistence follows the histories store's trade-off (#1171): failing to
// read or write must never disrupt the playground, so errors are swallowed and
// a malformed file simply reads as empty.
type History struct {
	items  []string
	file   string
	loaded bool
}

// NewHistory returns a history persisted to file; an empty file keeps it in
// memory only. The file is read lazily on first use.
func NewHistory(file string) *History { return &History{file: file} }

// historyEnvelope is the on-disk schema.
type historyEnvelope struct {
	Version  int      `json:"version"`
	Programs []string `json:"programs"`
}

// ensure loads the file once, appending its entries after those already
// recorded in memory (a program run before the first load is newer than
// anything on disk). Anything malformed reads as empty.
func (h *History) ensure() {
	if h.loaded || h.file == "" {
		return
	}
	h.loaded = true
	data, err := os.ReadFile(h.file)
	if err != nil {
		return
	}
	var env historyEnvelope
	if json.Unmarshal(data, &env) != nil || env.Version != historyVersion {
		return
	}
	seen := make(map[string]bool, len(h.items))
	for _, p := range h.items {
		seen[p] = true
	}
	for _, p := range env.Programs {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] || len(h.items) >= HistoryLimit {
			continue
		}
		seen[p] = true
		h.items = append(h.items, p)
	}
}

// save writes the list; errors are swallowed (see the type comment).
func (h *History) save() {
	if h.file == "" {
		return
	}
	data, err := json.Marshal(historyEnvelope{Version: historyVersion, Programs: h.items})
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.file), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(h.file, data, 0o644)
}

// Add records program as the newest entry, moving a repeat to the front
// instead of duplicating it, and persists the list when a file is attached.
// Empty programs are ignored.
func (h *History) Add(program string) {
	program = strings.TrimSpace(program)
	if program == "" {
		return
	}
	h.ensure()
	for i, p := range h.items {
		if p == program {
			h.items = append(h.items[:i], h.items[i+1:]...)
			break
		}
	}
	h.items = append([]string{program}, h.items...)
	if len(h.items) > HistoryLimit {
		h.items = h.items[:HistoryLimit]
	}
	h.save()
}

// Len reports how many programs are remembered.
func (h *History) Len() int {
	h.ensure()
	return len(h.items)
}

// At returns the i-th newest program; ok is false when i is out of range.
func (h *History) At(i int) (string, bool) {
	h.ensure()
	if i < 0 || i >= len(h.items) {
		return "", false
	}
	return h.items[i], true
}
