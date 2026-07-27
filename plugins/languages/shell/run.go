package langshell

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ike/internal/lang"
)

var (
	_ lang.Toolchain          = toolchain{}
	_ lang.RunCommandProvider = toolchain{}
)

// toolchain exists to carry the run-command seam (#1225); shell has no
// language-server settings to inject.
type toolchain struct{}

// Detect implements lang.Toolchain: nothing to hand to the server.
func (toolchain) Detect(string) (map[string]any, bool) { return nil, false }

// shellLook is the PATH-lookup seam for tests.
var shellLook = exec.LookPath

// RunCommand implements lang.RunCommandProvider (0350, #1225): `<interpreter>
// <file>`, deliberately not executing the file via its own shebang (a scratch
// is not executable and IKE never chmods user files). Resolution order:
// explicit [lang.shell] interpreter (arrives pre-resolved as interpreter) >
// the file's shebang interpreter when that binary is on PATH > the
// extension's natural shell (.bash → bash, .zsh → zsh, .sh → sh). The PATH
// check on the shebang shell is what keeps a `#!/usr/bin/fish` file runnable
// on a machine without fish — it falls back instead of producing an argv
// pointing at a missing binary.
func (toolchain) RunCommand(_ string, spec lang.RunSpec, interpreter string) ([]string, bool) {
	if interpreter == "" {
		interpreter = fileShell(spec.File)
	}
	return append([]string{interpreter, spec.File}, spec.Args...), true
}

// fileShell resolves the shell to run file with: its shebang interpreter when
// present and installed, the extension's natural shell otherwise.
func fileShell(file string) string {
	if s := lang.ShebangInterpreter(firstLine(file)); s != "" {
		if _, err := shellLook(s); err == nil {
			return s
		}
	}
	return extShell(file)
}

// extShell maps a file name to its natural shell, mirroring the extensions
// and rc base names the language registers; sh is the always-present default.
func extShell(file string) string {
	base := filepath.Base(file)
	switch base {
	case ".bashrc", ".bash_profile":
		return "bash"
	case ".zshrc", ".zprofile":
		return "zsh"
	}
	switch filepath.Ext(base) {
	case ".bash":
		return "bash"
	case ".zsh":
		return "zsh"
	}
	return "sh"
}

// firstLine reads file's first line ("" on any error — no shebang then).
func firstLine(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return ""
	}
	return strings.TrimRight(sc.Text(), "\r")
}
