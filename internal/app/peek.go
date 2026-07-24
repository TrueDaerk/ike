package app

import (
	"bufio"
	"os"
	"strconv"

	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// peek.go opens the peek-definition popup (#1154): the bridge resolves the
// target and sends a PeekDefinitionMsg; this side reads a bounded excerpt
// around the definition line — from the live buffer when the file is open
// (disk may be stale), from disk otherwise — and hands it to the focused
// editor's popup (editor/peek.go), which the popup compositor places at the
// cursor like hover.

// peekBefore is how many lines of context precede the definition line in the
// excerpt; peekLineCount bounds the excerpt (and the disk read).
const (
	peekBefore    = 3
	peekLineCount = 15
)

// openPeek reads the excerpt for msg and opens the peek popup on the focused
// editor. An unreadable target (deleted, permission) becomes a notice, never
// a silent no-op.
func (m *Model) openPeek(msg ilsp.PeekDefinitionMsg) {
	ed := m.focusedEditor()
	if ed == nil {
		return
	}
	path := canonicalPath(msg.Path)
	start := msg.Line - peekBefore
	if start < 0 {
		start = 0
	}
	lines, err := m.peekExcerpt(path, start)
	if err != nil {
		m.host.Notify(host.Warn, "peek definition: cannot read "+displayPath(path)+": "+err.Error())
		return
	}
	if len(lines) == 0 {
		m.host.Notify(host.Warn, "peek definition: nothing to show at "+displayPath(path)+":"+strconv.Itoa(msg.Line+1))
		return
	}
	title := displayPath(path) + ":" + strconv.Itoa(msg.Line+1)
	ed.OpenPeek(title, lines, path, msg.Line, msg.Col)
}

// peekExcerpt returns up to peekLineCount lines of path starting at start
// (0-based). An open buffer is the source of truth — its unsaved edits must
// show (#1154); an unopened file is read from disk, bounded: the scan stops
// after the excerpt instead of slurping the whole file.
func (m *Model) peekExcerpt(path string, start int) ([]string, error) {
	if views := m.editorViewsForPath(path); len(views) > 0 {
		return views[0].LineRange(start, peekLineCount), nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []string
	for i := 0; sc.Scan(); i++ {
		if i < start {
			continue
		}
		out = append(out, sc.Text())
		if len(out) == peekLineCount {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
