package httpclient

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// configskip_test.go covers the per-request `# @no-curlrc` / `# @no-netrc`
// directives at dispatch time (#3114).

// parseTwo parses a two-block file without errors.
func parseTwo(t *testing.T, src string) (*httpfile.Request, *httpfile.Request) {
	t.Helper()
	f := httpfile.Parse(src)
	if len(f.Errors) != 0 || len(f.Requests) != 2 {
		t.Fatalf("parse: errors=%v requests=%d", f.Errors, len(f.Requests))
	}
	return f.Requests[0], f.Requests[1]
}

// A `.curlrc` with a proxy and a header is ignored entirely for the
// `@no-curlrc` block; the next block of the same file still gets both.
func TestDispatchNoCurlrcSkipsProxyAndHeaders(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("origin|" + r.Header.Get("X-Curlrc") + "|" + r.Header.Get("User-Agent")))
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxy|" + r.Header.Get("X-Curlrc")))
	}))
	defer proxy.Close()

	dir := t.TempDir()
	curlrc := writeFile(t, dir, "curlrc", strings.Join([]string{
		"proxy = " + proxy.URL,
		`header = "X-Curlrc: yes"`,
		"user-agent = from-curlrc",
		"insecure",
	}, "\n"))
	opts := Options{CurlrcPath: curlrc, NetrcPath: filepath.Join(dir, "absent")}

	skip, next := parseTwo(t, "### a\n# @no-curlrc\nGET "+origin.URL+"/a\n\n### b\nGET "+origin.URL+"/b\n")
	resp, err := Dispatch(context.Background(), skip, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Body); !strings.HasPrefix(got, "origin||") || strings.Contains(got, "from-curlrc") {
		t.Errorf("@no-curlrc response = %q, want origin without curlrc header/user-agent", got)
	}
	if resp.Request == nil || !resp.Request.NoCurlrc {
		t.Errorf("snapshot must record @no-curlrc: %+v", resp.Request)
	}
	resp, err = Dispatch(context.Background(), next, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Body); got != "proxy|yes" {
		t.Errorf("next block response = %q, want it proxied with the curlrc header", got)
	}

	// The streaming path shares prepare and must skip the same way.
	resp, err = DispatchStream(context.Background(), skip, opts, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Body); !strings.HasPrefix(got, "origin||") {
		t.Errorf("stream @no-curlrc response = %q", got)
	}
}

// `insecure` from `.curlrc` does not apply under `@no-curlrc`: a self-signed
// server fails verification there and is reached by the next block.
func TestDispatchNoCurlrcSkipsInsecure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	opts := Options{CurlrcPath: writeFile(t, dir, "curlrc", "insecure\n"), NetrcPath: filepath.Join(dir, "absent")}

	skip, next := parseTwo(t, "# @no-curlrc\nGET "+srv.URL+"/\n\n###\nGET "+srv.URL+"/\n")
	if _, err := Dispatch(context.Background(), skip, opts); err == nil {
		t.Error("@no-curlrc must verify the certificate, got no error")
	}
	resp, err := Dispatch(context.Background(), next, opts)
	if err != nil || string(resp.Body) != "ok" {
		t.Errorf("next block: body=%v err=%v, want insecure from curlrc", resp, err)
	}
}

// Re-sending a stored `@no-curlrc` exchange keeps skipping the .curlrc client
// options: the proxy is not used.
func TestResendNoCurlrcSkipsProxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("origin"))
	}))
	defer origin.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("proxy"))
	}))
	defer proxy.Close()
	dir := t.TempDir()
	opts := Options{CurlrcPath: writeFile(t, dir, "curlrc", "proxy = "+proxy.URL+"\n"), NetrcPath: filepath.Join(dir, "absent")}

	snap := &RequestSnapshot{Method: "GET", URL: origin.URL + "/", NoCurlrc: true}
	resp, err := Resend(context.Background(), "x", snap, opts, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "origin" {
		t.Errorf("resend body = %q, want origin (no proxy)", resp.Body)
	}
	if !strings.HasPrefix(snap.Curl(), "curl -q ") {
		t.Errorf("snapshot curl = %q, want -q first", snap.Curl())
	}
}

// `@no-netrc` sends no .netrc credentials for a host the .netrc knows; the
// next block still does.
func TestDispatchNoNetrc(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Header.Get("Authorization") + "|" + r.Header.Get("X-Curlrc")))
	}))
	defer srv.Close()
	dir := t.TempDir()
	opts := Options{
		NetrcPath:  writeFile(t, dir, "netrc", "machine 127.0.0.1 login alice password s3cret\n"),
		CurlrcPath: writeFile(t, dir, "curlrc", `header = "X-Curlrc: yes"`+"\n"),
	}

	skip, next := parseTwo(t, "# @no-netrc\nGET "+srv.URL+"/\n\n###\nGET "+srv.URL+"/\n")
	resp, err := Dispatch(context.Background(), skip, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(resp.Body); got != "|yes" {
		t.Errorf("@no-netrc response = %q, want no Authorization but the curlrc header", got)
	}
	resp, err = Dispatch(context.Background(), next, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret")) + "|yes"
	if got := string(resp.Body); got != want {
		t.Errorf("next block response = %q, want %q", got, want)
	}
}

// Under `@no-curlrc` a .curlrc `netrc-file` path is a .curlrc option like any
// other: the .netrc lookup falls back to $NETRC.
func TestDispatchNoCurlrcNetrcFileFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Header.Get("Authorization")))
	}))
	defer srv.Close()
	dir := t.TempDir()
	custom := writeFile(t, dir, "custom-netrc", "machine 127.0.0.1 login curlrc password one\n")
	def := writeFile(t, dir, "default-netrc", "machine 127.0.0.1 login default password two\n")
	t.Setenv("NETRC", def)
	opts := Options{CurlrcPath: writeFile(t, dir, "curlrc", "netrc-file "+custom+"\n")}

	skip, next := parseTwo(t, "# @no-curlrc\nGET "+srv.URL+"/\n\n###\nGET "+srv.URL+"/\n")
	resp, err := Dispatch(context.Background(), skip, opts)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("default:two")); string(resp.Body) != want {
		t.Errorf("@no-curlrc auth = %q, want the $NETRC credentials", resp.Body)
	}
	resp, err = Dispatch(context.Background(), next, opts)
	if err != nil {
		t.Fatal(err)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("curlrc:one")); string(resp.Body) != want {
		t.Errorf("next block auth = %q, want the netrc-file credentials", resp.Body)
	}
}
