package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ike/internal/httpfile"
)

// echoMethodURL answers every request with "<method> <request URI>|<body>".
func echoMethodURL(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.Write([]byte(r.Method + " " + r.URL.RequestURI() + "|" + string(b)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRelativeTargetsUseFileHost(t *testing.T) {
	srv := echoMethodURL(t)
	src := "@host=" + srv.URL + "\n\n### Request A\nGET /do/stuff\n\n### Request B\nPOST /api/user?s=a\nContent-Type: application/json\n\n{\n  \"name\": \"x\"\n}\n"
	f := httpfile.Parse(src)
	if len(f.Requests) != 2 {
		t.Fatalf("parse: %d requests, errors %v", len(f.Requests), f.Errors)
	}
	opts := noConfig
	opts.Vars = &httpfile.Vars{File: f.VarMap()}
	want := []string{"GET /do/stuff|", "POST /api/user?s=a|"}
	urls := []string{srv.URL + "/do/stuff", srv.URL + "/api/user?s=a"}
	for i, req := range f.Requests {
		resp, err := Dispatch(context.Background(), req, opts)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if !strings.HasPrefix(string(resp.Body), want[i]) {
			t.Errorf("request %d: server saw %q, want prefix %q", i, resp.Body, want[i])
		}
		if resp.Request.URL != urls[i] {
			t.Errorf("request %d: snapshot URL %q, want %q", i, resp.Request.URL, urls[i])
		}
		if len(resp.Redirects) > 0 && resp.Redirects[0].URL != urls[i] {
			t.Errorf("request %d: first hop %q, want %q", i, resp.Redirects[0].URL, urls[i])
		}
	}
}

func TestRelativeTargetUsesEnvironmentHost(t *testing.T) {
	srv := echoMethodURL(t)
	opts := noConfig
	opts.Vars = &httpfile.Vars{Env: map[string]string{"base": srv.URL, "host": "{{base}}"}}
	resp, err := Dispatch(context.Background(), parseOne(t, "GET /env\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(resp.Body), "GET /env|") {
		t.Errorf("server saw %q", resp.Body)
	}
}

func TestRelativeTargetHostHeaderFallback(t *testing.T) {
	srv := echoMethodURL(t)
	hostport := strings.TrimPrefix(srv.URL, "http://")
	opts := noConfig
	opts.Lookup = func(string) (string, bool) { return "", false }
	resp, err := Dispatch(context.Background(), parseOne(t, "GET /hdr\nHost: "+hostport+"\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(resp.Body), "GET /hdr|") {
		t.Errorf("server saw %q", resp.Body)
	}
	if resp.Request.URL != srv.URL+"/hdr" {
		t.Errorf("snapshot URL %q", resp.Request.URL)
	}
}

func TestRelativeTargetTrailingSlashHost(t *testing.T) {
	srv := echoMethodURL(t)
	opts := noConfig
	opts.Vars = &httpfile.Vars{File: map[string]string{"host": srv.URL + "/"}}
	resp, err := Dispatch(context.Background(), parseOne(t, "GET /api\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(resp.Body), "GET /api|") {
		t.Errorf("server saw %q", resp.Body)
	}
	if resp.Request.URL != srv.URL+"/api" {
		t.Errorf("snapshot URL %q, want exactly one slash", resp.Request.URL)
	}
}

func TestRelativeTargetWithoutHostFails(t *testing.T) {
	opts := noConfig
	opts.Lookup = func(string) (string, bool) { return "", false }
	_, err := Dispatch(context.Background(), parseOne(t, "GET /do/stuff\n"), opts)
	want := `relative target "/do/stuff" needs a @host variable or Host header`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

func TestRelativeTargetResendAndCurl(t *testing.T) {
	srv := echoMethodURL(t)
	opts := noConfig
	opts.Vars = &httpfile.Vars{File: map[string]string{"host": srv.URL}}
	resp, err := Dispatch(context.Background(), parseOne(t, "GET /again\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if curl := resp.Request.Curl(); !strings.Contains(curl, srv.URL+"/again") {
		t.Errorf("curl export lacks absolute URL: %s", curl)
	}
	if h := resp.Request.HTTPie(); !strings.Contains(h, srv.URL+"/again") {
		t.Errorf("httpie export lacks absolute URL: %s", h)
	}
	again, err := Resend(context.Background(), resp.RequestKey, resp.Request, noConfig, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(again.Body), "GET /again|") {
		t.Errorf("re-send: server saw %q", again.Body)
	}
}

func TestRelativeTargetPlaceholderFrontUntouched(t *testing.T) {
	srv := echoMethodURL(t)
	opts := noConfig
	opts.Vars = &httpfile.Vars{File: map[string]string{"host": "http://unused.invalid", "path": srv.URL + "/p"}}
	resp, err := Dispatch(context.Background(), parseOne(t, "GET {{path}}\n"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(resp.Body), "GET /p|") {
		t.Errorf("server saw %q", resp.Body)
	}
}

func TestDispatchWSRelativeTarget(t *testing.T) {
	srv := wsEchoServer(t)
	host := "ws" + strings.TrimPrefix(srv.URL, "http")
	f := httpfile.Parse("@host=" + host + "\nWEBSOCKET /socket\n\nhello\n")
	if len(f.Requests) != 1 {
		t.Fatalf("parse: %+v", f.Errors)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	chunks := &collectChunks{}
	done := make(chan *Response, 1)
	go func() {
		resp, err := DispatchWS(ctx, f.Requests[0],
			Options{DisableConfig: true, Vars: &httpfile.Vars{File: f.VarMap()}},
			WSCallbacks{StreamCallbacks: StreamCallbacks{OnChunk: chunks.add}})
		if err != nil {
			t.Error(err)
		}
		done <- resp
	}()
	chunks.waitFor(t, "echo over relative target", func(lines []string) bool {
		return linesWith(lines, "←", "echo: hello") == 1
	})
	cancel()
	if resp := <-done; resp != nil && resp.Request != nil && resp.Request.URL != host+"/socket" {
		t.Errorf("snapshot URL %q, want %q", resp.Request.URL, host+"/socket")
	}
}
