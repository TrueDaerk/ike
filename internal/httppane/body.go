package httppane

// body.go prepares a response body for display (#2157): the raw/pretty
// toggle, the pretty-print cap, and the head-plus-spool handling of a body
// too large to hold in memory.
//
// Three rules, in the order they apply:
//
//   - **Pretty by default.** A JSON body is indented before it is composed, so
//     the common case — an API answer arriving minified — reads without a
//     single keystroke. `t` toggles to the bytes as received and back; the
//     toggle is per view, not per response, so it survives history browsing.
//   - **Formatting is capped.** Indenting, highlighting and fold-scanning a
//     body all cost time proportional to its size, and a pane that freezes for
//     a second on arrival is worse than one showing plain text. Past
//     PrettyLimit the body renders raw and unhighlighted, with a warning row
//     saying so — the cap is surfaced, never silent.
//   - **A large body is a window onto a file.** The dispatcher spools anything
//     past httpclient.SpoolThreshold (see internal/httpclient/spool.go), so
//     what the viewer holds is the head. `m` pulls in the next window, `o`
//     opens the whole body as a file in an editor, and the notice row names
//     both.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"ike/internal/httpclient"
)

const (
	// PrettyLimit is the largest body the viewer pretty-prints, highlights and
	// folds. Above it the body is composed as plain rows: the work all three
	// passes do is linear in the body size, and a viewer that takes a second
	// to answer a keystroke has stopped being a viewer.
	PrettyLimit = 2 << 20 // 2 MiB

	// LoadMoreChunk is how much of a spooled body one "load more" adds. It
	// matches the dispatcher's in-memory head, so the growth steps read as
	// "one more screenful of the same size".
	LoadMoreChunk = httpclient.SpoolThreshold
)

// JQPlaygroundMsg asks the host to open the jq playground over the shown
// response body (#2157), "q" in the focused viewer. The pane holds the body
// but knows nothing about panes, editors or the playground mode, so the host
// opens it — the same seam CopyMsg (#1266) and SaveBodyMsg (#2059) use.
type JQPlaygroundMsg struct{}

// OpenBodyFileMsg asks the host to open the spooled body of the shown
// response as a file (#2157), "o" in the focused viewer. Path is the spool
// file the dispatcher wrote (or the copy the history store adopted), which is
// the complete body — not the head the pane renders.
type OpenBodyFileMsg struct{ Path string }

// bodyView is one response body prepared for display: the composed lines, the
// fence tag to highlight them under ("" = plain, no folds) and the notice rows
// that explain what was capped, spooled or skipped.
type bodyView struct {
	lines   []string
	tag     string
	notices []string
}

// Raw reports whether the body is shown as received rather than
// pretty-printed (tests, footer).
func (m *Model) Raw() bool { return m.raw }

// ToggleRaw flips between the pretty-printed and the raw body and recomposes;
// it reports the new state. The flag belongs to the *view*, so browsing
// history keeps showing bodies the way the user asked for them. With nothing
// composed from a response — an empty pane, or a live stream, whose rows the
// recompose would throw away — the flag flips for the next response and the
// view is left alone.
func (m *Model) ToggleRaw() bool {
	m.raw = !m.raw
	if resp := m.CurrentResponse(); resp != nil {
		m.recompose(resp)
	}
	return m.raw
}

// bodyBytes is the body the viewer currently renders: the response's
// in-memory head plus whatever "load more" has pulled off the spool.
func (m *Model) bodyBytes(resp *httpclient.Response) []byte {
	if resp == nil {
		return nil
	}
	if len(m.more) == 0 {
		return resp.Body
	}
	out := make([]byte, 0, len(resp.Body)+len(m.more))
	return append(append(out, resp.Body...), m.more...)
}

// ShownBodyBytes is how many body bytes the composed rows cover (tests).
func (m *Model) ShownBodyBytes() int {
	resp := m.CurrentResponse()
	if resp == nil {
		return 0
	}
	return len(resp.Body) + len(m.more)
}

