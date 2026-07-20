// Package toolcatalog holds the curated list of commonly used TUI tools that
// IKE can offer to set up as custom tool panes (#751–#753, #759): the
// post-tour setup step and the Tools settings page both draw from it. Each
// entry names the binary, the [[tools.custom]] config it maps to, and the
// install recipes to try when the binary is missing.
package toolcatalog

import (
	"errors"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Entry is one curated tool.
type Entry struct {
	Name        string
	Command     string
	Args        []string
	Placement   string
	Description string
	// Requires is a binary that must be on PATH for the entry to be offered
	// at all (lazydocker without Docker is noise); "" offers unconditionally.
	Requires string
	// Recipes are candidate install argvs, tried in order: the first whose
	// tool (argv[0]) resolves on PATH is used. Mirrors the LSP install
	// recipes (plain argv, shelled out as-is).
	Recipes [][]string
}

// LookPath resolves a binary on PATH; a seam for tests.
var LookPath = exec.LookPath

// RunInstall executes an install recipe; a seam for tests.
var RunInstall = func(argv []string) ([]byte, error) {
	return exec.Command(argv[0], argv[1:]...).CombinedOutput()
}

// catalog is the curated list. Ordering is display order: the issue trio
// first, then further common TUIs.
var catalog = []Entry{
	{
		Name:        "lazygit",
		Command:     "lazygit",
		Placement:   "bottom",
		Description: "Git workflow TUI (staging, commits, branches, log)",
		Recipes: [][]string{
			{"brew", "install", "lazygit"},
			{"go", "install", "github.com/jesseduffield/lazygit@latest"},
		},
	},
	{
		Name:        "lazydocker",
		Command:     "lazydocker",
		Placement:   "bottom",
		Description: "Docker containers, images and logs TUI",
		Requires:    "docker",
		Recipes: [][]string{
			{"brew", "install", "lazydocker"},
			{"go", "install", "github.com/jesseduffield/lazydocker@latest"},
		},
	},
	{
		Name:        "sqlit",
		Command:     "sqlit",
		Placement:   "bottom",
		Description: "SQL database client TUI (Maxteabag/sqlit)",
		Recipes: [][]string{
			{"pipx", "install", "sqlit-tui"},
			{"uv", "tool", "install", "sqlit-tui"},
		},
	},
	{
		Name:        "k9s",
		Command:     "k9s",
		Placement:   "bottom",
		Description: "Kubernetes cluster TUI",
		Requires:    "kubectl",
		Recipes: [][]string{
			{"brew", "install", "k9s"},
			{"go", "install", "github.com/derailed/k9s@latest"},
		},
	},
	{
		Name:        "htop",
		Command:     "htop",
		Placement:   "bottom",
		Description: "Interactive process viewer",
		Recipes: [][]string{
			{"brew", "install", "htop"},
		},
	},
	{
		Name:        "btop",
		Command:     "btop",
		Placement:   "bottom",
		Description: "Resource monitor (CPU, memory, network, disks)",
		Recipes: [][]string{
			{"brew", "install", "btop"},
		},
	},
}

// All returns the full catalog.
func All() []Entry { return append([]Entry(nil), catalog...) }

// Offered returns the catalog entries whose requirement gate is satisfied.
func Offered() []Entry {
	var out []Entry
	for _, e := range catalog {
		if e.Requires != "" {
			if _, err := LookPath(e.Requires); err != nil {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// Installed reports whether the entry's binary is on PATH.
func (e Entry) Installed() bool {
	_, err := LookPath(e.Command)
	return err == nil
}

// InstallArgv picks the first recipe whose install tool is on PATH; false
// when none is available on this system.
func (e Entry) InstallArgv() ([]string, bool) {
	for _, r := range e.Recipes {
		if len(r) == 0 {
			continue
		}
		if _, err := LookPath(r[0]); err == nil {
			return r, true
		}
	}
	return nil, false
}

// installers names the recipe tools, for the "nothing available" error.
func (e Entry) installers() string {
	var names []string
	for _, r := range e.Recipes {
		if len(r) > 0 {
			names = append(names, r[0])
		}
	}
	return strings.Join(names, " or ")
}

// InstallResultMsg reports one finished install attempt. Err is nil on
// success; Detail carries the recipe run or the output tail on failure.
type InstallResultMsg struct {
	Name   string
	Err    error
	Detail string
}

// Install returns a command that installs the entry's binary via the first
// available recipe and re-verifies PATH afterwards (an exit-0 installer whose
// target still does not resolve is a failure, not a success). Already
// installed is an immediate success.
func Install(e Entry) tea.Cmd {
	return func() tea.Msg {
		if e.Installed() {
			return InstallResultMsg{Name: e.Name}
		}
		argv, ok := e.InstallArgv()
		if !ok {
			return InstallResultMsg{
				Name: e.Name,
				Err:  errors.New("no supported installer found (needs " + e.installers() + ")"),
			}
		}
		out, err := RunInstall(argv)
		ran := strings.Join(argv, " ")
		if err != nil {
			return InstallResultMsg{Name: e.Name, Err: err, Detail: ran + ": " + outputTail(out)}
		}
		if !e.Installed() {
			return InstallResultMsg{
				Name:   e.Name,
				Err:    errors.New(e.Command + " still not on PATH after install"),
				Detail: ran,
			}
		}
		return InstallResultMsg{Name: e.Name, Detail: ran}
	}
}

// outputTail keeps the last few lines of installer output for the error toast.
func outputTail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.TrimSpace(strings.Join(lines, " · "))
}
