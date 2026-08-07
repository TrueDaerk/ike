package httpclient

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ike/internal/httpfile"
)

func parseOne(t *testing.T, src string) *httpfile.Request {
	t.Helper()
	f := httpfile.Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 1 {
		t.Fatalf("parse: errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	return f.Requests[0]
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// noConfig disables .netrc/.curlrc so host machine config never leaks into
// tests.
var noConfig = Options{DisableConfig: true}

func TestDispatchBasic(t *testing.T) {
	var got *http.Request
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Clone(context.Background())
		b := make([]byte, r.ContentLength)
		r.Body.Read(b)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	req := parseOne(t, "POST "+srv.URL+"/things\nContent-Type: application/json\nX-Custom: yes\n\n{\"a\":1}\n")
	resp, err := Dispatch(context.Background(), req, noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 || !strings.Contains(resp.Status, "201") {
		t.Errorf("status: %q %d", resp.Status, resp.StatusCode)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("body: %q", resp.Body)
	}
	if resp.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("headers: %v", resp.Headers)
	}
	if got.Method != "POST" || got.URL.Path != "/things" {
		t.Errorf("server saw %s %s", got.Method, got.URL.Path)
	}
	if got.Header.Get("X-Custom") != "yes" {
		t.Errorf("custom header missing: %v", got.Header)
	}
	if gotBody != `{"a":1}` {
		t.Errorf("server body: %q", gotBody)
	}
	if resp.RequestKey != "0" {
		t.Errorf("request key: %q", resp.RequestKey)
	}
	if resp.Duration < 0 {
		t.Errorf("duration: %v", resp.Duration)
	}
}

func TestDispatchPlaceholderSubstitution(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Header.Get("Authorization")))
	}))
	defer srv.Close()

	req := parseOne(t, "GET ${BASE}/x\nAuthorization: Bearer {{$env TOKEN}}\n")
	opts := noConfig
	opts.Lookup = func(k string) (string, bool) {
		switch k {
		case "BASE":
			return srv.URL, true
		case "TOKEN":
			return "t0k", true
		}
		return "", false
	}
	resp, err := Dispatch(context.Background(), req, opts)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "Bearer t0k" {
		t.Errorf("substituted auth: %q", resp.Body)
	}
}

func TestDispatchUnresolvedPlaceholderAborts(t *testing.T) {
	req := parseOne(t, "GET https://example.invalid/${MISSING}\n")
	opts := noConfig
	opts.Lookup = func(string) (string, bool) { return "", false }
	_, err := Dispatch(context.Background(), req, opts)
	if err == nil || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("want unresolved-placeholder error, got %v", err)
	}
}

func TestDispatchNetrc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Header.Get("Authorization")))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	hostname := host[:strings.Index(host, ":")]

	dir := t.TempDir()
	netrc := writeFile(t, dir, "netrc", "machine "+hostname+" login alice password s3cret\n")

	req := parseOne(t, "GET "+srv.URL+"/\n")
	resp, err := Dispatch(context.Background(), req, Options{NetrcPath: netrc, CurlrcPath: filepath.Join(dir, "absent")})
	if err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	if string(resp.Body) != want {
		t.Errorf("netrc auth: got %q want %q", resp.Body, want)
	}
}

func TestDispatchExplicitAuthorizationWinsOverNetrc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Header.Get("Authorization")))
	}))
	defer srv.Close()

	dir := t.TempDir()
	netrc := writeFile(t, dir, "netrc", "default login bob password nope\n")

	req := parseOne(t, "GET "+srv.URL+"/\nAuthorization: Bearer explicit\n")
	resp, err := Dispatch(context.Background(), req, Options{NetrcPath: netrc, CurlrcPath: filepath.Join(dir, "absent")})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "Bearer explicit" {
		t.Errorf("explicit auth must win: %q", resp.Body)
	}
}

