package toolcatalog

import (
	"os"
	"path/filepath"
	"sync"
)

// prettier.go is the formatter install offer (#3023): the reformat chains of
// JSON, the web languages, YAML, Ansible and Markdown prefer prettier, so IKE
// offers to install it — from the Formatters settings page and from the
// first-reformat prompt — through the same async Install runner the tool
// panes use. It is not part of the curated tool-pane catalog (All / Offered):
// prettier is a formatter, not a TUI to open in a pane.

// PrettierInstall is the install command, shown as text whenever IKE cannot
// run it itself (npm missing).
const PrettierInstall = "npm install -g prettier"

// Prettier is the install entry: a global npm install, the one scope the
// offer supports.
var Prettier = Entry{
	Name:        "prettier",
	Command:     "prettier",
	Description: "Code formatter for JSON, JS/TS, CSS, HTML, YAML and Markdown",
	Recipes:     [][]string{{"npm", "install", "-g", "prettier"}},
}

// NpmAvailable reports whether npm — the only installer the prettier offer
// uses — is on PATH.
func NpmAvailable() bool {
	_, err := LookPath("npm")
	return err == nil
}

// PrettierLocal reports whether the project at root has a project-local
// prettier (node_modules/.bin/prettier) — the chains check it before PATH,
// so it counts as installed.
func PrettierLocal(root string) bool {
	if root == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(root, "node_modules", ".bin", "prettier"))
	return err == nil && !st.IsDir()
}

// installs tracks the async installs by entry name (#3023): whether one is
// running — so a second request never starts a second installer — and how
// the last one ended, for surfaces that show the state instead of a toast.
var installs = struct {
	sync.Mutex
	running map[string]bool
	last    map[string]InstallResultMsg
}{running: map[string]bool{}, last: map[string]InstallResultMsg{}}

// Installing reports whether an install of the named entry is in flight.
func Installing(name string) bool {
	installs.Lock()
	defer installs.Unlock()
	return installs.running[name]
}

// LastInstall returns the named entry's last finished install, false when
// none finished this session.
func LastInstall(name string) (InstallResultMsg, bool) {
	installs.Lock()
	defer installs.Unlock()
	r, ok := installs.last[name]
	return r, ok
}

// beginInstall marks name running; false when an install already is.
func beginInstall(name string) bool {
	installs.Lock()
	defer installs.Unlock()
	if installs.running[name] {
		return false
	}
	installs.running[name] = true
	return true
}

// endInstall records the finished install and clears the running mark.
func endInstall(r InstallResultMsg) {
	installs.Lock()
	defer installs.Unlock()
	delete(installs.running, r.Name)
	installs.last[r.Name] = r
}

// ResetInstallsForTest drops the install tracking (tests only).
func ResetInstallsForTest() {
	installs.Lock()
	defer installs.Unlock()
	installs.running = map[string]bool{}
	installs.last = map[string]InstallResultMsg{}
}
