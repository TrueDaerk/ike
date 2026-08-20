package remote

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestCachePathMirrorsRemoteTree guards the cache mapping: one directory per
// alias, the remote path mirrored underneath, the base name preserved so
// language lookup and viewer sniffs resolve from the real file name.
func TestCachePathMirrorsRemoteTree(t *testing.T) {
	got := CachePath("/cache", "web01", "/var/log/app.log")
	want := filepath.Join("/cache", "web01", "var", "log", "app.log")
	if got != want {
		t.Fatalf("CachePath = %q, want %q", got, want)
	}
}

// TestCachePathNeverEscapesRoot guards the sanitizer: "..", relative paths
// and separator-bearing aliases must stay inside the cache root.
func TestCachePathNeverEscapesRoot(t *testing.T) {
	cases := []struct{ alias, remote string }{
		{"web01", "/../../etc/passwd"},
		{"web01", "../relative"},
		{"a/b", "/x"},
		{"..", "/x"},
		{"web01", "/weird/../../../x"},
	}
	for _, c := range cases {
		got := CachePath("/cache", c.alias, c.remote)
		if !strings.HasPrefix(got, "/cache"+string(filepath.Separator)) {
			t.Fatalf("CachePath(%q, %q) = %q escapes the root", c.alias, c.remote, got)
		}
		if strings.Contains(got, "..") {
			t.Fatalf("CachePath(%q, %q) = %q keeps a dot-dot segment", c.alias, c.remote, got)
		}
	}
}

// TestVPathRoundTrip guards the virtual-path encoding both ways.
func TestVPathRoundTrip(t *testing.T) {
	v := VPath("web01", "/var/log/app.log")
	if v != "sftp://web01/var/log/app.log" {
		t.Fatalf("VPath = %q", v)
	}
	alias, p, ok := ParseVPath(v)
	if !ok || alias != "web01" || p != "/var/log/app.log" {
		t.Fatalf("ParseVPath(%q) = %q, %q, %v", v, alias, p, ok)
	}
}

// TestParseVPathRejectsForeignPaths guards the decoder against paths that are
// not ours: plain files, archive members, malformed prefixes.
func TestParseVPathRejectsForeignPaths(t *testing.T) {
	for _, v := range []string{"/var/log/app.log", "src.tar!main.go", "sftp://", "sftp://hostonly", ""} {
		if _, _, ok := ParseVPath(v); ok {
			t.Fatalf("ParseVPath(%q) accepted a foreign path", v)
		}
	}
}
