package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestClientCapabilitiesAdvertisePublishDiagnostics guards #1060: vtsls gates
// its push diagnostics on textDocument.publishDiagnostics — the initialize
// payload must carry the entry.
func TestClientCapabilitiesAdvertisePublishDiagnostics(t *testing.T) {
	raw, err := json.Marshal(clientCapabilities())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"publishDiagnostics":{"relatedInformation":true}`) {
		t.Fatalf("initialize capabilities missing publishDiagnostics:\n%s", s)
	}
	// The pyright interpreter path (#563) keeps its gate.
	if !strings.Contains(s, `"configuration":true`) {
		t.Fatalf("workspace.configuration gone missing:\n%s", s)
	}
}
