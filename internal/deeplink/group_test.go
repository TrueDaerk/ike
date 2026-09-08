package deeplink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// group_test.go covers the group form of the scheme (0510, #2576):
// ike://open?group=<name>[&project=…|&remote=…] — the parser's target-key
// rules and the member-restricted resolution.

func TestParseGroupLink(t *testing.T) {
	l, err := Parse("ike://open?group=web&file=cmd%2Fmain.go:7&tool=vcs")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if l.Group != "web" || l.Project != "" || l.RemoteKey != "" {
		t.Errorf("group alone must be the only target key, got %+v", l)
	}
	if l.File != "cmd/main.go" || l.Line != 7 || l.Tool != "vcs" {
		t.Errorf("payload = %q/%d/%q", l.File, l.Line, l.Tool)
	}
}

func TestParseGroupWithProject(t *testing.T) {
	l, err := Parse("ike://open?group=web&project=api")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if l.Group != "web" || l.Project != "api" {
		t.Errorf("group plus project must both survive, got %+v", l)
	}
}

func TestParseGroupWithRemote(t *testing.T) {
	l, err := Parse("ike://open?group=web&remote=git%40github.com%3AA%2FB.git")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if l.Group != "web" || l.RemoteKey != "github.com/a/b" {
		t.Errorf("group plus remote must both survive, got %+v", l)
	}
}

func TestParseGroupRejects(t *testing.T) {
	cases := []struct{ url, want string }{
		{"ike://open?remote=https://github.com/a/b&project=api", "mutually exclusive"},
		{"ike://open?group=web&group=api", "given twice"},
		{"ike://open?group=web%2Fapi", "path separators"},
		{"ike://open?file=a.go", "needs remote=, project= or group="},
	}
	for _, c := range cases {
		_, err := Parse(c.url)
		if err == nil {
			t.Errorf("%s must be refused", c.url)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q should mention %q", c.url, err, c.want)
		}
	}
}

// groupFixture builds three sibling project directories and returns them.
func groupFixture(t *testing.T) (base string, roots []string) {
	t.Helper()
	base = t.TempDir()
	for _, name := range []string{"api", "ui", "www"} {
		p := filepath.Join(base, name)
		mkdir(t, p)
		roots = append(roots, p)
	}
	return base, roots
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestResolveInLandsOnMember(t *testing.T) {
	_, roots := groupFixture(t)
	members := roots[:2] // api, ui — www is not in the group
	res := ResolveIn(mustLink(t, "ike://open?group=web&project=ui"), nil, "", members)
	if res.Kind != KindSwitch || !sameFile(res.Path, roots[1]) {
		t.Errorf("the member must resolve, got %+v", res)
	}
}

func TestResolveInRefusesNonMember(t *testing.T) {
	_, roots := groupFixture(t)
	members := roots[:2]
	// www exists and is even in the projects directory — but it is not a
	// member, so the group link is refused rather than switching to it.
	res := ResolveIn(mustLink(t, "ike://open?group=web&project=www"), nil, filepath.Dir(roots[0]), members)
	if res.Kind != KindNotFound {
		t.Errorf("a non-member must be refused, got %+v", res)
	}
}

func TestResolveInNeverClones(t *testing.T) {
	_, roots := groupFixture(t)
	res := ResolveIn(mustLink(t, "ike://open?group=web&remote=https://github.com/no/where"),
		nil, "", roots[:2])
	if res.Kind != KindNotFound {
		t.Errorf("a group link has no clone fallback, got %+v", res)
	}
}

func TestResolveInPrefersHistoryButKeepsMembersOnly(t *testing.T) {
	_, roots := groupFixture(t)
	members := roots[:2]
	// The history knows a same-named project outside the group; the member
	// scan is what answers the link.
	outside := filepath.Join(t.TempDir(), "ui")
	mkdir(t, outside)
	history := []Candidate{{Path: outside, Name: "ui"}}
	res := ResolveIn(mustLink(t, "ike://open?group=web&project=ui"), history, "", members)
	if res.Kind != KindSwitch || !sameFile(res.Path, roots[1]) {
		t.Errorf("only members may answer, got %+v", res)
	}
}

// sameFile compares two paths with symlinks resolved (macOS temp dirs).
func sameFile(a, b string) bool { return canonicalPath(a) == canonicalPath(b) }
