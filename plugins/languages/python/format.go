package langpython

import "ike/internal/format"

// format.go wires Python's default external formatters (Roadmap 0470, #1405):
// pyright advertises no formatting provider, so reformat runs `ruff format`
// when ruff is available, else `black`. Project-local virtualenv installs
// win over PATH installs — the relative candidates resolve against the
// process working directory, which is the project root (mirroring the
// venv-first interpreter resolution in toolchain.go). Both tools read the
// project's pyproject/ruff config themselves: they run with the project root
// as cwd and get the real file name via --stdin-filename/--stdin-filepath.
// `[format.python]` overrides the whole chain (#1402).

func init() {
	ruff := func(command string) format.External {
		return format.External{
			Command:   command,
			Args:      []string{"format", "--stdin-filename", "${FILE}", "-"},
			RangeArgs: []string{"format", "--range", "${START_LINE}-${END_LINE}", "--stdin-filename", "${FILE}", "-"},
			Install:   "pip install ruff",
		}
	}
	black := func(command string) format.External {
		return format.External{
			Command: command,
			Args:    []string{"-q", "--stdin-filename", "${FILE}", "-"},
			Install: "pip install black",
		}
	}
	format.RegisterExternalDefaults("python",
		ruff(".venv/bin/ruff"),
		ruff("venv/bin/ruff"),
		ruff("ruff"),
		black(".venv/bin/black"),
		black("venv/bin/black"),
		black("black"),
	)
}
