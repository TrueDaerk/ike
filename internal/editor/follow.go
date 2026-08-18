package editor

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"ike/internal/editor/buffer"
	"ike/internal/textenc"
	"ike/internal/watch"
)

// follow.go is follow ("tail -f") mode for buffers (#1928): view.toggleFollow
// streams content appended to the open file into the buffer while the
// viewport sticks to the end — the standard less-F / IDE-console behavior.
// The buffer is read-only while following (the incremental append and the
// undo model must never interleave), any cursor/scroll movement away from the
// end pauses the auto-scroll, and jumping back to the end resumes it.
//
// Appends are incremental: followOffset anchors how many bytes of the file
// the buffer holds, and each watcher event reads only the tail past it — the
// whole file is never re-read, and the log analyses (folding, deltas,
// highlighting) recompute through their normal version-keyed paths, with the
// repeat runs extended incrementally (logfold.go). A file that shrank or was
// replaced (truncation, logrotate) reloads wholesale with a toast.
//
// The events themselves come from the shared external-change service
// (internal/watch): fsnotify where it reaches, plus the app's demand-armed
// follow tick driving the poll fallback while at least one view follows —
// no idle cost otherwise (wiki/architecture/performance.md).

// FollowMsg announces a follow toggle to the app, which arms the follow poll
// tick (and re-stamps the poll tracker) on enable. Disable needs no message:
// the tick self-stops once no view follows.
type FollowMsg struct {
	Path string
	On   bool
}

// Following reports whether this view streams appended file content (#1928).
func (m Model) Following() bool { return m.follow }

// FollowPaused reports whether the auto-scroll is parked while following.
func (m Model) FollowPaused() bool { return m.followPaused }

// FollowLabel is the status-line badge: "" when not following, otherwise
// "FOLLOW", with the paused state spelled out.
func (m Model) FollowLabel() string {
	if !m.follow {
		return ""
	}
	if m.followPaused {
		return "FOLLOW (paused)"
	}
	return "FOLLOW"
}

// toggleFollow flips follow mode for this view (view.toggleFollow). Enabling
// re-syncs the buffer with the file and anchors the read offset at its end;
// disabling restores writability and reconciles once against disk (held-back
// partial bytes, anything missed while stopping).
func (m Model) toggleFollow() (Model, tea.Cmd) {
	if m.follow {
		return m.stopFollow()
	}
	if !m.HasFile() {
		m.cmdMsg = "E: follow needs a file on disk"
		return m, nil
	}
	if m.dirty {
		m.cmdMsg = "E: cannot follow a modified buffer; save it first"
		return m, nil
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		m.cmdMsg = "E: " + err.Error()
		return m, nil
	}
	var cmd tea.Cmd
	m, cmd = m.reloadFrom(data)
	m.follow = true
	m.followPaused = false
	m.followRotated = false
	m.followPrevRO = m.readOnly
	m.readOnly = true
	m.followOffset = int64(len(data))
	m.followTerm = len(data) > 0 && data[len(data)-1] == '\n'
	m.followToEnd()
	path := m.path
	return m, tea.Batch(cmd, func() tea.Msg { return FollowMsg{Path: path, On: true} })
}

// stopFollow leaves follow mode: writability restored, one reconcile against
// disk so the buffer ends exactly at the on-disk content.
func (m Model) stopFollow() (Model, tea.Cmd) {
	m.follow = false
	m.followPaused = false
	m.followRotated = false
	m.readOnly = m.followPrevRO
	nm, cmd := m.reloadFromDisk()
	nm.cmdMsg = "follow off"
	return nm, cmd
}

// refreshFollowPause re-derives the paused flag from the current cursor and
// viewport after user-driven movement: keys and actions via the Update
// wrapper, wheel/scrollbar scrolls from ScrollBy and the scrollbar drag.
func (m *Model) refreshFollowPause() {
	if m.follow {
		m.followPaused = !m.followAtBottom()
	}
}

// followAtBottom reports whether the view sits at the buffer's end: the
// cursor on the last line, and the viewport showing it.
func (m Model) followAtBottom() bool {
	last := m.buf.LineCount() - 1
	if m.cursor.Line < last {
		return false
	}
	h := m.view.Height()
	if h <= 0 {
		return true // unsized (headless): the cursor position alone decides
	}
	return last >= m.view.Top && last < m.view.Top+h
}

// followToEnd moves the cursor onto the last line and frames the viewport on
// it — the auto-scroll half of follow mode.
func (m *Model) followToEnd() {
	m.cursor = m.buf.ClampCursor(buffer.Position{Line: m.buf.LineCount() - 1, Col: 0})
	m.desiredCol = 0
	m.scroll()
}

