package langweb

import (
	"os"
	"path/filepath"
)

// tsToolchain points vtsls at the workspace TypeScript when the project
// vendors one (#1079): VS Code's "use workspace version". typescript.tsdk
// makes diagnostics and language features match the project's TS version
// instead of the version bundled with vtsls. Without a vendored TypeScript
// the server keeps its bundled default.
type tsToolchain struct{}

// tsStat is a seam for tests.
var tsStat = os.Stat

// Detect implements lang.Toolchain.
func (tsToolchain) Detect(root string) (map[string]any, bool) {
	lib := filepath.Join(root, "node_modules", "typescript", "lib")
	if st, err := tsStat(filepath.Join(lib, "tsserverlibrary.js")); err != nil || st.IsDir() {
		return nil, false
	}
	return map[string]any{
		"typescript": map[string]any{"tsdk": lib},
	}, true
}