// TotalBodyBytes is the size of the whole body, spooled part included
// (tests).
func (m *Model) TotalBodyBytes() int { return m.CurrentResponse().BodyBytes() }

// CanLoadMore reports whether the shown response has body bytes beyond what is
// composed — the gate for the "m" key and the footer hint. Bytes behind a body
// file that has gone missing (#2385) do not count: advertising a load that
// must fail helps nobody.
func (m *Model) CanLoadMore() bool {
	return !m.streaming && m.ShownBodyBytes() < m.TotalBodyBytes() && !m.BodyFileGone()
}

// BodyFilePath is the file holding the complete body of the shown response,
// "" when there is none (the body is small enough to live in memory, or the
// spool file is gone — see BodyFileGone). It gates the "o" key and the footer
// hint: a path that cannot be opened is not offered, so a pruned history or
// an externally deleted file never surfaces as a raw "no such file" error
// (#2385).
func (m *Model) BodyFilePath() string {
	resp := m.CurrentResponse()
	if resp == nil || !resp.Spooled() {
		return ""
	}
	if _, err := os.Stat(resp.SpoolPath); err != nil {
		return ""
	}
	return resp.SpoolPath
}

// BodyFileGone reports that the shown response *had* a body file which no
// longer exists (#2385) — the history was pruned, or the file was deleted
// externally. Hosts use it to explain why "o" and "m" have nothing to offer
// instead of claiming the body never had a file.
func (m *Model) BodyFileGone() bool {
	resp := m.CurrentResponse()
	if resp == nil || !resp.Spooled() {
		return false
	}
	_, err := os.Stat(resp.SpoolPath)
	return err != nil
}

// LoadMore pulls the next window of a spooled body into the view and
// recomposes, reporting whether anything was added. The window is read off
// the spool rather than the whole remainder: growing the view a screenful at a
// time is the point — pulling it all back in would undo the spooling.
func (m *Model) LoadMore() bool {
	if !m.CanLoadMore() {
		return false
	}
	resp := m.CurrentResponse()
	chunk, err := resp.BodyRange(m.ShownBodyBytes(), LoadMoreChunk)
	if err != nil || len(chunk) == 0 {
		return false
	}
	m.more = append(m.more, chunk...)
	// The view grows at its tail, so the spot the user is reading is still
	// there: keep the offsets rather than throwing them back to the top —
	// pressing "m" at the end of the head is how the next window is reached,
	// and landing at row 0 afterwards would lose the place every time.
	top, left := m.top, m.left
	m.recompose(resp)
	m.top, m.left = min(top, m.maxTop()), min(left, m.maxLeft())
	return true
}

// OpenBodyFileCmd emits the request to open the whole body as a file, or nil
// when the shown response has no spool file behind it.
func (m *Model) OpenBodyFileCmd() tea.Cmd {
	path := m.BodyFilePath()
	if path == "" {
		return nil
	}
	return func() tea.Msg { return OpenBodyFileMsg{Path: path} }
}

// JQInput is the text the jq playground opens over (#2157). It is the *whole*
// body, read back off the spool when the pane only holds the head: a program
// written against a truncated document answers questions about a document
// that never existed. The playground snapshots its input once, so the full
// copy lives no longer than the parse.
func (m *Model) JQInput() string {
	resp := m.CurrentResponse()
	if resp == nil || !resp.Spooled() {
		return m.BodyText()
	}
	full, err := resp.FullBody()
	if err != nil {
		return m.BodyText()
	}
	return string(full)
}

