package app

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	"ike/internal/editor"
	"ike/internal/host"
	ilsp "ike/internal/lsp"
)

// peek.go opens the peek-definition popup (#1154): the bridge resolves the
// target and sends a PeekDefinitionMsg; this side reads a bounded excerpt
// around the definition line — from the live buffer when the file is open
// (disk may be stale), from disk otherwise — and hands it to the focused
// editor's popup (editor/peek.go), which the popup compositor places at the
// cursor like hover. A multi-target answer is peeked the same way (#2168):
// every candidate's excerpt is read up front and the popup picks between them
// inline, so a peek never opens a modal list.

// peekBefore is how many lines of context precede the definition line in the
// excerpt; peekLineCount bounds the excerpt (and the disk read).
const (
	peekBefore    = 3
	peekLineCount = 15
)

// peekCandidateMax bounds how many candidates a peek shows inline (#2168):
// each one costs an excerpt read, and a list this long is better filtered in
// the palette picker, which keeps its peek intent above the cap.
const peekCandidateMax = 12

// openPeek reads the excerpt for msg and opens the peek popup on the focused
// editor. An unreadable target (deleted, permission) becomes a notice, never
// a silent no-op.
func (m *Model) openPeek(msg ilsp.PeekDefinitionMsg) {
	ed := m.focusedEditor()
	if ed == nil {
		return
	}
	target, err := m.peekTarget(msg.Path, msg.Line, msg.Col)
	if err != nil {
		m.host.Notify(host.Warn, "peek definition: "+err.Error())
		return
	}
	ed.OpenPeekTargets([]editor.PeekTarget{target})
}

// openPeekCandidates opens one peek popup over several definition candidates
// (#2168), the first selected; tab cycles between them inside the popup.
// Candidates whose excerpt cannot be read are skipped rather than shown as
// empty rows; all of them failing surfaces the first failure as a notice.
func (m *Model) openPeekCandidates(refs []ilsp.Reference) {
	ed := m.focusedEditor()
	if ed == nil {
		return
	}
	targets := make([]editor.PeekTarget, 0, len(refs))
	var firstErr error
	for _, ref := range refs {
		target, err := m.peekTarget(ref.Path, ref.Line, ref.Col)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		if firstErr != nil {
			m.host.Notify(host.Warn, "peek definition: "+firstErr.Error())
		}
		return
	}
	ed.OpenPeekTargets(targets)
}

// peekTarget builds one popup candidate: the bounded excerpt around line plus
// the jump target Enter navigates to. The error names the unreadable or empty
// target, so the caller can notify with it.
func (m *Model) peekTarget(path string, line, col int) (editor.PeekTarget, error) {
	path = canonicalPath(path)
	start := line - peekBefore
	if start < 0 {
		start = 0
	}
	lines, err := m.peekExcerpt(path, start)
	if err != nil {
		return editor.PeekTarget{}, fmt.Errorf("cannot read %s: %w", displayPath(path), err)
	}
	if len(lines) == 0 {
		return editor.PeekTarget{}, fmt.Errorf("nothing to show at %s:%s", displayPath(path), strconv.Itoa(line+1))
	}
	return editor.PeekTarget{
		Title: displayPath(path) + ":" + strconv.Itoa(line+1),
		Lines: lines,
		Path:  path,
		Line:  line,
		Col:   col,
	}, nil
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
