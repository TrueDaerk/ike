package forge

import (
	"strings"
	"testing"
)

// detect_test.go covers backend detection (#2083): remote URL parsing across
// the three git URL shapes, GitHub host classification, tea login matching,
// and the token lookup in tea's config document.

func TestParseRemote(t *testing.T) {
	cases := []struct {
		remote            string
		host, owner, repo string
		ok                bool
	}{
		{"https://github.com/TrueDaerk/ike.git", "github.com", "TrueDaerk", "ike", true},
		{"https://github.com/TrueDaerk/ike", "github.com", "TrueDaerk", "ike", true},
		{"git@github.com:TrueDaerk/ike.git", "github.com", "TrueDaerk", "ike", true},
		{"ssh://git@gitea.example.com:2222/org/repo.git", "gitea.example.com", "org", "repo", true},
		{"https://code.example.com/gitea/org/repo.git", "code.example.com", "org", "repo", true},
		{"https://gitea.example.com/onlyowner", "", "", "", false},
		{"/local/path/repo.git", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		host, owner, repo, ok := parseRemote(c.remote)
		if host != c.host || owner != c.owner || repo != c.repo || ok != c.ok {
			t.Errorf("parseRemote(%q) = (%q, %q, %q, %v), want (%q, %q, %q, %v)",
				c.remote, host, owner, repo, ok, c.host, c.owner, c.repo, c.ok)
		}
	}
}

func TestIsGitHubHost(t *testing.T) {
	for host, want := range map[string]bool{
		"github.com":         true,
		"GitHub.com":         true,
		"github.example.com": true,
		"gitea.example.com":  false,
		"codeberg.org":       false,
	} {
		if got := isGitHubHost(host); got != want {
			t.Errorf("isGitHubHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// teaLoginsFixture is one `tea logins list --output json` document.
const teaLoginsFixture = `[
  {"login": "codeberg", "url": "https://codeberg.org", "user": "someone", "default": "true", "ssh_host": "codeberg.org"},
  {"name": "internal", "url": "https://gitea.example.com/", "user": "dev"}
]`

func TestParseTeaLoginsAndMatch(t *testing.T) {
	logins, err := parseTeaLogins([]byte(teaLoginsFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(logins) != 2 {
		t.Fatalf("logins = %d, want 2", len(logins))
	}
	if logins[0].Name != "codeberg" || logins[0].URL != "https://codeberg.org" {
		t.Fatalf("login = %+v", logins[0])
	}
	if logins[1].Name != "internal" {
		t.Fatalf("the name key spelling must also parse: %+v", logins[1])
	}
	if l := matchLogin(logins, "gitea.example.com"); l == nil || l.Name != "internal" {
		t.Fatalf("matchLogin = %+v", l)
	}
	if l := matchLogin(logins, "Codeberg.org"); l == nil || l.Name != "codeberg" {
		t.Fatalf("matching must ignore case: %+v", l)
	}
	if l := matchLogin(logins, "unknown.example.com"); l != nil {
		t.Fatalf("unknown host matched %+v", l)
	}
}

func TestParseTeaLoginsBadJSON(t *testing.T) {
	if _, err := parseTeaLogins([]byte("tea: no logins configured")); err == nil {
		t.Fatal("non-JSON must error, not parse")
	}
}

const teaConfigFixture = `
logins:
  - name: codeberg
    url: https://codeberg.org
    token: cb-token-123
    default: true
  - name: internal
    url: https://gitea.example.com/
    token: internal-token-456
`

func TestTokenFromTeaConfig(t *testing.T) {
	if tok := tokenFromTeaConfig([]byte(teaConfigFixture), "internal", ""); tok != "internal-token-456" {
		t.Fatalf("token by name = %q", tok)
	}
	// URL matching tolerates the trailing slash on either side.
	if tok := tokenFromTeaConfig([]byte(teaConfigFixture), "", "https://gitea.example.com"); tok != "internal-token-456" {
		t.Fatalf("token by url = %q", tok)
	}
	if tok := tokenFromTeaConfig([]byte(teaConfigFixture), "missing", "https://other.example.com"); tok != "" {
		t.Fatalf("no match must yield no token, got %q", tok)
	}
	if tok := tokenFromTeaConfig([]byte("{not yaml"), "codeberg", ""); tok != "" {
		t.Fatalf("broken config must yield no token, got %q", tok)
	}
}

func TestDetectNoRemote(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-b", "main")
	f, setup := Detect(dir)
	if f != nil || !strings.Contains(setup, "no git remote") {
		t.Fatalf("Detect = (%v, %q)", f, setup)
	}
}

func TestDetectCachesSuccess(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-b", "main")
	gitIn(t, dir, "remote", "add", "origin", "https://github.com/TrueDaerk/ike.git")
	t.Cleanup(func() { ResetDetection(dir) })
	f1, setup := Detect(dir)
	if setup != "" {
		if strings.Contains(setup, "gh") {
			t.Skip("gh not installed")
		}
		t.Fatalf("setup = %q", setup)
	}
	if _, ok := f1.(*ghForge); !ok {
		t.Fatalf("backend = %T, want *ghForge", f1)
	}
	// A second call must hand back the same cached instance, even after the
	// remote changed — detection is per workspace root, not per call.
	gitIn(t, dir, "remote", "set-url", "origin", "https://gitea.example.com/o/r.git")
	f2, setup := Detect(dir)
	if setup != "" || f2 != f1 {
		t.Fatalf("second Detect = (%p, %q), want the cached %p", f2, setup, f1)
	}
	ResetDetection(dir)
	if f3, _ := Detect(dir); f3 == f1 {
		t.Fatal("ResetDetection must drop the cached backend")
	}
}