// BodyLang names the language of the shown body, the same tag the highlight
// pass runs under: "json", "yaml", "xml", "html", … or "" when the body is
// binary, empty or of a type the viewer does not classify (#2451). It is the
// Content-Type mapping, not a content sniff, and deliberately does *not*
// follow the PrettyLimit cap that drops the tag for highlighting: a body too
// large to paint is still a document the playground can query.
func (m *Model) BodyLang() string {
	if !m.HasBodyText() {
		return ""
	}
	resp := m.CurrentResponse()
	if resp == nil {
		return ""
	}
	return contentTag(resp.Headers.Get("Content-Type"))
}

// formatBody renders the response body for display: pretty-printed JSON, raw
// text for other recognized/unknown text types, a notice instead of content
// for binary bodies. body is what the viewer holds (head plus loaded windows),
// which for a spooled response is less than the response's whole body.
func formatBody(resp *httpclient.Response, body []byte, raw bool) bodyView {
	var v bodyView
	if len(body) == 0 {
		if resp.BodyBytes() > 0 {
			// Nothing in memory but bytes on the spool: say where they are
			// instead of rendering an empty response.
			v.notices = append(v.notices, spoolNotice(resp, 0))
		}
		return v
	}
	partial := len(body) < resp.BodyBytes()
	if partial {
		// The head was cut at a byte offset, so it may end inside a multi-byte
		// rune. Dropping that stump matters before isBinary runs: an invalid
		// trailing sequence is exactly what "this is not text" looks like, and
		// a perfectly good UTF-8 body would collapse to the binary notice.
		body = trimPartialRune(body)
	}
	if isBinary(body) {
		v.notices = append(v.notices, fmt.Sprintf("(binary body, %s — not shown)", byteSize(resp.BodyBytes())))
		return v
	}
	v.tag = contentTag(resp.Headers.Get("Content-Type"))
	text := string(body)
	switch {
	case len(body) > PrettyLimit:
		// The cap is surfaced, never silent: without the notice a minified
		// megabyte would just look like a viewer that forgot how to indent.
		// Dropping the tag drops the highlight index and the fold scan with
		// it — all three passes are linear in the body size.
		v.notices = append(v.notices, fmt.Sprintf("(body larger than %s — showing it raw and unhighlighted)", byteSize(PrettyLimit)))
		v.tag = ""
	case raw:
		// The user asked for the bytes as they arrived (#2157); highlighting
		// still applies — it is the *formatting* that is off.
	case v.tag == "json":
		var buf bytes.Buffer
		if err := json.Indent(&buf, body, "", "  "); err == nil {
			text = buf.String()
		}
	}
	if partial {
		// Only while something is still behind the head: once "load more" has
		// pulled the rest in, a "showing the first …" row would be a lie.
		v.notices = append(v.notices, spoolNotice(resp, len(body)))
	}
	v.lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
	return v
}

// trimPartialRune drops an incomplete multi-byte sequence from the end of a
// body window. Only the *last* rune can be a stump — everything before it was
// received whole — so at most utf8.UTFMax-1 bytes are inspected.
func trimPartialRune(b []byte) []byte {
	for i := 0; i < utf8.UTFMax && i < len(b); i++ {
		end := len(b) - i
		if r, size := utf8.DecodeLastRune(b[:end]); r != utf8.RuneError || size > 1 {
			return b[:end]
		}
	}
	return b
}

// spoolNotice is the head-of-a-large-body row: how much of the body is on
// screen, how much there is, and the two ways on — one more window, or the
// whole thing as a file. A body file that has gone missing (#2385) gets a row
// that says so instead of advertising keys that must fail.
func spoolNotice(resp *httpclient.Response, shown int) string {
	if resp.Spooled() {
		if _, err := os.Stat(resp.SpoolPath); err != nil {
			return fmt.Sprintf("(showing the first %s of %s — the rest is gone: its body file no longer exists)",
				byteSize(shown), byteSize(resp.BodyBytes()))
		}
	}
	return fmt.Sprintf("(showing the first %s of %s — m load more · o open as file)",
		byteSize(shown), byteSize(resp.BodyBytes()))
}

// byteSize renders a byte count the way a notice should read.
func byteSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
}
