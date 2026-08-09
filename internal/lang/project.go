package lang

import "fmt"

// project.go is the registry side of "New Project" (#1718): a language whose
// Toolchain implements ProjectScaffolder appears as a project type in the
// new-project wizard and owns the actual scaffolding (files, toolchain setup
// such as a Python virtual environment). The wizard UI lives in internal/app;
// this package only answers "which languages, which toolchain options, and
// run the scaffold".

// ProjectOption is one toolchain choice a language offers for a new project —
// for Python e.g. uv versus the stdlib venv module. Unavailable options are
// still listed (with the reason) so the wizard can show what is missing
// instead of hiding it.
type ProjectOption struct {
	ID        string // stable id within the language, e.g. "uv"
	Label     string // human-facing row, e.g. "uv — pyproject.toml + .venv"
	Detail    string // resolved detail (binary path, version), "" when none
	Available bool
	Reason    string // why the option is unavailable, "" when it is
}

// ProjectScaffolder is an optional Toolchain extension: implementing it makes
// the language a project type in the new-project wizard (#1718).
type ProjectScaffolder interface {
	// ProjectOptions lists the toolchain choices for a fresh project. Probing
	// must stay cheap (PATH lookups, no subprocesses) — it runs when the
	// wizard opens.
	ProjectOptions() []ProjectOption
	// ScaffoldProject populates root — a freshly created, empty directory —
	// for the option id. It runs inside an async tea.Cmd, so subprocesses
	// (uv init, python -m venv, go mod init) are fine; a returned error
	// should carry the decisive tool output.
	ScaffoldProject(root, option string) error
}

// ProjectLanguages returns the registered languages offering project
// scaffolding, sorted by ID.
func ProjectLanguages() []Language {
	var out []Language
	for _, l := range All() {
		if _, ok := l.Toolchain.(ProjectScaffolder); ok {
			out = append(out, l)
		}
	}
	return out
}

// ProjectOptions returns langID's toolchain choices, nil when the language is
// unknown or offers no scaffolding.
func ProjectOptions(langID string) []ProjectOption {
	l, ok := ByID(langID)
	if !ok {
		return nil
	}
	s, ok := l.Toolchain.(ProjectScaffolder)
	if !ok {
		return nil
	}
	return s.ProjectOptions()
}

// ScaffoldProject runs langID's scaffolder for option at root.
func ScaffoldProject(langID, root, option string) error {
	l, ok := ByID(langID)
	if !ok {
		return fmt.Errorf("unknown language %q", langID)
	}
	s, ok := l.Toolchain.(ProjectScaffolder)
	if !ok {
		return fmt.Errorf("%s offers no project scaffolding", langID)
	}
	return s.ScaffoldProject(root, option)
}
