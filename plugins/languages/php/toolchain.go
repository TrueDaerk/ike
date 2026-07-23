package langphp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ike/internal/lang"
)

// toolchain resolves the PHP interpreter (Roadmap 0160): PATH first, then the
// common install locations. The value feeds the settings page's display, the
// explicit [lang.php] interpreter choice, and 0170's terminal PATH shims.
//
// Detect additionally resolves the project's PHP *version* (#1079) and hands
// it to intelephense as intelephense.environment.phpVersion, so diagnostics
// match the project: a composer.json `require.php` constraint wins (its
// minimum bound is the version the project promises to run on), the detected
// interpreter's `php -v` is the fallback. Without either, the server keeps
// its default (latest).
type toolchain struct{}

// phpLook, phpResolve and phpVersionOut are seams for tests: PATH lookup,
// version-manager shim resolution (#650), and the `php -v` first line.
var (
	phpLook       = exec.LookPath
	phpResolve    = lang.ResolveShim
	phpVersionOut = func(bin string) string {
		out, err := exec.Command(bin, "-v").Output()
		if err != nil {
			return ""
		}
		return string(out)
	}
)

// Detect implements lang.Toolchain (#1079): inject the project PHP version.
func (t toolchain) Detect(root string) (map[string]any, bool) {
	v := composerPHPVersion(root)
	if v == "" {
		if bin, ok := t.Interpreter(root); ok {
			v = parsePHPVersion(phpVersionOut(bin))
		}
	}
	if v == "" {
		return nil, false
	}
	return map[string]any{
		"intelephense": map[string]any{
			"environment": map[string]any{"phpVersion": v},
		},
	}, true
}

// composerPHPVersion reads composer.json's require.php constraint at root and
// returns its minimum version bound ("7.3" from "^7.3", ">=7.3 <8.0", "7.3.*"
// …), "" when absent or unparsable. The first version literal in a composer
// constraint is its lower bound across the common operator forms.
func composerPHPVersion(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "composer.json"))
	if err != nil {
		return ""
	}
	var c struct {
		Require map[string]string `json:"require"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	return firstVersion(c.Require["php"])
}

// parsePHPVersion digs the version out of `php -v` output ("PHP 8.2.1 (cli) …").
func parsePHPVersion(out string) string { return firstVersion(out) }

// phpVersionRe matches the first major.minor(.patch) literal.
var phpVersionRe = regexp.MustCompile(`(\d+)\.(\d+)(?:\.\d+)?`)

// firstVersion returns the first version literal in s normalized to
// major.minor.patch (intelephense's expected shape), or "".
func firstVersion(s string) string {
	m := phpVersionRe.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	full := phpVersionRe.FindString(s)
	if strings.Count(full, ".") == 1 {
		full += ".0"
	}
	return full
}

// Interpreter implements lang.InterpreterDetector. A PATH hit that is a
// version-manager shim (mise/asdf) is resolved to the real executable (#650).
func (toolchain) Interpreter(root string) (string, bool) {
	if p, err := phpLook("php"); err == nil {
		return phpResolve(root, p), true
	}
	for _, p := range []string{"/opt/homebrew/bin/php", "/usr/local/bin/php", "/usr/bin/php"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}
