package deeplink

import "testing"

func TestParseRemoteLink(t *testing.T) {
	l, err := Parse("ike://open?remote=git%40github.com%3ATrueDaerk%2Fike.git&file=internal%2Fapp%2Fapp.go:42&tool=terminal")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if l.RemoteKey != "github.com/truedaerk/ike" {
		t.Errorf("RemoteKey = %q", l.RemoteKey)
	}
	if l.RemoteRaw != "git@github.com:TrueDaerk/ike.git" {
		t.Errorf("RemoteRaw = %q", l.RemoteRaw)
	}
	if l.File != "internal/app/app.go" || l.Line != 42 {
		t.Errorf("File/Line = %q/%d", l.File, l.Line)
	}
	if l.Tool != "terminal" {
		t.Errorf("Tool = %q", l.Tool)
	}
}

func TestParseProjectLink(t *testing.T) {
	l, err := Parse("ike://open?project=ike")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if l.Project != "ike" || l.RemoteKey != "" || l.File != "" || l.Tool != "" {
		t.Errorf("unexpected link %+v", l)
	}
}

func TestParseOpaqueForm(t *testing.T) {
	// Some surfaces strip the slashes: ike:open?… must parse the same.
	if _, err := Parse("ike:open?project=ike"); err != nil {
		t.Fatalf("opaque form: %v", err)
	}
}

func TestParseIgnoresUnknownParams(t *testing.T) {
	l, err := Parse("ike://open?project=ike&color=red&x=1")
	if err != nil || l.Project != "ike" {
		t.Fatalf("unknown params must be ignored: %v %+v", err, l)
	}
}

func TestParseFileWithoutLine(t *testing.T) {
	l, err := Parse("ike://open?project=ike&file=cmd/ike/main.go")
	if err != nil || l.File != "cmd/ike/main.go" || l.Line != 0 {
		t.Fatalf("got %+v, %v", l, err)
	}
	// A non-numeric suffix stays part of the path, like the CLI grammar.
	l, err = Parse("ike://open?project=ike&file=weird:name")
	if err != nil || l.File != "weird:name" || l.Line != 0 {
		t.Fatalf("got %+v, %v", l, err)
	}
}

func TestParseRejects(t *testing.T) {
	for _, raw := range []string{
		"",
		"not a url at all ://",
		"https://github.com/a/b",                      // wrong scheme
		"ike://clone?remote=git@github.com:a/b",       // unknown action
		"ike://open",                                  // neither remote nor project
		"ike://open?remote=git@github.com:a/b&project=x", // both
		"ike://open?remote=nonsense",                  // unparsable remote
		"ike://open?project=a/b",                      // path, not a name
		"ike://open?project=..",                       // traversal name
		"ike://open?project=ike&file=/etc/passwd",     // absolute file
		"ike://open?project=ike&file=..%2Fescape.go",  // traversal file
		"ike://open?project=ike&file=a%2F..%2F..%2Fb", // embedded traversal
	} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) accepted, want error", raw)
		}
	}
}

func TestNormalizeRemoteSpellings(t *testing.T) {
	want := "github.com/truedaerk/ike"
	for _, remote := range []string{
		"https://github.com/TrueDaerk/ike",
		"https://github.com/TrueDaerk/ike.git",
		"git@github.com:TrueDaerk/ike.git",
		"git@GitHub.com:truedaerk/ike",
		"ssh://git@github.com/TrueDaerk/ike",
		"ssh://git@github.com:22/TrueDaerk/ike.git",
	} {
		key, ok := NormalizeRemote(remote)
		if !ok || key != want {
			t.Errorf("NormalizeRemote(%q) = %q, %v; want %q", remote, key, ok, want)
		}
	}
}

func TestNormalizeRemoteKeepsPathPrefix(t *testing.T) {
	// A Gitea under a path prefix keeps only the last two segments, matching
	// forge's parser.
	key, ok := NormalizeRemote("https://git.example.com/gitea/Owner/Repo.git")
	if !ok || key != "git.example.com/owner/repo" {
		t.Errorf("got %q, %v", key, ok)
	}
}

func TestNormalizeRemoteRejects(t *testing.T) {
	for _, remote := range []string{"", "ike", "github.com/a", "git@host", "https://host/only"} {
		if key, ok := NormalizeRemote(remote); ok {
			t.Errorf("NormalizeRemote(%q) = %q, want reject", remote, key)
		}
	}
}
