package langweb

import (
	"testing"

	"ike/internal/lang"
)

// TestWebLanguagesRegistered guards #855: the web languages resolve by
// extension to their evaluated default servers.
func TestWebLanguagesRegistered(t *testing.T) {
	for _, tc := range []struct {
		path    string
		id      string
		command string
	}{
		{"/p/app.ts", "typescript", "vtsls"},
		{"/p/app.tsx", "typescript", "vtsls"},
		{"/p/app.js", "typescript", "vtsls"},
		{"/p/index.html", "html", "vscode-html-language-server"},
		{"/p/style.css", "css", "vscode-css-language-server"},
		{"/p/style.scss", "css", "vscode-css-language-server"},
	} {
		l, ok := lang.ByPath(tc.path)
		if !ok {
			t.Errorf("%s: no language registered", tc.path)
			continue
		}
		if l.ID != tc.id || l.Server == nil || l.Server.Command != tc.command {
			t.Errorf("%s → %s/%v, want %s/%s", tc.path, l.ID, l.Server, tc.id, tc.command)
		}
	}
}

// TestWebFormatterEnabled guards #1507: the extracted VS Code html/css
// servers only advertise documentFormattingProvider when the
// initializationOptions carry provideFormatter: true — without it,
// Reformat File has no provider for HTML/CSS.
func TestWebFormatterEnabled(t *testing.T) {
	for _, id := range []string{"html", "css"} {
		l, ok := lang.ByID(id)
		if !ok || l.Server == nil {
			t.Errorf("%s: no language/server registered", id)
			continue
		}
		if v, ok := l.Server.Settings["provideFormatter"].(bool); !ok || !v {
			t.Errorf("%s server settings = %v, want provideFormatter: true", id, l.Server.Settings)
		}
	}
}
