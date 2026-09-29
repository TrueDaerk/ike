package jqplay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// LastProgramLimit caps the remembered per-source programs.
const LastProgramLimit = 200

// lastProgramVersion stamps the on-disk envelope; a file of another version
// reads as empty rather than being guessed at.
const lastProgramVersion = 1

// lastProgramFileName is the per-user file under the IKE config directory
// ($IKE_CONFIG_DIR, else ~/.ike).
const lastProgramFileName = "playground-last.json"

// LastProgramFile resolves the per-user last-program file: $IKE_CONFIG_DIR/
// playground-last.json when the override is set (tests, sandboxed runs),
// else ~/.ike/playground-last.json. An undiscoverable home yields "", which
// keeps the store in memory only, the same degradation HistoryFile chooses.
func LastProgramFile() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, lastProgramFileName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ike", lastProgramFileName)
}

// LastPrograms remembers, per queried source, the last program that ran
// against it without error — the persisted half of #1982: the session map on
// the app model covers every source (file, unsaved buffer, HTTP response) for
// the run's lifetime, this store covers only the file-backed ones (#2774) and
// survives a restart. The key is the caller's choice; the playground uses the
// dialect and the absolute path (see playDocKey in internal/app), so the same
// path under a different dialect keeps its own entry.
//
// Entries are LRU-capped at LastProgramLimit: Set moves the key to the front,
// and once the count exceeds the cap the oldest (least recently set) entries
// are dropped. Persistence follows the history store's trade-off (#1171):
// failing to read or write must never disrupt the playground, so errors are
// swallowed and a malformed file simply reads as empty.
type LastPrograms struct {
	order  []string
	items  map[string]string
	flags  map[string]string
	vars   map[string]string
	file   string
	loaded bool
}

// NewLastPrograms returns a store persisted to file; an empty file keeps it
// in memory only. The file is read lazily on first use.
func NewLastPrograms(file string) *LastPrograms { return &LastPrograms{file: file} }

// lastProgramEntry is one on-disk record.
type lastProgramEntry struct {
	Key     string `json:"key"`
	Program string `json:"program"`
	// Flags are the run toggles (#2784) the program last ran with, spelled
	// as Options.Flags; absent in files written before they existed.
	Flags string `json:"flags,omitempty"`
	// Vars is the variables line (#2786) the program last ran with; absent
	// when there was none.
	Vars string `json:"vars,omitempty"`
}

// lastProgramEnvelope is the on-disk schema, newest first.
type lastProgramEnvelope struct {
	Version int                `json:"version"`
	Entries []lastProgramEntry `json:"entries"`
}

// ensure loads the file once, adding entries the memory map does not already
// hold (an entry set before the first load is newer than anything on disk).
// Anything malformed reads as empty.
func (l *LastPrograms) ensure() {
	if l.loaded || l.file == "" {
		return
	}
	l.loaded = true
	data, err := os.ReadFile(l.file)
	if err != nil {
		return
	}
	var env lastProgramEnvelope
	if json.Unmarshal(data, &env) != nil || env.Version != lastProgramVersion {
		return
	}
	if l.items == nil {
		l.items = map[string]string{}
	}
	for _, e := range env.Entries {
		key := strings.TrimSpace(e.Key)
		program := strings.TrimSpace(e.Program)
		if key == "" || program == "" {
			continue
		}
		if _, ok := l.items[key]; ok || len(l.order) >= LastProgramLimit {
			continue
		}
		l.items[key] = program
		if e.Flags != "" {
			l.flagMap()[key] = e.Flags
		}
		if e.Vars != "" {
			l.varMap()[key] = e.Vars
		}
		l.order = append(l.order, key)
	}
}

// save writes the store; errors are swallowed (see the type comment).
func (l *LastPrograms) save() {
	if l.file == "" {
		return
	}
	env := lastProgramEnvelope{Version: lastProgramVersion, Entries: make([]lastProgramEntry, 0, len(l.order))}
	for _, key := range l.order {
		env.Entries = append(env.Entries, lastProgramEntry{Key: key, Program: l.items[key], Flags: l.flags[key], Vars: l.vars[key]})
	}
	data, err := json.Marshal(env)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(l.file), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(l.file, data, 0o644)
}

// Get returns the program last recorded for key, if any.
func (l *LastPrograms) Get(key string) (string, bool) {
	l.ensure()
	p, ok := l.items[key]
	return p, ok
}

// Flags returns the run toggles (#2784) key's last program ran with, "" when
// none were on or nothing is remembered.
func (l *LastPrograms) Flags(key string) string {
	l.ensure()
	return l.flags[key]
}

// Options returns everything key's last program ran with: the toggles
// (#2784) and the variables line (#2786). The zero value when nothing is
// remembered.
func (l *LastPrograms) Options(key string) Options {
	l.ensure()
	o := ParseFlags(l.flags[key])
	o.Vars = l.vars[key]
	return o
}

// flagMap is the toggle map, allocated on first write.
func (l *LastPrograms) flagMap() map[string]string {
	if l.flags == nil {
		l.flags = map[string]string{}
	}
	return l.flags
}

// varMap is the variables-line map, allocated on first write.
func (l *LastPrograms) varMap() map[string]string {
	if l.vars == nil {
		l.vars = map[string]string{}
	}
	return l.vars
}

// Set records program as key's last program, moving it to the front of the
// LRU order, and persists the store when a file is attached. An empty key or
// program is ignored.
func (l *LastPrograms) Set(key, program string) { l.SetWithOptions(key, program, Options{}) }

// SetWithFlags is Set that also records the run toggles the program ran with
// (#2784), spelled as Options.Flags.
func (l *LastPrograms) SetWithFlags(key, program, flags string) {
	l.SetWithOptions(key, program, ParseFlags(flags))
}

// SetWithOptions is Set that also records what the program ran with: the
// toggles (#2784) and the variables line (#2786).
func (l *LastPrograms) SetWithOptions(key, program string, opts Options) {
	flags, vars := opts.Flags(), strings.TrimSpace(opts.Vars)
	key = strings.TrimSpace(key)
	program = strings.TrimSpace(program)
	if key == "" || program == "" {
		return
	}
	l.ensure()
	if l.items == nil {
		l.items = map[string]string{}
	}
	for i, k := range l.order {
		if k == key {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	l.order = append([]string{key}, l.order...)
	l.items[key] = program
	if flags != "" {
		l.flagMap()[key] = flags
	} else {
		delete(l.flags, key)
	}
	if vars != "" {
		l.varMap()[key] = vars
	} else {
		delete(l.vars, key)
	}
	for len(l.order) > LastProgramLimit {
		last := l.order[len(l.order)-1]
		l.order = l.order[:len(l.order)-1]
		delete(l.items, last)
		delete(l.flags, last)
		delete(l.vars, last)
	}
	l.save()
}

// Len reports how many sources are remembered.
func (l *LastPrograms) Len() int {
	l.ensure()
	return len(l.order)
}
