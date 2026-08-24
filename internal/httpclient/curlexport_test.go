package httpclient

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
)

// TestSnapshotCurlQuoting covers the serialization of a snapshot into a curl
// command (#2059), the quoting edge cases first: the command must survive a
// shell verbatim.
func TestSnapshotCurlQuoting(t *testing.T) {
	cases := []struct {
		name string
		snap *RequestSnapshot
		want string
	}{
		{
			name: "plain get",
			snap: &RequestSnapshot{Method: "GET", URL: "https://api.example.com/things"},
			want: `curl https://api.example.com/things`,
		},
		{
			name: "method and headers, sorted",
			snap: &RequestSnapshot{
				Method: "POST", URL: "https://api.example.com/things?q=a b",
				Headers: http.Header{
					"X-Trace":      {"abc"},
					"Content-Type": {"application/json"},
				},
			},
			want: `curl 'https://api.example.com/things?q=a b' -X POST -H 'Content-Type: application/json' -H 'X-Trace: abc'`,
		},
		{
			name: "body with single quotes",
			snap: &RequestSnapshot{
				Method: "POST", URL: "https://x.test/",
				Body: []byte(`{"who":"o'brien"}`),
			},
			want: `curl https://x.test/ -X POST --data-raw '{"who":"o'\''brien"}'`,
		},
		{
			name: "body with newlines",
			snap: &RequestSnapshot{
				Method: "PUT", URL: "https://x.test/",
				Body: []byte("line one\nline two\n"),
			},
			want: "curl https://x.test/ -X PUT --data-raw 'line one\nline two\n'",
		},
		{
			name: "repeated header values keep both",
			snap: &RequestSnapshot{
				Method: "GET", URL: "https://x.test/",
				Headers: http.Header{"Cookie": {"a=1", "b=2"}},
			},
			want: `curl https://x.test/ -H 'Cookie: a=1' -H 'Cookie: b=2'`,
		},
		{
			name: "basic auth becomes -u, other authorization stays a header",
			snap: &RequestSnapshot{
				Method: "GET", URL: "https://x.test/",
				Headers: http.Header{"Authorization": {"Basic " + base64.StdEncoding.EncodeToString([]byte("me:s3cr t"))}},
			},
			want: `curl https://x.test/ -u 'me:s3cr t'`,
		},
		{
			name: "bearer token is exported as sent, never masked",
			snap: &RequestSnapshot{
				Method: "GET", URL: "https://x.test/",
				Headers: http.Header{"Authorization": {"Bearer tok-123"}},
			},
			want: `curl https://x.test/ -H 'Authorization: Bearer tok-123'`,
		},
		{
			name: "host override travels as a header",
			snap: &RequestSnapshot{
				Method: "GET", URL: "http://127.0.0.1:8080/", Headers: http.Header{"Host": {"api.internal"}},
			},
			want: `curl http://127.0.0.1:8080/ -H 'Host: api.internal'`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.snap.Curl(); got != tc.want {
				t.Errorf("Curl()\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestSnapshotCurlStableAcrossCalls: header maps iterate randomly, the
// exported command must not.
func TestSnapshotCurlStableAcrossCalls(t *testing.T) {
	snap := &RequestSnapshot{
		Method: "GET", URL: "https://x.test/",
		Headers: http.Header{"A": {"1"}, "B": {"2"}, "C": {"3"}, "D": {"4"}, "E": {"5"}},
	}
	first := snap.Curl()
	for i := 0; i < 20; i++ {
		if got := snap.Curl(); got != first {
			t.Fatalf("call %d differs:\n %q\n %q", i, got, first)
		}
	}
}

// TestSnapshotCurlBinaryBody: bytes that cannot live inside a shell word are
// piped in through base64 instead of being pasted raw (#2059).
func TestSnapshotCurlBinaryBody(t *testing.T) {
	body := []byte{0x00, 0x01, 0xff, 'a'}
	snap := &RequestSnapshot{
		Method: "POST", URL: "https://x.test/upload",
		Headers: http.Header{"Content-Type": {"application/octet-stream"}},
		Body:    body,
	}
	got := snap.Curl()
	want := "base64 -d <<'IKE_BODY' | curl https://x.test/upload -X POST " +
		"-H 'Content-Type: application/octet-stream' --data-binary @-\n" +
		base64.StdEncoding.EncodeToString(body) + "\nIKE_BODY"
	if got != want {
		t.Errorf("Curl()\n got %q\nwant %q", got, want)
	}
	// The encoded payload must decode back to the original bytes.
	enc := strings.Split(got, "\n")[1]
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("payload does not decode: %v", err)
	}
	if string(raw) != string(body) {
		t.Errorf("decoded %q, want %q", raw, body)
	}
}

// TestSnapshotCurlMultipart: an inline multipart body exports as -F parts,
// the shared httpfile rule.
func TestSnapshotCurlMultipart(t *testing.T) {
	body := "--BOUND\r\nContent-Disposition: form-data; name=\"field\"\r\n\r\nvalue\r\n--BOUND--\r\n"
	snap := &RequestSnapshot{
		Method: "POST", URL: "https://x.test/upload",
		Headers: http.Header{"Content-Type": {"multipart/form-data; boundary=BOUND"}},
		Body:    []byte(body),
	}
	got := snap.Curl()
	if !strings.Contains(got, "-F field=value") {
		t.Errorf("Curl() = %q, want a -F part", got)
	}
	if strings.Contains(got, "boundary=BOUND") {
		t.Errorf("Curl() = %q, must not pin curl's own boundary", got)
	}
}

// TestSnapshotCurlNil: no snapshot, no command — the caller reports that
// instead of copying "curl".
func TestSnapshotCurlNil(t *testing.T) {
	var snap *RequestSnapshot
	if got := snap.Curl(); got != "" {
		t.Errorf("Curl() = %q, want empty", got)
	}
}
