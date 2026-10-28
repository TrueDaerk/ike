package httpclient

import (
	"errors"
	"io/fs"
	"net/url"
	"strconv"
	"strings"

	"ike/internal/httpfile"
)

// ErrorMessage is the one-line human explanation the response pane shows for
// a local failure (#3086) — an unresolved placeholder, an unparsable URL, an
// unreadable body file — in place of the raw Go error. Unlike ClassifyError
// it may name the placeholder or file, since it never leaves the process.
// It returns "" for every other reason, which keep their existing reporting.
func ErrorMessage(err error) string {
	switch ClassifyError(err) {
	case "placeholder":
		var u *httpfile.UnresolvedError
		errors.As(err, &u)
		forms := make([]string, len(u.Names))
		for i, n := range u.Names {
			forms[i] = "${" + n + "}"
		}
		if len(forms) > 1 {
			return "Unresolved placeholders " + strings.Join(forms, ", ") +
				" — define them in the environment file"
		}
		return "Unresolved placeholder " + strings.Join(forms, ", ") +
			" — define it in the environment file"
	case "url":
		var u *url.Error
		errors.As(err, &u)
		return "Invalid URL " + strconv.Quote(u.URL) + " — " + u.Err.Error()
	case "file":
		var p *fs.PathError
		errors.As(err, &p)
		return "Cannot read file " + p.Path + " — " + p.Err.Error()
	}
	return ""
}
