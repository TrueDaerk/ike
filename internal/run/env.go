package run

import (
	"errors"
	"sort"
	"strings"
)

// env.go is the editable form of a configuration's environment overlay
// (#2173): the run-configuration form edits `Env` as an ordered list of
// key/value rows, so the validation rules live next to the store instead of
// in the dialog. Rows are the editor's shape; the map is what persists.

// EnvRow is one key/value row of the environment editor.
type EnvRow struct {
	Key   string
	Value string
}

// EnvRows renders an environment map as sorted rows — the deterministic
// order the editor opens with (EnvSlice's order, so the form and the spawned
// process agree on what "the environment" looks like).
func EnvRows(env map[string]string) []EnvRow {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([]EnvRow, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, EnvRow{Key: k, Value: env[k]})
	}
	return rows
}

// ValidateEnvKey reports why key cannot name an environment variable: empty
// (after trimming), or carrying a character the KEY=VALUE spelling cannot
// survive. The message is user-facing — the form shows it verbatim.
func ValidateEnvKey(key string) error {
	k := strings.TrimSpace(key)
	if k == "" {
		return errors.New("environment key must not be empty")
	}
	if strings.Contains(k, "=") {
		return errors.New("environment key must not contain \"=\"")
	}
	if strings.ContainsAny(k, " \t\r\n\x00") {
		return errors.New("environment key must not contain whitespace")
	}
	return nil
}

// EnvMap validates rows and converts them back into the stored map: keys are
// trimmed, every key is checked with ValidateEnvKey, and a duplicate key is
// rejected by name rather than silently overwriting the earlier row. Values
// are taken verbatim (an empty value is a legitimate empty variable).
func EnvMap(rows []EnvRow) (map[string]string, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if err := ValidateEnvKey(r.Key); err != nil {
			return nil, err
		}
		k := strings.TrimSpace(r.Key)
		if _, dup := out[k]; dup {
			return nil, errors.New("duplicate environment key \"" + k + "\"")
		}
		out[k] = r.Value
	}
	return out, nil
}

// SetEnv validates rows and stores them as the configuration's environment
// overlay; an invalid set leaves the configuration untouched. An empty set
// clears the overlay (so the config serializes without an `env` member).
func (c *Config) SetEnv(rows []EnvRow) error {
	env, err := EnvMap(rows)
	if err != nil {
		return err
	}
	c.Env = env
	return nil
}
