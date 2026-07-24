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
	// Watched files (#1144): without the dynamicRegistration invitation
	// Intelephense never registers its globs and externally created files
	// stay out of its index.
	if !strings.Contains(s, `"didChangeWatchedFiles":{"dynamicRegistration":true}`) {
		t.Fatalf("workspace.didChangeWatchedFiles missing:\n%s", s)
	}
}
