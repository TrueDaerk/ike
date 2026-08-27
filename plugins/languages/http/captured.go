package langhttp

// captured.go feeds the third rung of the variable chain into "{{" completion
// (#2158): the values a `# @capture name = <jq>` directive took out of an
// earlier response (#1993). They are stored with the response that produced
// them (.ike/http/*.json), so nothing is kept in memory here — the names a
// request chain defines are read back from the same history the dispatch
// resolves them from, which is what makes a captured variable show up the
// moment its request has run.

import (
	"os"
	"path/filepath"

	"ike/internal/httpfile"
	"ike/internal/httphistory"
)

// httpHistoryDir is internal/app's httpHistoryDir, duplicated rather than
// imported for the same reason envselect.go duplicates the environment
// store's path: a language plugin has no business importing the application
// package to read one project-local file.
func httpHistoryDir() string {
	base := ".ike"
	if d := os.Getenv("IKE_CONFIG_DIR"); d != "" {
		base = d
	}
	return filepath.Join(base, "http")
}

// capturedVars returns the values earlier responses of source captured,
// mirroring Model.httpCaptured: only the requests that actually declare a
// directive are looked up, which keeps the read to the handful of history
// files that can contribute anything. A buffer with no file (#2033) has no
// history to read.
func capturedVars(source string, f *httpfile.File) map[string]string {
	if source == "" {
		return nil
	}
	var keys []string
	for _, r := range f.Requests {
		if len(r.Captures) > 0 {
			keys = append(keys, r.Key())
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return httphistory.New(httpHistoryDir()).Captured(source, keys)
}
