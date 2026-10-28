package settings

import (
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	ilsp "ike/internal/lsp"
)

// lsp_silent.go shows and resets the known-silent flag on the Language
// Servers page (#3080): a server whose project-switch warm-up ended quiet is
// remembered per project (ilsp.SilentStore, lsp-silent.json in the state
// store) and no longer waited for. The row's status reads "silent", the
// footer says what that means, and x ("Remove silent flag"; r already resets
// the overrides) forgets it so the next switch waits for the server again —
// the same effect as the lsp.forgetSilent command, for one server.

// LSPSilentResetMsg reports a finished reset of one server's silent flag.
type LSPSilentResetMsg struct {
	ID  string
	Err error
}

// silentCache is the page's copy of the active project's known-silent ids,
// re-read only when the file's size or modification time changed — the
// render path stats the file instead of parsing it on every frame.
type silentCache struct {
	stamp  time.Time
	size   int64
	exists bool
	read   bool
	ids    map[string]bool
}

// silentRoot is the project root the page's markers are keyed by.
func (p *LSPPage) silentRoot() string {
	if p.opts.ProjectRoot != "" {
		return p.opts.ProjectRoot
	}
	return "."
}

// silentIDs returns the active project's known-silent server ids.
func (p *LSPPage) silentIDs() map[string]bool {
	root := p.silentRoot()
	path := ilsp.SilentPath(root)
	fi, err := os.Stat(path)
	c := &p.silent
	exists := err == nil
	if c.read && exists == c.exists && (!exists || (fi.ModTime().Equal(c.stamp) && fi.Size() == c.size)) {
		return c.ids
	}
	c.read, c.exists, c.ids = true, exists, map[string]bool{}
	if exists {
		c.stamp, c.size = fi.ModTime(), fi.Size()
		s, _ := ilsp.LoadSilent(path)
		for _, id := range s.IDs(root) {
			c.ids[id] = true
		}
	}
	return c.ids
}

// isSilent reports whether the server is flagged silent in this project.
func (p *LSPPage) isSilent(id string) bool { return p.silentIDs()[id] }

// resetSilent forgets one server's silent flag; the write happens inside the
// returned command (#123). A server without the flag says so instead.
func (p *LSPPage) resetSilent(id string) tea.Cmd {
	if !p.isSilent(id) {
		p.notice = id + " is not marked silent in this project"
		return nil
	}
	root := p.silentRoot()
	return func() tea.Msg {
		_, err := ilsp.UpdateSilent(ilsp.SilentPath(root), func(s *ilsp.SilentStore) bool { return s.Clear(root, id) })
		return LSPSilentResetMsg{ID: id, Err: err}
	}
}

// receiveSilentReset turns a finished reset into the footer notice and drops
// the cache, so the row re-reads the file.
func (p *LSPPage) receiveSilentReset(msg LSPSilentResetMsg) {
	p.silent.read = false
	if msg.Err != nil {
		p.notice = "reset silent flag of " + msg.ID + ": " + msg.Err.Error()
		return
	}
	p.notice = "silent flag of " + msg.ID + " reset — the next switch into this project waits for it again"
}
