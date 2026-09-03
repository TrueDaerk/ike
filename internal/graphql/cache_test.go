package graphql

import (
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	if err := c.Store("example.com:8080", fixtureSchema()); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, ok := c.Load("example.com:8080")
	if !ok {
		t.Fatal("load missed the schema just stored")
	}
	if got.QueryType != "Query" || len(got.Types) != len(fixtureSchema().Types) {
		t.Errorf("loaded schema differs: %+v", got)
	}
	// The port belongs to the key — two services on one host are two schemas.
	if _, ok := c.Load("example.com"); ok {
		t.Error("a bare host loaded the schema of a host:port")
	}
	if want := filepath.Join(c.Dir, "example.com_8080.json"); c.Path("example.com:8080") != want {
		t.Errorf("path = %q, want %q", c.Path("example.com:8080"), want)
	}
}

func TestCacheLoadMissIsNotAnError(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	if _, ok := c.Load("nobody.invalid"); ok {
		t.Error("an empty cache answered a load")
	}
	if hosts := c.Hosts(); len(hosts) != 0 {
		t.Errorf("hosts = %v, want none", hosts)
	}
}

func TestHostOf(t *testing.T) {
	tests := []struct {
		target string
		host   string
		ok     bool
	}{
		{"https://Example.com/graphql", "example.com", true},
		{"http://localhost:4000/graphql", "localhost:4000", true},
		// An unresolved placeholder is not a host to cache under.
		{"{{host}}/graphql", "", false},
		{"https://${HOST}/graphql", "", false},
		{"/graphql", "", false},
	}
	for _, tc := range tests {
		host, ok := HostOf(tc.target)
		if host != tc.host || ok != tc.ok {
			t.Errorf("HostOf(%q) = (%q, %v), want (%q, %v)", tc.target, host, ok, tc.host, tc.ok)
		}
	}
}

// A request file usually talks to one endpoint through a variable, so the
// target cannot be resolved by the completion source. A single cached schema
// then answers anyway; two or more are ambiguous and answer nothing.
func TestCacheForFallsBackToTheSingleSchema(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	if err := c.Store("example.com", fixtureSchema()); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, ok := c.For("{{host}}/graphql"); !ok {
		t.Error("the only cached schema did not answer an unresolvable target")
	}
	if err := c.Store("other.test", fixtureSchema()); err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, ok := c.For("{{host}}/graphql"); ok {
		t.Error("an ambiguous cache answered an unresolvable target")
	}
	// A resolvable target still finds its own schema.
	if _, ok := c.For("https://other.test/graphql"); !ok {
		t.Error("a resolvable target missed its schema")
	}
}

// The completion source loads on every keystroke, so the parse is memoised —
// but a re-introspection must be seen at once, not after the memo happens to
// expire.
func TestCacheLoadMemoisesButFollowsAStore(t *testing.T) {
	c := &Cache{Dir: t.TempDir()}
	if err := c.Store("example.com", fixtureSchema()); err != nil {
		t.Fatalf("store: %v", err)
	}
	first, _ := c.Load("example.com")
	second, _ := c.Load("example.com")
	if first != second {
		t.Error("a second load re-parsed the file instead of reusing the memo")
	}

	changed := fixtureSchema()
	changed.Types = append(changed.Types, Type{Name: "Droid", Kind: KindObject})
	if err := c.Store("example.com", changed); err != nil {
		t.Fatalf("store: %v", err)
	}
	after, ok := c.Load("example.com")
	if !ok {
		t.Fatal("load missed the rewritten schema")
	}
	if _, ok := after.TypeByName("Droid"); !ok {
		t.Error("the memo answered with the schema from before the store")
	}
}

func TestCacheDirHonoursTheStateDirOverride(t *testing.T) {
	t.Setenv(configDirEnv, "/tmp/ike-state")
	if got, want := CacheDir(), filepath.Join("/tmp/ike-state", "graphql"); got != want {
		t.Errorf("CacheDir = %q, want %q", got, want)
	}
}
