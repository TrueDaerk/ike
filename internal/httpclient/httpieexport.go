package httpclient

import (
	"encoding/base64"

	"ike/internal/httpfile"
)

// httpieexport.go is the response side of the httpie export (#2384), the
// pendant of curlexport.go: http.copyAsHttpie exports the *block under the
// caret*, this exports the RequestSnapshot (#1832) behind the response on
// show — method, final URL, headers and body after substitution — so the
// command reproduces the exchange being looked at.
//
// The serialization is httpfile.ExportHTTPie's, exactly as the curl pair
// shares httpfile.ExportCurl: two sources, one set of format rules. Nothing
// is masked here either — an Authorization header goes out as it was sent.

// HTTPie renders the snapshot as an httpie command; "" for a nil snapshot.
//
// A binary body cannot survive inside a shell word, so it is piped in as a
// base64 heredoc — httpie reads a non-tty stdin as the raw body, so the
// command needs no flag for it and stays byte-exact.
func (s *RequestSnapshot) HTTPie() string {
	if s == nil {
		return ""
	}
	req := &httpfile.Request{Method: s.Method, Target: s.URL, Headers: curlHeaders(s.Headers)}
	if binaryBody(s.Body) {
		return "base64 -d <<'IKE_BODY' | " + httpfile.ExportHTTPie(req) + "\n" +
			base64.StdEncoding.EncodeToString(s.Body) + "\nIKE_BODY"
	}
	req.Body = string(s.Body)
	return httpfile.ExportHTTPie(req)
}