func TestDispatchCurlrcHeadersAndPrecedence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Header.Get("X-From") + "|" + r.Header.Get("User-Agent") + "|" + r.Header.Get("X-Only-Curlrc")))
	}))
	defer srv.Close()

	dir := t.TempDir()
	curlrc := writeFile(t, dir, "curlrc", strings.Join([]string{
		`header = "X-From: curlrc"`,
		`header = "X-Only-Curlrc: yes"`,
		"user-agent = ike-test",
		"--some-unsupported-option value",
	}, "\n"))

	req := parseOne(t, "GET "+srv.URL+"/\nX-From: request\n")
	resp, err := Dispatch(context.Background(), req, Options{CurlrcPath: curlrc, NetrcPath: filepath.Join(dir, "absent")})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(resp.Body), "|")
	if parts[0] != "request" {
		t.Errorf("explicit header must win over curlrc: %q", parts[0])
	}
	if parts[1] != "ike-test" {
		t.Errorf("user-agent from curlrc: %q", parts[1])
	}
	if parts[2] != "yes" {
		t.Errorf("curlrc-only header must apply: %q", parts[2])
	}
	if len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "some-unsupported-option") {
		t.Errorf("unsupported option must warn: %v", resp.Warnings)
	}
}

func TestDispatchCurlrcNoLocationStopsRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusFound)
			return
		}
		w.Write([]byte("followed"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	req := parseOne(t, "GET "+srv.URL+"/from\n")

	// Default: redirects followed.
	resp, err := Dispatch(context.Background(), req, Options{CurlrcPath: filepath.Join(dir, "absent"), NetrcPath: filepath.Join(dir, "absent")})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || string(resp.Body) != "followed" {
		t.Errorf("default must follow redirects: %d %q", resp.StatusCode, resp.Body)
	}
}

func TestDispatchInvalidTarget(t *testing.T) {
	req := parseOne(t, "GET :/bad\n")
	_, err := Dispatch(context.Background(), req, noConfig)
	if err == nil {
		t.Fatal("want error for invalid target")
	}
	req2 := parseOne(t, "GET /just-a-path\n")
	_, err = Dispatch(context.Background(), req2, noConfig)
	if err == nil || !strings.Contains(err.Error(), "no host") {
		t.Fatalf("want no-host error, got %v", err)
	}
}

func TestDispatchTruncatesHugeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chunk := strings.Repeat("x", 1<<20)
		for i := 0; i < 11; i++ {
			w.Write([]byte(chunk))
		}
	}))
	defer srv.Close()

	req := parseOne(t, "GET "+srv.URL+"/\n")
	resp, err := Dispatch(context.Background(), req, noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Truncated {
		t.Error("want Truncated=true")
	}
	if len(resp.Body) != MaxBodyBytes {
		t.Errorf("body length: %d", len(resp.Body))
	}
	found := false
	for _, w := range resp.Warnings {
		if strings.Contains(w, "truncated") {
			found = true
		}
	}
	if !found {
		t.Errorf("want truncation warning, got %v", resp.Warnings)
	}
}

func TestDispatchTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()

	req := parseOne(t, "GET "+srv.URL+"/\n")
	opts := noConfig
	opts.Timeout = 50 * time.Millisecond
	_, err := Dispatch(context.Background(), req, opts)
	if err == nil {
		t.Fatal("want timeout error")
	}
}

func TestNetrcParsing(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "netrc", strings.Join([]string{
		"machine one.test login u1 password p1",
		"machine two.test",
		"  login u2",
		"  password p2",
		"macdef init",
		"  echo hello",
		"",
		"default login du password dp",
	}, "\n"))

	cases := []struct {
		host, login, password string
	}{
		{"one.test", "u1", "p1"},
		{"two.test", "u2", "p2"},
		{"other.test", "du", "dp"},
	}
	for _, c := range cases {
		creds, err := lookupNetrc(p, c.host)
		if err != nil {
			t.Fatal(err)
		}
		if creds == nil || creds.Login != c.login || creds.Password != c.password {
			t.Errorf("%s: got %+v", c.host, creds)
		}
	}

	if creds, _ := lookupNetrc(filepath.Join(dir, "absent"), "one.test"); creds != nil {
		t.Errorf("missing file must yield nil, got %+v", creds)
	}
}

