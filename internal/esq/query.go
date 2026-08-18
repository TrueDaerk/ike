package esq

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// query.go owns the console's query buffers. A query buffer is an ordinary
// JSON file under the user state dir (the scratch-file philosophy,
// wiki/architecture/scratch-files.md): one file per endpoint+index at
// <state>/es/<endpoint>/<index>.es.json. Being a real file buys the full
// editor, JSON highlighting, folding, session restore and the completion
// engine for free — no special buffer type. This package is the single owner
// of the naming; the app never assembles query paths itself.

// configDirEnv mirrors config.Discover's user-layer override, so a sandboxed
// IKE (tests, power users) keeps its query buffers in the sandbox too.
const configDirEnv = "IKE_CONFIG_DIR"

// QueryExt is the query-buffer suffix. The double extension keeps the file an
// ordinary .json for highlighting while staying recognizable as a console
// buffer for the completion source's exclusive claim.
const QueryExt = ".es.json"

// QueryTemplate seeds a fresh query buffer with a runnable match-all body.
const QueryTemplate = "{\n  \"query\": {\n    \"match_all\": {}\n  }\n}\n"

// queryDir resolves the query-buffer root: $IKE_CONFIG_DIR/es when the
// override is set, else ~/.ike/es.
func queryDir() (string, error) {
	if d := os.Getenv(configDirEnv); d != "" {
		return filepath.Join(d, "es"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving es query dir: %w", err)
	}
	return filepath.Join(home, ".ike", "es"), nil
}

// slug makes one path segment file-safe. Elasticsearch index names are
// already nearly filename-safe; endpoint names are user text, so anything
// outside a conservative set collapses to a dash.
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "-"
	}
	return b.String()
}

// QueryPath allocates (creating directory and file as needed) the query
// buffer for endpoint+index and returns its absolute path. A fresh file is
// seeded with QueryTemplate so it is runnable as created. The mapping
// path→(endpoint, index) is remembered for QueryRef — slugging is lossy, so
// the file name alone cannot be trusted to round-trip.
func QueryPath(endpoint, index string) (string, error) {
	root, err := queryDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, slug(endpoint))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating es query dir: %w", err)
	}
	path := filepath.Join(dir, slug(index)+QueryExt)
	// O_EXCL keeps allocation race-free; an existing buffer is reused as-is.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		_, werr := f.WriteString(QueryTemplate)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", fmt.Errorf("seeding es query file: %w", werr)
		}
	} else if !os.IsExist(err) {
		return "", fmt.Errorf("creating es query file: %w", err)
	}
	registerQuery(path, endpoint, index)
	return path, nil
}

// IsQueryPath reports whether path is a console query buffer. The suffix
// alone decides — the claim must also cover files restored by the session
// before any console pane touched them this run.
func IsQueryPath(path string) bool {
	return strings.HasSuffix(path, QueryExt)
}

var (
	refMu sync.Mutex
	refs  = map[string]queryRef{}
)

type queryRef struct{ endpoint, index string }

func registerQuery(path, endpoint, index string) {
	refMu.Lock()
	defer refMu.Unlock()
	refs[path] = queryRef{endpoint: endpoint, index: index}
}

// QueryRef resolves a query buffer back to its endpoint and index: the
// remembered registration when the path was allocated this run, else the
// directory layout (<endpoint-slug>/<index-slug>.es.json) — good enough for
// completion after a session restore, since slugging is the identity for
// typical endpoint and index names.
func QueryRef(path string) (endpoint, index string, ok bool) {
	if !IsQueryPath(path) {
		return "", "", false
	}
	refMu.Lock()
	r, hit := refs[path]
	refMu.Unlock()
	if hit {
		return r.endpoint, r.index, true
	}
	index = strings.TrimSuffix(filepath.Base(path), QueryExt)
	endpoint = filepath.Base(filepath.Dir(path))
	if endpoint == "" || index == "" {
		return "", "", false
	}
	return endpoint, index, true
}
