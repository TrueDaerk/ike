package sshconf

import (
	"os"
	"path/filepath"
	"testing"
)

// write drops content at dir/name and returns the full path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// aliases projects the parsed hosts onto their alias names, the picker's list.
func aliases(hosts []Host) []string {
	out := make([]string, len(hosts))
	for i, h := range hosts {
		out[i] = h.Alias
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A plain config lists its hosts in file order, wildcard patterns excluded.
func TestHostsFromBasic(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "config", `
# leading comment
Host *
  ForwardAgent yes

Host build01
  HostName build01.example.com
  User ci
  Port 2222

Host db-primary
	HostName 10.0.0.5

Host web-?
  User www

Host !secret jump
  HostName jump.example.com
`)
	hosts := HostsFrom(path)
	want := []string{"build01", "db-primary", "jump"}
	if got := aliases(hosts); !equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	if h := hosts[0]; h.HostName != "build01.example.com" || h.User != "ci" || h.Port != "2222" {
		t.Fatalf("build01 = %+v", h)
	}
	if got, want := hosts[0].Detail(), "ci@build01.example.com:2222"; got != want {
		t.Fatalf("detail = %q, want %q", got, want)
	}
	if got, want := hosts[1].Detail(), "10.0.0.5"; got != want {
		t.Fatalf("detail = %q, want %q", got, want)
	}
}

// One Host line may name several aliases; all of them are listed and share
// the block's keywords.
func TestHostsFromMultipleAliases(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "config", `
Host node1 node2 node3
  User ops
`)
	hosts := HostsFrom(path)
	if got, want := aliases(hosts), []string{"node1", "node2", "node3"}; !equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	for _, h := range hosts {
		if h.User != "ops" {
			t.Fatalf("%s user = %q, want ops", h.Alias, h.User)
		}
	}
}

// A Host line mixing a wildcard with real aliases keeps only the real ones.
func TestHostsFromWildcardMixedLine(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "config", "Host * gateway\n  User root\n")
	if got, want := aliases(HostsFrom(path)), []string{"gateway"}; !equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
}

// Include pulls in relative (resolved against the including file's directory),
// glob and absolute files, at the position of the directive.
func TestHostsFromInclude(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "conf.d/10-work", "Host work-a\n  HostName a.internal\n")
	write(t, dir, "conf.d/20-work", "Host work-b\n")
	other := write(t, filepath.Join(dir, "elsewhere"), "extra", "Host extra1\n")
	path := write(t, dir, "config", `
Host first

Include conf.d/*

Include `+other+`

Host last
`)
	want := []string{"first", "work-a", "work-b", "extra1", "last"}
	hosts := HostsFrom(path)
	if got := aliases(hosts); !equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	if hosts[1].HostName != "a.internal" {
		t.Fatalf("included keywords lost: %+v", hosts[1])
	}
}

// An include of the file itself (or a cycle) terminates instead of looping.
func TestHostsFromIncludeCycle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b", "Host beta\nInclude a\n")
	path := write(t, dir, "a", "Host alpha\nInclude b\n")
	if got, want := aliases(HostsFrom(path)), []string{"alpha", "beta"}; !equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
}

// A directory matched by an Include glob is skipped, not parsed.
func TestHostsFromIncludeDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "conf.d", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "conf.d/host", "Host inc\n")
	path := write(t, dir, "config", "Include conf.d/*\n")
	if got, want := aliases(HostsFrom(path)), []string{"inc"}; !equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
}

// Keywords may be written "Key=value"; a repeated alias keeps its first
// block's values, and a Match block's keywords reach no alias.
func TestHostsFromEqualsAndPrecedence(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "config", `
Host=api
HostName=api.example.com

Match host api
  User nobody

Host api
  HostName later.example.com
  User deploy
`)
	hosts := HostsFrom(path)
	if got, want := aliases(hosts), []string{"api"}; !equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
	if hosts[0].HostName != "api.example.com" {
		t.Fatalf("hostname = %q, want the first block's", hosts[0].HostName)
	}
	if hosts[0].User != "deploy" {
		t.Fatalf("user = %q, want deploy (filled by the later block)", hosts[0].User)
	}
}

// A missing or unreadable config is not an error — it yields no hosts.
func TestHostsFromMissing(t *testing.T) {
	if got := HostsFrom(filepath.Join(t.TempDir(), "nope")); got != nil {
		t.Fatalf("hosts = %v, want nil", got)
	}
	if got := HostsFrom(""); got != nil {
		t.Fatalf("hosts = %v, want nil", got)
	}
	if got := HostsFrom(t.TempDir()); got != nil {
		t.Fatalf("directory as config: hosts = %v, want nil", got)
	}
}

// Quoted aliases and inline comments parse like their bare forms.
func TestSplitLineQuotesAndComments(t *testing.T) {
	kw, args := splitLine(`  Host "quoted" bare # trailing`)
	if kw != "host" || !equal(args, []string{"quoted", "bare"}) {
		t.Fatalf("splitLine = %q %v", kw, args)
	}
	if kw, args := splitLine("   # only a comment"); kw != "" || args != nil {
		t.Fatalf("comment line = %q %v", kw, args)
	}
}
