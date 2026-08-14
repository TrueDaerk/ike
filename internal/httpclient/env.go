package httpclient

import (
	"context"
	"net"
	"os"
	"time"

	"ike/internal/httpfile"
)

// lookupEnv adapts os.LookupEnv to the httpfile lookup signature.
func lookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

// variables builds the resolution chain of one dispatch (#1867): the caller's
// user variables (in-file `@name=value` definitions and the selected
// http-client.env.json environment) closed by the process environment. The
// caller's Vars is copied rather than filled in place, so an Options value
// reused for several dispatches is never mutated.
func variables(opts Options) *httpfile.Vars {
	vars := httpfile.Vars{}
	if opts.Vars != nil {
		vars = *opts.Vars
	}
	if vars.Lookup == nil {
		vars.Lookup = opts.Lookup
	}
	if vars.Lookup == nil {
		vars.Lookup = lookupEnv
	}
	return &vars
}

// timeoutDialer applies a .curlrc connect-timeout to connection setup only.
type timeoutDialer struct {
	timeout time.Duration
}

func (d *timeoutDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, network, addr)
}
