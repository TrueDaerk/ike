package forge

// detect.go picks the backend for a repository by its origin remote host
// (#2083): github.com → the gh binding; any other host where the tea CLI has
// a matching login → the tea binding; neither → an explanatory setup message
// naming what is missing. A successful detection is cached per workspace
// root, so a refresh does not re-probe CLIs and logins; failures are not
// cached — installing the missing CLI and pressing refresh must recover.

import (
	"encoding/json"
	"net/url"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// detectCache holds one detected backend per workspace root (absolute path).
var (
	detectMu    sync.Mutex
	detectCache = map[string]Forge{}
)

// Detect resolves the backend for the repository containing dir. It returns
// the backend, or "" and a setup message describing the state the user fixes
// outside the pane (missing CLI, no matching remote or login).
func Detect(dir string) (Forge, string) {
	key, err := filepath.Abs(dir)
	if err != nil {
		key = dir
	}
	detectMu.Lock()
	cached := detectCache[key]
	detectMu.Unlock()
	if cached != nil {
		return cached, ""
	}
	f, setup := detect(dir)
	if f != nil {
		detectMu.Lock()
		detectCache[key] = f
		detectMu.Unlock()
	}
	return f, setup
}

// detect runs one uncached probe.
func detect(dir string) (Forge, string) {
	out, err := runGitQuick(dir, "remote", "get-url", "origin")
	if err != nil {
		return nil, "no git remote — the issues window needs a forge repository"
	}
	remote := strings.TrimSpace(string(out))
	host, owner, repo, ok := parseRemote(remote)
	if !ok {
		return nil, "cannot parse the origin remote (" + remote + ")"
	}
	if isGitHubHost(host) {
		if _, err := exec.LookPath("gh"); err != nil {
			return nil, "GitHub CLI (gh) not found — install it to browse issues"
		}
		return &ghForge{dir: dir}, ""
	}
	// Any non-GitHub host: a Gitea/Forgejo the tea CLI knows about.
	if _, err := exec.LookPath("tea"); err != nil {
		return nil, "origin is " + host + " and the tea CLI is not installed — install tea and add a login for it"
	}
	loginOut, err := runTea(dir, "logins", "list", "--output", "json")
	if err != nil {
		return nil, "tea logins list failed (" + err.Error() + ")"
	}
	logins, err := parseTeaLogins(loginOut)
	if err != nil {
		return nil, "cannot parse tea's login list (" + err.Error() + ")"
	}
	login := matchLogin(logins, host)
	if login == nil {
		return nil, "no tea login for " + host + " — run `tea login add` for it"
	}
	// A login authenticating by OAuth (tea ≥ 0.14) has no token: field in
	// config.yml — the access token sits in tea's own credential store. Such
	// a login is not broken: the binding routes its calls through `tea api`
	// instead, which opens and refreshes that store itself (#2118). Only a
	// tea too old for the `api` command leaves nothing to fall back on.
	token := teaToken(login.Name, login.URL)
	if token == "" && !teaSupportsAPI(dir) {
		return nil, "the tea login " + login.Name + " has no token in tea's config.yml " +
			"(an OAuth login keeps it in tea's credential store) and this tea has no `tea api` " +
			"command to read it — upgrade tea to 0.12 or newer, or add a token login with " +
			"`tea login add --token …`"
	}
	return &teaForge{dir: dir, baseURL: strings.TrimRight(login.URL, "/"), owner: owner, repo: repo,
		token: token, name: login.Name, user: login.User}, ""
}

// teaSupportsAPI reports whether this tea has the `api` passthrough command
// (tea 0.12+), the transport an OAuth login depends on. The probe stays
// local — `--help` is answered before any command action runs.
func teaSupportsAPI(dir string) bool {
	_, err := runTea(dir, "api", "--help")
	return err == nil
}

// ResetDetection drops the cached backend for dir (tests, or a changed
// remote).
func ResetDetection(dir string) {
	key, err := filepath.Abs(dir)
	if err != nil {
		key = dir
	}
	detectMu.Lock()
	delete(detectCache, key)
	detectMu.Unlock()
	// The persistent listing cache (#2108) keys itself to the same remote a
	// detection reads — a reset invalidates both memos together.
	dropCachedRemote(dir)
}

// isGitHubHost reports whether host is GitHub proper or a GitHub Enterprise
// host — anything the gh CLI serves.
func isGitHubHost(host string) bool {
	h := strings.ToLower(host)
	return h == "github.com" || strings.HasPrefix(h, "github.") || strings.Contains(h, ".github.")
}

// parseRemote splits one git remote URL into host and owner/repo. It handles
// the three shapes git uses: https://host/owner/repo(.git),
// ssh://git@host(:port)/owner/repo(.git), and the scp-like
// git@host:owner/repo(.git).
func parseRemote(remote string) (host, owner, repo string, ok bool) {
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

// teaLogin is one entry of `tea logins list --output json`.
type teaLogin struct {
	Name string
	URL  string
	User string
}

// parseTeaLogins decodes tea's login listing. tea renders JSON through its
// table layer, so key names follow the column headers; the parser accepts
// the spellings tea has used (login/name, url) and ignores the rest.
func parseTeaLogins(out []byte) ([]teaLogin, error) {
	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	str := func(m map[string]any, keys ...string) string {
		for _, k := range keys {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	logins := make([]teaLogin, 0, len(raw))
	for _, m := range raw {
		logins = append(logins, teaLogin{
			Name: str(m, "login", "name"),
			URL:  str(m, "url"),
			User: str(m, "user"),
		})
	}
	return logins, nil
}

// matchLogin finds the login whose URL points at host, or nil.
func matchLogin(logins []teaLogin, host string) *teaLogin {
	for i := range logins {
		u, err := url.Parse(logins[i].URL)
		if err != nil {
			continue
		}
		if strings.EqualFold(u.Hostname(), host) {
			return &logins[i]
		}
	}
	return nil
}

// runTea executes one tea command in dir with the network timeout, mirroring
// runGH.
func runTea(dir string, args ...string) ([]byte, error) {
	return runCLI(dir, "tea", ghTimeout, args...)
}