func TestCurlrcParsing(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "curlrc", strings.Join([]string{
		"# comment",
		"",
		"insecure",
		"proxy http://proxy.test:3128",
		"user = alice:pw",
		"referer: https://ref.test",
		"max-time 2.5",
		"connect-timeout = 1",
		"location",
		"-H \"X-A: 1\"",
		"header X-B: 2",
	}, "\n"))
	cfg := parseCurlrc(p)
	if !cfg.Insecure {
		t.Error("insecure")
	}
	if cfg.Proxy != "http://proxy.test:3128" {
		t.Errorf("proxy: %q", cfg.Proxy)
	}
	if cfg.User != "alice:pw" {
		t.Errorf("user: %q", cfg.User)
	}
	if cfg.Referer != "https://ref.test" {
		t.Errorf("referer: %q", cfg.Referer)
	}
	if cfg.MaxTime != 2500*time.Millisecond {
		t.Errorf("max-time: %v", cfg.MaxTime)
	}
	if cfg.ConnectTimeout != time.Second {
		t.Errorf("connect-timeout: %v", cfg.ConnectTimeout)
	}
	if cfg.FollowRedirect == nil || !*cfg.FollowRedirect {
		t.Error("location")
	}
	if len(cfg.Headers) != 2 || cfg.Headers[0] != "X-A: 1" || cfg.Headers[1] != "X-B: 2" {
		t.Errorf("headers: %v", cfg.Headers)
	}
	if len(cfg.Warnings) != 0 {
		t.Errorf("warnings: %v", cfg.Warnings)
	}
}

// echoBodyServer answers with whatever body it received.
func echoBodyServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

