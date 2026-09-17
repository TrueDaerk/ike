package langgo

import (
	"testing"

	"ike/internal/lang"
)

// TestGoplsCompleteUnimportedOn guards #2610: gopls must offer members of
// packages the file does not import yet (the import arrives as an
// additionalTextEdit), so completeUnimported is pinned on in the server
// settings next to the inlay-hint switches.
func TestGoplsCompleteUnimportedOn(t *testing.T) {
	l, ok := lang.ByID("go")
	if !ok || l.Server == nil {
		t.Fatal("go: no language/server registered")
	}
	if v, ok := l.Server.Settings["completeUnimported"].(bool); !ok || !v {
		t.Errorf("settings = %v, want completeUnimported: true", l.Server.Settings)
	}
	if _, ok := l.Server.Settings["hints"]; !ok {
		t.Error("inlay-hint settings (#171) must stay alongside")
	}
}
