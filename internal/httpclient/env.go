package httpclient

import (
	"context"
	"net"
	"os"
	"time"
)

// lookupEnv adapts os.LookupEnv to the httpfile lookup signature.
func lookupEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

// timeoutDialer applies a .curlrc connect-timeout to connection setup only.
type timeoutDialer struct {
	timeout time.Duration
}

func (d *timeoutDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return (&net.Dialer{Timeout: d.timeout}).DialContext(ctx, network, addr)
}
