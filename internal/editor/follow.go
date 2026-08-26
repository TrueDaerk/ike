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
// The followed file is the buffer's own path, except for a merged rotation set
// (#1996, mergedlog.go): that buffer holds several files at once and tails the
// newest of them (followTarget), with the wholesale-reload cases turning into a
// re-merge request instead.
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

// followTarget is the file this view tails: its own path, or the merged
// rotation set's newest member for a merged timeline (#1996).
func (m Model) followTarget() string {
	if m.followSrc != "" {
		return m.followSrc
	}
	return m.path
}

// toggleFollow flips follow mode for this view (view.toggleFollow). Enabling
// re-syncs the buffer with the file and anchors the read offset at its end;
// disabling restores writability and reconciles once against disk (held-back
// partial bytes, anything missed while stopping).
func (m Model) toggleFollow() (Model, tea.Cmd) {
	if m.follow {
		return m.stopFollow()
	}
	if m.mergedLog {
		return m.followMerged()
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
	path := m.followTarget()
	return m, tea.Batch(cmd, func() tea.Msg { return FollowMsg{Path: path, On: true} })
}

// followMerged enters follow mode on a merged rotation set (#1996). There is
// nothing to re-sync: the merge already read the newest member to its end and
// handed over that offset, so the buffer is anchored where it stands — reading
// the source file into the buffer would throw the older members away.
func (m Model) followMerged() (Model, tea.Cmd) {
	if m.followSrc == "" {
		m.cmdMsg = "E: cannot follow this set — its newest member is compressed"
		return m, nil
	}
	m.follow = true
	m.followPaused = false
	m.followRotated = false
	m.followPrevRO = true // the merged timeline is read-only either way
	m.readOnly = true
	m.followToEnd()
	path := m.followSrc
	return m, func() tea.Msg { return FollowMsg{Path: path, On: true} }
}

// stopFollow leaves follow mode: writability restored, one reconcile against
// disk so the buffer ends exactly at the on-disk content.
func (m Model) stopFollow() (Model, tea.Cmd) {
	m.follow = false
	m.followPaused = false
	m.followRotated = false
	m.readOnly = m.followPrevRO
	if m.mergedLog {
		// A merged timeline has no file to reconcile against: its content is
		// the set as of the last merge, and re-reading the virtual path would
		// find nothing (#1996).
		m.mergeWait = false
		m.cmdMsg = "follow off"
		return m, nil
	}
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
	if m.mergeWait {
		// A re-merge of the rotation set is on its way (#1996): the offsets of
		// the file that triggered it mean nothing until it lands.
		return m, nil
	}
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
		return m.followReload("log rotated — reloaded " + filepath.Base(m.followTarget()))
	case watch.FileChanged:
	default:
		return m, nil
	}
	target := m.followTarget()
	if m.followRotated {
		return m.followReload("log rotated — reloaded " + filepath.Base(target))
	}
	st, err := os.Stat(target)
	if err != nil {
		return m, nil // gone (again); a later event decides
	}
	switch {
	case st.Size() < m.followOffset:
		return m.followReload("log truncated — reloaded " + filepath.Base(target))
	case st.Size() == m.followOffset:
		return m, nil // a bare touch, or a same-size rewrite: nothing to stream
	}
	if m.enc != textenc.UTF8 {
		// Incremental decoding of a multi-byte encoding is not safe on
		// arbitrary chunk boundaries: reload instead (silently — content
		// only grew).
		return m.followReload("")
	}
	chunk, err := readFileFrom(target, m.followOffset)
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
//
// A merged rotation set (#1996) cannot reload from a file: the replacement's
// lines belong *after* the ones the buffer already holds, which is a new merge
// of the whole set. The view asks the root model for one and parks until it
// arrives; noticeText is dropped there — it describes a reload that does not
// happen, and the root model toasts the merge it *does* run.
func (m Model) followReload(noticeText string) (Model, tea.Cmd) {
	if m.mergedLog {
		m.mergeWait = true
		m.followRotated = false
		path := m.path
		return m, func() tea.Msg { return MergeLogSetMsg{Path: path} }
	}
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
	// Re-evaluate the large-file gate as the tail grows (#2163): the flag
	// was only ever set at load, so a small log tailed to hundreds of MB
	// kept shipping its entire text with every append's EventChange — an
	// O(document) join per poll on the update loop, growing without bound.
	m.docBytes += int64(len(text))
	if !m.largeFile && m.limits().Exceeded(m.docBytes, m.buf.LineCount()) {
		m.largeFile = true
	}
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
