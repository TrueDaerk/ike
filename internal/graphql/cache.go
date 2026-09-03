package graphql

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// cache.go stores introspected schemas per host under the project state
// directory (#2423), following the internal/httphistory convention: JSON
// files below `.ike/`, best-effort, readable in the editor. One file per host
// is the right grain — a schema belongs to an endpoint, and the same `.http`
// file often talks to several — and it is what lets the completion source in
// plugins/languages/http answer without any dispatch of its own.

// configDirEnv mirrors config.Discover's user-layer override, so a sandboxed
// IKE (tests, power users) caches inside the sandbox.
const configDirEnv = "IKE_CONFIG_DIR"

// CacheDir is the project-local directory holding cached schemas.
func CacheDir() string {
	base := ".ike"
	if d := os.Getenv(configDirEnv); d != "" {
		base = d
	}
	return filepath.Join(base, "graphql")
}

// Cache is a directory of cached schemas, one file per host.
type Cache struct{ Dir string }

// NewCache returns the cache under the project state directory.
func NewCache() *Cache { return &Cache{Dir: CacheDir()} }

// hostUnsafe matches everything a host may hold that a file name should not.
var hostUnsafe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// HostOf reduces a request target to the cache key: host plus port, lowercased.
// A target that is not a URL — or one still carrying an unresolved placeholder
// where the host goes — has no key, which is what makes the caller say so
// rather than cache a schema under a nonsense name.
func HostOf(target string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(target))
	if err != nil || u.Host == "" || strings.ContainsAny(u.Host, "{}$") {
		return "", false
	}
	return strings.ToLower(u.Host), true
}

// Path is the file a host's schema is cached in.
func (c *Cache) Path(host string) string {
	return filepath.Join(c.Dir, hostUnsafe.ReplaceAllString(strings.ToLower(host), "_")+".json")
}

// Store writes a schema, creating the directory when missing.
func (c *Cache) Store(host string, s *Schema) error {
	if host == "" {
		return fmt.Errorf("no host to cache the schema under")
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %v", c.Dir, err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := c.Path(host)
	forget(path) // a re-introspection must not be answered from the old parse
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Load reads a host's cached schema; a missing or unreadable file is a miss,
// not an error the caller has to distinguish.
//
// The parse is memoised per file (see loadMemo): the completion source calls
// this on every keystroke inside a query section, and a real schema is a
// hundreds-of-kilobytes JSON document — re-reading and re-parsing it per
// keystroke is exactly the kind of cost that never shows up in a benchmark and
// always shows up in the editor.
func (c *Cache) Load(host string) (*Schema, bool) {
	if host == "" {
		return nil, false
	}
	path := c.Path(host)
	info, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if s, ok := memoised(path, info); ok {
		return s, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var s Schema
	if err := json.Unmarshal(data, &s); err != nil || len(s.Types) == 0 {
		return nil, false
	}
	memoise(path, info, &s)
	return &s, true
}

// loadMemo caches the parse of one cache file, keyed by path and validated
// against the file's modification time *and* size — either changing means the
// file was rewritten. The stored *Schema is shared with every caller, so it is
// read-only by contract; nothing in this package mutates a loaded schema.
var loadMemo struct {
	mu sync.Mutex
	by map[string]memoEntry
}

type memoEntry struct {
	mod    time.Time
	size   int64
	schema *Schema
}

func memoised(path string, info os.FileInfo) (*Schema, bool) {
	loadMemo.mu.Lock()
	defer loadMemo.mu.Unlock()
	e, ok := loadMemo.by[path]
	if !ok || !e.mod.Equal(info.ModTime()) || e.size != info.Size() {
		return nil, false
	}
	return e.schema, true
}

func memoise(path string, info os.FileInfo, s *Schema) {
	loadMemo.mu.Lock()
	defer loadMemo.mu.Unlock()
	if loadMemo.by == nil {
		loadMemo.by = map[string]memoEntry{}
	}
	loadMemo.by[path] = memoEntry{mod: info.ModTime(), size: info.Size(), schema: s}
}

func forget(path string) {
	loadMemo.mu.Lock()
	defer loadMemo.mu.Unlock()
	delete(loadMemo.by, path)
}

// Hosts lists the hosts with a cached schema, sorted. The names are the
// sanitised file names rather than the original hosts — they are only used to
// answer "is there exactly one?" and to name the cache in a notice.
func (c *Cache) Hosts() []string {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(out)
	return out
}

// For resolves the schema a request target completes against: the target's own
// host, or — when the target is unusable (an unresolved `{{host}}`) and the
// project has cached exactly one schema — that one. The fallback is what keeps
// completion working in the common single-endpoint file, where the URL is a
// variable and the plugin cannot resolve it.
func (c *Cache) For(target string) (*Schema, bool) {
	if host, ok := HostOf(target); ok {
		if s, ok := c.Load(host); ok {
			return s, true
		}
	}
	if hosts := c.Hosts(); len(hosts) == 1 {
		return c.Load(hosts[0])
	}
	return nil, false
}