// TestDispatchExternalBody guards #1305: `< ./payload.json` sends the file,
// resolved against the .http file's own directory.
func TestDispatchExternalBody(t *testing.T) {
	srv, got := echoBodyServer(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "payload.json"), []byte(`{"from":"disk"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	req := parseOne(t, "POST "+srv.URL+"/x\nContent-Type: application/json\n\n< ./payload.json\n")

	opts := noConfig
	opts.BaseDir = dir
	if _, err := Dispatch(context.Background(), req, opts); err != nil {
		t.Fatal(err)
	}
	if *got != `{"from":"disk"}` {
		t.Fatalf("sent body = %q, want the file contents", *got)
	}
}

// TestDispatchExternalBodySubstitutes: the `<@` form resolves placeholders
// inside the file, the plain form does not.
func TestDispatchExternalBodySubstitutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.json"), []byte(`{"id":"${ID}"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) (string, bool) {
		if name == "ID" {
			return "42", true
		}
		return "", false
	}

	srv, got := echoBodyServer(t)
	opts := noConfig
	opts.BaseDir, opts.Lookup = dir, lookup

	req := parseOne(t, "POST "+srv.URL+"/x\n\n<@ ./p.json\n")
	if _, err := Dispatch(context.Background(), req, opts); err != nil {
		t.Fatal(err)
	}
	if *got != `{"id":"42"}` {
		t.Fatalf("<@ must substitute the file, got %q", *got)
	}

	plain := parseOne(t, "POST "+srv.URL+"/x\n\n< ./p.json\n")
	if _, err := Dispatch(context.Background(), plain, opts); err != nil {
		t.Fatal(err)
	}
	if *got != `{"id":"${ID}"}` {
		t.Fatalf("< must send the file verbatim, got %q", *got)
	}
}

// TestDispatchExternalBodyMissingFile: a missing file is a named error, never
// a silently empty body.
func TestDispatchExternalBodyMissingFile(t *testing.T) {
	srv, got := echoBodyServer(t)
	opts := noConfig
	opts.BaseDir = t.TempDir()
	req := parseOne(t, "POST "+srv.URL+"/x\n\n< ./nope.json\n")

	_, err := Dispatch(context.Background(), req, opts)
	if err == nil {
		t.Fatal("a missing body file must fail the request")
	}
	if !strings.Contains(err.Error(), "nope.json") {
		t.Fatalf("error must name the file, got %v", err)
	}
	if *got != "" {
		t.Fatalf("nothing must be sent, got %q", *got)
	}
}

// TestDispatchExternalBodyUnresolvedPlaceholder: an unresolvable placeholder
// inside a <@ file aborts instead of sending the raw text.
func TestDispatchExternalBodyUnresolvedPlaceholder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "p.json"), []byte(`{"id":"${MISSING}"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, _ := echoBodyServer(t)
	opts := noConfig
	opts.BaseDir = dir
	opts.Lookup = func(string) (string, bool) { return "", false }

	if _, err := Dispatch(context.Background(), parseOne(t, "POST "+srv.URL+"/x\n\n<@ ./p.json\n"), opts); err == nil {
		t.Fatal("an unresolved placeholder in the body file must abort")
	}
}

// multipartServer parses the incoming request as multipart/form-data with the
// standard library and captures the form values/files — a request that fails
// to parse fails the exchange.
func multipartServer(t *testing.T) (*httptest.Server, *map[string]string, *string) {
	t.Helper()
	got := map[string]string{}
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		mr, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			data, _ := io.ReadAll(p)
			got[p.FormName()] = string(data)
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	return srv, &got, &query
}

// TestDispatchMultipartInline guards #1707's baseline: a hand-written
// multipart body (LF endings, no closing delimiter) — combined with a folded
// query line — round-trips through a standard multipart parser.
func TestDispatchMultipartInline(t *testing.T) {
	srv, got, query := multipartServer(t)
	req := parseOne(t, "POST "+srv.URL+"/import/\n"+
		"     & tags = my_tag\n"+
		"Content-Type: multipart/form-data; boundary=bound\n"+
		"\n"+
		"--bound\n"+
		"Content-Disposition: form-data; name=\"note\"\n"+
		"\n"+
		"hello world\n")
	resp, err := Dispatch(context.Background(), req, noConfig)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, resp.Body)
	}
	if (*got)["note"] != "hello world" {
		t.Fatalf("parts = %#v", *got)
	}
	if *query != "tags=my_tag" {
		t.Fatalf("query = %q", *query)
	}
}

// TestDispatchMultipartFilePart guards #1707: `< file` inside a part embeds
// the file — resolved against the .http file's directory — byte-verbatim.
func TestDispatchMultipartFilePart(t *testing.T) {
	srv, got, _ := multipartServer(t)
	dir := t.TempDir()
	binary := "id,name\x00\xff\r\nraw"
	writeFile(t, dir, "leads.csv", binary)

	req := parseOne(t, "POST "+srv.URL+"/import/\n"+
		"Content-Type: multipart/form-data; boundary=bound\n"+
		"\n"+
		"--bound\n"+
		"Content-Disposition: form-data; name=\"import\"; filename=\"import.csv\"\n"+
		"\n"+
		"< leads.csv\n"+
		"--bound\n"+
		"Content-Disposition: form-data; name=\"tag\"\n"+
		"\n"+
		"inline\n")
	opts := noConfig
	opts.BaseDir = dir
	resp, err := Dispatch(context.Background(), req, opts)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, resp.Body)
	}
	if (*got)["import"] != binary {
		t.Fatalf("file part = %q, want %q", (*got)["import"], binary)
	}
	if (*got)["tag"] != "inline" {
		t.Fatalf("inline part = %q", (*got)["tag"])
	}
}

// TestDispatchMultipartMissingFile: a missing part file fails before anything
// is sent.
func TestDispatchMultipartMissingFile(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	t.Cleanup(srv.Close)
	req := parseOne(t, "POST "+srv.URL+"/x\n"+
		"Content-Type: multipart/form-data; boundary=bound\n"+
		"\n"+
		"--bound\n"+
		"Content-Disposition: form-data; name=\"f\"\n"+
		"\n"+
		"< missing.bin\n")
	opts := noConfig
	opts.BaseDir = t.TempDir()
	if _, err := Dispatch(context.Background(), req, opts); err == nil ||
		!strings.Contains(err.Error(), "missing.bin") {
		t.Fatalf("err = %v", err)
	}
	if hit {
		t.Fatal("request must not reach the server")
	}
}
