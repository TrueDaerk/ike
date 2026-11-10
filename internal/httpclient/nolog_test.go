package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// nolog_test.go covers the `# @no-log` directive at dispatch time (#3115):
// the flag rides on the request snapshot, so the app can keep the exchange
// off disk and a re-send of it stays unlogged too.
func TestDispatchNoLogRecordedOnSnapshot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	opts := Options{CurlrcPath: filepath.Join(dir, "absent"), NetrcPath: filepath.Join(dir, "absent")}

	quiet, next := parseTwo(t, "### a\n# @no-log\nGET "+srv.URL+"/a\n\n### b\nGET "+srv.URL+"/b\n")
	resp, err := Dispatch(context.Background(), quiet, opts)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Request == nil || !resp.Request.NoLog {
		t.Fatalf("snapshot must record @no-log: %+v", resp.Request)
	}
	if c := resp.Request.Clone(); !c.NoLog {
		t.Errorf("Clone must keep NoLog")
	}
	again, err := Resend(context.Background(), quiet.Key(), resp.Request, opts, StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if again.Request == nil || !again.Request.NoLog {
		t.Errorf("a re-send of a @no-log exchange must stay unlogged: %+v", again.Request)
	}
	resp, err = Dispatch(context.Background(), next, opts)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Request == nil || resp.Request.NoLog {
		t.Errorf("the next block must log again: %+v", resp.Request)
	}
}
