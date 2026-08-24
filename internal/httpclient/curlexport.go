package httpclient

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"sort"
	"unicode/utf8"

	"ike/internal/httpfile"
)

// curlexport.go renders a captured request as a runnable curl command
// (#2059). http.copyAsCurl (#1994) exports the *block under the caret*; this
// is the response side of the same conversion: what the viewer shows came
// from a RequestSnapshot (#1832) — method, final URL, headers and body after
// substitution — so the exported command reproduces exactly the exchange on
// screen, including a re-sent or a stored one whose .http file has changed
// since.
//
// The serialization itself is httpfile.ExportCurl's, so both directions share
// one set of rules (basic-auth becomes -u, multipart becomes -F, values are
// shell-quoted). Nothing is masked: an Authorization header is exported as it
// was sent, exactly like the editor-side export — an explicit command, never
// something offered on its own.

// Curl renders the snapshot as a curl command; "" for a nil snapshot.
//
// A binary body cannot survive inside a shell word, so it is exported as a
// base64 heredoc piped into `curl --data-binary @-` instead of being pasted
// raw into the command line — the command stays runnable and byte-exact.
func (s *RequestSnapshot) Curl() string {
	if s == nil {
		return ""
	}
	req := &httpfile.Request{Method: s.Method, Target: s.URL, Headers: curlHeaders(s.Headers)}
	if binaryBody(s.Body) {
		cmd := httpfile.ExportCurl(req) + " --data-binary @-"
		return "base64 -d <<'IKE_BODY' | " + cmd + "\n" +
			base64.StdEncoding.EncodeToString(s.Body) + "\nIKE_BODY"
	}
	req.Body = string(s.Body)
	return httpfile.ExportCurl(req)
}

// curlHeaders flattens the snapshot's header map into the parser's ordered
// header list. Go's map iteration is random, so the names are sorted: the
// same request must always export the same command.
func curlHeaders(h http.Header) []httpfile.Header {
	if len(h) == 0 {
		return nil
	}
	names := make([]string, 0, len(h))
	for n := range h {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]httpfile.Header, 0, len(names))
	for _, n := range names {
		for _, v := range h[n] {
			out = append(out, httpfile.Header{Name: n, Value: v})
		}
	}
	return out
}

// binaryBody reports whether a body cannot be carried as shell text: invalid
// UTF-8 or an embedded NUL, the same rule the viewer and the history store
// use to decide a payload is not text.
func binaryBody(b []byte) bool {
	return len(b) > 0 && (bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b))
}
