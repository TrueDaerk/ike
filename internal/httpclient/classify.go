package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"
)

// ClassifyError reduces a dispatch failure to a small closed vocabulary for
// telemetry (#2631): the response pane already knows why a request failed,
// but until now that reason never left the process. The result is
// structural only — never the host, URL or the error text itself.
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}

	var timedOut *TimeoutError
	if errors.As(err, &timedOut) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}

	var tlsErr tls.RecordHeaderError
	if errors.As(err, &tlsErr) {
		return "tls"
	}
	var certErr *x509.UnknownAuthorityError
	if errors.As(err, &certErr) {
		return "tls"
	}
	var certInvalidErr *x509.CertificateInvalidError
	if errors.As(err, &certInvalidErr) {
		return "tls"
	}
	var hostnameErr *x509.HostnameError
	if errors.As(err, &hostnameErr) {
		return "tls"
	}

	if errors.Is(err, syscall.ECONNREFUSED) {
		return "refused"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return "reset"
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return "timeout"
		}
		return "other"
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}

	return "other"
}
