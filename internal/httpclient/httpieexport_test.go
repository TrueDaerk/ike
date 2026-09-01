package httpclient

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

// TestSnapshotHTTPie covers the response-side httpie export (#2384): the
// snapshot's method, final URL, headers and substituted body in httpie's item
// syntax, with the same rules the editor-side export follows.
func TestSnapshotHTTPie(t *testing.T) {
	cases := []struct {
		name string
		snap *RequestSnapshot
		want string
	}{
		{
			name: "nil snapshot exports nothing",
			snap: nil,
			want: "",
		},
		{
			name: "plain get",
			snap: &RequestSnapshot{Method: "GET", URL: "https://api.example.com/things"},
			want: `http GET https://api.example.com/things`,
		},
		{
			name: "headers sorted, query split off the url",
			snap: &RequestSnapshot{
				Method: "POST", URL: "https://api.example.com/things?q=a%20b",
				Headers: http.Header{
					"X-Trace":      {"abc"},
					"Content-Type": {"application/json"},
				},
			},
			want: `http POST https://api.example.com/things Content-Type:application/json X-Trace:abc 'q==a b'`,
		},
		{
			name: "json body becomes fields",
			snap: &RequestSnapshot{
				Method: "POST", URL: "https://x.test/",
				Headers: http.Header{"Content-Type": {"application/json"}},
				Body:    []byte(`{"who":"o'brien","n":3}`),
			},
			want: `http POST https://x.test/ Content-Type:application/json 'who=o'\''brien' n:=3`,
		},
		{
			name: "repeated header values keep both",
			snap: &RequestSnapshot{
				Method: "GET", URL: "https://x.test/",
				Headers: http.Header{"Cookie": {"a=1", "b=2"}},
			},
			want: `http GET https://x.test/ Cookie:a=1 Cookie:b=2`,
		},
		{
			name: "basic auth becomes -a",
			snap: &RequestSnapshot{
				Method: "GET", URL: "https://x.test/",
				Headers: http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte("me:s3cr t"))}},
			},
			want: `http -a 'me:s3cr t' GET https://x.test/`,
		},
		{
			name: "bearer token is exported as sent, never masked",
			snap: &RequestSnapshot{
				Method: "GET", URL: "https://x.test/",
				Headers: http.Header{"Authorization": {"Bearer tok-123"}},
			},
			want: `http GET https://x.test/ 'Authorization:Bearer tok-123'`,
		},
		{
			name: "a plain text body rides --raw",
			snap: &RequestSnapshot{
				Method: "PUT", URL: "https://x.test/",
				Headers: http.Header{"Content-Type": {"text/plain"}},
				Body:    []byte("line one\nline two\n"),
			},
			want: "http --raw 'line one\nline two\n' PUT https://x.test/ Content-Type:text/plain",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snap.HTTPie(); got != tc.want {
				t.Errorf("HTTPie()\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestSnapshotHTTPieStableAcrossCalls: header maps iterate randomly, the
// exported command must not.
func TestSnapshotHTTPieStableAcrossCalls(t *testing.T) {
	snap := &RequestSnapshot{
		Method: "GET", URL: "https://x.test/",
		Headers: http.Header{"A": {"1"}, "B": {"2"}, "C": {"3"}, "D": {"4"}, "E": {"5"}},
	}
	first := snap.HTTPie()
	for i := 0; i < 20; i++ {
		if got := snap.HTTPie(); got != first {
			t.Fatalf("call %d differs:\n %q\n %q", i, got, first)
		}
	}
}

// TestSnapshotHTTPieBinaryBody: bytes that cannot live inside a shell word go
// in as a base64 heredoc, which httpie reads off stdin — the command carries
// the body byte for byte.
func TestSnapshotHTTPieBinaryBody(t *testing.T) {
	body := []byte{0x00, 0x01, 0xff, 'a'}
	snap := &RequestSnapshot{
		Method: "POST", URL: "https://x.test/upload",
		Headers: http.Header{"Content-Type": {"application/octet-stream"}},
		Body:    body,
	}
	got := snap.HTTPie()
	want := "base64 -d <<'IKE_BODY' | http POST https://x.test/upload " +
		"Content-Type:application/octet-stream\n" +
		base64.StdEncoding.EncodeToString(body) + "\nIKE_BODY"
	if got != want {
		t.Errorf("HTTPie()\n got %q\nwant %q", got, want)
	}
	enc := strings.Split(got, "\n")[1]
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("payload does not decode: %v", err)
	}
	if string(raw) != string(body) {
		t.Errorf("decoded %q, want %q", raw, body)
	}
}

// TestSnapshotHTTPieMultipart: an inline multipart body exports as --form
// items, the shared httpfile rule.
func TestSnapshotHTTPieMultipart(t *testing.T) {
	body := "--BOUND\r\nContent-Disposition: form-data; name=\"field\"\r\n\r\nvalue\r\n--BOUND--\r\n"
	snap := &RequestSnapshot{
		Method: "POST", URL: "https://x.test/upload",
		Headers: http.Header{"Content-Type": {"multipart/form-data; boundary=BOUND"}},
		Body:    []byte(body),
	}
	want := `http --form POST https://x.test/upload field=value`
	if got := snap.HTTPie(); got != want {
		t.Errorf("HTTPie()\n got %q\nwant %q", got, want)
	}
}
