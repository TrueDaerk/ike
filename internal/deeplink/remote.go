package deeplink

import (
	"net/url"
	"strings"
)

// remote.go canonicalises git remotes. Every spelling of the same repository —
// https://github.com/A/B.git, git@github.com:a/b, ssh://git@github.com/a/b —
// normalises to one key ("github.com/a/b"), so a link and a local clone
// compare equal however either was written down.

// ParseRemote splits one git remote URL into host and owner/repo. It handles
// the three shapes git uses: https://host/owner/repo(.git),
// ssh://git@host(:port)/owner/repo(.git), and the scp-like
// git@host:owner/repo(.git). It is the one implementation; internal/forge
// delegates here (#2396 moved it out of forge so the link parser stays a leaf).
func ParseRemote(remote string) (host, owner, repo string, ok bool) {
	var path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return "", "", "", false
		}
		host = u.Hostname()
		path = strings.Trim(u.Path, "/")
	} else if at := strings.Index(remote, "@"); at >= 0 && strings.Contains(remote[at:], ":") {
		rest := remote[at+1:]
		colon := strings.Index(rest, ":")
		host = rest[:colon]
		path = strings.Trim(rest[colon+1:], "/")
	} else {
		return "", "", "", false
	}
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if host == "" || len(parts) < 2 || parts[len(parts)-2] == "" || parts[len(parts)-1] == "" {
		return "", "", "", false
	}
	// Keep the last two segments: Gitea can serve under a path prefix.
	return host, parts[len(parts)-2], parts[len(parts)-1], true
}

// NormalizeRemote reduces any spelling of a git remote to the canonical
// comparison key "host/owner/repo" — lower-case, no .git suffix. Hosts and the
// big forges' repo paths are case-insensitive in practice; a false ok means
// the string is not recognisably a git remote.
func NormalizeRemote(remote string) (key string, ok bool) {
	host, owner, repo, ok := ParseRemote(strings.TrimSpace(remote))
	if !ok {
		return "", false
	}
	return strings.ToLower(host + "/" + owner + "/" + repo), true
}
