package manager

import (
	"errors"
	"strings"
	"testing"

	"ike/internal/lsp"
)

// TestStartupErrorExtractsStderrTail guards #1062: a server that dies during
// the handshake surfaces its decisive stderr line, not the transport error.
func TestStartupErrorExtractsStderrTail(t *testing.T) {
	closed := errors.New("jsonrpc: connection closed")
	stderr := func() string {
		return "some noise\nERROR operation failed error=the LSP is not part of this build, please consult the documentation about enabling the functionality\n"
	}
	got := startupError(closed, stderr)
	if !strings.Contains(got.Error(), "the LSP is not part of this build") {
		t.Fatalf("startupError = %q, want the stderr complaint", got)
	}
	// No recognizable line / no stderr capture: the original error stands.
	if got := startupError(closed, func() string { return "" }); got != closed {
		t.Fatalf("empty stderr: got %q", got)
	}
	if got := startupError(closed, nil); got != closed {
		t.Fatalf("nil stderr fn: got %q", got)
	}
}

// TestStatusForErrPointsAtLog guards #1062: the launch-failure notification
// names the open-log path, like the repeated-crash disable message (#715).
func TestStatusForErrPointsAtLog(t *testing.T) {
	text, kind := statusForErr("taplo", errors.New("the LSP is not part of this build"))
	if !strings.Contains(text, "taplo: the LSP is not part of this build") ||
		!strings.Contains(text, `"LSP: Show Server Log"`) {
		t.Fatalf("text = %q", text)
	}
	if kind != lsp.ServerEventError {
		t.Fatalf("kind = %v want ServerEventError", kind)
	}
}
