package lsp

import (
	"os"
	"os/exec"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"ike/internal/config"
	"ike/internal/host"
	"ike/internal/lang"
	ilsp "ike/internal/lsp"
)

// install.go is the missing-server install helper (Roadmap 0180, #131).
// Activation implies installation: when a language's server binary is missing
// — detected on the first attempt to launch it (file open) — the plugin's
// install recipe runs automatically in the background, gated by
// lsp.auto_install (default true). The Language Servers settings page offers
// the same install manually (the fallback, and the retry path after a
// failure). Guard rails: never two installs of one language concurrently, and
// no automatic retry after a failed attempt — a failure would otherwise loop
// on every file open.

// installRunner executes one install recipe, returning its combined output.
// Injectable for tests.
type installRunner func(name string, args ...string) ([]byte, error)

// execInstall is the real runner.
func execInstall(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// installer serialises install attempts per language.
type installer struct {
	mu       sync.Mutex
	inflight map[string]bool
	failed   map[string]bool
	run      installRunner
}

func newInstaller() *installer {
	return &installer{inflight: map[string]bool{}, failed: map[string]bool{}, run: execInstall}
}

// begin reserves an install slot for lang. It refuses while an install is
// already running, and — on the automatic path — after a failed attempt
// (backoff); a manual install is always allowed to retry.
func (i *installer) begin(langID string, manual bool) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.inflight[langID] {
		return false
	}
	if !manual && i.failed[langID] {
		return false
	}
	i.inflight[langID] = true
	return true
}

// finish releases the slot and records the outcome for the auto backoff.
func (i *installer) finish(langID string, err error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.inflight, langID)
	i.failed[langID] = err != nil
}

// installMissing kicks the install recipe for every enabled language whose
// server binary is missing (#133: enabling a language plugin from the plugin
// manager triggers installation). It reads the configuration fresh off disk —
// the toggle's write-back may still be racing the in-memory reload — and
// reports when there is nothing to do.
func (b *bridge) installMissing(h host.API) tea.Cmd {
	b.ensure(h)
	return func() tea.Msg {
		cfg, _ := config.Load(config.Discover("."))
		kicked := 0
		for _, l := range lang.All() {
			if l.Server == nil || len(l.Server.Install) == 0 {
				continue
			}
			if !cfg.LSP.Enabled || !serverEnabled(cfg, l.ID) || !pluginEnabled(cfg, "lang-"+l.ID) {
				continue
			}
			if _, err := exec.LookPath(l.Server.Command); err == nil {
				continue
			}
			go func(id string) { _ = b.installLang(id)() }(l.ID)
			kicked++
		}
		if kicked == 0 {
			return ilsp.ServerStatusMsg{Text: "all enabled language servers are installed", Kind: ilsp.ServerEventInfo}
		}
		return nil
	}
}

// autoInstallEnabled reads lsp.auto_install live.
func autoInstallEnabled() bool {
	c := config.Get()
	return c == nil || c.LSP.AutoInstall
}

// outputTail returns the last few lines of an install's combined output, for
// the failure toast.
func outputTail(out []byte) string {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) > 3 {
		lines = lines[len(lines)-3:]
	}
	return strings.TrimSpace(strings.Join(lines, " · "))
}

// autoInstall is the automatic path, called from the file-open goroutine after
// a launch failed with a missing binary. It honours the opt-out and the
// backoff, and delivers its result via Send (we are off the Update loop).
func (b *bridge) autoInstall(langID, path string) {
	if !autoInstallEnabled() {
		return
	}
	if msg := b.runInstall(langID, path, false); msg != nil {
		b.send(msg)
	}
}

// installLang is the manual install action of the Language Servers settings
// page (#130). The work runs inside the returned tea.Cmd (#123 rules); the
// result message is returned, never Sent.
func (b *bridge) installLang(langID string) tea.Cmd {
	return func() tea.Msg {
		path := ""
		if cur, _, _ := b.cur(); cur != "" {
			if l, ok := lang.ByPath(cur); ok && l.ID == langID {
				path = cur
			}
		}
		msg := b.runInstall(langID, path, true)
		if msg == nil {
			return ilsp.ServerStatusMsg{Lang: langID, Text: langID + " server install already running", Kind: ilsp.ServerEventInfo}
		}
		return msg
	}
}

// runInstall executes the language's install recipe synchronously (callers
// are goroutines or tea.Cmds, never the Update loop) and returns the result
// message. nil means nothing ran (guarded). On success the triggering
// document is re-opened so the fresh server starts without further
// interaction.
func (b *bridge) runInstall(langID, path string, manual bool) tea.Msg {
	l, ok := lang.ByID(langID)
	if !ok || l.Server == nil || len(l.Server.Install) == 0 {
		if manual {
			return ilsp.ServerStatusMsg{Lang: langID, Text: "no install recipe for " + langID + " — install its server manually", Kind: ilsp.ServerEventWarn}
		}
		return nil
	}
	if !b.inst.begin(langID, manual) {
		return nil
	}
	recipe := l.Server.Install
	b.send(ilsp.ServerStatusMsg{Lang: langID,
		Text: "installing " + l.Server.Command + " (" + strings.Join(recipe, " ") + ")…",
		Kind: ilsp.ServerEventInfo})
	out, err := b.inst.run(recipe[0], recipe[1:]...)
	b.inst.finish(langID, err)
	if err != nil {
		text := l.Server.Command + " install failed: " + err.Error()
		if tail := outputTail(out); tail != "" {
			text += " — " + tail
		}
		return ilsp.ServerStatusMsg{Lang: langID, Text: text, Kind: ilsp.ServerEventError}
	}
	// Success: re-open the triggering document so the server starts now; other
	// documents pick the server up on their next event.
	if path != "" {
		if data, rerr := os.ReadFile(path); rerr == nil {
			if mgr := b.manager(); mgr != nil {
				_ = mgr.Open(path, langID, string(data))
			}
		}
	}
	return ilsp.ServerStatusMsg{Lang: langID, Text: l.Server.Command + " installed", Kind: ilsp.ServerEventInfo}
}

// send delivers a message into the program loop when the bridge is activated;
// a bridge without a captured host (no file opened yet) drops it.
func (b *bridge) send(msg tea.Msg) {
	b.mu.Lock()
	h := b.h
	b.mu.Unlock()
	if h != nil {
		h.Send(msg)
	}
}
