package httpclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"
)

// TestClassifyError covers every vocabulary value ClassifyError can return
// (#2631), each with a representative error shape drawn from the paths
// dispatch.go and timeout.go actually produce.
func TestClassifyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"timeout error", &TimeoutError{Key: "k", Limit: 30 * time.Second}, "timeout"},
		{"deadline exceeded", context.DeadlineExceeded, "timeout"},
		{"net timeout", fakeNetTimeoutErr{}, "timeout"},
		{"canceled", context.Canceled, "canceled"},
		{"dns error", &net.DNSError{Err: "no such host", Name: "example.invalid"}, "dns"},
		{"tls record header", tls.RecordHeaderError{Msg: "bad header"}, "tls"},
		{"unknown authority", &x509.UnknownAuthorityError{}, "tls"},
		{"certificate invalid", &x509.CertificateInvalidError{}, "tls"},
		{"hostname mismatch", &x509.HostnameError{}, "tls"},
		{"connection refused", syscall.ECONNREFUSED, "refused"},
		{"connection reset", syscall.ECONNRESET, "reset"},
		{"op error timeout", &net.OpError{Op: "dial", Err: fakeNetTimeoutErr{}}, "timeout"},
		{"op error other", &net.OpError{Op: "dial", Err: errors.New("boom")}, "other"},
		{"other", errors.New("boom"), "other"},
		{"nil", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyError(tt.err); got != tt.want {
				t.Errorf("ClassifyError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

type fakeNetTimeoutErr struct{}

func (fakeNetTimeoutErr) Error() string   { return "i/o timeout" }
func (fakeNetTimeoutErr) Timeout() bool   { return true }
func (fakeNetTimeoutErr) Temporary() bool { return false }
