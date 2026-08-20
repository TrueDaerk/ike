package remote

// cache.go maps a (host alias, remote path) pair onto the local download
// cache. The mapping mirrors the remote tree under one directory per alias,
// so the cached copy keeps its own base name — the tab title, the language
// lookup and every viewer's format sniff resolve from the name they would see
// on the host. Every segment is sanitized: the cache must stay inside its
// root whatever the remote side calls its entries.

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// VPathPrefix marks a remote buffer's virtual path: "sftp://<alias><path>".
// Like an archive member's "<archive>!<entry>" it names nothing on disk —
// which is exactly why the buffer is read-only — while its tail is the remote
// file's own name, so titles and highlighting resolve without special casing.
const VPathPrefix = "sftp://"

// VPath renders the virtual path of one remote file.
func VPath(alias, remotePath string) string {
	return VPathPrefix + alias + path.Clean("/"+remotePath)
}

// ParseVPath splits a virtual path back into alias and remote path; ok=false
// for anything that is not one of ours.
func ParseVPath(vpath string) (alias, remotePath string, ok bool) {
	rest, found := strings.CutPrefix(vpath, VPathPrefix)
	if !found {
		return "", "", false
	}
	i := strings.Index(rest, "/")
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i:], true
}

// DefaultCacheRoot is the download cache location: the user cache dir, with
// the temp dir as the degraded fallback so a missing HOME still browses.
func DefaultCacheRoot() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "ike", "sftp")
}

// CachePath maps one remote file into the cache under root: one directory per
// alias, then the remote path mirrored segment by segment. The remote path is
// cleaned as an absolute slash path first, so ".." never climbs, and each
// segment (the alias included) is sanitized for the local filesystem.
func CachePath(root, alias, remotePath string) string {
	out := filepath.Join(root, sanitizeSegment(alias))
	clean := path.Clean("/" + remotePath)
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" {
			continue
		}
		out = filepath.Join(out, sanitizeSegment(seg))
	}
	return out
}

// sanitizeSegment makes one path segment safe as a local directory entry:
// separators and NULs are replaced, and the two relative names that would
// re-route the join are neutralized.
func sanitizeSegment(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0:
			return '_'
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}
