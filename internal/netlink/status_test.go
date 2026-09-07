package netlink

import (
	"testing"

	"ike/internal/deeplink"
)

// pairClient walks the pairing handshake and returns the token.
func pairClient(t *testing.T, srv *Server, rec *syncRecorder, c *client) string {
	t.Helper()
	if ch := c.send(Request{Cmd: "pair", Client: "phone"}); ch.Type != "challenge" {
		t.Fatalf("challenge %+v", ch)
	}
	paired := c.send(Request{Cmd: "pair", Code: rec.last().Code.String()})
	if paired.Type != "paired" {
		t.Fatalf("paired %+v", paired)
	}
	return paired.Token
}

// TestServerStatus: a paired client gets the whole snapshot, link included,
// and the answer round-trips through the deep-link grammar.
func TestServerStatus(t *testing.T) {
	st := Status{
		Project: "ike",
		Root:    "/home/dev/ike",
		Remote:  "github.com/truedaerk/ike",
		File:    "internal/app/app.go",
		Line:    42,
		Col:     7,
	}
	srv, rec, _ := testServer(t, func(o *Options) { o.State = func() Status { return st } })
	c := dial(t, srv)
	pairClient(t, srv, rec, c)

	r := c.send(Request{Cmd: "status"})
	if r.Type != "status" {
		t.Fatalf("status %+v", r)
	}
	if r.Project != "ike" || r.Root != "/home/dev/ike" || r.Remote != "github.com/truedaerk/ike" {
		t.Errorf("project/root/remote %+v", r)
	}
	if r.File != "internal/app/app.go" || r.Line != 42 || r.Col != 7 {
		t.Errorf("file/cursor %+v", r)
	}
	l, err := deeplink.Parse(r.Link)
	if err != nil {
		t.Fatalf("the answered link must parse: %q: %v", r.Link, err)
	}
	if l.RemoteKey != st.Remote || l.File != st.File || l.Line != st.Line {
		t.Errorf("link %q parsed to %+v", r.Link, l)
	}
}

// TestServerStatusWithoutFileOrRemote: a project without a git remote and
// without a focused editor answers the plain fields only — the optional ones
// stay absent, and the link addresses the project by name.
func TestServerStatusWithoutFileOrRemote(t *testing.T) {
	st := Status{Project: "scratch", Root: "/tmp/scratch"}
	srv, rec, _ := testServer(t, func(o *Options) { o.State = func() Status { return st } })
	c := dial(t, srv)
	pairClient(t, srv, rec, c)

	r := c.send(Request{Cmd: "status"})
	if r.Type != "status" || r.Remote != "" || r.File != "" || r.Line != 0 || r.Col != 0 {
		t.Fatalf("status %+v", r)
	}
	if r.Link != "ike://open?project=scratch" {
		t.Errorf("link %q", r.Link)
	}
}

// TestServerStatusUnauthorized: status is guarded, and — unlike open — it
// never starts pairing, so no popup appears for an unpaired asker.
func TestServerStatusUnauthorized(t *testing.T) {
	srv, rec, _ := testServer(t, func(o *Options) {
		o.State = func() Status { return Status{Project: "ike", Root: "/home/dev/ike"} }
	})
	c := dial(t, srv)
	if r := c.send(Request{Cmd: "status"}); r.Type != "error" || r.Error != CodeUnauthorized {
		t.Fatalf("status unpaired %+v", r)
	}
	if r := c.send(Request{Cmd: "status", Token: "not-a-token"}); r.Error != CodeUnauthorized {
		t.Fatalf("status with a bogus token %+v", r)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.issued) != 0 {
		t.Fatalf("status must not open a pairing challenge: %+v", rec.issued)
	}
}

// TestServerStatusUnavailable: no state getter — or a getter with nothing to
// report — answers the unavailable code, not an empty status.
func TestServerStatusUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state func() Status
	}{
		{"nil getter", nil},
		{"no project", func() Status { return Status{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec, _ := testServer(t, func(o *Options) { o.State = tc.state })
			c := dial(t, srv)
			pairClient(t, srv, rec, c)
			if r := c.send(Request{Cmd: "status"}); r.Type != "error" || r.Error != CodeUnavailable {
				t.Fatalf("status %+v", r)
			}
		})
	}
}

// TestLinkFromStatus: the remote wins over the project, the cursor line
// rides on the file, and a snapshot naming neither has no link.
func TestLinkFromStatus(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   Status
		want string
	}{
		{"remote and file", Status{Project: "ike", Root: "/r", Remote: "github.com/a/b", File: "x.go", Line: 3, Col: 2},
			"ike://open?file=x.go%3A3&remote=https%3A%2F%2Fgithub.com%2Fa%2Fb"},
		{"project only", Status{Project: "ike", Root: "/r"}, "ike://open?project=ike"},
		{"project and file", Status{Project: "ike", Root: "/r", File: "a/b.go", Line: 9}, "ike://open?file=a%2Fb.go%3A9&project=ike"},
		{"file without a line", Status{Project: "ike", Root: "/r", File: "a.go"}, "ike://open?file=a.go&project=ike"},
		{"nothing named", Status{Root: "/r"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := LinkFromStatus(tc.st)
			if got != tc.want {
				t.Fatalf("link = %q, want %q", got, tc.want)
			}
			if got == "" {
				return
			}
			l, err := deeplink.Parse(got)
			if err != nil {
				t.Fatalf("parse %q: %v", got, err)
			}
			if l.RemoteKey != tc.st.Remote || l.File != tc.st.File || l.Line != tc.st.Line {
				t.Fatalf("%q parsed to %+v", got, l)
			}
			if tc.st.Remote == "" && l.Project != tc.st.Project {
				t.Fatalf("%q parsed to project %q", got, l.Project)
			}
		})
	}
}