// followHandleEvent consumes one watcher event for the followed file
// (dispatched from handleExternalChange). Content growth appends the tail
// past followOffset; shrinkage and re-creation reload wholesale.
func (m Model) followHandleEvent(msg watch.EventMsg) (Model, tea.Cmd) {
	switch msg.Kind {
	case watch.FileRemoved:
		// Rotation in progress (logrotate moved the file away): keep the
		// buffer — offsets into the old file mean nothing in its replacement,
		// so the next change reloads wholesale.
		m.followRotated = true
		return m, nil
	case watch.FileCreated:
		// The path was re-created under the follower: a rename-style
		// rotation, whatever the content size says.
		return m.followReload("log rotated — reloaded " + filepath.Base(m.path))
	case watch.FileChanged:
	default:
		return m, nil
	}
	if m.followRotated {
		return m.followReload("log rotated — reloaded " + filepath.Base(m.path))
	}
	st, err := os.Stat(m.path)
	if err != nil {
		return m, nil // gone (again); a later event decides
	}
	switch {
	case st.Size() < m.followOffset:
		return m.followReload("log truncated — reloaded " + filepath.Base(m.path))
	case st.Size() == m.followOffset:
		return m, nil // a bare touch, or a same-size rewrite: nothing to stream
	}
	if m.enc != textenc.UTF8 {
		// Incremental decoding of a multi-byte encoding is not safe on
		// arbitrary chunk boundaries: reload instead (silently — content
		// only grew).
		return m.followReload("")
	}
	chunk, err := readFileFrom(m.path, m.followOffset)
	if err != nil || len(chunk) == 0 {
		return m, nil
	}
	use, _ := splitIncompleteTail(chunk)
	if len(use) == 0 {
		return m, nil // only held-back bytes so far; the next write completes them
	}
	m.followOffset += int64(len(use))
	return m.followAppend(string(use))
}

// followReload replaces the buffer with the file's current content and
// re-anchors the follow offset — the truncation/rotation path. A non-empty
// noticeText surfaces as a toast.
func (m Model) followReload(noticeText string) (Model, tea.Cmd) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return m, nil // still mid-rotation; a later event finds the new file
	}
	var cmd tea.Cmd
	m, cmd = m.reloadFrom(data)
	m.followOffset = int64(len(data))
	m.followTerm = len(data) > 0 && data[len(data)-1] == '\n'
	m.followRotated = false
	if !m.followPaused {
		m.followToEnd()
	}
	if noticeText == "" {
		return m, cmd
	}
	return m, tea.Batch(cmd, notice(noticeText))
}

// followAppend streams decoded tail text into the buffer: an unterminated
// last line is continued in place, complete lines append below it. The
// change flows through the normal EventChange path (docVersion bump, shared
// views, LSP sync) and re-parses off-loop, so highlighting and the log
// analyses pick the new lines up without blocking the UI.
func (m Model) followAppend(text string) (Model, tea.Cmd) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	endsNL := strings.HasSuffix(text, "\n")
	segs := strings.Split(text, "\n")
	if endsNL {
		segs = segs[:len(segs)-1] // the terminator is line-end, not a new line
	}
	if len(segs) == 0 {
		m.followTerm = endsNL
		return m, nil
	}
	prevCount := m.buf.LineCount()
	merged := !m.followTerm
	i := 0
	if merged {
		m.buf.AppendToLastLine(segs[0])
		i = 1
	}
	for ; i < len(segs); i++ {
		m.buf.AppendLine(segs[i])
	}
	m.followTerm = endsNL
	m.diskHash = "" // the load-time hash no longer describes the buffer
	m.emit(EventChange)
	m.noteLogAppend(prevCount, merged)
	if !m.followPaused {
		m.followToEnd()
	}
	return m, m.parseCmd()
}

// readFileFrom reads path's content from byte offset off to its current end.
func readFileFrom(path string, off int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

// splitIncompleteTail cuts an incomplete trailing sequence off a chunk read
// mid-write: a lone '\r' that may be the first half of a CRLF pair, or the
// leading bytes of a multi-byte UTF-8 rune. Held bytes stay past the follow
// offset and are consumed whole with the next append.
func splitIncompleteTail(b []byte) (use, held []byte) {
	n := len(b)
	if n > 0 && b[n-1] == '\r' {
		n--
	}
	for back := 1; back < utf8.UTFMax && back <= n; back++ {
		c := b[n-back]
		if c < utf8.RuneSelf {
			break // ASCII: nothing dangling
		}
		if c&0xC0 == 0xC0 { // start byte of a multi-byte rune
			if !utf8.Valid(b[n-back : n]) {
				n -= back
			}
			break
		}
	}
	return b[:n], b[n:]
}
