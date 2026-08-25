package langhttp

// envselect.go picks the environment a "{{" completion (complete.go, #2135)
// resolves its variable names against. internal/app/http_env.go persists the
// user's choice per directory in .ike/httpenv.json (IKE_CONFIG_DIR
// redirects it, like every other per-project store); a language plugin has
// no business importing the application package just to read one small JSON
// file, so this reads the same store directly, mirroring the read-only-store
// pattern every other subsystem under internal/ already follows for its own
// IKE_CONFIG_DIR-scoped file.

import (
	"encoding/json"
	"os"
	"path/filepath"

	"ike/internal/httpfile"
)

// httpEnvStorePath is internal/app's httpEnvFile, duplicated rather than
// imported.
func httpEnvStorePath() string {
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "httpenv.json")
	}
	return filepath.Join(".ike", "httpenv.json")
}

// selectedHTTPEnv reads the persisted environment selection for dir, "" when
// the store is missing, malformed, or has no entry for dir — completion then
// falls back the same way dispatch does.
func selectedHTTPEnv(dir string) string {
	data, err := os.ReadFile(httpEnvStorePath())
	if err != nil {
		return ""
	}
	var store struct {
		Selected map[string]string `json:"selected"`
	}
	if json.Unmarshal(data, &store) != nil {
		return ""
	}
	return store.Selected[canonicalDir(dir)]
}

// canonicalDir mirrors internal/app's canonicalPath: the cleaned absolute
// form, so a selection saved for one spelling of a directory is found under
// another.
func canonicalDir(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

// activeEnvironment picks the environment name a placeholder completion
// pulls variables from: the persisted selection while it still names a real
// environment, else the only one when the file defines just one (no reason
// to make completion ambiguous over a choice the user was never asked to
// make) — mirroring httpEnvName in internal/app/http_env.go.
func activeEnvironment(dir string, envs *httpfile.Environments) string {
	if sel := selectedHTTPEnv(dir); sel != "" && envs.Has(sel) {
		return sel
	}
	if envs.Len() == 1 {
		return envs.Names()[0]
	}
	return ""
}
